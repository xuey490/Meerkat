package resourcealert

import (
	"testing"
	"time"
)

// TestRemindIntervalByLevel 锁住「分级重申」：严重级用严重间隔，其余级别用警告间隔。
//
// 这是「警告 30 分钟提醒一次、严重 5 分钟提醒一次」的基础，两个级别必须互相独立，
// 否则把严重级调快会连带把警告级也调快。
func TestRemindIntervalByLevel(t *testing.T) {
	rule := EffectiveRule{AlertRule: AlertRule{
		RemindIntervalSeconds: 1800, RemindIntervalCriticalSeconds: 300,
	}}
	if got := remindInterval(rule, LevelWarning); got != 30*time.Minute {
		t.Errorf("警告级应为 30 分钟，实际 %v", got)
	}
	if got := remindInterval(rule, LevelCritical); got != 5*time.Minute {
		t.Errorf("严重级应为 5 分钟，实际 %v", got)
	}

	zeroWarn := EffectiveRule{AlertRule: AlertRule{
		RemindIntervalSeconds: 0, RemindIntervalCriticalSeconds: 300,
	}}
	if got := remindInterval(zeroWarn, LevelWarning); got != 0 {
		t.Errorf("警告级 0 应表示不重申，实际 %v", got)
	}
	if got := remindInterval(zeroWarn, LevelCritical); got != 5*time.Minute {
		t.Errorf("严重级不应受警告级 0 影响，实际 %v", got)
	}

	zeroCritical := EffectiveRule{AlertRule: AlertRule{
		RemindIntervalSeconds: 1800, RemindIntervalCriticalSeconds: 0,
	}}
	if got := remindInterval(zeroCritical, LevelCritical); got != 0 {
		t.Errorf("严重级 0 应表示不重申，实际 %v", got)
	}

	unset := EffectiveRule{AlertRule: AlertRule{
		RemindIntervalSeconds: -1, RemindIntervalCriticalSeconds: -1,
	}}
	if got := remindInterval(unset, LevelWarning); got != defaultRemindInterval {
		t.Errorf("警告级未配置应回退内置默认 %v，实际 %v", defaultRemindInterval, got)
	}
	if got := remindInterval(unset, LevelCritical); got != defaultRemindCriticalInterval {
		t.Errorf("严重级未配置应回退内置默认 %v，实际 %v", defaultRemindCriticalInterval, got)
	}
}

// TestResetStartsNewCycleAfterCooldown 锁住「人工确认后重新超标 = 全新告警周期」。
//
// 背景：确认只关数据库那一行是不够的——状态机若不复位，指标没恢复时它只会在重申间隔
// 到达后再输出 repeated，新记录于是沿用了旧周期的首触发时间（看起来像叠加在已处理的
// 记录上）。复位后必须满足：冷却期内静默、冷却期后输出 fired 且时间从零起算。
func TestResetStartsNewCycleAfterCooldown(t *testing.T) {
	machine := NewMachine()
	rule := EffectiveRule{AlertRule: AlertRule{
		RemindIntervalSeconds: 1800, RemindIntervalCriticalSeconds: 600, CooldownSeconds: 600,
	}}
	key := "cpu_usage:agent:a"
	base := time.Date(2026, 10, 12, 10, 0, 0, 0, time.Local)

	if event, _ := machine.Apply(key, LevelCritical, rule, base); event != EventFired {
		t.Fatalf("首次判定应为 fired，实际 %q", event)
	}

	// 操作者在 5 分钟后确认并结束该告警。
	ackedAt := base.Add(5 * time.Minute)
	machine.Reset(key, ackedAt)
	if level := machine.Level(key); level != LevelOK {
		t.Fatalf("确认后状态应复位为正常，实际 %q", level)
	}

	// 冷却期（10 分钟）内指标仍超标：静默，不能立刻再报。
	if event, _ := machine.Apply(key, LevelCritical, rule, ackedAt.Add(9*time.Minute)); event != "" {
		t.Errorf("冷却期内应静默，实际 %q", event)
	}
	// 冷却期结束后仍超标：必须是 fired（全新周期），而不是沿用旧周期的 repeated。
	reFiredAt := ackedAt.Add(11 * time.Minute)
	event, since := machine.Apply(key, LevelCritical, rule, reFiredAt)
	if event != EventFired {
		t.Fatalf("冷却期后应重新触发（fired），实际 %q", event)
	}
	if !since.Equal(reFiredAt) {
		t.Errorf("新周期的起点应为重新触发时刻 %v，实际 %v", reFiredAt, since)
	}
}

// TestResetIsIdempotentForUnknownKey 确保对一个从未告警过的去重键复位不会出错。
func TestResetIsIdempotentForUnknownKey(t *testing.T) {
	machine := NewMachine()
	machine.Reset("mem_usage:agent:unknown", time.Now())
	if level := machine.Level("mem_usage:agent:unknown"); level != LevelOK {
		t.Fatalf("未告警过的键复位后仍应为正常，实际 %q", level)
	}
}

// TestApplyUsesCriticalRemindInterval 验证状态机确实按级别取间隔，而不只是函数算对了。
func TestApplyUsesCriticalRemindInterval(t *testing.T) {
	machine := NewMachine()
	rule := EffectiveRule{AlertRule: AlertRule{
		RemindIntervalSeconds: 1800, RemindIntervalCriticalSeconds: 60, CooldownSeconds: 600,
	}}
	base := time.Date(2026, 10, 9, 10, 0, 0, 0, time.Local)

	if event, _ := machine.Apply("cpu_usage:agent:a", LevelCritical, rule, base); event != EventFired {
		t.Fatalf("首次判定应为 fired，实际 %q", event)
	}
	// 严重级间隔 60 秒：2 分钟后必须重申（若误用警告级的 30 分钟则不会）。
	if event, _ := machine.Apply("cpu_usage:agent:a", LevelCritical, rule, base.Add(2*time.Minute)); event != EventRepeated {
		t.Errorf("严重级应在 2 分钟后重申，实际 %q", event)
	}

	// 警告级仍按 30 分钟：2 分钟后不该重申。
	if event, _ := machine.Apply("mem_usage:agent:a", LevelWarning, rule, base); event != EventFired {
		t.Fatalf("首次判定应为 fired，实际 %q", event)
	}
	if event, _ := machine.Apply("mem_usage:agent:a", LevelWarning, rule, base.Add(2*time.Minute)); event != "" {
		t.Errorf("警告级 2 分钟内不应重申，实际 %q", event)
	}
}
