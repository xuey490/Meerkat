package resourcealert

import (
	"encoding/json"
	"strings"
)

// 指标标识常量：规则与告警都以此为键。
const (
	// MetricCPUUsage CPU 利用率（口径：100 - usage_idle）。
	MetricCPUUsage = "cpu_usage"
	// MetricMemUsage 内存利用率（口径：100 - available_percent）。
	MetricMemUsage = "mem_usage"
	// MetricLoadPerCore 系统负载（口径：load1 / 逻辑核数）。
	MetricLoadPerCore = "load_per_core"
	// MetricDiskUsage 磁盘利用率（口径：used_percent，按挂载点分别判定）。
	MetricDiskUsage = "disk_usage"
)

// 默认阈值与判定期。取值依据见方案：CPU/内存 5 分钟、负载 10 分钟、磁盘 30 分钟；
// 负载用「核数倍数」而非百分比（load1 是绝对值，必须归一化后才能跨机比较）。
const (
	// DefaultCPUWarning CPU 警告阈值（%）。
	DefaultCPUWarning = 85.0
	// DefaultCPUCritical CPU 严重阈值（%）。
	DefaultCPUCritical = 95.0
	// DefaultCPUForSeconds CPU 判定期（秒）。
	DefaultCPUForSeconds = 300

	// DefaultMemWarning 内存警告阈值（%）。
	DefaultMemWarning = 85.0
	// DefaultMemCritical 内存严重阈值（%）。
	DefaultMemCritical = 95.0
	// DefaultMemForSeconds 内存判定期（秒）。
	DefaultMemForSeconds = 300

	// DefaultLoadWarning 负载警告阈值（核数倍数）。
	DefaultLoadWarning = 1.2
	// DefaultLoadCritical 负载严重阈值（核数倍数）。
	DefaultLoadCritical = 2.0
	// DefaultLoadForSeconds 负载判定期（秒）。负载是慢变量，窗口更长。
	DefaultLoadForSeconds = 600

	// DefaultDiskWarning 磁盘警告阈值（%）。
	DefaultDiskWarning = 85.0
	// DefaultDiskCritical 磁盘严重阈值（%）。磁盘到 95% 往往已来不及处置，提前到 92%。
	DefaultDiskCritical = 92.0
	// DefaultDiskForSeconds 磁盘判定期（秒）。
	DefaultDiskForSeconds = 1800

	// DefaultBreachRatio 默认的窗口内超标点占比下限。
	DefaultBreachRatio = 0.9
	// DefaultLoadBreachRatio 负载类指标的占比下限（负载抖动更大，适当放宽）。
	DefaultLoadBreachRatio = 0.8

	// DefaultRemindIntervalSeconds 警告级同级重申间隔（秒）。
	DefaultRemindIntervalSeconds = 1800
	// DefaultRemindIntervalCriticalSeconds 严重级同级重申间隔（秒）。
	//
	// 严重意味着「已经在伤业务」，需要比警告更快把人叫醒，所以默认更短。
	DefaultRemindIntervalCriticalSeconds = 600
	// DefaultCooldownSeconds 恢复后冷却时长（秒）。
	DefaultCooldownSeconds = 600
)

// defaultIgnoreFSTypes 磁盘判定默认忽略的伪文件系统：这些挂载点占用率会剧烈波动，
// 参与判定只会产生噪音。
var defaultIgnoreFSTypes = []string{"tmpfs", "devtmpfs", "overlay", "squashfs", "ramfs", "autofs", "nfs", "nfs4", "cifs"}

// MetricSpec 描述一类可告警指标的口径与展示信息。
type MetricSpec struct {
	// Key 指标标识。
	Key string `json:"key"`
	// Name 中文名称。
	Name string `json:"name"`
	// Unit 单位：% 或 倍。
	Unit string `json:"unit"`
	// PerMount 是否按挂载点分别判定（仅磁盘为 true，每个挂载点独立成条告警）。
	PerMount bool `json:"perMount"`
	// Explain 口径说明（前端展示与日志使用）。
	Explain string `json:"explain"`
}

