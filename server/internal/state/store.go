package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"monitor-server/internal/config"
)

type Agent struct {
	ID                   string    `gorm:"primaryKey;size:128"`
	Hostname             string    `gorm:"size:255"`
	CurrentIP            string    `gorm:"size:64"`
	OS                   string    `gorm:"size:64"`
	Architecture         string    `gorm:"size:32"`
	Environment          string    `gorm:"size:64;index"`
	Site                 string    `gorm:"size:64;index"`
	Role                 string    `gorm:"size:64;index"`
	LabelsJSON           string    `gorm:"type:jsonb"`
	AgentVersion         string    `gorm:"size:64"`
	TelegrafVersion      string    `gorm:"size:64"`
	ConfigVersion        string    `gorm:"size:128"`
	BootID               string    `gorm:"size:128;index"`
	OnlineState          string    `gorm:"size:16;index"`
	TelegrafState        string    `gorm:"size:16;index"`
	LastHeartbeatAt      time.Time `gorm:"index"`
	LastMetricAt         time.Time `gorm:"index"`
	LastTelegrafMetric   time.Time `gorm:"index"`
	LastSequence         uint64
	QueueBytes           int64
	DroppedBatches       uint64
	TelegrafRunning      bool
	TelegrafPID          uint32 `gorm:"column:telegraf_pid"`
	AgentStartedAt       time.Time
	AgentMemoryBytes     uint64
	AgentGoroutines      uint64
	QueueCapacityBytes   int64
	QueueUsagePercent    float64
	QueueAlertLevel      string
	OSVersion            string
	SystemTime           time.Time
	MemoryTotalBytes     uint64
	MemoryUsedBytes      uint64
	ProcessCount         uint64
	ListeningPortCount   uint64
	Load1                float64
	SystemUptimeSeconds  uint64
	CPUModel             string  `gorm:"column:cpu_model;size:255"`
	PhysicalCPUCores     uint64  `gorm:"column:physical_cpu_cores"`
	LogicalCPUCores      uint64  `gorm:"column:logical_cpu_cores"`
	HardwareTemperatureC float64 `gorm:"column:hardware_temperature_c"`
	ProcessesJSON        string  `gorm:"column:processes_json;type:jsonb"`
	TemperaturesJSON     string  `gorm:"column:temperatures_json;type:jsonb"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// BeforeCreate keeps jsonb columns valid: Postgres rejects empty-string json.
func (a *Agent) BeforeCreate(*gorm.DB) error {
	a.LabelsJSON = jsonbObject(a.LabelsJSON)
	a.ProcessesJSON = jsonbArray(a.ProcessesJSON)
	a.TemperaturesJSON = jsonbArray(a.TemperaturesJSON)
	return nil
}

func jsonbObject(raw string) string {
	if raw == "" {
		return "{}"
	}
	return raw
}

func jsonbArray(raw string) string {
	if raw == "" {
		return "[]"
	}
	return raw
}

type AgentIPHistory struct {
	ID        uint   `gorm:"primaryKey"`
	AgentID   string `gorm:"index;size:128"`
	IPAddress string `gorm:"size:64;index"`
	FirstSeen time.Time
	LastSeen  time.Time
}

type ServiceProbeState struct {
	ID            uint   `gorm:"primaryKey"`
	AgentID       string `gorm:"index;size:128"`
	TargetID      string `gorm:"index;size:128"`
	Name          string `gorm:"size:255"`
	State         string `gorm:"size:16;index"`
	CheckStage    string `gorm:"size:32"`
	LatencyMS     int64
	LastCheckedAt time.Time `gorm:"index"`
	LastError     string    `gorm:"type:text"`
}

type DockerContainerState struct {
	ID          uint      `gorm:"primaryKey"`
	AgentID     string    `gorm:"index;size:128"`
	ContainerID string    `gorm:"size:128;index"`
	Name        string    `gorm:"size:255"`
	Image       string    `gorm:"size:255"`
	State       string    `gorm:"size:32;index"`
	LastSeenAt  time.Time `gorm:"index"`
}

type AlertRule struct {
	ID                uint   `gorm:"primaryKey"`
	Name              string `gorm:"size:255"`
	Metric            string `gorm:"size:128;index"`
	WarningThreshold  float64
	CriticalThreshold float64
	TriggerCycles     int
	RecoveryCycles    int
	Enabled           bool   `gorm:"index"`
	SelectorJSON      string `gorm:"type:jsonb"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Alert struct {
	ID            uint   `gorm:"primaryKey"`
	RuleID        uint   `gorm:"index"`
	AgentID       string `gorm:"index;size:128"`
	TargetID      string `gorm:"index;size:128"`
	DedupKey      string `gorm:"uniqueIndex;size:384"`
	State         string `gorm:"size:16;index"`
	CurrentValue  float64
	FirstFiredAt  time.Time
	RecoveredAt   *time.Time
	Acknowledged  bool
	SilencedUntil *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type MaintenanceWindow struct {
	ID        uint      `gorm:"primaryKey"`
	AgentID   string    `gorm:"index;size:128"`
	TargetID  string    `gorm:"index;size:128"`
	StartsAt  time.Time `gorm:"index"`
	EndsAt    time.Time `gorm:"index"`
	Reason    string    `gorm:"type:text"`
	CreatedBy string    `gorm:"size:128"`
	CreatedAt time.Time
}

type AuditRecord struct {
	ID          uint      `gorm:"primaryKey"`
	Actor       string    `gorm:"size:128;index"`
	Action      string    `gorm:"size:128;index"`
	Resource    string    `gorm:"size:128;index"`
	ResourceID  string    `gorm:"size:128"`
	DetailsJSON string    `gorm:"type:jsonb"`
	CreatedAt   time.Time `gorm:"index"`
}

type ConfigDocument struct {
	ID           uint   `gorm:"primaryKey"`
	AgentID      string `gorm:"index;size:128"`
	Version      string `gorm:"size:128;index"`
	DocumentJSON string `gorm:"type:jsonb"`
	Signature    string `gorm:"type:text"`
	Active       bool   `gorm:"index"`
	CreatedAt    time.Time
}

type OutboxEvent struct {
	ID          uint      `gorm:"primaryKey"`
	Topic       string    `gorm:"size:64;index"`
	Key         string    `gorm:"size:256;index"`
	PayloadJSON string    `gorm:"type:jsonb"`
	AvailableAt time.Time `gorm:"index"`
	ClaimedAt   *time.Time
	CreatedAt   time.Time `gorm:"index"`
}

type MetricReceipt struct {
	ID        uint   `gorm:"primaryKey"`
	AgentID   string `gorm:"index:idx_metric_receipt,unique;size:128"`
	Sequence  uint64 `gorm:"index:idx_metric_receipt,unique"`
	CreatedAt time.Time
}

type Store struct {
	db *gorm.DB
}

type ProcessSnapshot struct {
	PID         int    `json:"pid"`
	Name        string `json:"name"`
	MemoryBytes uint64 `json:"memoryBytes"`
}

type TemperatureSnapshot struct {
	Name    string  `json:"name"`
	Celsius float64 `json:"celsius"`
}

// slogWriter 把 GORM 的日志桥接到 slog，统一 Collector 的日志出口，
// 使 GORM 的慢查询告警与业务日志共用同一套时间戳格式。
type slogWriter struct{}

// Printf 输出一条 GORM 日志。GORM 在该层只回调已按级别过滤后的消息。
//
// 参数 Parameters:
//   - format (string): 消息模板，由 GORM logger 传入。
//   - args (...any): 模板参数。
func (slogWriter) Printf(format string, args ...any) {
	slog.Warn(strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func Open(cfg config.PostgresConfig) (*Store, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, cfg.SSLMode,
	)
	// GORM 默认把 ErrRecordNotFound 记成错误级日志，而 ClaimMetric 用
	// metric_receipts 查询做幂等探测：每个新 sequence 首次到达必然查不到，
	// 属正常路径，却会每个批次刷一行红色 "record not found"。
	// 这里显式关闭 not-found 日志，其余（慢查询、真实错误）照常输出。
	gormLogger := logger.New(slogWriter{}, logger.Config{
		SlowThreshold:             500 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
	})
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormLogger})
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	// Older Collector builds let GORM derive TelegrafPID as telegraf_p_id.
	// Normalize that legacy column before AutoMigrate and heartbeat writes.
	if err := db.Exec(`
DO $$
BEGIN
    IF to_regclass('public.agents') IS NOT NULL
       AND EXISTS (
           SELECT 1 FROM information_schema.columns
           WHERE table_schema = 'public'
             AND table_name = 'agents'
             AND column_name = 'telegraf_p_id'
       )
       AND NOT EXISTS (
           SELECT 1 FROM information_schema.columns
           WHERE table_schema = 'public'
             AND table_name = 'agents'
             AND column_name = 'telegraf_pid'
       ) THEN
        ALTER TABLE public.agents RENAME COLUMN telegraf_p_id TO telegraf_pid;
    END IF;
END $$;
`).Error; err != nil {
		return nil, fmt.Errorf("normalize PostgreSQL Agent schema: %w", err)
	}
	if err := db.AutoMigrate(
		&Agent{}, &AgentIPHistory{}, &ServiceProbeState{}, &DockerContainerState{},
		&AlertRule{}, &Alert{}, &MaintenanceWindow{}, &AuditRecord{},
		&ConfigDocument{}, &OutboxEvent{}, &MetricReceipt{},
	); err != nil {
		return nil, fmt.Errorf("migrate PostgreSQL schema: %w", err)
	}
	return &Store{db: db}, nil
}

type AgentSnapshot struct {
	AgentID         string
	Hostname        string
	CurrentIP       string
	OS              string
	Architecture    string
	Environment     string
	Site            string
	Role            string
	Labels          map[string]string
	AgentVersion    string
	TelegrafVersion string
	ConfigVersion   string
	BootID          string
	OnlineState     string
	TelegrafState   string
	QueueBytes      int64
	DroppedBatches  uint64
}

func (s *Store) UpsertAgent(snapshot AgentSnapshot, at time.Time) error {
	labels, err := json.Marshal(snapshot.Labels)
	if err != nil {
		return err
	}
	if snapshot.Labels == nil {
		labels = []byte("{}")
	}
	fields := map[string]any{
		"hostname": snapshot.Hostname, "current_ip": snapshot.CurrentIP,
		"os": snapshot.OS, "architecture": snapshot.Architecture,
		"environment": snapshot.Environment, "site": snapshot.Site, "role": snapshot.Role,
		"labels_json":   string(labels),
		"agent_version": snapshot.AgentVersion, "telegraf_version": snapshot.TelegrafVersion,
		"config_version": snapshot.ConfigVersion, "boot_id": snapshot.BootID,
		"online_state": snapshot.OnlineState, "telegraf_state": snapshot.TelegrafState,
		"last_heartbeat_at": at,
	}
	agent := Agent{ID: snapshot.AgentID, LabelsJSON: "{}", ProcessesJSON: "[]", TemperaturesJSON: "[]"}
	return s.db.Where(Agent{ID: snapshot.AgentID}).Assign(fields).FirstOrCreate(&agent).Error
}

func (s *Store) UpdateHeartbeat(agentID, bootID string, sequence uint64, queueBytes, dropped uint64, at time.Time) error {
	return s.db.Model(&Agent{}).Where("id = ?", agentID).Updates(map[string]any{
		"boot_id": bootID, "last_heartbeat_at": at, "last_sequence": sequence,
		"queue_bytes": queueBytes, "dropped_batches": dropped, "online_state": "online",
	}).Error
}

func (s *Store) UpdateHeartbeatDetails(agentID, bootID string, sequence uint64, queueBytes, dropped uint64,
	telegrafRunning bool, telegrafPID uint32, agentStartedAt time.Time, agentMemoryBytes, agentGoroutines,
	queueCapacityBytes uint64, queueUsagePercent float64, queueAlertLevel, osVersion string,
	systemTime time.Time, memoryTotalBytes, memoryUsedBytes, processCount, listeningPortCount uint64,
	load1 float64, systemUptimeSeconds uint64, cpuModel string, physicalCPUCores, logicalCPUCores uint64,
	hardwareTemperatureC float64, processes []ProcessSnapshot, temperatures []TemperatureSnapshot, at time.Time) error {
	processesJSON, err := json.Marshal(processes)
	if err != nil {
		return err
	}
	temperaturesJSON, err := json.Marshal(temperatures)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"boot_id": bootID, "last_heartbeat_at": at, "last_sequence": sequence,
		"queue_bytes": queueBytes, "dropped_batches": dropped, "online_state": "online",
		"telegraf_running": telegrafRunning, "telegraf_pid": telegrafPID,
		"agent_started_at": agentStartedAt, "agent_memory_bytes": agentMemoryBytes,
		"agent_goroutines": agentGoroutines, "queue_capacity_bytes": queueCapacityBytes,
		"queue_usage_percent": queueUsagePercent, "queue_alert_level": queueAlertLevel,
		"os_version": osVersion, "system_time": systemTime,
		"memory_total_bytes": memoryTotalBytes, "memory_used_bytes": memoryUsedBytes,
		"process_count": processCount, "listening_port_count": listeningPortCount,
		"load1": load1, "system_uptime_seconds": systemUptimeSeconds,
		"cpu_model": cpuModel, "physical_cpu_cores": physicalCPUCores,
		"logical_cpu_cores": logicalCPUCores, "hardware_temperature_c": hardwareTemperatureC,
	}
	if len(processes) > 0 {
		updates["processes_json"] = string(processesJSON)
	}
	if len(temperatures) > 0 {
		updates["temperatures_json"] = string(temperaturesJSON)
	}
	return s.db.Model(&Agent{}).Where("id = ?", agentID).Updates(updates).Error
}

