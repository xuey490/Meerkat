// Package monitor 封装监控数据的只读查询：PostgreSQL 中的 Agent 资产、告警、
// 服务探测与容器状态，以及 InfluxDB 中的指标曲线。
//
// 本包不改动监控库结构，也不写入监控库；所有查询语义与迁移前的 implementation 保持一致，
// 以保证「服务器列表 / 服务器详情 / 告警中心」等功能完整可用。
package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"gorm.io/gorm"
)

// Agent 对应 PostgreSQL 中的 agents 表（监控资产与最新状态）。
type Agent struct {
	// ID Agent 唯一标识。
	ID string `gorm:"column:id" json:"id"`
	// Hostname 主机名。
	Hostname string `json:"hostname"`
	// CurrentIP 当前 IP。
	CurrentIP string `json:"currentIp"`
	// OS 操作系统。
	OS string `json:"os"`
	// Architecture 架构。
	Architecture string `json:"architecture"`
	// Environment 环境标识。
	Environment string `json:"environment"`
	// Site 站点。
	Site string `json:"site"`
	// Role 角色。
	Role string `json:"role"`
	// AgentVersion Agent 版本。
	AgentVersion string `json:"agentVersion"`
	// TelegrafVersion Telegraf 版本。
	TelegrafVersion string `json:"telegrafVersion"`
	// BootID 启动标识。
	BootID string `json:"bootId"`
	// OnlineState 在线状态：online / suspect / offline。
	OnlineState string `json:"onlineState"`
	// TelegrafState Telegraf 状态。
	TelegrafState string `json:"telegrafState"`
	// LastHeartbeatAt 最后心跳时间。
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt"`
	// LastMetricAt 最后指标时间。
	LastMetricAt time.Time `json:"lastMetricAt"`
	// LastTelegrafMetric 最后 Telegraf 指标时间。
	LastTelegrafMetric time.Time `json:"lastTelegrafMetric"`
	// QueueBytes 队列字节数。
	QueueBytes int64 `json:"queueBytes"`
	// QueueCapacityBytes 队列容量。
	QueueCapacityBytes int64 `json:"queueCapacityBytes"`
	// QueueUsagePercent 队列使用率。
	QueueUsagePercent float64 `json:"queueUsagePercent"`
	// QueueAlertLevel 队列告警级别。
	QueueAlertLevel string `json:"queueAlertLevel"`
	// MemoryTotalBytes 内存总量。
	MemoryTotalBytes uint64 `json:"memoryTotalBytes"`
	// MemoryUsedBytes 内存使用量。
	MemoryUsedBytes uint64 `json:"memoryUsedBytes"`
	// ProcessCount 进程数。
	ProcessCount uint64 `json:"processCount"`
	// ListeningPortCount 监听端口数。
	ListeningPortCount uint64 `json:"listeningPortCount"`
	// Load1 1 分钟负载。
	Load1 float64 `json:"load1"`
	// SystemUptimeSeconds 系统运行时长。
	SystemUptimeSeconds uint64 `json:"systemUptimeSeconds"`
	// OSVersion 系统版本。
	OSVersion string `json:"osVersion"`
	// SystemTime 系统时间。
	SystemTime time.Time `json:"systemTime"`
	// CPUModel CPU 型号。
	CPUModel string `gorm:"column:cpu_model" json:"cpuModel"`
	// PhysicalCPUCores 物理核心数。
	PhysicalCPUCores uint64 `gorm:"column:physical_cpu_cores" json:"physicalCpuCores"`
	// LogicalCPUCores 逻辑核心数。
	LogicalCPUCores uint64 `gorm:"column:logical_cpu_cores" json:"logicalCpuCores"`
	// HardwareTemperatureC 硬件温度。
	HardwareTemperatureC float64 `gorm:"column:hardware_temperature_c" json:"hardwareTemperatureC"`
	// ProcessesJSON 进程快照 JSON。
	ProcessesJSON string `gorm:"column:processes_json" json:"-"`
	// TemperaturesJSON 温度快照 JSON。
	TemperaturesJSON string `gorm:"column:temperatures_json" json:"-"`
}

// TableName 返回 agents 表名。
func (Agent) TableName() string { return "agents" }

// Process 是进程快照条目。
type Process struct {
	// PID 进程号。
	PID int `json:"pid"`
	// Name 进程名。
	Name string `json:"name"`
	// MemoryBytes 内存占用。
	MemoryBytes uint64 `json:"memoryBytes"`
}

