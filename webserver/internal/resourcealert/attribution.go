package resourcealert

import (
	"context"
	"fmt"
	"sort"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

// procstatWindow 是进程归因的统计窗口：取最近若干分钟的均值，避免瞬时抖动。
const procstatWindow = 5 * time.Minute

// Attribution 是一条进程归因记录。
type Attribution struct {
	// Process 进程名（procstat 的 process_name / 心跳快照的 name）。
	Process string `json:"process"`
	// PID 进程号；procstat 默认不把 pid 作为 tag（会撑大 series 基数），因此可能为空。
	PID int `json:"pid,omitempty"`
	// User 进程属主（procstat 提供）。
	User string `json:"user,omitempty"`
	// CPUPercent 进程 CPU 占用（百分比）。
	CPUPercent float64 `json:"cpuPercent"`
	// MemoryRSSMB 进程常驻内存（MB）。
	MemoryRSSMB float64 `json:"memoryRssMb"`
	// MemoryPercent 进程内存占物理内存的百分比（心跳兜底时可算）。
	MemoryPercent float64 `json:"memoryPercent,omitempty"`
	// Source 数据来源：procstat（进程级采集）/ heartbeat（心跳进程快照兜底）。
	Source string `json:"source"`
}

// CPUDetail 是 CPU 明细：区分 user / system / iowait，用于判断瓶颈性质。
//
// iowait 高说明进程在等磁盘而不是在算，归因方向完全不同——这是「综合分析」的一部分。
type CPUDetail struct {
	// User 用户态占比。
	User float64 `json:"user"`
	// System 内核态占比。
	System float64 `json:"system"`
	// IOWait 等待 IO 占比。
	IOWait float64 `json:"iowait"`
}

// SystemContext 是告警的系统上下文，用于判断瓶颈性质（算力 or IO）。
type SystemContext struct {
	// Load1 1 分钟负载（绝对值）。
	Load1 float64 `json:"load1"`
	// CPUCores 逻辑核数。
	CPUCores uint64 `json:"cpuCores"`
	// LoadPerCore 归一化负载（load1 / 核数）。
	LoadPerCore float64 `json:"loadPerCore"`
	// MemoryUsedPercent 内存利用率（来自心跳）。
	MemoryUsedPercent float64 `json:"memoryUsedPercent"`
	// SwapUsedPercent swap 使用率（时序库补充，取不到时为 0）。
	SwapUsedPercent float64 `json:"swapUsedPercent,omitempty"`
	// IOWaitPercent iowait 占比（时序库补充）：高则说明瓶颈在磁盘而非 CPU 算力。
	IOWaitPercent float64 `json:"iowaitPercent,omitempty"`
	// ProcessCount 进程总数。
	ProcessCount uint64 `json:"processCount,omitempty"`
}

// Attributor 负责告警的进程归因与系统上下文汇总。
type Attributor struct {
	// store 持久化入口（心跳进程快照兜底）。
	store *Store
	// client 时序库客户端，可为 nil。
	client influxdb2.Client
	// org 组织名。
	org string
	// bucket 桶名。
	bucket string
	// topN 归因条数。
	topN int
	// preferProcstat 是否优先使用进程级采集数据。
	preferProcstat bool
}

// NewAttributor 创建归因器。
//
// 参数 Parameters:
//   - store (*Store): 持久化入口。
//   - client (influxdb2.Client): 时序库客户端，可为 nil。
//   - org (string): 组织名。
//   - bucket (string): 桶名。
//   - topN (int): 归因条数，非正数时取 5。
//   - preferProcstat (bool): 是否优先使用 procstat。
//
// 返回 Returns:
//   - attributor (*Attributor): 归因器。
func NewAttributor(store *Store, client influxdb2.Client, org, bucket string, topN int, preferProcstat bool) *Attributor {
	if topN <= 0 {
		topN = 5
	}
	return &Attributor{store: store, client: client, org: org, bucket: bucket, topN: topN, preferProcstat: preferProcstat}
}

// ProcessTop 返回某主机在指定指标下占用最高的进程。
//
// 归因策略：
//   - CPU 告警按进程 CPU 占用排序；内存告警按进程 RSS 排序；
//   - 优先使用 Telegraf 的进程级采集（procstat），任意进程都能被归因到；
//   - procstat 未启用或查询失败时退化为心跳里的进程内存快照（只有内存维度，
//     CPU 占比会是 0），保证告警不会因为「拿不到归因」而变成一句话。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - agentID (string): 主机标识。
//   - metricKey (string): 指标标识（决定排序维度）。
//   - memoryTotalBytes (uint64): 物理内存总量（心跳兜底时算占比）。
//
// 返回 Returns:
//   - items ([]Attribution): 归因列表（按重要度降序）。
func (a *Attributor) ProcessTop(ctx context.Context, agentID, metricKey string, memoryTotalBytes uint64) []Attribution {
	sortField := "cpu_usage"
	if metricKey == MetricMemUsage {
		sortField = "memory_rss"
	}
	if a.preferProcstat && a.client != nil {
		items, err := a.procstatTop(ctx, agentID, sortField)
		if err == nil && len(items) > 0 {
			return items
		}
	}
	items, err := a.heartbeatTop(agentID, memoryTotalBytes)
	if err != nil {
		return nil
	}
	return items
}

// procstatTop 从时序库读取进程级 Top N 归因。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - agentID (string): 主机标识。
//   - sortField (string): 排序字段（cpu_usage / memory_rss）。
//
// 返回 Returns:
//   - items ([]Attribution): 归因列表。
//   - err (error): 查询失败时返回非 nil。
func (a *Attributor) procstatTop(ctx context.Context, agentID, sortField string) ([]Attribution, error) {
	// 先按 1 分钟降采样再聚合，避免把 30 秒原始粒度全量拉回来。
	query := fmt.Sprintf(`from(bucket: %q)
  |> range(start: -%ds)
  |> filter(fn: (r) => r._measurement == "procstat" and r.agent_id == %q)
  |> filter(fn: (r) => r._field == "cpu_usage" or r._field == "memory_rss")
  |> aggregateWindow(every: 1m, fn: mean, createEmpty: false)
  |> group(columns: ["process_name", "user", "_field"])
  |> mean()
  |> keep(columns: ["process_name", "user", "_field", "_value"])`,
		a.bucket, int(procstatWindow.Seconds()), agentID)
	result, err := a.client.QueryAPI(a.org).Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	byProcess := make(map[string]*Attribution, 32)
	for result.Next() {
		record := result.Record()
		name, _ := record.ValueByKey("process_name").(string)
		if name == "" {
			continue
		}
		field, _ := record.ValueByKey("_field").(string)
		user, _ := record.ValueByKey("user").(string)
		value, ok := toFloat(record.Value())
		if !ok {
			continue
		}
		item, exists := byProcess[name]
		if !exists {
			item = &Attribution{Process: name, User: user, Source: "procstat"}
			byProcess[name] = item
		}
		switch field {
		case "cpu_usage":
			item.CPUPercent = value
		case "memory_rss":
			item.MemoryRSSMB = value / (1024 * 1024)
		}
	}
	if err := result.Err(); err != nil {
		return nil, err
	}

	list := make([]Attribution, 0, len(byProcess))
	for _, item := range byProcess {
		list = append(list, *item)
	}
	sort.Slice(list, func(i, j int) bool {
		return attributionScore(list[i], sortField) > attributionScore(list[j], sortField)
	})
	if len(list) > a.topN {
		list = list[:a.topN]
	}
	return list, nil
}

// heartbeatTop 用心跳里的进程内存快照兜底归因。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - memoryTotalBytes (uint64): 物理内存总量。
//
// 返回 Returns:
//   - items ([]Attribution): 归因列表（按内存降序）。
//   - err (error): 查询失败时返回非 nil。
func (a *Attributor) heartbeatTop(agentID string, memoryTotalBytes uint64) ([]Attribution, error) {
	processes, err := a.store.AgentProcesses(agentID)
	if err != nil {
		return nil, err
	}
	sort.Slice(processes, func(i, j int) bool { return processes[i].MemoryBytes > processes[j].MemoryBytes })
	items := make([]Attribution, 0, a.topN)
	for i, process := range processes {
		if i >= a.topN {
			break
		}
		item := Attribution{
			Process:     process.Name,
			PID:         process.PID,
			MemoryRSSMB: float64(process.MemoryBytes) / (1024 * 1024),
			Source:      "heartbeat",
		}
		if memoryTotalBytes > 0 {
			item.MemoryPercent = float64(process.MemoryBytes) / float64(memoryTotalBytes) * 100
		}
		items = append(items, item)
	}
	return items, nil
}

// SystemContext 汇总某主机的系统上下文。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - snapshot (AgentSnapshot): 主机快照（心跳字段）。
//
// 返回 Returns:
//   - context (SystemContext): 系统上下文。
func (a *Attributor) SystemContext(ctx context.Context, snapshot AgentSnapshot) SystemContext {
	systemContext := SystemContext{
		Load1:        snapshot.Load1,
		CPUCores:     snapshot.LogicalCPUCores,
		ProcessCount: snapshot.ProcessCount,
	}
	cores := float64(snapshot.LogicalCPUCores)
	if cores <= 0 {
		cores = 1
	}
	systemContext.LoadPerCore = snapshot.Load1 / cores
	if snapshot.MemoryTotalBytes > 0 {
		systemContext.MemoryUsedPercent = float64(snapshot.MemoryUsedBytes) / float64(snapshot.MemoryTotalBytes) * 100
	}
	// swap 从时序库补充：取不到不影响告警本身。
	systemContext.SwapUsedPercent = a.latestField(ctx, snapshot.AgentID, "mem", "swap_used_percent", "")
	return systemContext
}

// CPUDetail 读取主机的 CPU 明细（user / system / iowait），一次查询取三个字段。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - detail (CPUDetail): CPU 明细；取不到时为零值。
func (a *Attributor) CPUDetail(ctx context.Context, agentID string) CPUDetail {
	detail := CPUDetail{}
	if a.client == nil {
		return detail
	}
	query := fmt.Sprintf(`from(bucket: %q)
  |> range(start: -5m)
  |> filter(fn: (r) => r._measurement == "cpu" and r.cpu == "cpu-total" and r.agent_id == %q)
  |> filter(fn: (r) => r._field == "usage_user" or r._field == "usage_system" or r._field == "usage_iowait")
  |> last()`,
		a.bucket, agentID)
	result, err := a.client.QueryAPI(a.org).Query(ctx, query)
	if err != nil {
		return detail
	}
	defer result.Close()
	for result.Next() {
		record := result.Record()
		value, ok := toFloat(record.Value())
		if !ok {
			continue
		}
		switch record.Field() {
		case "usage_user":
			detail.User = value
		case "usage_system":
			detail.System = value
		case "usage_iowait":
			detail.IOWait = value
		}
	}
	return detail
}

// latestField 读取某字段在最近 5 分钟内的最后一个值。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - agentID (string): 主机标识。
//   - measurement (string): measurement 名。
//   - field (string): 字段名。
//   - extraFilter (string): 附加 Flux 过滤片段。
//
// 返回 Returns:
//   - value (float64): 字段值；取不到时返回 0。
func (a *Attributor) latestField(ctx context.Context, agentID, measurement, field, extraFilter string) float64 {
	if a.client == nil {
		return 0
	}
	query := fmt.Sprintf(`from(bucket: %q)
  |> range(start: -5m)
  |> filter(fn: (r) => r._measurement == %q and r._field == %q and r.agent_id == %q%s)
  |> last()`,
		a.bucket, measurement, field, agentID, extraFilter)
	result, err := a.client.QueryAPI(a.org).Query(ctx, query)
	if err != nil {
		return 0
	}
	defer result.Close()
	for result.Next() {
		if value, ok := toFloat(result.Record().Value()); ok {
			return value
		}
	}
	return 0
}

// attributionScore 返回归因排序使用的分值。
//
// 参数 Parameters:
//   - item (Attribution): 归因记录。
//   - sortField (string): 排序字段。
//
// 返回 Returns:
//   - score (float64): 分值。
func attributionScore(item Attribution, sortField string) float64 {
	if sortField == "memory_rss" {
		return item.MemoryRSSMB
	}
	return item.CPUPercent
}