func (s *Store) UpdateTelegrafState(agentID, state, version string, lastMetric time.Time) error {
	return s.db.Model(&Agent{}).Where("id = ?", agentID).Updates(map[string]any{
		"telegraf_state": state, "telegraf_version": version, "last_telegraf_metric": lastMetric,
	}).Error
}

func (s *Store) Publish(topic, key string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.db.Create(&OutboxEvent{
		Topic: topic, Key: key, PayloadJSON: string(data), AvailableAt: time.Now().UTC(),
	}).Error
}

func (s *Store) ClaimEvents(topic string, limit int) ([]OutboxEvent, error) {
	var events []OutboxEvent
	err := s.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Where("topic = ? AND claimed_at IS NULL AND available_at <= ?", topic, now).
			Order("id").Limit(limit).Find(&events).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		return tx.Model(&OutboxEvent{}).Where("id IN ?", eventIDs(events)).
			Update("claimed_at", now).Error
	})
	return events, err
}

func eventIDs(events []OutboxEvent) []uint {
	ids := make([]uint, len(events))
	for i := range events {
		ids[i] = events[i].ID
	}
	return ids
}

func (s *Store) AckEvents(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Where("id IN ?", ids).Delete(&OutboxEvent{}).Error
}

func (s *Store) ReleaseEvents(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Model(&OutboxEvent{}).Where("id IN ?", ids).
		Updates(map[string]any{"claimed_at": nil, "available_at": time.Now().UTC().Add(5 * time.Second)}).Error
}

