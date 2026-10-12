package resourcealert

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// alertReport 构造一条用于写入测试的告警报告。
func alertReport(agentID string, at time.Time) Report {
	return Report{
		Time: at, Event: EventFired, Level: LevelWarning,
		Agent: AgentRef{ID: agentID, Hostname: agentID, IP: "192.168.1.10"},
		Metric: MetricPart{
			Key: MetricCPUUsage, Name: "CPU 利用率", Unit: "%",
			Value: 96.8, Warning: 85, Critical: 95, Threshold: 95,
		},
	}
}

// newTestWriter 创建测试用写入器，并在用例结束时关闭句柄。
func newTestWriter(t *testing.T, opts LogOptions) *Writer {
	t.Helper()
	writer, err := NewWriter(opts)
	if err != nil {
		t.Fatalf("创建写入器失败: %v", err)
	}
	if writer == nil {
		t.Fatal("写入器不应为 nil")
	}
	t.Cleanup(func() { _ = writer.Close() })
	return writer
}

// listNames 返回目录下的文件名（已排序），便于整体比对。
func listNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func TestSanitizeFileToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{"loca-windows", "loca-windows"},
		{"loca-linux-001", "loca-linux-001"},
		{"node.1", "node.1"},
		{"a/b:c", "a_b_c"},
		{"  spaced id  ", "spaced_id"},
		// 路径穿越片段必须被拆掉斜杠，否则会写到目录之外。
		{"../../etc/passwd", "_.._etc_passwd"},
		{"..", "unknown"},
		{"", "unknown"},
		{"___", "unknown"},
		{"日本語ホスト", "unknown"},
	}
	for _, c := range cases {
		if got := sanitizeFileToken(c.in); got != c.want {
			t.Errorf("sanitizeFileToken(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestWriter按主机与日期分文件(t *testing.T) {
	dir := t.TempDir()
	writer := newTestWriter(t, LogOptions{
		Enabled: true, Format: "both", Dir: dir, NamePattern: DefaultAlertNamePattern,
		MaxSizeMB: 8, MaxBackups: 2, MaxAgeDays: 1,
	})
	at := time.Date(2026, 10, 9, 10, 0, 0, 0, time.Local)
	for _, agent := range []string{"loca-windows", "loca-linux-001"} {
		if err := writer.Write(alertReport(agent, at)); err != nil {
			t.Fatalf("写入 %s 失败: %v", agent, err)
		}
	}
	want := []string{
		"loca-linux-001-20261009-alerts.log",
		"loca-linux-001-20261009-alerts.txt",
		"loca-windows-20261009-alerts.log",
		"loca-windows-20261009-alerts.txt",
	}
	if got := listNames(t, dir); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("文件列表不符\n期望: %v\n实际: %v", want, got)
	}
}

func TestWriter跨日期生成新文件(t *testing.T) {
	dir := t.TempDir()
	writer := newTestWriter(t, LogOptions{
		Enabled: true, Format: "text", Dir: dir, NamePattern: DefaultAlertNamePattern,
		MaxSizeMB: 8, MaxBackups: 2, MaxAgeDays: 1,
	})
	// 23:59 与次日 00:01 必须落到不同文件，否则「按日期分文件」就失去意义。
	first := time.Date(2026, 10, 9, 23, 59, 0, 0, time.Local)
	if err := writer.Write(alertReport("loca-windows", first)); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := writer.Write(alertReport("loca-windows", first.Add(2*time.Minute))); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	want := []string{
		"loca-windows-20261009-alerts.txt",
		"loca-windows-20261010-alerts.txt",
	}
	if got := listNames(t, dir); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("跨日期文件列表不符\n期望: %v\n实际: %v", want, got)
	}
}