// metricSpecs 内置指标定义，顺序固定（前端按此顺序展示）。
var metricSpecs = []MetricSpec{
	{Key: MetricCPUUsage, Name: "CPU 利用率", Unit: "%", Explain: "100 - usage_idle（整机合计）"},
	{Key: MetricMemUsage, Name: "内存利用率", Unit: "%", Explain: "100 - available_percent（不含可回收的 cache）"},
	{Key: MetricLoadPerCore, Name: "系统负载", Unit: "倍", Explain: "load1 / 逻辑核数"},
	{Key: MetricDiskUsage, Name: "磁盘利用率", Unit: "%", PerMount: true, Explain: "used_percent（按挂载点，已排除伪文件系统）"},
}

// Metrics 返回内置指标定义（副本，调用方可安全修改）。
//
// 返回 Returns:
//   - specs ([]MetricSpec): 指标定义列表。
func Metrics() []MetricSpec {
	out := make([]MetricSpec, len(metricSpecs))
	copy(out, metricSpecs)
	return out
}

// MetricSpecOf 按标识查询指标定义。
//
// 参数 Parameters:
//   - key (string): 指标标识。
//
// 返回 Returns:
//   - spec (MetricSpec): 指标定义；未找到时为零值。
//   - found (bool): 是否存在。
func MetricSpecOf(key string) (MetricSpec, bool) {
	for _, spec := range metricSpecs {
		if spec.Key == key {
			return spec, true
		}
	}
	return MetricSpec{}, false
}

// RuleParams 是 AlertRule.ParamsJSON 的结构化视图。
type RuleParams struct {
	// IgnoreFSTypes 忽略的文件系统（磁盘指标）；为空时使用内置默认集合。
	IgnoreFSTypes []string `json:"ignoreFstypes,omitempty"`
	// IncludeMounts 仅判定这些挂载点；为空表示全部（排除 IgnoreFSTypes 后）。
	IncludeMounts []string `json:"includeMounts,omitempty"`
	// TopN 归因进程条数；0 表示使用配置段中的默认值。
	TopN int `json:"topN,omitempty"`
}

// ParseParams 解析规则扩展参数。
//
// 参数 Parameters:
//   - raw (string): JSON 文本，可为空或非法。
//
// 返回 Returns:
//   - params (RuleParams): 解析结果；为空或非法时返回内置默认参数。
func ParseParams(raw string) RuleParams {
	params := RuleParams{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return params
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		// 参数非法不应影响告警本身，退化为默认参数。
		return RuleParams{}
	}
	return params
}

