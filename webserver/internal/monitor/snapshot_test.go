package monitor

import (
	"math"
	"testing"
	"time"
)

// baseTime 是测试用固定基准时间，避免依赖真实时钟。
var baseTime = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// point 构造一个测试数据点。
//
// 参数 Parameters:
//   - offset (time.Duration): 相对 baseTime 的时间偏移。
//   - field (string): 字段名。
//   - value (float64): 字段值。
//   - tags (map[string]any): 业务标签。
//
// 返回 Returns:
//   - point (MetricPoint): 数据点。
func point(offset time.Duration, field string, value float64, tags map[string]any) MetricPoint {
	if tags == nil {
		tags = map[string]any{}
	}
	return MetricPoint{Time: baseTime.Add(offset), Field: field, Value: value, Tags: tags}
}

// assertNear 断言浮点值在容差内相等。
func assertNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

// TestRateByTag 覆盖计数器速率换算：正常增量、计数器重置与单点序列。
func TestRateByTag(t *testing.T) {
	points := []MetricPoint{
		point(0, "bytes_recv", 1000, map[string]any{"interface": "eth0"}),
		point(30*time.Second, "bytes_recv", 1600, map[string]any{"interface": "eth0"}),
		// 计数器重置：新值小于旧值，按 0 速率处理，不能出现负值。
		point(60*time.Second, "bytes_recv", 100, map[string]any{"interface": "eth0"}),
		// 单点序列：不足以算出速率，不应出现在结果中。
		point(0, "bytes_recv", 500, map[string]any{"interface": "lo"}),
	}
	rates := rateByTag(points, "bytes_recv", "interface")
	assertNear(t, "eth0 速率", rates["eth0"], 0)
	if _, exists := rates["lo"]; exists {
		t.Fatalf("单点序列不应产出速率: %v", rates)
	}

	normal := []MetricPoint{
		point(0, "bytes_sent", 0, map[string]any{"interface": "eth0"}),
		point(30*time.Second, "bytes_sent", 1500, map[string]any{"interface": "eth0"}),
	}
	assertNear(t, "eth0 上行速率", rateByTag(normal, "bytes_sent", "interface")["eth0"], 50)
}

// TestSumLatestPrefixed 覆盖 netstat 多状态字段求和。
func TestSumLatestPrefixed(t *testing.T) {
	points := []MetricPoint{
		point(0, "tcp_established", 3, nil),
		point(0, "tcp_listen", 1, nil),
		point(0, "tcp_time_wait", 2, nil),
		point(0, "udp_socket", 1, nil),
		// 同字段的旧点不应被重复计入。
		point(-30*time.Second, "tcp_established", 99, nil),
	}
	assertNear(t, "tcp 合计", sumLatestPrefixed(points, "tcp_"), 6)
	assertNear(t, "udp 合计", sumLatestPrefixed(points, "udp_"), 1)
}

