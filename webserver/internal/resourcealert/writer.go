package resourcealert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// DefaultAlertNamePattern 是告警日志的默认命名模板（展开为 <主机>-<日期>-alerts）。
	DefaultAlertNamePattern = "{agent}-{date}-alerts"
	// defaultAlertIdleTTL 是文件句柄的空闲回收时长。
	//
	// 按主机分文件意味着句柄数随主机数增长；长期运行的主机每天还会因日期变化换一个新文件。
	// 不回收的话句柄只增不减（20 台机跑到年底就是上千个），这里对「空闲超过该时长」的文件
	// 主动关闭并移出缓存，下次该主机再有告警时自动重新打开。
	defaultAlertIdleTTL = 15 * time.Minute
	// agentTokenMaxLength 是主机标识进文件名前的最大长度，避免个别超长 id 触发文件系统上限。
	agentTokenMaxLength = 64
)

// Writer 把告警写入按「主机 + 日期」分开的轮转日志文件。
//
// 命名由 LogOptions.NamePattern 决定（默认 `{agent}-{date}-alerts`），展开后同时产出
// 结构化文件（.log，JSON Lines）与人读文件（.txt）：
//
//	logs/loca-windows-20261009-alerts.log
//	logs/loca-windows-20261009-alerts.txt
//
// 这样一份文件就是「某台机某天发生过的全部告警」，便于直接发给相关人，
// 也不会被其他主机的告警淹没。
type Writer struct {
	// mu 串行化写入（lumberjack 不是并发安全的，files 也需要保护）。
	mu sync.Mutex
	// dir 告警日志目录。
	dir string
	// pattern 文件名模板，支持 {agent} 与 {date}。
	pattern string
	// format 输出形态：json / text / both。
	format string
	// rotate 轮转参数。
	rotate rotateOptions
	// now 当前时间（可注入，便于测试跨日期与空闲回收）。
	now func() time.Time
	// ttl 空闲回收时长。
	ttl time.Duration
	// files 已打开的文件，键为展开后的文件名（不含扩展名）。
	//
	// 用「展开后的文件名」而不是「主机 id」作键是有意义的：如果模板里没写 {agent}，
	// 多台机会落到同一个文件，此时必须复用同一个 lumberjack 实例，
	// 否则两个实例交替写同一文件会互相破坏轮转状态。
	files map[string]*agentLogFile
}

// rotateOptions 是轮转参数（从 LogOptions 归一化而来）。
type rotateOptions struct {
	// maxSizeMB 单文件大小上限（MB）。
	maxSizeMB int
	// maxBackups 保留的历史文件数。
	maxBackups int
	// maxAgeDays 历史文件保留天数。
	maxAgeDays int
	// compress 是否压缩历史文件。
	compress bool
}

// agentLogFile 是某台主机某一天对应的两个文件句柄。
type agentLogFile struct {
	// jsonFile 结构化日志文件。
	jsonFile *lumberjack.Logger
	// textFile 人读文本文件。
	textFile *lumberjack.Logger
	// lastUsed 最近一次写入时间（空闲回收依据）。
	lastUsed time.Time
}

// NewWriter 创建告警日志写入器。
//
// 参数 Parameters:
//   - opts (LogOptions): 日志配置（目录、命名模板与轮转参数）。
//
// 返回 Returns:
//   - writer (*Writer): 写入器；未启用文件输出或输出形态无效时返回 (nil, nil)，调用方需容忍 nil。
//   - err (error): 日志目录创建失败时返回非 nil。
func NewWriter(opts LogOptions) (*Writer, error) {
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if !opts.Enabled || format == "" || format == "none" {
		return nil, nil
	}
	if format != "json" && format != "text" && format != "both" {
		return nil, nil
	}
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		dir = "logs"
	}
	pattern := strings.TrimSpace(opts.NamePattern)
	if pattern == "" {
		pattern = DefaultAlertNamePattern
	}
	// lumberjack 会创建文件但不会创建目录，缺目录时第一次写入就会失败
	// （与录像器踩过的是同一个坑）。
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Writer{
		dir:     dir,
		pattern: pattern,
		format:  format,
		rotate: rotateOptions{
			maxSizeMB:  opts.MaxSizeMB,
			maxBackups: opts.MaxBackups,
			maxAgeDays: opts.MaxAgeDays,
			compress:   opts.Compress,
		},
		now:   time.Now,
		ttl:   defaultAlertIdleTTL,
		files: make(map[string]*agentLogFile),
	}, nil
}