func TestWriter模板无占位符时复用同一文件(t *testing.T) {
	dir := t.TempDir()
	writer := newTestWriter(t, LogOptions{
		Enabled: true, Format: "both", Dir: dir, NamePattern: "alerts",
		MaxSizeMB: 8, MaxBackups: 2, MaxAgeDays: 1,
	})
	at := time.Date(2026, 10, 9, 10, 0, 0, 0, time.Local)
	for _, agent := range []string{"a", "b", "c"} {
		if err := writer.Write(alertReport(agent, at)); err != nil {
			t.Fatalf("写入 %s 失败: %v", agent, err)
		}
	}
	// 模板没写 {agent} 时多台机共用一个文件，此时必须复用同一个句柄：
	// 两个 lumberjack 实例交替写同一文件会互相破坏轮转状态。
	if len(writer.files) != 1 {
		t.Errorf("应复用同一个文件句柄，实际 %d 个", len(writer.files))
	}
	want := []string{"alerts.log", "alerts.txt"}
	if got := listNames(t, dir); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("文件列表不符\n期望: %v\n实际: %v", want, got)
	}
}

func TestWriter空闲句柄被回收(t *testing.T) {
	dir := t.TempDir()
	writer := newTestWriter(t, LogOptions{
		Enabled: true, Format: "text", Dir: dir, NamePattern: DefaultAlertNamePattern,
		MaxSizeMB: 8, MaxBackups: 2, MaxAgeDays: 1,
	})
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.Local)
	writer.now = func() time.Time { return now }
	writer.ttl = 10 * time.Minute

	if err := writer.Write(alertReport("agent-a", now)); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if len(writer.files) != 1 {
		t.Fatalf("应打开 1 个句柄，实际 %d", len(writer.files))
	}

	// 空闲超过 TTL 后，下一次写入应把旧句柄回收掉，避免句柄随主机数只增不减。
	now = now.Add(11 * time.Minute)
	if err := writer.Write(alertReport("agent-b", now)); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if len(writer.files) != 1 {
		t.Errorf("空闲句柄应被回收，实际保留 %d 个", len(writer.files))
	}
	if _, ok := writer.files["agent-a-20261009-alerts"]; ok {
		t.Error("agent-a 的句柄应已被回收")
	}

	// 回收过的文件仍可再次写入（会重新打开）。
	now = now.Add(time.Minute)
	if err := writer.Write(alertReport("agent-a", now)); err != nil {
		t.Fatalf("回收后重新写入失败: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "agent-a-20261009-alerts.txt"))
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if strings.Count(string(raw), "\n") < 2 {
		t.Errorf("两次写入应产生两行，实际内容: %q", string(raw))
	}
}

func TestWriter写入结构化与人读两份内容(t *testing.T) {
	dir := t.TempDir()
	writer := newTestWriter(t, LogOptions{
		Enabled: true, Format: "both", Dir: dir, NamePattern: DefaultAlertNamePattern,
		MaxSizeMB: 8, MaxBackups: 2, MaxAgeDays: 1,
	})
	at := time.Date(2026, 10, 9, 10, 23, 0, 0, time.Local)
	if err := writer.Write(alertReport("loca-windows", at)); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	logRaw, err := os.ReadFile(filepath.Join(dir, "loca-windows-20261009-alerts.log"))
	if err != nil {
		t.Fatalf("读取结构化日志失败: %v", err)
	}
	var decoded Report
	if err := json.Unmarshal(bytes.TrimSpace(logRaw), &decoded); err != nil {
		t.Fatalf("结构化日志不是合法 JSON: %v", err)
	}
	if decoded.Agent.ID != "loca-windows" {
		t.Errorf("结构化日志 agent 不符: %q", decoded.Agent.ID)
	}

	textRaw, err := os.ReadFile(filepath.Join(dir, "loca-windows-20261009-alerts.txt"))
	if err != nil {
		t.Fatalf("读取人读日志失败: %v", err)
	}
	text := string(textRaw)
	if !strings.Contains(text, "loca-windows") {
		t.Errorf("人读日志缺少主机标识: %s", text)
	}
	if !strings.Contains(text, "CPU 利用率 96.8%") {
		t.Errorf("人读日志缺少指标明细: %s", text)
	}
}

func TestWriter目录不存在时自动创建(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "alerts")
	writer := newTestWriter(t, LogOptions{
		Enabled: true, Format: "text", Dir: dir, NamePattern: DefaultAlertNamePattern,
		MaxSizeMB: 8, MaxBackups: 2, MaxAgeDays: 1,
	})
	if err := writer.Write(alertReport("agent-a", time.Now())); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("目录应被自动创建: %v", err)
	}
}
