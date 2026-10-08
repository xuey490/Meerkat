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

type Monitor struct {
	options Options
	mu      sync.RWMutex
	state   Snapshot
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

func (m *Monitor) version(ctx context.Context) string {
	if m.options.BinaryPath == "" {
		return ""
	}
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, m.options.BinaryPath, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
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
	pid, _ := strconv.ParseUint(strings.Fields(string(output))[0], 10, 32)
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
