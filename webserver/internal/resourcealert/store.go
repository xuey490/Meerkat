// Package resourcealert 实现主机资源（CPU / 内存 / 系统负载 / 磁盘）持续超阈值告警。
//
// 职责：
//   - 规则按「全局 → 角色 → 单机」三层存储在监控库，可在前端配置；
//   - 评估时查询 InfluxDB 时序窗口，判定「窗口内超标点占比」，而非瞬时值；
//   - 输出带进程归因的告警：写入本地轮转日志、告警表，并推送到告警中心。
//
// 落点说明：本能力放在 webserver 而不是 collector，原因是「持续超标」必须做时序窗口
// 查询，而 collector 端只写不查 InfluxDB，且没有文件日志设施。
package resourcealert

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// 作用域常量：规则生效层级。
const (
	// ScopeGlobal 全局默认规则（target 为空）。
	ScopeGlobal = "global"
	// ScopeRole 角色级规则（target 为 Agent 上报的 role 标签）。
	ScopeRole = "role"
	// ScopeAgent 单机覆盖规则（target 为 agent_id）。
	ScopeAgent = "agent"
)

// AlertRule 对应 alert_rules 表。
//
// 该表由 collector 端创建，本服务复用同一张表以保持「一份规则」的语义：
// 仅新增下列扩展列（metric_key / scope / target / for_seconds / breach_ratio /
// remind_interval_seconds / cooldown_seconds / params_json），不改动也不删除
// collector 端已有列。GORM AutoMigrate 只加列，因此两个服务各自的模型可共存。
type AlertRule struct {
	// ID 主键。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Name 规则名称（兼容 collector 端字段）。
	Name string `gorm:"column:name;size:255" json:"name"`
	// Metric 指标标识（兼容 collector 端字段，写入时与 MetricKey 保持一致）。
	Metric string `gorm:"column:metric;size:128;index" json:"metric"`
	// MetricKey 指标标识：cpu_usage / mem_usage / load_per_core / disk_usage。
	MetricKey string `gorm:"column:metric_key;size:64;index" json:"metricKey"`
	// Scope 作用域：global / role / agent。
	Scope string `gorm:"column:scope;size:16;index" json:"scope"`
	// Target 作用域目标：role 为角色名、agent 为 agent_id、global 为空。
	Target string `gorm:"column:target;size:128;index" json:"target"`
	// WarningThreshold 警告阈值。
	WarningThreshold float64 `gorm:"column:warning_threshold" json:"warningThreshold"`
	// CriticalThreshold 严重阈值。
	CriticalThreshold float64 `gorm:"column:critical_threshold" json:"criticalThreshold"`
	// ForSeconds 判定期（秒）：窗口内需要持续超标的时间长度。
	ForSeconds int64 `gorm:"column:for_seconds" json:"forSeconds"`
	// BreachRatio 窗口内超标采样点占比下限，取值 0~1。
	BreachRatio float64 `gorm:"column:breach_ratio" json:"breachRatio"`
	// RemindIntervalSeconds 警告级（warning）的同级重申间隔（秒），0 表示不重申。
	RemindIntervalSeconds int64 `gorm:"column:remind_interval_seconds" json:"remindIntervalSeconds"`
	// RemindIntervalCriticalSeconds 严重级（critical）的同级重申间隔（秒），0 表示不重申。
	//
	// 严重告警需要更快把人叫醒，因此与警告级分开配置（默认更短）。
	// default:0 保证新库建列时存量行被填 0（而非 NULL）；老库由 backfillNewColumns 补齐。
	RemindIntervalCriticalSeconds int64 `gorm:"column:remind_interval_critical_seconds;default:0" json:"remindIntervalCriticalSeconds"`
	// CooldownSeconds 恢复后冷却时长（秒），抑制临界值反复触发。
	CooldownSeconds int64 `gorm:"column:cooldown_seconds" json:"cooldownSeconds"`
	// ParamsJSON 扩展参数的 JSON 文本（忽略的文件系统、指定挂载点、归因 TopN 等）。
	ParamsJSON string `gorm:"column:params_json;type:text" json:"paramsJson"`
	// TriggerCycles collector 端预留字段；本模块以 ForSeconds 表达持续时长，保持闲置以兼容。
	TriggerCycles int `gorm:"column:trigger_cycles" json:"triggerCycles"`
	// RecoveryCycles collector 端预留字段；同上，保持闲置。
	RecoveryCycles int `gorm:"column:recovery_cycles" json:"recoveryCycles"`
	// Enabled 是否启用。
	Enabled bool `gorm:"column:enabled;index" json:"enabled"`
	// SelectorJSON 选择器（collector 端字段，jsonb；写入前由 BeforeSave 保证为合法 JSON）。
	SelectorJSON string `gorm:"column:selector_json;type:jsonb" json:"selectorJson"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
	// UpdatedAt 更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 返回 alert_rules 表名。
func (AlertRule) TableName() string { return "alert_rules" }

// BeforeSave 保证 jsonb 列始终是合法 JSON——Postgres 会拒绝把空字符串写进 jsonb 列。
func (r *AlertRule) BeforeSave(*gorm.DB) error {
	if strings.TrimSpace(r.SelectorJSON) == "" {
		r.SelectorJSON = "{}"
	}
	return nil
}

// Alert 对应 alerts 表（与 collector 端同一张表）。
//
// webserver 侧新增四个扩展列：metric_key / level / details_json / last_seen_at，
// 用于承载资源告警的指标标识、级别、归因详情快照与「最近仍处于该状态」的时间。
type Alert struct {
	// ID 主键。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// RuleID 命中的规则 ID。
	RuleID uint `gorm:"column:rule_id;index" json:"ruleId"`
	// AgentID 主机标识。
	AgentID string `gorm:"column:agent_id;index;size:128" json:"agentId"`
	// TargetID 告警目标：磁盘告警为挂载点，其余为空。
	TargetID string `gorm:"column:target_id;index;size:128" json:"targetId"`
	// DedupKey 去重键，形如 cpu_usage:agent:<id>、disk_usage:agent:<id>:<mount>。
	//
	// 不能做成唯一索引：一条告警是一次「事件」，同一去重键在历史上会有多条记录
	//（确认结束或自动恢复之后再触发，就是全新的一条）。
	//「同一告警同时只能有一条未结束」由部分唯一索引 uk_alerts_active 在数据库层保证，
	// 见 ensureActiveAlertIndex。
	DedupKey string `gorm:"column:dedup_key;index;size:384" json:"dedupKey"`
	// State 状态：warning / critical / ok。
	State string `gorm:"column:state;size:16;index" json:"state"`
	// CurrentValue 触发时的指标值。
	CurrentValue float64 `gorm:"column:current_value" json:"currentValue"`
	// FirstFiredAt 首次触发时间（同一去重键持续期间保持不变）。
	FirstFiredAt time.Time `gorm:"column:first_fired_at" json:"firstFiredAt"`
	// RecoveredAt 恢复时间（未恢复为空）。
	RecoveredAt *time.Time `gorm:"column:recovered_at" json:"recoveredAt"`
	// Acknowledged 是否已人工确认。
	Acknowledged bool `gorm:"column:acknowledged" json:"acknowledged"`
	// SilencedUntil 静默截止时间（为空表示未静默）。
	SilencedUntil *time.Time `gorm:"column:silenced_until" json:"silencedUntil"`
	// MetricKey 指标标识（扩展列）。
	MetricKey string `gorm:"column:metric_key;size:64;index" json:"metricKey"`
	// Level 告警级别：warning / critical（扩展列）。
	Level string `gorm:"column:level;size:16" json:"level"`
	// DetailsJSON 归因与系统上下文的 JSON 快照（扩展列），供告警中心详情展示。
	DetailsJSON string `gorm:"column:details_json;type:text" json:"detailsJson"`
	// LastSeenAt 最近一次评估仍处于该状态的时间（扩展列）。
	LastSeenAt *time.Time `gorm:"column:last_seen_at" json:"lastSeenAt"`
	// RepeatCount 本轮告警期间的重申次数（扩展列）：级别变化或恢复时清零。
	//
	// 它让「这条告警至今重复提醒了多少次」在告警中心列表上就能看到，不必点进详情翻时间线。
	// default:0 保证新库建列时存量行被填 0（而非 NULL）；老库由 backfillNewColumns 补齐。
	RepeatCount int `gorm:"column:repeat_count;default:0" json:"repeatCount"`
	// LastRepeatedAt 最近一次重申时间（扩展列，为空表示尚未重申过）。
	LastRepeatedAt *time.Time `gorm:"column:last_repeated_at" json:"lastRepeatedAt"`
	// ClosedAt 告警结束时间（扩展列，为空表示仍在活动）。
	//
	// 结束有两种来源：人工在告警中心确认（CloseReason=acknowledged）、
	// 或指标自动回落正常（CloseReason=recovered）。它只标记「这条告警结束了」，
	// 不改动 State——State 始终如实反映指标状态，确认时指标可能仍然超限。
	// 因此不能在结束时就写 state=ok，那等于谎报「指标已恢复正常」。
	ClosedAt *time.Time `gorm:"column:closed_at;index" json:"closedAt"`
	// CloseReason 结束原因：acknowledged / recovered；未结束时为空。
	CloseReason string `gorm:"column:close_reason;size:16" json:"closeReason"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
	// UpdatedAt 更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 返回 alerts 表名。
func (Alert) TableName() string { return "alerts" }

// AlertEvent 对应 alert_events 表：告警状态流转时间线，供告警中心详情弹窗展示。
type AlertEvent struct {
	// ID 主键。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// AlertID 所属告警。
	AlertID uint `gorm:"column:alert_id;index" json:"alertId"`
	// AgentID 主机标识（便于按主机检索）。
	AgentID string `gorm:"column:agent_id;size:128;index" json:"agentId"`
	// MetricKey 指标标识。
	MetricKey string `gorm:"column:metric_key;size:64;index" json:"metricKey"`
	// Event 事件类型：fired / upgraded / downgraded / repeated / recovered / acknowledged。
	Event string `gorm:"column:event;size:32" json:"event"`
	// FromState 变更前状态。
	FromState string `gorm:"column:from_state;size:16" json:"fromState"`
	// ToState 变更后状态。
	ToState string `gorm:"column:to_state;size:16" json:"toState"`
	// Value 事件发生时的指标值。
	Value float64 `gorm:"column:value" json:"value"`
	// Message 事件说明（人读文本）。
	Message string `gorm:"column:message;type:text" json:"message"`
	// CreatedAt 事件时间。
	CreatedAt time.Time `gorm:"column:created_at;index" json:"createdAt"`
}

// TableName 返回 alert_events 表名。
func (AlertEvent) TableName() string { return "alert_events" }

// Store 是资源告警的持久化入口（PostgreSQL 监控库）。
type Store struct {
	// db 监控库连接。
	db *gorm.DB
}

// NewStore 创建资源告警持久化入口。
//
// 参数 Parameters:
//   - db (*gorm.DB): PostgreSQL 监控库连接（与 collector 同一个库）。
//
// 返回 Returns:
//   - store (*Store): 持久化入口。
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// 告警结束原因常量（与 alerts.close_reason 取值一致）。
const (
	// CloseReasonAcknowledged 人工在告警中心确认导致告警结束。
	CloseReasonAcknowledged = "acknowledged"
	// CloseReasonRecovered 指标自动回落正常导致告警结束。
	CloseReasonRecovered = "recovered"
)

// Migrate 幂等创建/扩展告警相关表，并把去重约束升级为「仅活动告警唯一」。
//
// 说明：AutoMigrate 只加列、不改已有列类型、不删列，因此不会影响 collector 端
// 对 alert_rules / alerts 的既有读写。
//
// 返回 Returns:
//   - err (error): 迁移失败时返回非 nil。
func (s *Store) Migrate() error {
	if err := s.db.AutoMigrate(&AlertRule{}, &Alert{}, &AlertEvent{}); err != nil {
		return err
	}
	if err := s.backfillNewColumns(); err != nil {
		return err
	}
	return s.ensureActiveAlertIndex()
}

// ensureActiveAlertIndex 把 alerts 的去重约束从「dedup_key 全局唯一」
// 改成「同一 dedup_key 只能有一条未结束的告警」。
//
// 为什么要改：一条告警是一次「事件」——确认结束或自动恢复之后再触发，应当是新的一条记录，
// 而不是在旧行上继续累加。原来的全局唯一索引会让第二次触发直接插不进去（duplicate key）。
//
// 两个必须注意的点：
//  1. GORM 的 AutoMigrate 只加不删索引。把模型里的 uniqueIndex 标签去掉后，旧索引仍留在库里，
//     写入照样失败，且报错只在运行时抛一句 duplicate key。所以这里按「唯一、非主键、只有一列、
//     且该列是 dedup_key」显式 DROP——不硬编码索引名，因为它取决于 GORM 版本与建表历史。
//     （与 remote_endpoints 那次踩的是同一个坑。）
//  2. 用部分唯一索引（WHERE closed_at IS NULL）而不是干脆去掉唯一性：
//     既允许历史存多行，又能在数据库层挡住「同一告警同时出现两条活动记录」——
//     只靠应用层先查后插，在并发评估下并不足以保证。
//
// 返回 Returns:
//   - err (error): 迁移失败时返回非 nil。
func (s *Store) ensureActiveAlertIndex() error {
	// 只删「完整索引」（indpred is null）。
	// 部分索引（也就是下面要建的 uk_alerts_active）同样满足「唯一 + 非主键 + 单列 + dedup_key」，
	// 若不加这个条件，它会被一起删掉——实测中真实踩到：新旧索引同时消失，
	// 于是「同一告警只能有一条活动」的约束形同虚设，重复的活动告警可以直接插进去。
	const dropLegacy = `
do $$
declare
	idx_name text;
begin
	for idx_name in
		select i.relname
		from pg_index x
		join pg_class t on t.oid = x.indrelid
		join pg_class i on i.oid = x.indexrelid
		where t.relname = 'alerts'
		  and x.indisunique
		  and not x.indisprimary
		  and x.indnatts = 1
		  and x.indpred is null
		  and (select a.attname from pg_attribute a
		       where a.attrelid = t.oid and a.attnum = x.indkey[0]) = 'dedup_key'
	loop
		execute format('drop index if exists %I', idx_name);
	end loop;
end $$`
	if err := s.db.Exec(dropLegacy).Error; err != nil {
		return err
	}
	// 旧的唯一索引删掉后，dedup_key 上仍需要一个普通索引：
	// AutoMigrate 那次因「同名索引已存在」被跳过（GORM 只比对索引名，不比对唯一性），
	// 这里显式补齐，否则 upsert 查询与历史检索会退化成全表扫描。
	if err := s.db.Exec(`create index if not exists idx_alerts_dedup_key on alerts (dedup_key)`).Error; err != nil {
		return err
	}
	// closed_at 为 NULL 的行即「活动告警」；同一 dedup_key 只允许存在一条。
	return s.db.Exec(`create unique index if not exists uk_alerts_active
		on alerts (dedup_key) where closed_at is null`).Error
}

// backfillNewColumns 把新增列在存量行上的 NULL 补齐。
//
// 为什么必须做：新加的列在已有行上是 NULL，而 Go 侧模型字段是值类型
// （repeat_count 是 int、remind_interval_critical_seconds 是 int64），
// 读到 NULL 会直接报「converting NULL to string is unsupported」，
// 让规则查询与告警中心列表整体失败——不是显示不准，是功能不可用。
//
// 条件用 IS NULL，因此天然幂等，也不会覆盖用户之后显式设置的值
// （例如把严重级重申显式设为 0 表示「不重申」）。
//
// 返回 Returns:
//   - err (error): 回填失败时返回非 nil。
func (s *Store) backfillNewColumns() error {
	// 严重级重申间隔是后加的语义：警告级本来就没开重申的规则，严重级也保持不重申；
	// 警告级开了重申的，视为「未配置严重级间隔」，取默认值。
	err := s.db.Exec(`update alert_rules
		set remind_interval_critical_seconds = case when remind_interval_seconds > 0 then ? else 0 end
		where remind_interval_critical_seconds is null`, DefaultRemindIntervalCriticalSeconds).Error
	if err != nil {
		return err
	}
	if err := s.db.Exec(`update alerts set repeat_count = 0 where repeat_count is null`).Error; err != nil {
		return err
	}
	// 已恢复的存量行补上「结束时间/原因」：否则改造后它们的 closed_at 为空，
	// 会被当成活动告警继续留在告警中心的默认列表里（这些行的 state 早已是 ok）。
	// 用 recovered_at 兜底 updated_at；条件带 closed_at is null，天然幂等。
	return s.db.Exec(`update alerts
		set closed_at = coalesce(recovered_at, updated_at), close_reason = ?
		where closed_at is null and state = ?`, CloseReasonRecovered, LevelOK).Error
}

// ListRules 按作用域查询规则；scope 为空时返回本模块管理的全部规则。
//
// 仅返回带 metric_key 的行——collector 端通过其 API 写入的旧规则不受本模块管理，
// 混入解析会导致「空指标」规则污染生效值。
//
// 参数 Parameters:
//   - scope (string): 作用域，空表示不过滤。
//   - target (string): 作用域目标，空表示不按目标过滤。
//
// 返回 Returns:
//   - rules ([]AlertRule): 规则列表。
//   - err (error): 查询失败时返回非 nil。
func (s *Store) ListRules(scope, target string) ([]AlertRule, error) {
	rules := make([]AlertRule, 0, 16)
	query := s.db.Where("metric_key <> ''")
	if scope != "" {
		query = query.Where("scope = ?", scope)
	}
	if target != "" {
		query = query.Where("target = ?", target)
	}
	if err := query.Order("metric_key, scope, target").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// ListManagedRules 返回本模块管理的全部规则（metric_key 非空），供批量解析生效值。
//
// 返回 Returns:
//   - rules ([]AlertRule): 规则列表。
//   - err (error): 查询失败时返回非 nil。
func (s *Store) ListManagedRules() ([]AlertRule, error) {
	return s.ListRules("", "")
}

// SaveScopeRules 覆盖式保存某个层级的规则。
//
// 采用「先删同层级、再批量插入」，天然保证 (metric_key, scope, target) 唯一，
// 也避免依赖唯一索引——collector 端写入的旧规则 metric_key 为空，加唯一索引会冲突。
//
// 参数 Parameters:
//   - scope (string): 作用域。
//   - target (string): 作用域目标。
//   - rules ([]AlertRule): 待保存的规则（ID 由本方法重置）。
//
// 返回 Returns:
//   - saved ([]AlertRule): 落库后的规则（含新 ID）。
//   - err (error): 作用域非法或写入失败时返回非 nil。
func (s *Store) SaveScopeRules(scope, target string, rules []AlertRule) ([]AlertRule, error) {
	if scope != ScopeGlobal && scope != ScopeRole && scope != ScopeAgent {
		return nil, errors.New("规则作用域非法")
	}
	if scope != ScopeGlobal && strings.TrimSpace(target) == "" {
		return nil, errors.New("角色级与单机级规则必须指定目标")
	}
	saved := make([]AlertRule, 0, len(rules))
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scope = ? AND target = ?", scope, target).Delete(&AlertRule{}).Error; err != nil {
			return err
		}
		for _, rule := range rules {
			rule.ID = 0
			rule.Scope = scope
			rule.Target = target
			rule.Metric = rule.MetricKey
			if err := tx.Create(&rule).Error; err != nil {
				return err
			}
			saved = append(saved, rule)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// DeleteRule 删除单条规则。
//
// 参数 Parameters:
//   - id (uint): 规则主键。
//
// 返回 Returns:
//   - err (error): 删除失败时返回非 nil。
func (s *Store) DeleteRule(id uint) error {
	return s.db.Delete(&AlertRule{}, id).Error
}

// EnsureDefaults 在某个层级完全没有规则时写入内置默认值（开箱可用）。
//
// 参数 Parameters:
//   - scope (string): 作用域，通常为 global。
//
// 返回 Returns:
//   - err (error): 写入失败时返回非 nil。
func (s *Store) EnsureDefaults(scope string) error {
	var count int64
	if err := s.db.Model(&AlertRule{}).
		Where("metric_key <> '' AND scope = ? AND target = ''", scope).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := s.SaveScopeRules(scope, "", DefaultRules())
	return err
}

// AgentRuleItem 是指标维度的规则视图，用于「服务器列表 → 告警设置」弹窗展示继承关系。
type AgentRuleItem struct {
	// Metric 指标口径定义（名称、单位、是否按挂载点）。
	Metric MetricSpec `json:"metric"`
	// Effective 当前生效的规则；三级都未配置时为零值。
	Effective AlertRule `json:"effective"`
	// Source 生效来源：global / role / agent；为空表示三级都未配置。
	Source string `json:"source"`
	// Override 单机覆盖值；为空表示未覆盖（继承上级）。
	Override *AlertRule `json:"override"`
}

// AgentRules 是一台主机的资源告警规则视图。
type AgentRules struct {
	// AgentID 主机标识。
	AgentID string `json:"agentId"`
	// Role 主机角色（agents.role，可能为空）。
	Role string `json:"role"`
	// Items 每个指标的规则与继承关系。
	Items []AgentRuleItem `json:"items"`
}

// AgentRole 读取主机的角色标签（agents.role），角色级规则解析依赖它。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - role (string): 角色标签；主机不存在或未设置时为空。
//   - err (error): 查询失败时返回非 nil。
func (s *Store) AgentRole(agentID string) (string, error) {
	roles := make([]string, 0, 1)
	if err := s.db.Table("agents").Where("id = ?", agentID).Limit(1).Pluck("role", &roles).Error; err != nil {
		return "", err
	}
	if len(roles) == 0 {
		return "", nil
	}
	return roles[0], nil
}

// AgentRules 返回某台主机的资源告警规则视图：每个指标的生效值、来源层级与单机覆盖值。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - view (AgentRules): 规则视图。
//   - err (error): 查询失败时返回非 nil。
func (s *Store) AgentRules(agentID string) (AgentRules, error) {
	role, err := s.AgentRole(agentID)
	if err != nil {
		return AgentRules{}, err
	}
	rules, err := s.ListManagedRules()
	if err != nil {
		return AgentRules{}, err
	}
	effective := ResolveEffective(rules, agentID, role)
	overrides := make(map[string]AlertRule, len(rules))
	for _, rule := range rules {
		if rule.Scope == ScopeAgent && rule.Target == agentID {
			overrides[rule.MetricKey] = rule
		}
	}
	specs := Metrics()
	view := AgentRules{AgentID: agentID, Role: role, Items: make([]AgentRuleItem, 0, len(specs))}
	for _, spec := range specs {
		item := AgentRuleItem{Metric: spec}
		if eff, ok := effective[spec.Key]; ok {
			item.Effective = eff.AlertRule
			item.Source = eff.Source
		}
		if override, ok := overrides[spec.Key]; ok {
			copied := override
			item.Override = &copied
		}
		view.Items = append(view.Items, item)
	}
	return view, nil
}

// ProcessSnapshot 是 Agent 心跳上报的进程内存快照（agents.processes_json 的元素）。
type ProcessSnapshot struct {
	// PID 进程号。
	PID int `json:"pid"`
	// Name 进程名。
	Name string `json:"name"`
	// MemoryBytes 常驻内存字节数。
	MemoryBytes uint64 `json:"memoryBytes"`
}

// AgentSnapshot 是主机的最新状态快照（来自 agents 表的心跳字段）。
type AgentSnapshot struct {
	// AgentID 主机标识。
	AgentID string
	// Hostname 主机名。
	Hostname string
	// Role 角色标签（角色级规则解析用）。
	Role string
	// CurrentIP 上报 IP。
	CurrentIP string
	// Load1 最近一次心跳的 1 分钟负载（绝对值）。
	Load1 float64
	// LogicalCPUCores 逻辑 CPU 核数（负载归一化用）。
	LogicalCPUCores uint64
	// MemoryTotalBytes 物理内存总量。
	MemoryTotalBytes uint64
	// MemoryUsedBytes 已用内存。
	MemoryUsedBytes uint64
	// ProcessCount 进程总数。
	ProcessCount uint64
}

// agentRow 是 agents 表的轻量查询载体。
type agentRow struct {
	ID               string  `gorm:"column:id"`
	Hostname         string  `gorm:"column:hostname"`
	Role             string  `gorm:"column:role"`
	CurrentIP        string  `gorm:"column:current_ip"`
	Load1            float64 `gorm:"column:load1"`
	LogicalCPUCores  uint64  `gorm:"column:logical_cpu_cores"`
	MemoryTotalBytes uint64  `gorm:"column:memory_total_bytes"`
	MemoryUsedBytes  uint64  `gorm:"column:memory_used_bytes"`
	ProcessCount     uint64  `gorm:"column:process_count"`
}

// AgentSnapshots 批量返回主机快照。
//
// 只取轻量字段（不含 processes_json）——每轮评估都需要全部主机的核数与角色，
// 但进程快照体量大，仅在真的产生告警时按需单台查询（见 AgentProcesses）。
//
// 返回 Returns:
//   - snapshots (map[string]AgentSnapshot): 主机标识到快照的映射。
//   - err (error): 查询失败时返回非 nil。
func (s *Store) AgentSnapshots() (map[string]AgentSnapshot, error) {
	rows := make([]agentRow, 0, 64)
	err := s.db.Table("agents").
		Select("id, hostname, role, current_ip, load1, logical_cpu_cores, memory_total_bytes, memory_used_bytes, process_count").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	snapshots := make(map[string]AgentSnapshot, len(rows))
	for _, row := range rows {
		snapshots[row.ID] = AgentSnapshot{
			AgentID: row.ID, Hostname: row.Hostname, Role: row.Role, CurrentIP: row.CurrentIP,
			Load1: row.Load1, LogicalCPUCores: row.LogicalCPUCores,
			MemoryTotalBytes: row.MemoryTotalBytes, MemoryUsedBytes: row.MemoryUsedBytes,
			ProcessCount: row.ProcessCount,
		}
	}
	return snapshots, nil
}

// AgentProcesses 返回某主机心跳上报的进程内存快照（procstat 未启用时的归因兜底）。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - processes ([]ProcessSnapshot): 进程快照；解析失败或为空时返回空切片。
//   - err (error): 查询失败时返回非 nil。
func (s *Store) AgentProcesses(agentID string) ([]ProcessSnapshot, error) {
	var rows []string
	if err := s.db.Table("agents").Where("id = ?", agentID).Limit(1).Pluck("processes_json", &rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 || strings.TrimSpace(rows[0]) == "" {
		return []ProcessSnapshot{}, nil
	}
	processes := make([]ProcessSnapshot, 0, 16)
	if err := json.Unmarshal([]byte(rows[0]), &processes); err != nil {
		// 进程快照异常不应影响告警本身：降级为空。
		return []ProcessSnapshot{}, nil
	}
	return processes, nil
}

// AlertStateUpdate 描述一次告警状态的落库动作。
type AlertStateUpdate struct {
	// Alert 要写入或更新的告警（按 DedupKey 定位）。
	Alert Alert
	// Event 事件类型：fired / upgraded / downgraded / recovered / repeated；为空表示只更新状态。
	Event string
	// Message 事件说明（人读）。
	Message string
}

// UpsertAlert 按去重键写入或更新告警，并追加一条流转事件。
//
// 去重键是唯一索引，同一告警在整个持续期间只有一行，避免「每条评估都插一行」。
//
// 参数 Parameters:
//   - update (AlertStateUpdate): 更新内容。
//
// 返回 Returns:
//   - alertID (uint): 告警主键。
//   - err (error): 写入失败时返回非 nil。
func (s *Store) UpsertAlert(update AlertStateUpdate) (uint, error) {
	var alertID uint
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 只在「未结束」的告警里找同一条：确认结束或自动恢复之后再触发，
		// 就是全新的一条记录，而不是在旧行上继续累加——这是告警改为事件式的关键一步。
		var existing Alert
		findErr := tx.Where("dedup_key = ? AND closed_at IS NULL", update.Alert.DedupKey).First(&existing).Error
		previousState := ""
		switch {
		case errors.Is(findErr, gorm.ErrRecordNotFound):
			row := update.Alert
			// 行不存在时通常是首次触发；防御性处理「首条就是重申」的极端情况。
			if update.Event == EventRepeated {
				repeatedAt := time.Now()
				row.RepeatCount = 1
				row.LastRepeatedAt = &repeatedAt
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			alertID = row.ID
		case findErr != nil:
			return findErr
		default:
			previousState = existing.State
			updates := map[string]any{
				"rule_id":       update.Alert.RuleID,
				"agent_id":      update.Alert.AgentID,
				"target_id":     update.Alert.TargetID,
				"metric_key":    update.Alert.MetricKey,
				"level":         update.Alert.Level,
				"state":         update.Alert.State,
				"current_value": update.Alert.CurrentValue,
				"details_json":  update.Alert.DetailsJSON,
				"last_seen_at":  update.Alert.LastSeenAt,
				"recovered_at":  update.Alert.RecoveredAt,
				"acknowledged":  update.Alert.Acknowledged,
				"closed_at":     update.Alert.ClosedAt,
				"close_reason":  update.Alert.CloseReason,
			}
			// 重申计数在这里维护，而不是由调用方传入：它完全由事件类型决定，
			// 收在同一处就不会出现「次数算错」这类难以察觉的数据不一致。
			// 级别变化（升级/降级）意味着进入新一轮提醒，恢复则清零。
			switch update.Event {
			case EventRepeated:
				// 用 coalesce 兜底：列刚建（尚未回填）时是 NULL，而 NULL + 1 仍是 NULL，
				// 会出现「有最近重申时间但次数为 0」的错位记录。
				updates["repeat_count"] = gorm.Expr("coalesce(repeat_count, 0) + 1")
				updates["last_repeated_at"] = time.Now()
			case "":
				// 仅刷新状态、不产生事件：不改动计数。
			default:
				updates["repeat_count"] = 0
				updates["last_repeated_at"] = nil
			}
			if err := tx.Model(&existing).Updates(updates).Error; err != nil {
				return err
			}
			alertID = existing.ID
		}
		if update.Event == "" {
			return nil
		}
		if previousState == "" {
			previousState = LevelOK
		}
		event := AlertEvent{
			AlertID:   alertID,
			AgentID:   update.Alert.AgentID,
			MetricKey: update.Alert.MetricKey,
			Event:     update.Event,
			FromState: previousState,
			ToState:   update.Alert.State,
			Value:     update.Alert.CurrentValue,
			Message:   update.Message,
			CreatedAt: time.Now(),
		}
		return tx.Create(&event).Error
	})
	return alertID, err
}

// AlertDetail 返回一条告警及其状态流转时间线（告警中心详情弹窗用）。
type AlertDetail struct {
	// Alert 告警本体。
	Alert Alert `json:"alert"`
	// Events 状态流转事件（按时间升序）。
	Events []AlertEvent `json:"events"`
}

// AlertDetail 按主键返回告警详情与流转时间线。
//
// 参数 Parameters:
//   - id (uint): 告警主键。
//
// 返回 Returns:
//   - detail (AlertDetail): 详情。
//   - err (error): 未找到或查询失败时返回非 nil。
func (s *Store) AlertDetail(id uint) (AlertDetail, error) {
	var alert Alert
	if err := s.db.Where("id = ?", id).First(&alert).Error; err != nil {
		return AlertDetail{}, err
	}
	events := make([]AlertEvent, 0, 16)
	if err := s.db.Where("alert_id = ?", id).Order("created_at").Find(&events).Error; err != nil {
		return AlertDetail{}, err
	}
	return AlertDetail{Alert: alert, Events: events}, nil
}

// Acknowledge 人工确认并结束告警。
//
// 语义是「确认即关闭」：操作者已经看到这条告警并处理（或决定稍后处理），它就此结束、
// 不再重申。若问题依然存在，会由评估器在冷却期（规则 CooldownSeconds）过后重新产生
// 一条全新告警（首次触发，而非重申），重申次数与首次触发时间都记在那条新告警上。
//
// 只写 closed_at / close_reason，不动 state：确认时指标可能仍然超限，
// 把 state 写成 ok 等于谎报「指标已恢复正常」，会污染监控曲线与其它筛选条件。
//
// 参数 Parameters:
//   - id (uint): 告警主键。
//
// 返回 Returns:
//   - alert (Alert): 被确认的告警（已结束或本次刚结束）。
//   - closedNow (bool): 本次调用是否真的执行了「结束」；重复确认已结束的告警时为 false。
//     调用方据此决定要不要复位状态机——重复点击不应再次顺延冷却期。
//   - err (error): 更新失败时返回非 nil。
func (s *Store) Acknowledge(id uint) (Alert, bool, error) {
	var (
		alert     Alert
		closedNow bool
	)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).First(&alert).Error; err != nil {
			return err
		}
		// 已结束的告警重复确认按幂等处理：不覆盖原先的结束时间与原因。
		if alert.ClosedAt != nil {
			return nil
		}
		now := time.Now()
		if err := tx.Model(&alert).Updates(map[string]any{
			"acknowledged": true,
			"closed_at":    now,
			"close_reason": CloseReasonAcknowledged,
		}).Error; err != nil {
			return err
		}
		// 追加一条结束事件，让详情时间线能看到「何时被人工确认并结束」。
		if err := tx.Create(&AlertEvent{
			AlertID:   alert.ID,
			AgentID:   alert.AgentID,
			MetricKey: alert.MetricKey,
			Event:     EventAcknowledged,
			FromState: alert.State,
			ToState:   alert.State,
			Value:     alert.CurrentValue,
			Message:   "人工确认，告警结束",
			CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		closedAt := now
		alert.ClosedAt = &closedAt
		alert.CloseReason = CloseReasonAcknowledged
		closedNow = true
		return nil
	})
	return alert, closedNow, err
}

// DeleteAlerts 按主键批量删除告警记录，并连带删除它们的流转事件。
//
// 为什么必须一起删事件：alert_events.alert_id 只是普通索引（无外键级联），
// 只删 alerts 会留下一堆指向不存在告警的孤儿时间线——占空间，且详情抽屉再也打不开。
//
// 返回被删除的告警（含 dedup_key / closed_at / metric_key）：调用方据此复位状态机。
// 否则「删掉仍在活动的告警」之后内存里仍认为它在告警，后续重申会再插一条新记录，
// 表现为「清理完又自己冒出来」。
//
// 参数 Parameters:
//   - ids ([]uint): 告警主键列表。
//
// 返回 Returns:
//   - removed ([]Alert): 实际被删除的告警。
//   - err (error): 删除失败时返回非 nil。
func (s *Store) DeleteAlerts(ids []uint) ([]Alert, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	removed := make([]Alert, 0, len(ids))
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", ids).Find(&removed).Error; err != nil {
			return err
		}
		if len(removed) == 0 {
			return nil
		}
		alertIDs := make([]uint, 0, len(removed))
		for _, alert := range removed {
			alertIDs = append(alertIDs, alert.ID)
		}
		if err := tx.Where("alert_id IN ?", alertIDs).Delete(&AlertEvent{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", alertIDs).Delete(&Alert{}).Error
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

// RecentlyClosedResourceAlerts 返回某时刻之后结束的资源告警。
//
// 用途：进程重启后内存状态机是空的。已恢复/已确认的告警不再算「活动」，本来不必恢复；
// 但处于冷却期内的那部分必须恢复成「正常 + 冷却中」，否则冷却期会随重启凭空消失，
// 指标仍超标的主机在下一轮就会立刻重新告警——与不重启时的行为不一致。
//
// 参数 Parameters:
//   - since (time.Time): 冷却窗口起点（一般取 now - 最大冷却时长）。
//
// 返回 Returns:
//   - alerts ([]Alert): 已结束但仍在冷却窗口内的资源告警。
//   - err (error): 查询失败时返回非 nil。
func (s *Store) RecentlyClosedResourceAlerts(since time.Time) ([]Alert, error) {
	alerts := make([]Alert, 0, 16)
	err := s.db.Where("coalesce(metric_key, '') <> '' AND closed_at IS NOT NULL AND closed_at >= ?", since).
		Find(&alerts).Error
	if err != nil {
		return nil, err
	}
	return alerts, nil
}

// ActiveResourceAlerts 返回当前处于告警状态的资源告警。
//
// 用途：进程重启后内存里的状态机是空的，需要用库里的状态把它重建起来，
// 否则已处于告警的主机会被重新判为「首次触发」，产生重复告警。
//
// 返回 Returns:
//   - alerts ([]Alert): 告警列表。
//   - err (error): 查询失败时返回非 nil。
func (s *Store) ActiveResourceAlerts() ([]Alert, error) {
	alerts := make([]Alert, 0, 64)
	// 「活动」的判据是「未结束」（closed_at 为空），而不是「state 非 ok」：
	// 已人工确认结束的告警 state 可能仍是 critical，但它不该被当成活动告警恢复进状态机，
	// 否则那头告警会被重新判为「首次触发」并盖掉 closed_at。
	err := s.db.Where("coalesce(metric_key, '') <> '' AND closed_at IS NULL").Find(&alerts).Error
	if err != nil {
		return nil, err
	}
	return alerts, nil
}

// IsSilenced 判断主机当前是否处于维护窗口内（维护窗口与已静默的告警不写日志）。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - now (time.Time): 当前时间。
//
// 返回 Returns:
//   - silenced (bool): 是否静默。
//   - err (error): 查询失败时返回非 nil（调用方按「不静默」处理）。
func (s *Store) IsSilenced(agentID string, now time.Time) (bool, error) {
	var count int64
	err := s.db.Table("maintenance_windows").
		Where("agent_id = ? AND starts_at <= ? AND ends_at >= ?", agentID, now, now).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
