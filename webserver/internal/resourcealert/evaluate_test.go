package resourcealert

import (
	"testing"
	"time"
)

// makeWindow 按固定步长构造窗口序列（值为原始字段值）。
func makeWindow(agentID string, values []float64, start time.Time, step time.Duration) MetricWindow {
	points := make([]WindowPoint, 0, len(values))
	for i, value := range values {
		points = append(points, WindowPoint{At: start.Add(time.Duration(i) * step), Value: value})
	}
	return MetricWindow{AgentID: agentID, Points: points}
}

// cpuRule 返回一个 CPU 规则（警告 85 / 严重 95 / 占比 0.9）。
func cpuRule() EffectiveRule {
	return EffectiveRule{
		AlertRule: AlertRule{
			MetricKey: MetricCPUUsage, WarningThreshold: 85, CriticalThreshold: 95,
			ForSeconds: 300, BreachRatio: 0.9,
			RemindIntervalSeconds: 1800, RemindIntervalCriticalSeconds: 1800, CooldownSeconds: 600,
		},
		Source: ScopeGlobal,
	}
}

func TestConvertValue口径(t *testing.T) {
	cases := []struct {
		metric string
		raw    float64
		cores  float64
		want   float64
	}{
		{MetricCPUUsage, 2, 4, 98},    // 100 - usage_idle
		{MetricMemUsage, 8, 4, 92},    // 100 - available_percent
		{MetricLoadPerCore, 20, 4, 5}, // load1 / 核数
		{MetricLoadPerCore, 4, 0, 4},  // 核数缺失时按 1 处理，避免除零
		{MetricDiskUsage, 93, 4, 93},  // 磁盘不需要换算
	}
	for _, c := range cases {
		if got := ConvertValue(c.metric, c.raw, c.cores); got != c.want {
			t.Errorf("%s raw=%v cores=%v: 期望 %v，实际 %v", c.metric, c.raw, c.cores, c.want, got)
		}
	}
}

func TestJudge持续超标判为严重(t *testing.T) {
	step := time.Minute
	window := makeWindow("a1", []float64{2, 2, 2, 2, 2, 2, 2, 2, 2, 2}, time.Now().Add(-10*step), step)
	verdict := Judge(window, cpuRule(), 4, QuerySpec{MetricKey: MetricCPUUsage, Window: 10 * time.Minute, Step: step})

	if verdict.Level != LevelCritical {
		t.Fatalf("期望 critical，实际 %s", verdict.Level)
	}
	if verdict.Value != 98 {
		t.Errorf("期望利用率 98，实际 %v", verdict.Value)
	}
	if verdict.BreachRatio != 1 {
		t.Errorf("期望超标占比 1，实际 %v", verdict.BreachRatio)
	}
	if verdict.SustainedSeconds != 9*60 {
		t.Errorf("期望持续 540 秒，实际 %d", verdict.SustainedSeconds)
	}
}

func TestJudge抖动不触发(t *testing.T) {
	step := time.Minute
	// 只有最后一个点超标：占比 10% < 90%，属于抖动，不应触发。
	window := makeWindow("a1", []float64{90, 90, 90, 90, 90, 90, 90, 90, 90, 1}, time.Now().Add(-10*step), step)
	verdict := Judge(window, cpuRule(), 4, QuerySpec{MetricKey: MetricCPUUsage, Window: 10 * time.Minute, Step: step})
	if verdict.Level != LevelOK {
		t.Fatalf("抖动场景不应触发，实际 %s", verdict.Level)
	}
}

func TestJudge已恢复不触发(t *testing.T) {
	step := time.Minute
	// 前半段严重超标、后半段恢复：末尾点正常 → 判为 ok。
	window := makeWindow("a1", []float64{2, 2, 2, 2, 2, 90, 90, 90, 90, 90}, time.Now().Add(-10*step), step)
	verdict := Judge(window, cpuRule(), 4, QuerySpec{MetricKey: MetricCPUUsage, Window: 10 * time.Minute, Step: step})
	if verdict.Level != LevelOK {
		t.Fatalf("已恢复场景应判为 ok，实际 %s", verdict.Level)
	}
	if verdict.SustainedSeconds != 0 {
		t.Errorf("已恢复时持续时长应为 0，实际 %d", verdict.SustainedSeconds)
	}
}

func TestJudge警告级别(t *testing.T) {
	step := time.Minute
	// 利用率 88%（usage_idle=12）：越过警告 85 但未到严重 95。
	window := makeWindow("a1", []float64{12, 12, 12, 12, 12, 12, 12, 12, 12, 12}, time.Now().Add(-10*step), step)
	verdict := Judge(window, cpuRule(), 4, QuerySpec{MetricKey: MetricCPUUsage, Window: 10 * time.Minute, Step: step})
	if verdict.Level != LevelWarning {
		t.Fatalf("期望 warning，实际 %s", verdict.Level)
	}
	if verdict.Threshold != 85 {
		t.Errorf("期望命中警告阈值 85，实际 %v", verdict.Threshold)
	}
}

func TestJudge无数据不下结论(t *testing.T) {
	verdict := Judge(MetricWindow{AgentID: "a1"}, cpuRule(), 4, QuerySpec{MetricKey: MetricCPUUsage, Window: 10 * time.Minute, Step: time.Minute})
	if verdict.Level != LevelOK || verdict.TotalPoints != 0 {
		t.Fatalf("无数据应为 ok 且点数为 0，实际 level=%s points=%d", verdict.Level, verdict.TotalPoints)
	}
}