func (s *Store) EvaluateOnlineStates(now time.Time) error {
	var agents []Agent
	if err := s.db.Find(&agents).Error; err != nil {
		return err
	}
	for _, agent := range agents {
		age := now.Sub(agent.LastHeartbeatAt)
		state := "online"
		if age >= 15*time.Second {
			state = "offline"
		} else if age >= 10*time.Second {
			state = "suspect"
		}
		if agent.OnlineState != state {
			if err := s.db.Model(&Agent{}).Where("id = ?", agent.ID).
				Update("online_state", state).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) ListAgents() ([]Agent, error) {
	var agents []Agent
	return agents, s.db.Order("id").Find(&agents).Error
}

func (s *Store) GetAgent(agentID string) (Agent, error) {
	var agent Agent
	return agent, s.db.Where("id = ?", agentID).First(&agent).Error
}

func (s *Store) DeleteAgent(agentID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&Alert{}).Error; err != nil {
			return err
		}
		if err := tx.Where("agent_id = ?", agentID).Delete(&ServiceProbeState{}).Error; err != nil {
			return err
		}
		if err := tx.Where("agent_id = ?", agentID).Delete(&DockerContainerState{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", agentID).Delete(&Agent{}).Error
	})
}

func (s *Store) ListAlerts() ([]Alert, error) {
	var alerts []Alert
	return alerts, s.db.Order("updated_at DESC").Limit(500).Find(&alerts).Error
}

func (s *Store) ActiveConfig(agentID string) (ConfigDocument, error) {
	var document ConfigDocument
	return document, s.db.Where("agent_id = ? AND active = ?", agentID, true).
		Order("created_at DESC").First(&document).Error
}

func (s *Store) CreateAlertRule(rule *AlertRule) error {
	return s.db.Create(rule).Error
}

func (s *Store) ListAlertRules() ([]AlertRule, error) {
	var rules []AlertRule
	return rules, s.db.Order("id").Find(&rules).Error
}

func (s *Store) CreateMaintenance(window *MaintenanceWindow) error {
	return s.db.Create(window).Error
}

func (s *Store) ListMaintenance(now time.Time) ([]MaintenanceWindow, error) {
	var windows []MaintenanceWindow
	return windows, s.db.Where("ends_at >= ?", now).Order("starts_at").Find(&windows).Error
}

func (s *Store) ListServiceProbes(agentID string) ([]ServiceProbeState, error) {
	var probes []ServiceProbeState
	return probes, s.db.Where("agent_id = ?", agentID).Order("name").Find(&probes).Error
}

func (s *Store) ListContainers(agentID string) ([]DockerContainerState, error) {
	var containers []DockerContainerState
	return containers, s.db.Where("agent_id = ?", agentID).Order("name").Find(&containers).Error
}

func (s *Store) AddAudit(record *AuditRecord) error {
	return s.db.Create(record).Error
}

func (s *Store) UpsertOfflineAlert(agentID string, now time.Time) (bool, error) {
	const dedupKeyPrefix = "host_offline:"
	var alert Alert
	err := s.db.Where("dedup_key = ?", dedupKeyPrefix+agentID).First(&alert).Error
	if err == gorm.ErrRecordNotFound {
		alert = Alert{
			RuleID: 0, AgentID: agentID, TargetID: agentID,
			DedupKey: dedupKeyPrefix + agentID, State: "critical",
			FirstFiredAt: now, CurrentValue: 1,
		}
		return true, s.db.Create(&alert).Error
	}
	if err != nil {
		return false, err
	}
	if alert.State == "critical" {
		return false, nil
	}
	return true, s.db.Model(&alert).Updates(map[string]any{
		"state": "critical", "recovered_at": nil, "updated_at": now,
	}).Error
}

func (s *Store) Register(agentID, hostname string, at time.Time) error {
	agent := Agent{ID: agentID, LabelsJSON: "{}", ProcessesJSON: "[]", TemperaturesJSON: "[]"}
	return s.db.Where(Agent{ID: agentID}).Assign(map[string]any{
		"hostname": hostname, "last_heartbeat_at": at,
	}).FirstOrCreate(&agent).Error
}

func (s *Store) RecordHeartbeat(agentID string, sequence uint64, at time.Time) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		agent := Agent{ID: agentID}
		if err := tx.FirstOrCreate(&agent, Agent{ID: agentID}).Error; err != nil {
			return err
		}
		return tx.Model(&Agent{}).Where("id = ?", agentID).Updates(map[string]any{
			"last_heartbeat_at": at,
			"last_sequence":     sequence,
		}).Error
	})
}