// Temperature 是温度快照条目。
type Temperature struct {
	// Name 传感器名称。
	Name string `json:"name"`
	// Celsius 摄氏度。
	Celsius float64 `json:"celsius"`
}

// Alert 对应 alerts 表。
type Alert struct {
	// ID 告警主键。
	ID uint `json:"id"`
	// AgentID 所属 Agent。
	AgentID string `json:"agentId"`
	// TargetID 告警目标。
	TargetID string `json:"targetId"`
	// DedupKey 去重键。
	DedupKey string `json:"dedupKey"`
	// State 状态。
	State string `json:"state"`
	// CurrentValue 当前值。
	CurrentValue float64 `json:"currentValue"`
	// FirstFiredAt 首次触发时间。
	FirstFiredAt time.Time `json:"firstFiredAt"`
	// RecoveredAt 恢复时间。
	RecoveredAt *time.Time `json:"recoveredAt"`
	// Acknowledged 是否已确认。
	Acknowledged bool `json:"acknowledged"`
	// SilencedUntil 静默截止时间。
	SilencedUntil *time.Time `json:"silencedUntil"`
	// UpdatedAt 更新时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName 返回 alerts 表名。
func (Alert) TableName() string { return "alerts" }

// ServiceProbe 对应 service_probe_states 表。
type ServiceProbe struct {
	// ID 主键。
	ID uint `json:"id"`
	// AgentID 所属 Agent。
	AgentID string `json:"agentId"`
	// TargetID 探测目标。
	TargetID string `json:"targetId"`
	// Name 名称。
	Name string `json:"name"`
	// State 状态。
	State string `json:"state"`
	// CheckStage 检查阶段。
	CheckStage string `json:"checkStage"`
	// LatencyMS 延迟（毫秒）。
	LatencyMS int64 `json:"latencyMs"`
	// LastCheckedAt 最后检查时间。
	LastCheckedAt time.Time `json:"lastCheckedAt"`
	// LastError 最后错误。
	LastError string `json:"lastError"`
}

// TableName 返回 service_probe_states 表名。
func (ServiceProbe) TableName() string { return "service_probe_states" }

// Container 对应 docker_container_states 表。
type Container struct {
	// ID 主键。
	ID uint `json:"id"`
	// AgentID 所属 Agent。
	AgentID string `json:"agentId"`
	// ContainerID 容器 ID。
	ContainerID string `json:"containerId"`
	// Name 容器名。
	Name string `json:"name"`
	// Image 镜像。
	Image string `json:"image"`
	// State 状态。
	State string `json:"state"`
	// LastSeenAt 最后上报时间。
	LastSeenAt time.Time `json:"lastSeenAt"`
}

// TableName 返回 docker_container_states 表名。
func (Container) TableName() string { return "docker_container_states" }

// MaintenanceWindow 是维护窗口记录（只读）。
type MaintenanceWindow struct {
	// ID 主键。
	ID uint `json:"id"`
	// AgentID 所属 Agent。
	AgentID string `json:"agentId"`
	// TargetID 目标。
	TargetID string `json:"targetId"`
	// StartsAt 开始时间。
	StartsAt time.Time `json:"startsAt"`
	// EndsAt 结束时间。
	EndsAt time.Time `json:"endsAt"`
	// Reason 原因。
	Reason string `json:"reason"`
	// CreatedBy 创建人。
	CreatedBy string `json:"createdBy"`
}

// MetricPoint 是一条时序数据点。
type MetricPoint struct {
	// Time 时间戳。
	Time time.Time `json:"time"`
	// Field 字段名。
	Field string `json:"field"`
	// Value 字段值。
	Value any `json:"value"`
	// Tags 全部标签。
	Tags map[string]any `json:"tags"`
}

// Page 是监控列表的分页返回结构（与前端 PageResult 对齐）。
type Page[T any] struct {
	// List 当前页数据。
	List []T `json:"list"`
	// Total 记录总数。
	Total int64 `json:"total"`
}

// OverviewResult 是监控总览的聚合结果。
type OverviewResult struct {
	// Total Agent 总数。
	Total int `json:"total"`
	// Online 在线数。
	Online int `json:"online"`
	// Suspect 疑似异常数。
	Suspect int `json:"suspect"`
	// Offline 离线数。
	Offline int `json:"offline"`
	// TelegrafStale Telegraf 数据陈旧数。
	TelegrafStale int `json:"telegrafStale"`
	// TelegrafDown Telegraf 异常数。
	TelegrafDown int `json:"telegrafDown"`
	// ActiveAlerts 活跃告警数。
	ActiveAlerts int64 `json:"activeAlerts"`
}

// 指标白名单：与前端可选的测量项一致，避免任意 measurement 注入 Flux 查询。
var allowedMeasurements = map[string]bool{
	"cpu": true, "mem": true, "disk": true, "diskio": true, "net": true, "system": true,
	"processes": true, "mysql": true, "redis": true, "postgresql": true,
	"rabbitmq": true, "http_response": true, "docker": true,
	"win_cpu": true, "win_memory": true, "win_system": true,
}

// Service 提供监控数据查询。
type Service struct {
	// db 监控库（PostgreSQL）会话。
	db *gorm.DB
	// influx InfluxDB 客户端。
	influx influxdb2.Client
	// influxOrg InfluxDB 组织名。
	influxOrg string
	// influxBucket InfluxDB 桶名。
	influxBucket string
}

// New 创建监控查询服务。
//
// 参数 Parameters:
//   - db (*gorm.DB): 监控库会话。
//   - influx (influxdb2.Client): InfluxDB 客户端，可为 nil。
//   - organization (string): InfluxDB 组织名。
//   - bucket (string): InfluxDB 桶名。
//
// 返回 Returns:
//   - service (*Service): 监控查询服务。
func New(db *gorm.DB, influx influxdb2.Client, organization, bucket string) *Service {
	return &Service{db: db, influx: influx, influxOrg: organization, influxBucket: bucket}
}

// Overview 汇总 Agent 在线状态与活跃告警数量。
//
// 返回 Returns:
//   - result (OverviewResult): 总览数据。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) Overview() (OverviewResult, error) {
	var agents []Agent
	if err := s.db.Find(&agents).Error; err != nil {
		return OverviewResult{}, err
	}
	result := OverviewResult{Total: len(agents)}
	for _, agent := range agents {
		switch agent.OnlineState {
		case "online":
			result.Online++
		case "suspect":
			result.Suspect++
		case "offline":
			result.Offline++
		}
		switch agent.TelegrafState {
		case "stale":
			result.TelegrafStale++
		case "down", "unhealthy":
			result.TelegrafDown++
		}
	}
	var activeAlerts int64
	_ = s.db.Model(&Alert{}).Where("state IN ?", []string{"warning", "critical", "offline"}).Count(&activeAlerts).Error
	result.ActiveAlerts = activeAlerts
	return result, nil
}