func TestJudge负载按核数归一化(t *testing.T) {
	step := time.Minute
	// load1 = 20，4 核 → 5 倍；规则警告 1.2 / 严重 2.0。
	rule := EffectiveRule{AlertRule: AlertRule{
		MetricKey: MetricLoadPerCore, WarningThreshold: 1.2, CriticalThreshold: 2.0,
		ForSeconds: 600, BreachRatio: 0.8, RemindIntervalSeconds: 1800, CooldownSeconds: 600,
	}}
	window := makeWindow("a1", []float64{20, 20, 20, 20, 20, 20, 20, 20, 20, 20}, time.Now().Add(-10*step), step)
	verdict := Judge(window, rule, 4, QuerySpec{MetricKey: MetricLoadPerCore, Window: 10 * time.Minute, Step: step})
	if verdict.Level != LevelCritical {
		t.Fatalf("期望 critical，实际 %s", verdict.Level)
	}
	if verdict.Value != 5 {
		t.Errorf("期望归一化后 5 倍，实际 %v", verdict.Value)
	}
}

func TestMachine状态跃迁与抑制(t *testing.T) {
	machine := NewMachine()
	rule := cpuRule()
	now := time.Now()

	if event, _ := machine.Apply("k", LevelOK, rule, now); event != "" {
		t.Fatalf("首次正常不应输出，实际 %q", event)
	}
	if event, _ := machine.Apply("k", LevelCritical, rule, now); event != EventFired {
		t.Fatalf("正常→严重应输出 fired，实际 %q", event)
	}
	// 同级持续：未到重申间隔（30 分钟）不输出。
	if event, _ := machine.Apply("k", LevelCritical, rule, now.Add(time.Minute)); event != "" {
		t.Fatalf("同级持续不应输出，实际 %q", event)
	}
	// 达到重申间隔输出 repeated。
	if event, _ := machine.Apply("k", LevelCritical, rule, now.Add(31*time.Minute)); event != EventRepeated {
		t.Fatalf("达到重申间隔应输出 repeated，实际 %q", event)
	}
	// 严重→警告为降级。
	if event, _ := machine.Apply("k", LevelWarning, rule, now.Add(32*time.Minute)); event != EventDowngraded {
		t.Fatalf("严重→警告应输出 downgraded，实际 %q", event)
	}
	// 恢复。
	if event, _ := machine.Apply("k", LevelOK, rule, now.Add(33*time.Minute)); event != EventRecovered {
		t.Fatalf("恢复应输出 recovered，实际 %q", event)
	}
	// 冷却期（10 分钟）内再次触发：抑制，避免抖动刷屏。
	if event, _ := machine.Apply("k", LevelCritical, rule, now.Add(34*time.Minute)); event != "" {
		t.Fatalf("冷却期内应抑制，实际 %q", event)
	}
	// 冷却期结束后可以重新触发。
	if event, _ := machine.Apply("k", LevelCritical, rule, now.Add(45*time.Minute)); event != EventFired {
		t.Fatalf("冷却期后应重新触发，实际 %q", event)
	}
}

func TestMachine重申可关闭(t *testing.T) {
	machine := NewMachine()
	rule := cpuRule()
	// 分级重申：要完全关闭重申，两个级别都要设为 0（0 表示不重申）。
	rule.RemindIntervalSeconds = 0
	rule.RemindIntervalCriticalSeconds = 0
	now := time.Now()
	machine.Apply("k", LevelCritical, rule, now)
	if event, _ := machine.Apply("k", LevelCritical, rule, now.Add(24*time.Hour)); event != "" {
		t.Fatalf("重申关闭后不应输出，实际 %q", event)
	}
}

func TestResolveEffective三层覆盖(t *testing.T) {
	rules := []AlertRule{
		{MetricKey: MetricCPUUsage, Scope: ScopeGlobal, WarningThreshold: 85, Target: ""},
		{MetricKey: MetricMemUsage, Scope: ScopeGlobal, WarningThreshold: 85, Target: ""},
		{MetricKey: MetricMemUsage, Scope: ScopeRole, Target: "db", WarningThreshold: 90},
		{MetricKey: MetricCPUUsage, Scope: ScopeAgent, Target: "a1", WarningThreshold: 70},
		// 角色不匹配（web ≠ db）的规则不应命中。
		{MetricKey: MetricDiskUsage, Scope: ScopeRole, Target: "web", WarningThreshold: 80},
	}
	got := ResolveEffective(rules, "a1", "db")

	if rule, ok := got[MetricCPUUsage]; !ok || rule.Source != ScopeAgent || rule.WarningThreshold != 70 {
		t.Errorf("CPU 应命中单机覆盖（agent/70），实际 %+v", rule)
	}
	if rule, ok := got[MetricMemUsage]; !ok || rule.Source != ScopeRole || rule.WarningThreshold != 90 {
		t.Errorf("内存应命中角色规则（role/90），实际 %+v", rule)
	}
	if _, ok := got[MetricDiskUsage]; ok {
		t.Error("磁盘规则角色不匹配，不应命中")
	}
}

func TestResolveEffective空角色不命中角色层(t *testing.T) {
	rules := []AlertRule{{MetricKey: MetricCPUUsage, Scope: ScopeRole, Target: "", WarningThreshold: 90}}
	if _, ok := ResolveEffective(rules, "a1", "")[MetricCPUUsage]; ok {
		t.Error("主机角色为空时，角色层规则（target 为空）不应命中")
	}
}
