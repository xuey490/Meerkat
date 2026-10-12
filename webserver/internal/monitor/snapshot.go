package monitor

import (
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// snapshotLookback 是快照现算的回溯窗口。
//
// Telegraf 各插件间隔为 10s（cpu/mem/net）或 30s（disk/diskio/processes），
// 回溯 3 分钟足以取到最新点，又能算出计数器速率（至少需要两个点）。
const snapshotLookback = "-3m"

// snapshotLimit 是单个 measurement 的点数上限。
//
// Windows 的 win_perf_counters 会按实例产生较多序列，留出比默认值更大的余量。
const snapshotLimit = 20000

// queryTimeout 是单次 InfluxDB 查询的超时上限，避免 Influx 卡住时拖垮 HTTP 请求。
const queryTimeout = 15 * time.Second

// CPUStat 是 CPU 使用率构成（百分比）。
type CPUStat struct {
	// Total 总使用率。
	Total float64 `json:"total"`
	// User 用户态占比。
	User float64 `json:"user"`
	// System 内核态占比。
	System float64 `json:"system"`
	// Iowait 等待 IO 占比（仅 Linux 采集）。
	Iowait float64 `json:"iowait"`
	// Idle 空闲占比（仅 Linux 采集）。
	Idle float64 `json:"idle"`
	// Source 取数口径：取到数据时为 "cpu"（Telegraf 原生 cpu 插件，两端通用）。
	// 为空表示该主机没有 cpu 指标，前端据此显示「暂无数据」而不是假的 0%。
	Source string `json:"source"`
}

// MemoryStat 是物理内存用量（字节）。
type MemoryStat struct {
	// TotalBytes 总量。
	TotalBytes float64 `json:"totalBytes"`
	// UsedBytes 已用。
	UsedBytes float64 `json:"usedBytes"`
	// AvailableBytes 可用。
	AvailableBytes float64 `json:"availableBytes"`
	// UsedPercent 使用率（百分比，0-100）。
	UsedPercent float64 `json:"usedPercent"`
}

// SwapStat 是交换分区用量（字节）。
//
// Linux 的 swap 是交换分区；Windows 的原生 swap 插件上报的是页面文件（已实测有 total/used/free）。
type SwapStat struct {
	// TotalBytes 总量。
	TotalBytes float64 `json:"totalBytes"`
	// UsedBytes 已用。
	UsedBytes float64 `json:"usedBytes"`
	// FreeBytes 空闲。
	FreeBytes float64 `json:"freeBytes"`
	// UsedPercent 使用率（百分比，0-100）。
	UsedPercent float64 `json:"usedPercent"`
}

// LoadStat 是系统负载与系统级计数。
type LoadStat struct {
	// Load1 1 分钟平均负载。
	Load1 float64 `json:"load1"`
	// Load5 5 分钟平均负载。
	Load5 float64 `json:"load5"`
	// Load15 15 分钟平均负载。
	Load15 float64 `json:"load15"`
	// QueueLength 处理器队列长度（仅 Windows 采集）。
	QueueLength float64 `json:"queueLength"`
	// HasQueue 是否真的采到处理器队列长度：false 表示该主机没上报，
	// 前端据此显示「暂无数据」，避免把「没采到」画成 0（看起来像「正常」）。
	HasQueue bool `json:"hasQueue"`
	// UptimeSeconds 运行时长（秒）。
	UptimeSeconds float64 `json:"uptimeSeconds"`
	// Users 登录用户数（仅 Linux 采集）。
	Users float64 `json:"users"`
	// CPUCores 逻辑核心数（system 插件采集值）。
	CPUCores float64 `json:"cpuCores"`
}

// ProcessesStat 是进程状态计数。
type ProcessesStat struct {
	// Total 进程总数。
	Total float64 `json:"total"`
	// Running 运行中。
	Running float64 `json:"running"`
	// Sleeping 休眠。
	Sleeping float64 `json:"sleeping"`
	// Blocked 阻塞。
	Blocked float64 `json:"blocked"`
	// Zombies 僵尸。
	Zombies float64 `json:"zombies"`
}

// NetstatStat 是连接数统计。
//
// TCP 是各状态（established / listen / time_wait ...）之和；
// 未启用 netstat 插件的旧 Agent 三项均为 0，展示层应回退到心跳上报的监听端口数。
type NetstatStat struct {
	// TCP TCP 连接数。
	TCP float64 `json:"tcp"`
	// UDP UDP 套接字数。
	UDP float64 `json:"udp"`
	// Total 连接总数。
	Total float64 `json:"total"`
}

// DiskStat 是一个挂载点的容量与使用率。
type DiskStat struct {
	// Path 挂载点。
	Path string `json:"path"`
	// FSType 文件系统类型。
	FSType string `json:"fstype"`
	// Device 设备名（磁盘分区）。
	Device string `json:"device"`
	// TotalBytes 分区总容量。
	TotalBytes float64 `json:"totalBytes"`
	// UsedBytes 已用容量。
	UsedBytes float64 `json:"usedBytes"`
	// FreeBytes 空闲容量。
	FreeBytes float64 `json:"freeBytes"`
	// UsedPercent 使用率（百分比，0-100）。
	UsedPercent float64 `json:"usedPercent"`
}

// DiskIOStat 是一个块设备的实时吞吐与 IOPS。
type DiskIOStat struct {
	// Name 设备名，如 sda / vda1 / nvme0n1。
	Name string `json:"name"`
	// ReadBytesPerSec 每秒读取字节数。
	ReadBytesPerSec float64 `json:"readBytesPerSec"`
	// WriteBytesPerSec 每秒写入字节数。
	WriteBytesPerSec float64 `json:"writeBytesPerSec"`
	// ReadIops 每秒读次数。
	ReadIops float64 `json:"readIops"`
	// WriteIops 每秒写次数。
	WriteIops float64 `json:"writeIops"`
}

// NetworkStat 是一个网卡的实时上下行速率。
type NetworkStat struct {
	// Name 网卡名，如 eth0 / eth0.100。
	Name string `json:"name"`
	// UpBytesPerSec 每秒发送字节数。
	UpBytesPerSec float64 `json:"upBytesPerSec"`
	// DownBytesPerSec 每秒接收字节数。
	DownBytesPerSec float64 `json:"downBytesPerSec"`
}

// MetricSnapshot 是服务器详情「主机概况」所需的当前值快照。
//
// 全部由 InfluxDB 最近数分钟的点现算，不落库：这些值随 Telegraf 采集滚动变化，
// 缓存反而会引入一致性负担；单机查询量在毫秒级，无需额外表。
type MetricSnapshot struct {
	// CPU CPU 使用率构成。
	CPU CPUStat `json:"cpu"`
	// Memory 物理内存用量。
	Memory MemoryStat `json:"memory"`
	// Swap 交换分区用量。
	Swap SwapStat `json:"swap"`
	// Load 系统负载与运行时长。
	Load LoadStat `json:"load"`
	// Processes 进程状态计数。
	Processes ProcessesStat `json:"processes"`
	// Netstat 连接数统计。
	Netstat NetstatStat `json:"netstat"`
	// Disks 挂载点容量列表，按使用率倒序。
	Disks []DiskStat `json:"disks"`
	// DiskIO 块设备实时吞吐列表，按读写合计倒序。
	DiskIO []DiskIOStat `json:"diskIo"`
	// Networks 网卡实时速率列表，按上下行合计倒序。
	Networks []NetworkStat `json:"networks"`
	// CollectedAt 快照生成时间（UTC）。
	CollectedAt time.Time `json:"collectedAt"`
}

// Snapshot 汇总服务器详情「主机概况」需要的当前值。
//
// 参数 Parameters:
//   - agentID (string): 服务器 ID；空串 / undefined / null 视为非法。
//
// 返回 Returns:
//   - snapshot (MetricSnapshot): 指标快照；某项无数据时对应字段为零值。
//   - err (error): 资产不存在时返回 gorm.ErrRecordNotFound；参数非法返回 ErrInvalidAgent；
//     InfluxDB 不可用返回 ErrInfluxUnavailable；部分 measurement 查询失败时降级为零值并记日志。
//
// 行为约束：阻塞直到 InfluxDB 返回或 queryTimeout 超时；并发发起各 measurement 查询（最多 11 个，
// 与 agent 操作系统相关），不改动任何持久化状态。
func (s *Service) Snapshot(agentID string) (MetricSnapshot, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || agentID == "undefined" || agentID == "null" {
		return MetricSnapshot{}, ErrInvalidAgent
	}
	var agent Agent
	if err := s.db.First(&agent, "id = ?", agentID).Error; err != nil {
		return MetricSnapshot{}, err
	}
	if s.influx == nil {
		return MetricSnapshot{}, ErrInfluxUnavailable
	}

	windows := strings.Contains(strings.ToLower(agent.OS), "windows")
	measurements := snapshotMeasurements(windows)
	series := make(map[string][]MetricPoint, len(measurements))

	var (
		mutex    sync.Mutex
		wait     sync.WaitGroup
		failures int
		lastErr  error
	)
	for _, measurement := range measurements {
		wait.Add(1)
		go func(name string) {
			defer wait.Done()
			points, err := s.Metrics(MetricQuery{
				Measurement: name,
				AgentID:     agentID,
				Start:       snapshotLookback,
				Limit:       snapshotLimit,
			})
			if err != nil {
				logSnapshotQueryFailure(name, agentID, err)
				mutex.Lock()
				failures++
				lastErr = err
				mutex.Unlock()
				return
			}
			mutex.Lock()
			series[name] = points
			mutex.Unlock()
		}(measurement)
	}
	wait.Wait()
	if failures == len(measurements) && lastErr != nil {
		return MetricSnapshot{}, lastErr
	}
	return buildSnapshot(agent, series), nil
}

// snapshotMeasurements 返回快照需要查询的 measurement 列表。
//
// 参数 Parameters:
//   - windows (bool): 目标主机是否为 Windows。
//
// 返回 Returns:
//   - measurements ([]string): measurement 名列表。
//
// 说明：CPU / 内存 / 交换分区 / 负载 / 磁盘 / 网卡都用 Telegraf 的原生插件，两端字段名一致，
// 因此列表基本通用；Windows 额外查 win_system —— 原生 system 插件在 Windows 上只给
// uptime/n_cpus，load1/5/15 恒为 0，处理器队列只能来自 win_perf_counters
// （已实测：本机 Windows 同时写入 cpu/mem/system 与 win_cpu/win_memory/win_system）。
func snapshotMeasurements(windows bool) []string {
	common := []string{"cpu", "mem", "swap", "system", "disk", "diskio", "net", "processes", "netstat"}
	if windows {
		return append(common, "win_system")
	}
	return common
}

// buildSnapshot 把原始数据点换算为指标快照。
//
// 参数 Parameters:
//   - agent (Agent): 资产记录，用于判断平台并提供内存总量等兜底值。
//   - series (map[string][]MetricPoint): measurement → 最近数分钟的数据点。
//
// 返回 Returns:
//   - snapshot (MetricSnapshot): 指标快照；缺失的 measurement 对应字段为零值。
//
// 行为约束：纯函数，不发起 IO，可被并发调用；入参 map 只读。
func buildSnapshot(agent Agent, series map[string][]MetricPoint) MetricSnapshot {
	windows := strings.Contains(strings.ToLower(agent.OS), "windows")
	return MetricSnapshot{
		CPU:         buildCPUStat(series),
		Memory:      buildMemoryStat(series, agent),
		Swap:        buildSwapStat(series["swap"]),
		Load:        buildLoadStat(series, agent, windows),
		Processes:   buildProcessesStat(series["processes"]),
		Netstat:     buildNetstatStat(series["netstat"]),
		Disks:       buildDiskStats(series["disk"]),
		DiskIO:      buildDiskIOStats(series["diskio"]),
		Networks:    buildNetworkStats(series["net"]),
		CollectedAt: time.Now().UTC(),
	}
}

// buildCPUStat 汇总 CPU 使用率。
//
// 参数 Parameters:
//   - series (map[string][]MetricPoint): 快照数据点。
//
// 返回 Returns:
//   - stat (CPUStat): 优先取 cpu-total 汇总序列；无数据时 Source 为空（前端据此显示「暂无数据」）。
//
// 说明：Windows 与 Linux 都用 Telegraf 的原生 cpu 插件（Windows 侧同样由性能计数器给出
// usage_* 与 cpu-total），不再读 win_cpu —— 那是另一套字段名（Percent_Processor_Time），
// 曾经因为按 "% Processor Time" 取值取不到，导致详情页 CPU 恒为 0%。
func buildCPUStat(series map[string][]MetricPoint) CPUStat {
	points := series["cpu"]
	total := filterTag(points, "cpu", "cpu-total")
	if len(total) == 0 {
		total = points
	}
	idle, hasIdle := latestValue(total, "usage_idle")
	userValue, hasUser := latestValue(total, "usage_user")
	if !hasIdle && !hasUser {
		return CPUStat{}
	}
	system, _ := latestValue(total, "usage_system")
	iowait, _ := latestValue(total, "usage_iowait")
	stat := CPUStat{User: userValue, System: system, Iowait: iowait, Idle: idle, Source: "cpu"}
	if hasIdle {
		stat.Total = clampPercent(100 - idle)
	} else {
		// 个别内核/配置只上报 usage_user，此时用用户态近似总使用率。
		stat.Total = clampPercent(userValue)
	}
	stat.User = clampPercent(stat.User)
	stat.System = clampPercent(stat.System)
	stat.Iowait = clampPercent(stat.Iowait)
	return stat
}

// buildMemoryStat 汇总内存用量。
//
// 参数 Parameters:
//   - series (map[string][]MetricPoint): 快照数据点。
//   - agent (Agent): 资产记录，mem 插件缺失时用其内存总量 / 已用量兜底。
//
// 返回 Returns:
//   - stat (MemoryStat): 使用率优先取采集值，缺失时按「已用 / 总量」现算。
//
// 说明：Windows 与 Linux 都用原生 mem 插件（两端都有 total/used/available/used_percent）。
// 不再读 win_memory：它的字段是 Committed_Bytes_In_Use / Available_Bytes，曾因取不到
// Available Bytes 让「可用内存」退化成 0，于是页面显示成「已用 = 总量 = 100%」。
func buildMemoryStat(series map[string][]MetricPoint, agent Agent) MemoryStat {
	points := series["mem"]
	total, hasTotal := latestValue(points, "total")
	used, hasUsed := latestValue(points, "used")
	available, hasAvailable := latestValue(points, "available")
	usedPercent, hasPercent := latestValue(points, "used_percent")
	if !hasTotal && !hasUsed {
		// mem 插件没数据（例如刚重启的 Agent）：退回心跳上报的内存值。
		rescue := MemoryStat{
			TotalBytes: float64(agent.MemoryTotalBytes),
			UsedBytes:  float64(agent.MemoryUsedBytes),
		}
		if rescue.TotalBytes > 0 {
			rescue.AvailableBytes = math.Max(0, rescue.TotalBytes-rescue.UsedBytes)
			rescue.UsedPercent = clampPercent(rescue.UsedBytes / rescue.TotalBytes * 100)
		}
		return rescue
	}
	if total <= 0 {
		total = float64(agent.MemoryTotalBytes)
	}
	stat := MemoryStat{TotalBytes: total, UsedBytes: used, AvailableBytes: available}
	if !hasAvailable && total > 0 {
		stat.AvailableBytes = math.Max(0, total-used)
	}
	if hasPercent {
		stat.UsedPercent = clampPercent(usedPercent)
	} else if total > 0 {
		stat.UsedPercent = clampPercent(used / total * 100)
	}
	return stat
}

// buildSwapStat 汇总交换分区 / 页面文件用量；未启用 swap 插件时返回零值。
//
// 参数 Parameters:
//   - points ([]MetricPoint): measurement swap 的数据点。
//
// 返回 Returns:
//   - stat (SwapStat): 交换分区用量。
func buildSwapStat(points []MetricPoint) SwapStat {
	if len(points) == 0 {
		return SwapStat{}
	}
	total, _ := latestValue(points, "total")
	used, _ := latestValue(points, "used")
	free, _ := latestValue(points, "free")
	usedPercent, hasPercent := latestValue(points, "used_percent")
	stat := SwapStat{TotalBytes: total, UsedBytes: used, FreeBytes: free}
	if hasPercent {
		stat.UsedPercent = clampPercent(usedPercent)
	} else if total > 0 {
		stat.UsedPercent = clampPercent(used / total * 100)
	}
	return stat
}

// buildLoadStat 汇总系统负载与系统级计数。
//
// 参数 Parameters:
//   - series (map[string][]MetricPoint): 快照数据点。
//   - agent (Agent): 资产记录，运行时长缺失时用其心跳上报值兜底。
//   - windows (bool): 目标主机是否为 Windows。
//
// 返回 Returns:
//   - stat (LoadStat): Windows 无 load1/5/15（原生 system 插件在 Windows 上恒为 0），
//     改为回填 win_perf_counters 的处理器队列长度，并用 HasQueue 标明该值是否真的采到。
func buildLoadStat(series map[string][]MetricPoint, agent Agent, windows bool) LoadStat {
	stat := LoadStat{UptimeSeconds: float64(agent.SystemUptimeSeconds)}
	points := series["system"]
	stat.CPUCores, _ = latestValue(points, "n_cpus")
	if uptime, ok := latestValue(points, "uptime"); ok && uptime > 0 {
		stat.UptimeSeconds = uptime
	}
	if windows {
		if queue, ok := latestWinCounter(series["win_system"], "Processor Queue Length"); ok {
			stat.QueueLength = queue
			stat.HasQueue = true
		}
		return stat
	}
	stat.Load1, _ = latestValue(points, "load1")
	stat.Load5, _ = latestValue(points, "load5")
	stat.Load15, _ = latestValue(points, "load15")
	stat.Users, _ = latestValue(points, "n_users")
	return stat
}

// winCounterField 把 Windows 性能计数器名转换为 Telegraf win_perf_counters 落库的字段名。
//
// Telegraf 会规范化计数器名：`% ` 前缀 → `Percent_`、`/` → `_per`、空格 → `_`。
// 例如 `% Processor Time` → `Percent_Processor_Time`、`System Calls/sec` → `System_Calls_persec`。
// 该规范化随版本变化，所以取值时同时尝试规范化名与原始名（见 latestWinCounter）。
//
// 参数 Parameters:
//   - counter (string): 配置里的计数器名，如 "Processor Queue Length"。
//
// 返回 Returns:
//   - field (string): 规范化后的字段名。
func winCounterField(counter string) string {
	field := strings.ReplaceAll(counter, "% ", "Percent_")
	field = strings.ReplaceAll(field, "/", "_per")
	return strings.ReplaceAll(field, " ", "_")
}

// latestWinCounter 取 Windows 计数器的最新值，兼容字段名是否被规范化。
//
// 参数 Parameters:
//   - points ([]MetricPoint): measurement 的数据点。
//   - counter (string): 配置里的计数器名。
//
// 返回 Returns:
//   - value (float64): 最新值。
//   - ok (bool): 取到值时返回 true；两个候选名都没数据时返回 false。
func latestWinCounter(points []MetricPoint, counter string) (float64, bool) {
	for _, field := range []string{winCounterField(counter), counter} {
		if value, ok := latestValue(points, field); ok {
			return value, true
		}
	}
	return 0, false
}

// buildProcessesStat 汇总进程状态计数。
//
// 参数 Parameters:
//   - points ([]MetricPoint): measurement processes 的数据点。
//
// 返回 Returns:
//   - stat (ProcessesStat): 进程状态计数。
func buildProcessesStat(points []MetricPoint) ProcessesStat {
	stat := ProcessesStat{}
	stat.Total, _ = latestValue(points, "total")
	stat.Running, _ = latestValue(points, "running")
	stat.Sleeping, _ = latestValue(points, "sleeping")
	stat.Blocked, _ = latestValue(points, "blocked")
	stat.Zombies, _ = latestValue(points, "zombies")
	return stat
}

// buildNetstatStat 汇总连接数统计。
//
// 参数 Parameters:
//   - points ([]MetricPoint): measurement netstat 的数据点。
//
// 返回 Returns:
//   - stat (NetstatStat): 三项全为 0 表示该主机未启用 netstat 插件。
func buildNetstatStat(points []MetricPoint) NetstatStat {
	stat := NetstatStat{
		TCP: sumLatestPrefixed(points, "tcp_"),
		UDP: sumLatestPrefixed(points, "udp_"),
	}
	stat.Total = stat.TCP + stat.UDP
	return stat
}

// buildDiskStats 汇总各挂载点的容量与使用率。
//
// 参数 Parameters:
//   - points ([]MetricPoint): measurement disk 的数据点。
//
// 返回 Returns:
//   - stats ([]DiskStat): 按使用率倒序的挂载点列表；无数据时为空切片（非 nil）。
//
// 行为约束：按 path 标签分组，同一挂载点存在多条旧序列时只取最新值。
func buildDiskStats(points []MetricPoint) []DiskStat {
	stats := make([]DiskStat, 0, 4)
	for _, path := range tagValues(points, "path") {
		scoped := filterTag(points, "path", path)
		total, _ := latestValue(scoped, "total")
		used, _ := latestValue(scoped, "used")
		free, _ := latestValue(scoped, "free")
		usedPercent, hasPercent := latestValue(scoped, "used_percent")
		stat := DiskStat{
			Path:       path,
			FSType:     firstTagValue(scoped, "fstype"),
			Device:     firstTagValue(scoped, "device"),
			TotalBytes: total,
			UsedBytes:  used,
			FreeBytes:  free,
		}
		if hasPercent {
			stat.UsedPercent = clampPercent(usedPercent)
		} else if total > 0 {
			stat.UsedPercent = clampPercent(used / total * 100)
		}
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].UsedPercent != stats[j].UsedPercent {
			return stats[i].UsedPercent > stats[j].UsedPercent
		}
		return stats[i].Path < stats[j].Path
	})
	return stats
}