// Agents 分页查询服务器列表。
//
// 参数 Parameters:
//   - page (int): 页码。
//   - size (int): 每页条数。
//   - keywords (string): 关键字（主机名 / ID / IP）。
//   - state (string): 在线状态过滤。
//
// 返回 Returns:
//   - result (Page[Agent]): 分页结果。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) Agents(page, size int, keywords, state string) (Page[Agent], error) {
	db := s.db.Model(&Agent{})
	if strings.TrimSpace(keywords) != "" {
		like := "%" + strings.TrimSpace(keywords) + "%"
		db = db.Where("(hostname LIKE ? OR id LIKE ? OR current_ip LIKE ?)", like, like, like)
	}
	if state != "" {
		db = db.Where("online_state = ?", state)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[Agent]{}, err
	}
	var agents []Agent
	if err := db.Order("id").Offset((page - 1) * size).Limit(size).Find(&agents).Error; err != nil {
		return Page[Agent]{}, err
	}
	return Page[Agent]{List: agents, Total: total}, nil
}

// AgentDetail 返回服务器详情（含服务探测、容器、进程与温度快照）。
//
// 参数 Parameters:
//   - agentID (string): Agent ID。
//
// 返回 Returns:
//   - agent (Agent): Agent 实体。
//   - services ([]ServiceProbe): 服务探测状态。
//   - containers ([]Container): 容器状态。
//   - processes ([]Process): 进程快照。
//   - temperatures ([]Temperature): 温度快照。
//   - err (error): 查询失败时返回非 nil（不存在时返回 gorm.ErrRecordNotFound）。
func (s *Service) AgentDetail(agentID string) (Agent, []ServiceProbe, []Container, []Process, []Temperature, error) {
	var agent Agent
	if err := s.db.First(&agent, "id = ?", agentID).Error; err != nil {
		return Agent{}, nil, nil, nil, nil, err
	}
	var services []ServiceProbe
	var containers []Container
	processes := make([]Process, 0)
	temperatures := make([]Temperature, 0)
	_ = json.Unmarshal([]byte(agent.ProcessesJSON), &processes)
	_ = json.Unmarshal([]byte(agent.TemperaturesJSON), &temperatures)
	_ = s.db.Where("agent_id = ?", agent.ID).Order("name").Find(&services).Error
	_ = s.db.Where("agent_id = ?", agent.ID).Order("name").Find(&containers).Error
	return agent, services, containers, processes, temperatures, nil
}

