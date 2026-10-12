package resourcealert

import "time"

// 告警级别常量（与 alerts.state 一致）。
const (
	// LevelOK 正常。
	LevelOK = "ok"
	// LevelWarning 警告。
	LevelWarning = "warning"
	// LevelCritical 严重。
	LevelCritical = "critical"
)

// 状态流转事件常量（与 alert_events.event 一致）。
const (
	// EventFired 正常 → 警告/严重。
	EventFired = "fired"
	// EventUpgraded 警告 → 严重。
	EventUpgraded = "upgraded"
	// EventDowngraded 严重 → 警告。
	EventDowngraded = "downgraded"
	// EventRecovered 恢复 → 正常。
	EventRecovered = "recovered"
	// EventRepeated 同级持续时的重申。
	EventRepeated = "repeated"
	// EventAcknowledged 人工在告警中心确认并结束了该告警。
	//
	// 与其余事件的区别：它不代表指标状态变化，而是生命周期结束
	//（配合 alerts.closed_at / close_reason 使用）。
	EventAcknowledged = "acknowledged"
)

// TimedValue 是换算成指标口径后的时序点。
type TimedValue struct {
	// At 采样时间。
	At time.Time
	// Value 指标值（已换算，可直接与阈值比较）。
	Value float64
}

// Verdict 是一台主机（磁盘为某挂载点）某个指标的判定结果。
type Verdict struct {
	// AgentID 主机标识。
	AgentID string
	// Target 告警目标：磁盘为挂载点，其余为空。
	Target string
	// MetricKey 指标标识。
	MetricKey string
	// Rule 生效规则（含来源层级）。
	Rule EffectiveRule
	// Level 判定级别：ok / warning / critical。
	Level string
	// Value 当前值（窗口内最后一个聚合点换算后的指标值）。
	Value float64
	// Threshold 命中级别的阈值；正常时展示警告阈值。
	Threshold float64
	// Unit 单位（% 或 倍）。
	Unit string
	// Explain 口径说明。
	Explain string
	// TotalPoints 窗口内采样点总数。
	TotalPoints int
	// BreachPoints 超标点数（按命中级别阈值统计）。
	BreachPoints int
	// BreachRatio 超标点占比。
	BreachRatio float64
	// SustainedSeconds 已持续超标时长（自末尾连续超标段起点起算）。
	SustainedSeconds int64
	// Window 查询窗口。
	Window time.Duration
	// Step 聚合粒度。
	Step time.Duration
	// Values 换算后的完整序列（供归因与日志使用）。
	Values []TimedValue
}

// DedupKey 返回该判定的去重键。
//
// 返回 Returns:
//   - key (string): 去重键。
func (v Verdict) DedupKey() string {
	return BuildDedupKey(v.MetricKey, v.AgentID, v.Target)
}

// windowStats 是某个阈值下的窗口统计结果。
type windowStats struct {
	// breaches 超标点数。
	breaches int
	// ratio 超标点占比。
	ratio float64
	// lastBreached 窗口内最后一个点是否超标（判断「当前是否仍在超标」）。
	lastBreached bool
	// sustainedSeconds 末尾连续超标段的时长。
	sustainedSeconds int64
}

// ConvertValue 把 Influx 中的原始字段均值换算为指标口径的值。
//
//   - cpu / mem：字段是「空闲/可用」百分比，利用率 = 100 - 该值；
//   - load：load1 是绝对值，除以逻辑核数归一化后才能跨机器比较；
//   - disk：used_percent 即利用率，无需换算。
//
// 参数 Parameters:
//   - metricKey (string): 指标标识。
//   - raw (float64): 原始字段均值。
//   - cores (float64): 逻辑 CPU 核数（仅负载使用）。
//
// 返回 Returns:
//   - value (float64): 指标值。
func ConvertValue(metricKey string, raw, cores float64) float64 {
	switch metricKey {
	case MetricCPUUsage, MetricMemUsage:
		return 100 - raw
	case MetricLoadPerCore:
		if cores <= 0 {
			cores = 1
		}
		return raw / cores
	default:
		return raw
	}
}