// TestBuildSnapshotLinux 覆盖 Linux 主机的快照换算。
func TestBuildSnapshotLinux(t *testing.T) {
	agent := Agent{ID: "01H", OS: "linux", MemoryTotalBytes: 4 << 30, MemoryUsedBytes: 2 << 30, SystemUptimeSeconds: 3600}
	series := map[string][]MetricPoint{
		"cpu": {
			point(0, "usage_idle", 98, map[string]any{"cpu": "cpu-total"}),
			point(0, "usage_user", 1, map[string]any{"cpu": "cpu-total"}),
			point(0, "usage_system", 0.5, map[string]any{"cpu": "cpu-total"}),
			point(0, "usage_iowait", 0.5, map[string]any{"cpu": "cpu-total"}),
			// 未聚合的分核数据不应覆盖 cpu-total。
			point(0, "usage_idle", 10, map[string]any{"cpu": "cpu0"}),
		},
		"mem": {
			point(0, "total", 4<<30, nil),
			point(0, "used", 2.5*float64(uint64(1)<<30), nil),
			point(0, "used_percent", 62.5, nil),
		},
		"swap": {
			point(0, "total", 1<<30, nil),
			point(0, "used", 0, nil),
			point(0, "used_percent", 0, nil),
		},
		"system": {
			point(0, "load1", 0.10, nil),
			point(0, "load5", 0.07, nil),
			point(0, "load15", 0.05, nil),
			point(0, "n_cpus", 4, nil),
			point(0, "uptime", 7200, nil),
		},
		"processes": {
			point(0, "total", 120, nil),
			point(0, "running", 2, nil),
			point(0, "sleeping", 118, nil),
		},
		"netstat": {
			point(0, "tcp_established", 3, nil),
			point(0, "tcp_listen", 1, nil),
			point(0, "udp_socket", 1, nil),
		},
		"disk": {
			point(0, "total", 200<<30, map[string]any{"path": "/", "fstype": "ext4", "device": "vda1"}),
			point(0, "used", 130<<30, map[string]any{"path": "/", "fstype": "ext4", "device": "vda1"}),
			point(0, "used_percent", 65.03, map[string]any{"path": "/", "fstype": "ext4", "device": "vda1"}),
		},
		"diskio": {
			point(0, "read_bytes", 1000, map[string]any{"name": "vda"}),
			point(30*time.Second, "read_bytes", 4000, map[string]any{"name": "vda"}),
			point(0, "write_bytes", 2000, map[string]any{"name": "vda"}),
			point(30*time.Second, "write_bytes", 2000, map[string]any{"name": "vda"}),
			point(0, "reads", 0, map[string]any{"name": "vda"}),
			point(30*time.Second, "reads", 300, map[string]any{"name": "vda"}),
		},
		"net": {
			point(0, "bytes_sent", 0, map[string]any{"interface": "eth0"}),
			point(30*time.Second, "bytes_sent", 1290, map[string]any{"interface": "eth0"}),
			point(0, "bytes_recv", 0, map[string]any{"interface": "eth0"}),
			point(30*time.Second, "bytes_recv", 750, map[string]any{"interface": "eth0"}),
		},
	}

	snapshot := buildSnapshot(agent, series)

	assertNear(t, "CPU 总使用率", snapshot.CPU.Total, 2)
	assertNear(t, "CPU 用户态", snapshot.CPU.User, 1)
	assertNear(t, "CPU 内核态", snapshot.CPU.System, 0.5)
	if snapshot.CPU.Source != "cpu" {
		t.Fatalf("CPU.Source = %q, want cpu", snapshot.CPU.Source)
	}
	assertNear(t, "内存总量", snapshot.Memory.TotalBytes, float64(4<<30))
	assertNear(t, "内存使用率", snapshot.Memory.UsedPercent, 62.5)
	assertNear(t, "交换分区使用率", snapshot.Swap.UsedPercent, 0)
	assertNear(t, "负载 load1", snapshot.Load.Load1, 0.10)
	assertNear(t, "负载 load15", snapshot.Load.Load15, 0.05)
	assertNear(t, "运行时长", snapshot.Load.UptimeSeconds, 7200)
	assertNear(t, "进程总数", snapshot.Processes.Total, 120)
	assertNear(t, "TCP 连接数", snapshot.Netstat.TCP, 4)
	assertNear(t, "UDP 连接数", snapshot.Netstat.UDP, 1)
	assertNear(t, "连接总数", snapshot.Netstat.Total, 5)
	if len(snapshot.Disks) != 1 {
		t.Fatalf("磁盘数量 = %d, want 1", len(snapshot.Disks))
	}
	assertNear(t, "磁盘使用率", snapshot.Disks[0].UsedPercent, 65.03)
	if snapshot.Disks[0].Device != "vda1" || snapshot.Disks[0].FSType != "ext4" {
		t.Fatalf("磁盘标签解析不正确: %+v", snapshot.Disks[0])
	}
	if len(snapshot.DiskIO) != 1 {
		t.Fatalf("磁盘设备数量 = %d, want 1", len(snapshot.DiskIO))
	}
	assertNear(t, "磁盘读吞吐", snapshot.DiskIO[0].ReadBytesPerSec, 100)
	assertNear(t, "磁盘写吞吐", snapshot.DiskIO[0].WriteBytesPerSec, 0)
	assertNear(t, "磁盘读 IOPS", snapshot.DiskIO[0].ReadIops, 10)
	if len(snapshot.Networks) != 1 {
		t.Fatalf("网卡数量 = %d, want 1", len(snapshot.Networks))
	}
	assertNear(t, "网卡上行", snapshot.Networks[0].UpBytesPerSec, 43)
	assertNear(t, "网卡下行", snapshot.Networks[0].DownBytesPerSec, 25)
	if snapshot.CollectedAt.IsZero() {
		t.Fatal("CollectedAt 未填充")
	}
}

// TestBuildSnapshotWindows 覆盖 Windows 主机的快照换算。
func TestBuildSnapshotWindows(t *testing.T) {
	agent := Agent{ID: "01H", OS: "windows 10.0.26100", MemoryTotalBytes: 4 << 30}
	series := map[string][]MetricPoint{
		// Windows 与 Linux 一样用原生 cpu / mem 插件（实测两端字段名一致）。
		"cpu": {
			point(0, "usage_idle", 96.74, map[string]any{"cpu": "cpu-total"}),
			point(0, "usage_user", 1.50, map[string]any{"cpu": "cpu-total"}),
			point(0, "usage_system", 1.76, map[string]any{"cpu": "cpu-total"}),
		},
		"mem": {
			point(0, "total", 4<<30, nil),
			point(0, "used", 2.75*float64(uint64(1)<<30), nil),
			point(0, "used_percent", 68.75, nil),
		},
		// 处理器队列来自 win_perf_counters，字段名已被 Telegraf 规范化。
		"win_system": {
			point(0, "Processor_Queue_Length", 0.07, map[string]any{"objectname": "System"}),
			point(0, "System_Up_Time", 8504.98, nil),
		},
	}

	snapshot := buildSnapshot(agent, series)

	assertNear(t, "CPU 总使用率", snapshot.CPU.Total, 3.26)
	if snapshot.CPU.Source != "cpu" {
		t.Fatalf("CPU.Source = %q, want cpu", snapshot.CPU.Source)
	}
	assertNear(t, "内存使用率", snapshot.Memory.UsedPercent, 68.75)
	assertNear(t, "处理器队列", snapshot.Load.QueueLength, 0.07)
	if !snapshot.Load.HasQueue {
		t.Fatal("采到处理器队列时 HasQueue 应为 true")
	}
	assertNear(t, "网卡数量为 0", float64(len(snapshot.Networks)), 0)
}

