package resourcealert

import (
	"sync"
	"time"
)

// 默认的抑制参数（规则未配置时兜底）。
const (
	// defaultCooldown 恢复后冷却时长：抑制临界值抖动导致的反复触发。
	defaultCooldown = 10 * time.Minute
	// defaultRemindInterval 警告级同级重申间隔：持续未恢复时补一条简报。
	defaultRemindInterval = 30 * time.Minute
	// defaultRemindCriticalInterval 严重级同级重申间隔：比警告级更短，尽快把人叫醒。
	defaultRemindCriticalInterval = 10 * time.Minute
)

// runtimeState 是内存中的告警运行态（按去重键维护）。
//
// 状态以内存为准、以告警表为持久化副本：进程重启后由告警表的当前状态重建，
// 因此不需要额外的状态表。
type runtimeState struct {
	// Level 当前级别：ok / warning / critical。
	Level string
	// Since 进入当前级别的时间（用于展示「已持续多久」）。
	Since time.Time
	// LastWriteAt 上次写出日志/告警的时间（重申间隔的基准）。
	LastWriteAt time.Time
	// LastFiredAt 上次触发或恢复的时间（冷却的基准）。
	LastFiredAt time.Time
}

// Machine 是资源告警的状态机，负责决定「本轮是否需要输出」以及输出什么事件。
//
// 这是控制日志量与告警噪音的核心：只有状态跃迁或达到重申间隔时才输出，
// 持续处于同一级别不会反复写日志。
type Machine struct {
	// mu 保护 states（评估在单协程内跑，加锁是为了让 API 层可安全查询）。
	mu sync.Mutex
	// states 运行态，键为去重键。
	states map[string]*runtimeState
}

// NewMachine 创建状态机。
//
// 返回 Returns:
//   - machine (*Machine): 状态机。
func NewMachine() *Machine {
	return &Machine{states: make(map[string]*runtimeState)}
}

// Restore 用持久化的告警状态重建运行态（进程启动或重启后调用）。
//
// 参数 Parameters:
//   - key (string): 去重键。
//   - level (string): 已持久化的级别。
//   - since (time.Time): 首次触发时间。
//   - lastWriteAt (time.Time): 上次写出时间。
func (m *Machine) Restore(key, level string, since, lastWriteAt time.Time) {
	if key == "" || level == "" || level == LevelOK {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[key] = &runtimeState{Level: level, Since: since, LastWriteAt: lastWriteAt, LastFiredAt: lastWriteAt}
}

// Reset 结束某个去重键当前的告警周期，并从 at 起重新进入冷却。
//
// 用途：告警被人工在告警中心确认（确认即结束）之后调用。
//
// 复位后状态为「正常 + LastFiredAt = at」，于是后续评估是：
//   - 冷却期（规则 CooldownSeconds）内即使指标仍超标也不输出 —— 这是人为处理后的收敛期；
//   - 冷却期结束后仍超标 → 输出 fired（而不是 repeated），也就是一条全新的告警周期：
//     FirstFiredAt 取新的进入时间、重申次数从 0 开始，不会叠加到刚被结束的那条记录上。
//
// 参数 Parameters:
//   - key (string): 去重键。
//   - at (time.Time): 周期结束时刻（冷却起算点）。
func (m *Machine) Reset(key string, at time.Time) {
	if m == nil || key == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[key] = &runtimeState{Level: LevelOK, Since: at, LastWriteAt: at, LastFiredAt: at}
}

// Level 返回某去重键当前的级别（不存在时返回 ok）。
//
// 参数 Parameters:
//   - key (string): 去重键。
//
// 返回 Returns:
//   - level (string): 当前级别。
func (m *Machine) Level(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.states[key]; ok {
		return state.Level
	}
	return LevelOK
}

// ForEach 遍历所有处于告警中的运行态（供统计与测试）。
//
// 参数 Parameters:
//   - fn (func(string, string)): 回调，参数为去重键与级别。
func (m *Machine) ForEach(fn func(key, level string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, state := range m.states {
		fn(key, state.Level)
	}
}

// Apply 推进状态机，返回本轮应输出的事件类型。
//
// 判定与输出的对应关系：
//   - ok    → warning / critical：fired
//   - warning → critical：upgraded；critical → warning：downgraded
//   - 任意告警 → ok：recovered
//   - 同级持续：达到重申间隔输出 repeated，否则不输出
//   - 恢复后处于冷却期内再次触发：抑制（不输出），避免临界值抖动刷屏
//
// 参数 Parameters:
//   - key (string): 去重键。
//   - level (string): 本轮判定级别（ok / warning / critical）。
//   - rule (EffectiveRule): 生效规则（提供重申间隔与冷却）。
//   - now (time.Time): 当前时间。
//
// 返回 Returns:
//   - event (string): 事件类型；空字符串表示不需要输出。
//   - sustainedSince (time.Time): 进入当前级别的时间。
func (m *Machine) Apply(key, level string, rule EffectiveRule, now time.Time) (string, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cooldown := time.Duration(rule.CooldownSeconds) * time.Second
	if cooldown <= 0 {
		cooldown = defaultCooldown
	}
	remind := remindInterval(rule, level)

	state, exists := m.states[key]
	if !exists {
		state = &runtimeState{Level: LevelOK, Since: now}
		m.states[key] = state
	}

	if level == LevelOK {
		if state.Level == LevelOK {
			return "", state.Since
		}
		state.Level = LevelOK
		state.Since = now
		state.LastWriteAt = now
		state.LastFiredAt = now
		return EventRecovered, state.Since
	}

	// 冷却期：刚恢复不久又超标，视为抖动，不输出也不改变状态（保持正常）。
	if state.Level == LevelOK {
		if !state.LastFiredAt.IsZero() && now.Sub(state.LastFiredAt) < cooldown {
			return "", state.Since
		}
		state.Level = level
		state.Since = now
		state.LastWriteAt = now
		state.LastFiredAt = now
		return EventFired, state.Since
	}

	if state.Level != level {
		event := EventUpgraded
		if level == LevelWarning {
			event = EventDowngraded
		}
		state.Level = level
		state.Since = now
		state.LastWriteAt = now
		return event, state.Since
	}

	// 同级持续：仅在达到重申间隔时输出一条简报。
	if remind > 0 && now.Sub(state.LastWriteAt) >= remind {
		state.LastWriteAt = now
		return EventRepeated, state.Since
	}
	return "", state.Since
}

// remindInterval 返回当前级别应使用的重申间隔。
//
// 严重级用 RemindIntervalCriticalSeconds、其余级别用 RemindIntervalSeconds，
// 于是可以做到「警告 30 分钟提醒一次、严重 5 分钟提醒一次」，而不必二选一。
// 两者语义一致：0 表示不重申，负值（未配置）回退到内置默认值。
//
// 参数 Parameters:
//   - rule (EffectiveRule): 生效规则。
//   - level (string): 本轮判定级别。
//
// 返回 Returns:
//   - interval (time.Duration): 重申间隔；0 表示不重申。
func remindInterval(rule EffectiveRule, level string) time.Duration {
	seconds := rule.RemindIntervalSeconds
	fallback := defaultRemindInterval
	if level == LevelCritical {
		seconds = rule.RemindIntervalCriticalSeconds
		fallback = defaultRemindCriticalInterval
	}
	switch {
	case seconds == 0:
		return 0
	case seconds < 0:
		return fallback
	default:
		return time.Duration(seconds) * time.Second
	}
}