// MarshalParams 序列化规则扩展参数；空参数返回空字符串，避免写入无意义的 "{}"。
//
// 参数 Parameters:
//   - params (RuleParams): 参数。
//
// 返回 Returns:
//   - raw (string): JSON 文本。
func MarshalParams(params RuleParams) string {
	if len(params.IgnoreFSTypes) == 0 && len(params.IncludeMounts) == 0 && params.TopN == 0 {
		return ""
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	return string(raw)
}

// EffectiveIgnoreFSTypes 返回磁盘判定实际使用的忽略文件系统集合。
//
// 参数 Parameters:
//   - params (RuleParams): 规则扩展参数。
//
// 返回 Returns:
//   - fstypes ([]string): 忽略的文件系统（未配置时为内置默认集合）。
func EffectiveIgnoreFSTypes(params RuleParams) []string {
	if len(params.IgnoreFSTypes) == 0 {
		return defaultIgnoreFSTypes
	}
	return params.IgnoreFSTypes
}

// DefaultRules 返回内置默认规则（全局层）。
//
// 首次启动且全局层没有任何规则时落库，保证开箱即可产生告警；之后以数据库为准。
//
// 返回 Returns:
//   - rules ([]AlertRule): 默认规则列表。
func DefaultRules() []AlertRule {
	return []AlertRule{
		{
			Name: "CPU 利用率过高", MetricKey: MetricCPUUsage,
			WarningThreshold: DefaultCPUWarning, CriticalThreshold: DefaultCPUCritical,
			ForSeconds: DefaultCPUForSeconds, BreachRatio: DefaultBreachRatio,
			RemindIntervalSeconds:         DefaultRemindIntervalSeconds,
			RemindIntervalCriticalSeconds: DefaultRemindIntervalCriticalSeconds,
			CooldownSeconds:               DefaultCooldownSeconds,
			Enabled:                       true,
		},
		{
			Name: "内存利用率过高", MetricKey: MetricMemUsage,
			WarningThreshold: DefaultMemWarning, CriticalThreshold: DefaultMemCritical,
			ForSeconds: DefaultMemForSeconds, BreachRatio: DefaultBreachRatio,
			RemindIntervalSeconds:         DefaultRemindIntervalSeconds,
			RemindIntervalCriticalSeconds: DefaultRemindIntervalCriticalSeconds,
			CooldownSeconds:               DefaultCooldownSeconds,
			Enabled:                       true,
		},
		{
			Name: "系统负载过高", MetricKey: MetricLoadPerCore,
			WarningThreshold: DefaultLoadWarning, CriticalThreshold: DefaultLoadCritical,
			ForSeconds: DefaultLoadForSeconds, BreachRatio: DefaultLoadBreachRatio,
			RemindIntervalSeconds:         DefaultRemindIntervalSeconds,
			RemindIntervalCriticalSeconds: DefaultRemindIntervalCriticalSeconds,
			CooldownSeconds:               DefaultCooldownSeconds,
			Enabled:                       true,
		},
		{
			Name: "磁盘利用率过高", MetricKey: MetricDiskUsage,
			WarningThreshold: DefaultDiskWarning, CriticalThreshold: DefaultDiskCritical,
			ForSeconds: DefaultDiskForSeconds, BreachRatio: DefaultBreachRatio,
			RemindIntervalSeconds:         DefaultRemindIntervalSeconds,
			RemindIntervalCriticalSeconds: DefaultRemindIntervalCriticalSeconds,
			CooldownSeconds:               DefaultCooldownSeconds,
			ParamsJSON:                    MarshalParams(RuleParams{IgnoreFSTypes: defaultIgnoreFSTypes}),
			Enabled:                       true,
		},
	}
}

// EffectiveRule 是一台主机某个指标的最终生效规则，并标注来源层级。
type EffectiveRule struct {
	// AlertRule 生效的规则内容。
	AlertRule
	// Source 生效来源：global / role / agent。
	Source string `json:"source"`
}

// ResolveEffective 按「全局 → 角色 → 单机」优先级合并规则，得到该主机每个指标的生效规则。
//
// 参数 Parameters:
//   - rules ([]AlertRule): 本模块管理的全部规则（含各层级）。
//   - agentID (string): 主机标识。
//   - role (string): 主机角色（Agent 上报的 role 标签，可能为空）。
//
// 返回 Returns:
//   - effective (map[string]EffectiveRule): key 为指标标识。
func ResolveEffective(rules []AlertRule, agentID, role string) map[string]EffectiveRule {
	result := make(map[string]EffectiveRule, len(metricSpecs))
	// 按层级由低到高依次覆盖，保证「单机 > 角色 > 全局」。
	for _, layer := range []string{ScopeGlobal, ScopeRole, ScopeAgent} {
		for _, rule := range rules {
			if rule.Scope != layer || rule.MetricKey == "" {
				continue
			}
			switch layer {
			case ScopeGlobal:
				if rule.Target != "" {
					continue
				}
			case ScopeRole:
				// 角色为空时角色层不参与，避免「空角色」规则误命中全部主机。
				if role == "" || rule.Target != role {
					continue
				}
			case ScopeAgent:
				if rule.Target != agentID {
					continue
				}
			}
			result[rule.MetricKey] = EffectiveRule{AlertRule: rule, Source: layer}
		}
	}
	return result
}

// BuildDedupKey 生成告警去重键：同一主机同一指标（磁盘指标再加挂载点）在持续期间只保留一条，
// 与 collector 端「离线告警」共用同一套去重约定（dedup_key 唯一索引）。
//
// 参数 Parameters:
//   - metricKey (string): 指标标识。
//   - agentID (string): 主机标识。
//   - target (string): 告警目标（磁盘为挂载点，其余为空）。
//
// 返回 Returns:
//   - key (string): 去重键。
func BuildDedupKey(metricKey, agentID, target string) string {
	if target == "" {
		return metricKey + ":agent:" + agentID
	}
	return metricKey + ":agent:" + agentID + ":" + target
}