// Judge 对单个窗口做「持续超标」判定。
//
// 判据不是瞬时值，而是三点：窗口内最后一个点超标（当前仍在超标）、超标点占比达到
// 规则要求（容忍 Telegraf 偶发丢点）、以及由此得出的已持续时长。
//
// 参数 Parameters:
//   - window (MetricWindow): 窗口序列（原始字段值）。
//   - rule (EffectiveRule): 生效规则。
//   - cores (float64): 逻辑 CPU 核数（负载归一化用）。
//   - spec (QuerySpec): 查询描述（提供指标与窗口参数）。
//
// 返回 Returns:
//   - verdict (Verdict): 判定结果；窗口无数据时级别为 ok。
func Judge(window MetricWindow, rule EffectiveRule, cores float64, spec QuerySpec) Verdict {
	specMeta, _ := MetricSpecOf(spec.MetricKey)
	verdict := Verdict{
		AgentID:   window.AgentID,
		Target:    window.Target,
		MetricKey: spec.MetricKey,
		Rule:      rule,
		Level:     LevelOK,
		Unit:      specMeta.Unit,
		Explain:   specMeta.Explain,
		Window:    spec.Window,
		Step:      spec.Step,
	}
	values := make([]TimedValue, 0, len(window.Points))
	for _, point := range window.Points {
		values = append(values, TimedValue{At: point.At, Value: ConvertValue(spec.MetricKey, point.Value, cores)})
	}
	verdict.Values = values
	verdict.TotalPoints = len(values)
	if len(values) == 0 {
		// 无数据时不下结论：交给状态机按「未变化」处理，避免误报恢复。
		verdict.Threshold = rule.WarningThreshold
		return verdict
	}
	verdict.Value = values[len(values)-1].Value

	warn := analyze(values, rule.WarningThreshold)
	critical := analyze(values, rule.CriticalThreshold)
	switch {
	case critical.lastBreached && critical.ratio >= rule.BreachRatio:
		verdict.Level = LevelCritical
		verdict.Threshold = rule.CriticalThreshold
		verdict.BreachPoints = critical.breaches
		verdict.BreachRatio = critical.ratio
		verdict.SustainedSeconds = critical.sustainedSeconds
	case warn.lastBreached && warn.ratio >= rule.BreachRatio:
		verdict.Level = LevelWarning
		verdict.Threshold = rule.WarningThreshold
		verdict.BreachPoints = warn.breaches
		verdict.BreachRatio = warn.ratio
		verdict.SustainedSeconds = warn.sustainedSeconds
	default:
		// 未达到持续判据：展示警告阈值下的占比，便于排查「为什么没触发」。
		verdict.Threshold = rule.WarningThreshold
		verdict.BreachPoints = warn.breaches
		verdict.BreachRatio = warn.ratio
	}
	return verdict
}

// analyze 统计某阈值下的超标点数、占比与末尾连续超标时长。
//
// 参数 Parameters:
//   - values ([]TimedValue): 换算后的序列（时间升序）。
//   - threshold (float64): 阈值。
//
// 返回 Returns:
//   - stats (windowStats): 统计结果。
func analyze(values []TimedValue, threshold float64) windowStats {
	stats := windowStats{}
	for _, value := range values {
		if value.Value >= threshold {
			stats.breaches++
		}
	}
	if len(values) == 0 {
		return stats
	}
	stats.ratio = float64(stats.breaches) / float64(len(values))
	stats.lastBreached = values[len(values)-1].Value >= threshold
	// 自末尾向前找连续超标段：起点越早，说明「已经持续多久」。
	start := -1
	for i := len(values) - 1; i >= 0; i-- {
		if values[i].Value < threshold {
			break
		}
		start = i
	}
	if start >= 0 && start < len(values)-1 {
		stats.sustainedSeconds = int64(values[len(values)-1].At.Sub(values[start].At).Seconds())
	}
	return stats
}