// Write 写出一条告警；目标文件由该告警的主机与发生日期决定。
//
// 参数 Parameters:
//   - report (Report): 告警报告。
//
// 返回 Returns:
//   - err (error): 任一文件写入失败时返回该错误（不影响另一份文件）。
func (w *Writer) Write(report Report) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	entry, err := w.entryLocked(w.baseName(report))
	if err != nil {
		return err
	}
	entry.lastUsed = w.now()

	var firstErr error
	if entry.jsonFile != nil {
		raw, marshalErr := json.Marshal(report)
		switch {
		case marshalErr != nil:
			firstErr = marshalErr
		default:
			if _, err := entry.jsonFile.Write(append(raw, '\n')); err != nil {
				firstErr = err
			}
		}
	}
	if entry.textFile != nil {
		if _, err := entry.textFile.Write([]byte(report.Text() + "\n")); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// 顺带回收空闲句柄：有告警在写的时候才需要关心句柄数，这里是最自然的时机。
	w.evictIdleLocked()
	return firstErr
}

// Close 关闭全部文件句柄（幂等，可重复调用）。
//
// 返回 Returns:
//   - err (error): 关闭失败时返回非 nil。
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var firstErr error
	for base, entry := range w.files {
		if err := entry.close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(w.files, base)
	}
	return firstErr
}

// entryLocked 取回（或惰性创建）某个文件名对应的句柄集合。
//
// 参数 Parameters:
//   - base (string): 展开后的文件名（不含扩展名）。
//
// 返回 Returns:
//   - entry (*agentLogFile): 句柄集合。
//   - err (error): 目录创建失败时返回非 nil。
func (w *Writer) entryLocked(base string) (*agentLogFile, error) {
	if entry, ok := w.files[base]; ok {
		return entry, nil
	}
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return nil, err
	}
	entry := &agentLogFile{lastUsed: w.now()}
	if w.wantsJSON() {
		entry.jsonFile = w.newRotator(base + ".log")
	}
	if w.wantsText() {
		entry.textFile = w.newRotator(base + ".txt")
	}
	w.files[base] = entry
	return entry, nil
}

// evictIdleLocked 关闭并移除长时间未使用的文件句柄。
func (w *Writer) evictIdleLocked() {
	if w.ttl <= 0 {
		return
	}
	now := w.now()
	for base, entry := range w.files {
		if now.Sub(entry.lastUsed) < w.ttl {
			continue
		}
		_ = entry.close()
		delete(w.files, base)
	}
}

// baseName 把命名模板展开成最终的文件名（不含扩展名）。
//
// 参数 Parameters:
//   - report (Report): 告警报告（提供主机与事件时间）。
//
// 返回 Returns:
//   - base (string): 文件名。
func (w *Writer) baseName(report Report) string {
	name := w.pattern
	name = strings.ReplaceAll(name, "{agent}", sanitizeFileToken(report.Agent.ID))
	name = strings.ReplaceAll(name, "{date}", report.Time.Local().Format("20060102"))
	return name
}

// newRotator 构造一个轮转文件写入器。
//
// 参数 Parameters:
//   - filename (string): 目录内的文件名。
//
// 返回 Returns:
//   - logger (*lumberjack.Logger): 轮转写入器。
func (w *Writer) newRotator(filename string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   filepath.Join(w.dir, filename),
		MaxSize:    w.rotate.maxSizeMB,
		MaxBackups: w.rotate.maxBackups,
		MaxAge:     w.rotate.maxAgeDays,
		Compress:   w.rotate.compress,
		LocalTime:  true,
	}
}

// wantsJSON 返回是否输出结构化日志。
func (w *Writer) wantsJSON() bool { return w.format == "json" || w.format == "both" }

// wantsText 返回是否输出人读日志。
func (w *Writer) wantsText() bool { return w.format == "text" || w.format == "both" }

// close 关闭该主机对应的文件句柄（幂等）。
//
// 返回 Returns:
//   - err (error): 关闭失败时返回非 nil。
func (e *agentLogFile) close() error {
	var firstErr error
	if e.jsonFile != nil {
		if err := e.jsonFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		e.jsonFile = nil
	}
	if e.textFile != nil {
		if err := e.textFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		e.textFile = nil
	}
	return firstErr
}

// sanitizeFileToken 把主机标识转换为安全的文件名片段。
//
// 主机标识来自 Agent 上报（UUID、主机名或自定义 id），可能含 "/"、":"、空格甚至
// 路径穿越片段（"../"）。直接拼进文件名会创建失败或把文件写到目录之外，
// 因此只保留字母、数字与 . _ -，其余一律替换为下划线。
//
// 参数 Parameters:
//   - value (string): 原始主机标识。
//
// 返回 Returns:
//   - token (string): 安全片段；无法得到有效名字时返回 "unknown"。
func sanitizeFileToken(value string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '_', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	// 去掉首尾的点：避免产生 "."（当前目录）、".."（上级目录）这类特殊名字。
	token := strings.Trim(builder.String(), ".")
	if strings.Trim(token, "_") == "" {
		// 全是下划线（例如 id 全由非 ASCII 字符组成）同样无法用于命名。
		return "unknown"
	}
	if len(token) > agentTokenMaxLength {
		token = token[:agentTokenMaxLength]
	}
	return token
}