// ClaimMetric makes agent_id + sequence idempotent for the local Collector.
func (s *Store) ClaimMetric(agentID string, sequence uint64, at time.Time) (bool, error) {
	var existing MetricReceipt
	err := s.db.Where("agent_id = ? AND sequence = ?", agentID, sequence).Take(&existing).Error
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	receipt := MetricReceipt{AgentID: agentID, Sequence: sequence}
	if err := s.db.Create(&receipt).Error; err != nil {
		if isUniqueViolation(err) {
			// Stale SERIAL on id (metric_receipts_pkey) rejects new sequences.
			if strings.Contains(err.Error(), "metric_receipts_pkey") {
				if err := s.resyncMetricReceiptSerial(); err != nil {
					return false, err
				}
				if err := s.db.Create(&receipt).Error; err != nil {
					if isUniqueViolation(err) {
						return false, nil
					}
					return false, err
				}
			} else {
				return false, nil
			}
		} else {
			return false, err
		}
	}
	if err := s.db.FirstOrCreate(&Agent{ID: agentID, LabelsJSON: "{}", ProcessesJSON: "[]", TemperaturesJSON: "[]"}, Agent{ID: agentID}).Error; err != nil {
		return false, err
	}
	if err := s.db.Model(&Agent{}).Where("id = ?", agentID).Updates(map[string]any{
		"last_metric_at":    at,
		"last_heartbeat_at": at,
		"last_sequence":     sequence,
		"online_state":      "online",
	}).Error; err != nil {
		return false, err
	}
	return true, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}

func (s *Store) resyncMetricReceiptSerial() error {
	return s.db.Exec(`
SELECT setval(
  pg_get_serial_sequence('metric_receipts', 'id'),
  GREATEST(COALESCE((SELECT MAX(id) FROM metric_receipts), 1), 1)
)`).Error
}

func (s *Store) ReleaseMetric(agentID string, sequence uint64) error {
	return s.db.Where("agent_id = ? AND sequence = ?", agentID, sequence).Delete(&MetricReceipt{}).Error
}