// TestBuildSnapshotWindowsWithoutQueue 覆盖「队列没采到」：不能退化成 0 让页面显示「正常」。
func TestBuildSnapshotWindowsWithoutQueue(t *testing.T) {
	agent := Agent{ID: "01H", OS: "windows", MemoryTotalBytes: 4 << 30}
	snapshot := buildSnapshot(agent, map[string][]MetricPoint{
		"mem": {point(0, "total", 4<<30, nil), point(0, "used", 1<<30, nil)},
	})

	if snapshot.Load.HasQueue {
		t.Fatal("没有 win_system 数据时 HasQueue 应为 false")
	}
	if snapshot.CPU.Source != "" {
		t.Fatalf("没有 cpu 数据时 Source 应为空, got %q", snapshot.CPU.Source)
	}
}

// TestWinCounterField 覆盖 Windows 计数器名到字段名的规范化。
func TestWinCounterField(t *testing.T) {
	tests := map[string]string{
		"% Processor Time":       "Percent_Processor_Time",
		"% User Time":            "Percent_User_Time",
		"Processor Queue Length": "Processor_Queue_Length",
		"Available Bytes":        "Available_Bytes",
		"Committed Bytes In Use": "Committed_Bytes_In_Use",
		"System Up Time":         "System_Up_Time",
		"System Calls/sec":       "System_Calls_persec",
	}
	for counter, want := range tests {
		if got := winCounterField(counter); got != want {
			t.Fatalf("winCounterField(%q) = %q, want %q", counter, got, want)
		}
	}
}

// TestLatestWinCounter 覆盖字段名兼容：规范化名与原始名都要能取到。
func TestLatestWinCounter(t *testing.T) {
	sanitized := []MetricPoint{point(0, "Processor_Queue_Length", 1.5, nil)}
	if value, ok := latestWinCounter(sanitized, "Processor Queue Length"); !ok || value != 1.5 {
		t.Fatalf("规范化字段名取值失败: value=%v ok=%v", value, ok)
	}

	raw := []MetricPoint{point(0, "Processor Queue Length", 2.5, nil)}
	if value, ok := latestWinCounter(raw, "Processor Queue Length"); !ok || value != 2.5 {
		t.Fatalf("原始字段名取值失败: value=%v ok=%v", value, ok)
	}

	if _, ok := latestWinCounter(nil, "Processor Queue Length"); ok {
		t.Fatal("无数据时应返回 ok=false")
	}
}

// TestBuildSnapshotEmpty 覆盖采集缺口：InfluxDB 无数据时用心跳上报值兜底，不 panic。
func TestBuildSnapshotEmpty(t *testing.T) {
	agent := Agent{ID: "01H", OS: "linux", MemoryTotalBytes: 8 << 30, MemoryUsedBytes: 3 << 30, SystemUptimeSeconds: 60}
	snapshot := buildSnapshot(agent, map[string][]MetricPoint{})

	assertNear(t, "内存总量兜底", snapshot.Memory.TotalBytes, float64(8<<30))
	assertNear(t, "内存使用率兜底", snapshot.Memory.UsedPercent, 37.5)
	assertNear(t, "运行时长兜底", snapshot.Load.UptimeSeconds, 60)
	if len(snapshot.Disks) != 0 || len(snapshot.DiskIO) != 0 || len(snapshot.Networks) != 0 {
		t.Fatalf("无数据时列表应为空: disks=%d diskio=%d net=%d",
			len(snapshot.Disks), len(snapshot.DiskIO), len(snapshot.Networks))
	}
}

// TestSnapshotMeasurements 覆盖不同平台的 measurement 组合。
func TestSnapshotMeasurements(t *testing.T) {
	linux := snapshotMeasurements(false)
	for _, want := range []string{"cpu", "mem", "swap", "system", "disk", "diskio", "net", "netstat"} {
		if !contains(linux, want) {
			t.Fatalf("Linux measurement 缺少 %q: %v", want, linux)
		}
	}
	if contains(linux, "win_system") {
		t.Fatalf("Linux 不应查询 win_system: %v", linux)
	}

	windows := snapshotMeasurements(true)
	for _, want := range []string{"cpu", "mem", "disk", "diskio", "net", "win_system"} {
		if !contains(windows, want) {
			t.Fatalf("Windows measurement 缺少 %q: %v", want, windows)
		}
	}
	// CPU / 内存已统一到原生插件，不应再查 win_cpu / win_memory。
	if contains(windows, "win_cpu") || contains(windows, "win_memory") {
		t.Fatalf("Windows 不应再查询 win_cpu / win_memory: %v", windows)
	}
}

// contains 判断字符串切片是否包含指定取值。
func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
