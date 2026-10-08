package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Recorder 把终端会话旁路录制成 asciinema v2（cast）格式。
//
// 格式：首行 JSON 头（version/width/height/timestamp），其后每行一个事件
// [相对秒数, "o"|"i", 数据]。asciinema-player 可直接回放。
type Recorder struct {
	mu      sync.Mutex
	file    *os.File
	start   time.Time
	started bool
}

type castHeader struct {
	Version   int   `json:"version"`
	Width     int   `json:"width"`
	Height    int   `json:"height"`
	Timestamp int64 `json:"timestamp"`
}

// NewRecorder 创建录像器并写入 asciinema v2 头。
//
// 参数 Parameters:
//   - path (string): cast 文件绝对路径。
//   - width (int): 终端列数。
//   - height (int): 终端行数。
//
// 返回 Returns:
//   - rec (*Recorder): 录像器。
//   - err (error): 文件创建或头写入失败时返回非 nil。
func NewRecorder(path string, width, height int) (*Recorder, error) {
	// 录像路径带日期子目录（20261007/000001.cast），而 os.Create 不会创建中间目录：
	// 启动时只建了录像根目录，此处必须再补一次 MkdirAll，否则第一条会话就会因
	// ENOENT 直接失败（表现为 SSH 终端握手 500）。
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建录像目录 %s 失败: %w", dir, err)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	rec := &Recorder{file: file, start: time.Now()}
	header := castHeader{Version: 2, Width: width, Height: height, Timestamp: rec.start.Unix()}
	raw, err := json.Marshal(header)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	raw = append(raw, '\n')
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return nil, err
	}
	rec.started = true
	return rec, nil
}

// writeEvent 写入一个事件（线程安全）。
func (r *Recorder) writeEvent(kind string, data []byte) error {
	if r == nil || r.file == nil || !r.started {
		return nil
	}
	event := [3]any{elapsed(r.start), kind, string(data)}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err = r.file.Write(raw)
	return err
}

// Output 记录一段终端输出（"o" 事件）。
func (r *Recorder) Output(data []byte) error { return r.writeEvent("o", data) }

// Input 记录一段终端输入（"i" 事件）。
func (r *Recorder) Input(data []byte) error { return r.writeEvent("i", data) }

// Close 关闭并刷盘。
func (r *Recorder) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	return r.file.Close()
}

// elapsed 返回自录像开始以来的秒数（保留 6 位小数，避免时间精度丢失）。
func elapsed(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1e6
}

// CommandCapture 从终端输入流中按回车聚合命令，供命令审计落库。
//
// 局限：它只识别「回车结束的可见字符」，无法解析方向键/退格/补全等控制序列，
// 因此复杂交互（如 vim、补全后再回车）得到的命令可能与真实执行命令有差异——
// 完整过程以录像回放为准。
type CommandCapture struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	seq  int
	sink func(seq int, command string)
}

// NewCommandCapture 创建命令捕获器。
//
// 参数 Parameters:
//   - sink (func(int, string)): 每聚合出一条命令时回调（seq 为会话内序号）。
func NewCommandCapture(sink func(seq int, command string)) *CommandCapture {
	return &CommandCapture{sink: sink}
}

// Write 喂入一段输入字节；遇到 CR/LF 时聚合一条命令。
func (c *CommandCapture) Write(data []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range data {
		switch b {
		case '\r', '\n':
			c.flushLocked()
		case 0x15: // Ctrl+U：清空当前输入行
			c.buf.Reset()
		case 0x03: // Ctrl+C：取消当前输入行
			c.buf.Reset()
		case 0x7f, 0x08: // DEL / Backspace：忽略，避免把退格写进命令
			continue
		default:
			c.buf.WriteByte(b)
		}
	}
}

// Flush 冲刷缓冲区（会话结束时调用，兜底未以回车结束的输入）。
func (c *CommandCapture) Flush() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushLocked()
}

func (c *CommandCapture) flushLocked() {
	command := strings.TrimSpace(c.buf.String())
	c.buf.Reset()
	if command == "" {
		return
	}
	c.seq++
	if c.sink != nil {
		c.sink(c.seq, command)
	}
}