// buildDiskIOStats 汇总块设备的实时吞吐与 IOPS。
//
// 参数 Parameters:
//   - points ([]MetricPoint): measurement diskio 的计数器点。
//
// 返回 Returns:
//   - stats ([]DiskIOStat): 按读写字节合计倒序的设备列表；无数据时为空切片（非 nil）。
func buildDiskIOStats(points []MetricPoint) []DiskIOStat {
	readBytes := rateByTag(points, "read_bytes", "name")
	writeBytes := rateByTag(points, "write_bytes", "name")
	reads := rateByTag(points, "reads", "name")
	writes := rateByTag(points, "writes", "name")
	stats := make([]DiskIOStat, 0, len(readBytes)+len(writeBytes))
	for _, name := range tagValues(points, "name") {
		stat := DiskIOStat{
			Name:             name,
			ReadBytesPerSec:  readBytes[name],
			WriteBytesPerSec: writeBytes[name],
			ReadIops:         reads[name],
			WriteIops:        writes[name],
		}
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		left := stats[i].ReadBytesPerSec + stats[i].WriteBytesPerSec
		right := stats[j].ReadBytesPerSec + stats[j].WriteBytesPerSec
		if left != right {
			return left > right
		}
		return stats[i].Name < stats[j].Name
	})
	return stats
}

