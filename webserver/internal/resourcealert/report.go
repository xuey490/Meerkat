package resourcealert

import (
	"fmt"
	"strings"
	"time"
)

// AgentRef 是告警中的主机信息。
type AgentRef struct {
	// ID 主机标识。
	ID string `json:"id"`
	// Hostname 主机名。
	Hostname string `json:"hostname,omitempty"`
	// IP 上报 IP。
	IP string `json:"ip,omitempty"`
	// Role 角色标签。
	Role string `json:"role,omitempty"`
}

// MetricPart 是告警中的指标信息。
type MetricPart struct {
	// Key 指标标识。
	Key string `json:"key"`
	// Name 中文名。
	Name string `json:"name"`
	// Unit 单位。
	Unit string `json:"unit"`
	// Explain 口径说明。
	Explain string `json:"explain"`
	// Value 当前值。
	Value float64 `json:"value"`
	// Warning 生效的警告阈值。
	Warning float64 `json:"warning"`
	// Critical 生效的严重阈值。
	Critical float64 `json:"critical"`
	// Threshold 本次命中的阈值。
	Threshold float64 `json:"threshold"`
	// Target 告警目标（磁盘为挂载点）。
	Target string `json:"target,omitempty"`
	// WindowSeconds 判定窗口（秒）。
	WindowSeconds int64 `json:"windowSeconds"`
	// SustainedSeconds 已持续超标时长（秒）。
	SustainedSeconds int64 `json:"sustainedSeconds"`
	// BreachPoints 窗口内超标点数。
	BreachPoints int `json:"breachPoints"`
	// TotalPoints 窗口内采样点总数。
	TotalPoints int `json:"totalPoints"`
	// BreachRatio 超标点占比。
	BreachRatio float64 `json:"breachRatio"`
}

// Report 是一条告警的输出载体：既能序列化成结构化日志，也能渲染成人读文本。
type Report struct {
	// Time 事件时间。
	Time time.Time `json:"ts"`
	// Event 事件类型：fired / upgraded / downgraded / recovered / repeated。
	Event string `json:"event"`
	// Level 事件后的级别。
	Level string `json:"level"`
	// Agent 主机信息。
	Agent AgentRef `json:"agent"`
	// Metric 指标信息。
	Metric MetricPart `json:"metric"`
	// RuleSource 规则来源层级：global / role / agent。
	RuleSource string `json:"ruleSource"`
	// DedupKey 去重键。
	DedupKey string `json:"dedupKey"`
	// CPUDetail CPU 明细（仅 CPU 告警）。
	CPUDetail *CPUDetail `json:"cpuDetail,omitempty"`
	// Attribution 进程归因 Top N。
	Attribution []Attribution `json:"attribution,omitempty"`
	// Context 系统上下文。
	Context SystemContext `json:"context"`
	// Correlated 同机其它指标的当前状态。
	Correlated []string `json:"correlated,omitempty"`
	// Message 人读摘要。
	Message string `json:"message"`
}

// eventLabel 返回事件的中文标签。
//
// 返回 Returns:
//   - label (string): 中文标签。
func (r Report) eventLabel() string {
	switch r.Event {
	case EventFired:
		return "触发告警"
	case EventUpgraded:
		return "级别升级"
	case EventDowngraded:
		return "级别降级"
	case EventRecovered:
		return "已恢复"
	case EventRepeated:
		return "仍在持续"
	default:
		return r.Event
	}
}

// levelLabel 返回级别的中文标签（大写，便于在日志中一眼扫到）。
//
// 返回 Returns:
//   - label (string): 中文标签。
func (r Report) levelLabel() string {
	switch r.Level {
	case LevelCritical:
		return "CRITICAL"
	case LevelWarning:
		return "WARNING"
	case LevelOK:
		return "OK"
	default:
		return strings.ToUpper(r.Level)
	}
}

// SourceLabel 返回规则来源的中文说明。
//
// 返回 Returns:
//   - label (string): 来源说明。
func (r Report) SourceLabel() string {
	switch r.RuleSource {
	case ScopeAgent:
		return "单机覆盖"
	case ScopeRole:
		return "角色规则"
	case ScopeGlobal:
		return "全局默认"
	default:
		return r.RuleSource
	}
}