// Services 返回指定 Agent 的服务探测状态。
//
// 参数 Parameters:
//   - agentID (string): Agent ID。
//
// 返回 Returns:
//   - rows ([]ServiceProbe): 服务探测状态。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) Services(agentID string) ([]ServiceProbe, error) {
	var rows []ServiceProbe
	if err := s.db.Where("agent_id = ?", agentID).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Containers 返回指定 Agent 的容器状态。
//
// 参数 Parameters:
//   - agentID (string): Agent ID。
//
// 返回 Returns:
//   - rows ([]Container): 容器状态。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) Containers(agentID string) ([]Container, error) {
	var rows []Container
	if err := s.db.Where("agent_id = ?", agentID).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Alerts 分页查询告警。
//
// 参数 Parameters:
//   - page (int): 页码。
//   - size (int): 每页条数。
//   - state (string): 状态过滤。
//   - agentID (string): Agent 过滤。
//
// 返回 Returns:
//   - result (Page[Alert]): 分页结果。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) Alerts(page, size int, state, agentID string) (Page[Alert], error) {
	db := s.db.Model(&Alert{})
	if state != "" {
		db = db.Where("state = ?", state)
	}
	if agentID != "" {
		db = db.Where("agent_id = ?", agentID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[Alert]{}, err
	}
	var rows []Alert
	if err := db.Order("updated_at desc").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return Page[Alert]{}, err
	}
	return Page[Alert]{List: rows, Total: total}, nil
}

// Maintenance 返回未结束的维护窗口。
//
// 返回 Returns:
//   - rows ([]MaintenanceWindow): 维护窗口列表。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) Maintenance() ([]MaintenanceWindow, error) {
	var rows []MaintenanceWindow
	err := s.db.Table("maintenance_windows").
		Where("ends_at >= ?", time.Now().UTC()).
		Order("starts_at").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Metrics 查询 InfluxDB 中的指标曲线。
//
// 参数 Parameters:
//   - measurement (string): 指标类型，必须在白名单内。
//   - agentID (string): Agent ID。
//   - start (string): Flux range start，如 -1h。
//
// 返回 Returns:
//   - points ([]MetricPoint): 数据点。
//   - err (error): 参数非法时返回 ErrInvalidMeasurement / ErrInvalidAgent / ErrInvalidRange。
func (s *Service) Metrics(measurement, agentID, start string) ([]MetricPoint, error) {
	if !allowedMeasurements[measurement] {
		return nil, ErrInvalidMeasurement
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || agentID == "undefined" || agentID == "null" {
		return nil, ErrInvalidAgent
	}
	if len(start) > 32 {
		return nil, ErrInvalidRange
	}
	if s.influx == nil {
		return nil, ErrInfluxUnavailable
	}
	safeAgentID := strings.ReplaceAll(agentID, `"`, `\"`)
	query := fmt.Sprintf(
		`from(bucket: %q) |> range(start: %s) |> filter(fn: (r) => r._measurement == %q and r.agent_id == %q) |> limit(n: 5000)`,
		s.influxBucket, start, measurement, safeAgentID)
	result, err := s.influx.QueryAPI(s.influxOrg).Query(context.Background(), query)
	if err != nil {
		slog.Error("influx 查询失败", "org", s.influxOrg, "bucket", s.influxBucket, "measurement", measurement, "error", err)
		return nil, ErrInfluxUnavailable
	}
	defer result.Close()
	points := make([]MetricPoint, 0)
	for result.Next() {
		record := result.Record()
		points = append(points, MetricPoint{
			Time:  record.Time(),
			Field: record.Field(),
			Value: record.Value(),
			Tags:  record.Values(),
		})
	}
	if result.Err() != nil {
		slog.Error("influx 读取失败", "org", s.influxOrg, "bucket", s.influxBucket, "measurement", measurement, "error", result.Err())
		return nil, ErrInfluxUnavailable
	}
	return points, nil
}