// buildNetworkStats 汇总各网卡的实时上下行速率。
//
// 参数 Parameters:
//   - points ([]MetricPoint): measurement net 的计数器点。
//
// 返回 Returns:
//   - stats ([]NetworkStat): 按上下行合计倒序的网卡列表；无数据时为空切片（非 nil）。
func buildNetworkStats(points []MetricPoint) []NetworkStat {
	sent := rateByTag(points, "bytes_sent", "interface")
	received := rateByTag(points, "bytes_recv", "interface")
	stats := make([]NetworkStat, 0, len(sent)+len(received))
	for _, name := range tagValues(points, "interface") {
		stats = append(stats, NetworkStat{
			Name:            name,
			UpBytesPerSec:   sent[name],
			DownBytesPerSec: received[name],
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		left := stats[i].UpBytesPerSec + stats[i].DownBytesPerSec
		right := stats[j].UpBytesPerSec + stats[j].DownBytesPerSec
		if left != right {
			return left > right
		}
		return stats[i].Name < stats[j].Name
	})
	return stats
}

// firstTagValue 返回点集中首个非空的标签取值。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 数据点。
//   - tagKey (string): 标签名。
//
// 返回 Returns:
//   - value (string): 标签取值；全部为空时返回空串。
func firstTagValue(points []MetricPoint, tagKey string) string {
	for _, point := range points {
		if value := tagValue(point, tagKey); value != "" {
			return value
		}
	}
	return ""
}

// clampPercent 把百分比收敛到 [0,100]。
//
// 参数 Parameters:
//   - value (float64): 原始百分比，可能因浮点误差溢出边界。
//
// 返回 Returns:
//   - percent (float64): 收敛后的百分比。
func clampPercent(value float64) float64 {
	if math.IsNaN(value) {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

// logSnapshotQueryFailure 记录单个 measurement 查询失败的上下文。
//
// 快照接口对单个 measurement 的失败采取降级策略（该卡片显示为空），
// 但不能静默：Influx 权限/字段类型问题只有日志里才看得见。
//
// 参数 Parameters:
//   - measurement (string): 查询失败的 measurement。
//   - agentID (string): 服务器 ID。
//   - err (error): 失败原因。
func logSnapshotQueryFailure(measurement, agentID string, err error) {
	slog.Warn("快照指标查询失败", "measurement", measurement, "agent_id", agentID, "error", err)
}
