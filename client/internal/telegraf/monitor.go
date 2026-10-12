package telegraf

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Options struct {
	BinaryPath  string
	ServiceName string
	CheckEvery  time.Duration
	AutoRecover bool
}

type Snapshot struct {
	Running      bool
	PID          uint32
	Version      string
	LastMetricAt time.Time
	LastCheckAt  time.Time
	LastError    string
	RestartCount uint64
}

// 版本探测相关常量。
const (
	// versionTimeout 是单次探测 Telegraf 版本的超时。
	versionTimeout = 3 * time.Second
	// versionRetryInterval 是「取不到版本」时的重试间隔。
	//
	// check 每 5 秒执行一次，而取版本需要 fork 外部进程：取不到时若不节流，
	// 配置有误的机器会长期每 5 秒 fork 一个必然失败的进程。取到后则完全不再探测
	// （版本只在升级 Telegraf 时变化，重启 agent 即可刷新）。
	versionRetryInterval = time.Minute
)

type Monitor struct {
	options Options
	mu      sync.RWMutex
	state   Snapshot
	// versionCache 已取到的 Telegraf 版本；为空表示尚未取到。
	versionCache string
	// versionCheckedAt 上次尝试探测的时间，用于失败节流。
	versionCheckedAt time.Time
}

func New(options Options) *Monitor {
	if options.CheckEvery <= 0 {
		options.CheckEvery = 5 * time.Second
	}
	return &Monitor{options: options}
}

func (m *Monitor) Run(ctx context.Context) {
	m.check(ctx)
	ticker := time.NewTicker(m.options.CheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

func (m *Monitor) MarkMetric(at time.Time) {
	m.mu.Lock()
	if at.After(m.state.LastMetricAt) {
		m.state.LastMetricAt = at
	}
	m.mu.Unlock()
}

func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Monitor) check(ctx context.Context) {
	now := time.Now().UTC()
	pid, running := findProcess()
	version := m.version(ctx)
	lastError := ""
	if !running && m.options.AutoRecover && m.options.ServiceName != "" {
		if err := restartService(ctx, m.options.ServiceName); err != nil {
			lastError = err.Error()
		} else {
			m.mu.Lock()
			m.state.RestartCount++
			m.mu.Unlock()
			pid, running = findProcess()
		}
	}
	m.mu.Lock()
	m.state.Running = running
	m.state.PID = pid
	m.state.Version = version
	m.state.LastCheckAt = now
	m.state.LastError = lastError
	m.mu.Unlock()
}

// version 返回 Telegraf 的版本信息（形如 "Telegraf 1.40.1 (git: HEAD@26b8f1d)"）。
//
// 取到一次后就缓存复用：check 每 5 秒执行一次，而这里要 fork 外部进程，
// 不缓存等于每 5 秒起一个进程。取不到时按 versionRetryInterval 节流重试。
//
// 返回 Returns:
//   - version (string): 版本信息；尚未取到时返回空串。
func (m *Monitor) version(ctx context.Context) string {
	m.mu.RLock()
	cached, checkedAt := m.versionCache, m.versionCheckedAt
	m.mu.RUnlock()
	if cached != "" {
		return cached
	}
	if !checkedAt.IsZero() && time.Since(checkedAt) < versionRetryInterval {
		return ""
	}
	value := m.probeVersion(ctx)
	m.mu.Lock()
	m.versionCheckedAt = time.Now()
	if value != "" {
		m.versionCache = value
	}
	m.mu.Unlock()
	return value
}

// probeVersion 依次尝试候选命令，返回第一份成功的版本输出。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//
// 返回 Returns:
//   - version (string): 版本信息；全部候选都失败时返回空串。
func (m *Monitor) probeVersion(ctx context.Context) string {
	for _, binary := range m.binaryCandidates() {
		commandCtx, cancel := context.WithTimeout(ctx, versionTimeout)
		output, err := exec.CommandContext(commandCtx, binary, "--version").CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		if line := firstLine(output); line != "" {
			return line
		}
	}
	return ""
}

// binaryCandidates 返回探测版本时依次尝试的命令。
//
// 除配置里的路径外，最后总会兜底尝试 PATH 中的 telegraf。原因是 agent.yaml 常被跨平台
// 拷贝：Windows 包里写的是 "Telegraf/telegraf.exe"，直接拿到 Linux 上跑就必然失败，
// 表现为服务器列表里 Telegraf 版本一栏空白。而 Telegraf 在 Linux 上通常已经装进 PATH
// （官方包或 systemd 服务），兜底一次就能拿到版本，不需要逐台改配置。
//
// 返回 Returns:
//   - candidates ([]string): 候选命令（按优先级）。
func (m *Monitor) binaryCandidates() []string {
	candidates := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	for _, path := range []string{strings.TrimSpace(m.options.BinaryPath), "telegraf"} {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		candidates = append(candidates, path)
	}
	return candidates
}

// firstLine 取输出的第一行非空内容（telegraf --version 只有一行，此处仅作防御）。
//
// 参数 Parameters:
//   - output ([]byte): 命令的合并输出。
//
// 返回 Returns:
//   - line (string): 第一行非空内容；没有内容时返回空串。
func firstLine(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseTasklistOutput(output string) (uint32, bool) {
	line := strings.TrimSpace(output)
	if line == "" || !strings.Contains(strings.ToLower(line), "telegraf.exe") {
		return 0, false
	}
	parts := strings.Split(line, "\",\"")
	if len(parts) < 2 {
		return 0, true
	}
	pid, _ := strconv.ParseUint(strings.Trim(parts[1], "\""), 10, 32)
	return uint32(pid), true
}

func findProcess() (uint32, bool) {
	if runtime.GOOS == "windows" {
		output, err := exec.Command("tasklist", "/FI", "IMAGENAME eq telegraf.exe", "/FO", "CSV", "/NH").Output()
		if err != nil {
			return 0, false
		}
		return parseTasklistOutput(string(output))
	}
	output, err := exec.Command("pgrep", "-x", "telegraf").Output()
	if err != nil {
		return 0, false
	}
	// pgrep 命中时才返回 0，但输出理论上仍可能为空，这里防御越界（空切片取 [0] 会 panic）。
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return 0, false
	}
	pid, _ := strconv.ParseUint(fields[0], 10, 32)
	return uint32(pid), true
}

func restartService(ctx context.Context, service string) error {
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if runtime.GOOS == "windows" {
		return exec.CommandContext(commandCtx, "sc.exe", "start", service).Run()
	}
	return exec.CommandContext(commandCtx, "systemctl", "restart", service).Run()
}

var versionPattern = regexp.MustCompile(`(?i)telegraf\s+v?([0-9][^ \r\n]*)`)

func NormalizeVersion(raw string) string {
	match := versionPattern.FindStringSubmatch(raw)
	if len(match) == 2 {
		return match[1]
	}
	return strings.TrimSpace(raw)
}