// Text 把人读文本渲染成多行字符串。
//
// 输出刻意包含「超标幅度 + 持续时长 + 进程归因 + 系统上下文」四段：告警的价值在于
// 一眼看出「谁、占了什么、持续多久、是不是 IO 瓶颈」，而不是一句「某指标超警戒值」。
//
// 返回 Returns:
//   - text (string): 多行文本（不含结尾换行）。
func (r Report) Text() string {
	var builder strings.Builder
	metric := r.Metric
	target := ""
	if metric.Target != "" {
		target = " " + metric.Target
	}
	fmt.Fprintf(&builder, "[%s] %s %s%s · %s（%s）\n",
		r.Time.Local().Format("2006-01-02 15:04:05"), r.levelLabel(), metric.Name, target, r.hostLabel(), r.eventLabel())
	fmt.Fprintf(&builder, "  指标: %s %.1f%s（命中阈值 %.1f%s，窗口 %s 内 %d/%d 点超标，已持续 %s）\n",
		metric.Name, metric.Value, metric.Unit, metric.Threshold, metric.Unit,
		formatDuration(time.Duration(metric.WindowSeconds)*time.Second),
		metric.BreachPoints, metric.TotalPoints, formatDuration(time.Duration(metric.SustainedSeconds)*time.Second))
	fmt.Fprintf(&builder, "  生效: 警告 %.1f%s / 严重 %.1f%s（来源：%s）\n",
		metric.Warning, metric.Unit, metric.Critical, metric.Unit, r.SourceLabel())
	if r.CPUDetail != nil {
		fmt.Fprintf(&builder, "  明细: user=%.1f%%  system=%.1f%%  iowait=%.1f%%\n",
			r.CPUDetail.User, r.CPUDetail.System, r.CPUDetail.IOWait)
	}
	if len(r.Attribution) > 0 {
		parts := make([]string, 0, len(r.Attribution))
		for _, item := range r.Attribution {
			parts = append(parts, fmt.Sprintf("%s %.1f%% CPU / %.0fMB", attributionName(item), item.CPUPercent, item.MemoryRSSMB))
		}
		fmt.Fprintf(&builder, "  归因: 进程 Top%d —— %s（来源：%s）\n",
			len(r.Attribution), strings.Join(parts, "；"), r.Attribution[0].Source)
	} else {
		builder.WriteString("  归因: 未取到进程级数据（可在被监控机启用 Telegraf procstat）\n")
	}
	fmt.Fprintf(&builder, "  上下文: load1=%.2f（%d 核，%.2f 倍） 内存 %.1f%%",
		r.Context.Load1, r.Context.CPUCores, r.Context.LoadPerCore, r.Context.MemoryUsedPercent)
	if r.Context.SwapUsedPercent > 0 {
		fmt.Fprintf(&builder, " swap %.1f%%", r.Context.SwapUsedPercent)
	}
	if r.Context.IOWaitPercent > 0 {
		fmt.Fprintf(&builder, " iowait %.1f%%", r.Context.IOWaitPercent)
	}
	builder.WriteString("\n")
	if len(r.Correlated) > 0 {
		fmt.Fprintf(&builder, "  关联: %s\n", strings.Join(r.Correlated, "；"))
	}
	return builder.String()
}

// hostLabel 返回「主机名（IP / ID）」形式的主机标签。
//
// 返回 Returns:
//   - label (string): 主机标签。
func (r Report) hostLabel() string {
	host := r.Agent.Hostname
	if host == "" {
		host = r.Agent.ID
	}
	extras := make([]string, 0, 2)
	if r.Agent.IP != "" {
		extras = append(extras, r.Agent.IP)
	}
	if r.Agent.ID != "" && r.Agent.ID != host {
		extras = append(extras, r.Agent.ID)
	}
	if len(extras) == 0 {
		return host
	}
	return fmt.Sprintf("%s (%s)", host, strings.Join(extras, " / "))
}

// attributionName 返回归因项的展示名（带 PID 时附加进程号）。
//
// 参数 Parameters:
//   - item (Attribution): 归因项。
//
// 返回 Returns:
//   - name (string): 展示名。
func attributionName(item Attribution) string {
	if item.PID > 0 {
		return fmt.Sprintf("%s(pid %d)", item.Process, item.PID)
	}
	return item.Process
}

// formatDuration 把时长格式化为「7m20s」这类紧凑形式。
//
// 参数 Parameters:
//   - d (time.Duration): 时长。
//
// 返回 Returns:
//   - text (string): 紧凑时长文本。
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	if minutes >= 60 {
		return fmt.Sprintf("%dh%dm", minutes/60, minutes%60)
	}
	if seconds == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}
