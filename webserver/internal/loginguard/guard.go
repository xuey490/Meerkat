// Package loginguard 维护登录失败计数与「账号 + 来源 IP」双维度临时锁定。
//
// 设计要点：
//   - 计数与锁定状态同源：失败计数键自身的 TTL 就是锁定时长，计数达到阈值即为锁定，
//     TTL 到期自动解锁，不需要任何巡检/清理任务；
//   - 后端复用业务缓存（cachestore）：Redis 可用时多实例共享、重启后仍有效；
//     未启用 Redis 时退化为进程内缓存，功能正确（单实例生效）；
//   - 键名前缀 login:fail: 便于在「系统管理 → 缓存管理」里按分组查看剩余 TTL 或手工解锁。
package loginguard

import (
	"context"
	"strings"
	"time"

	"github.com/company/monitor-webserver/internal/cachestore"
)

// 缓存键前缀。
const (
	// accountKeyPrefix 账号维度失败计数。
	accountKeyPrefix = "login:fail:account:"
	// ipKeyPrefix 来源 IP 维度失败计数。
	ipKeyPrefix = "login:fail:ip:"
	// defaultPrefix 未显式指定缓存前缀时的兜底前缀。
	defaultPrefix = "monitor:"
)

// 锁定作用域。
const (
	// ScopeNone 未锁定。
	ScopeNone = ""
	// ScopeAccount 账号维度锁定。
	ScopeAccount = "account"
	// ScopeIP 来源 IP 维度锁定。
	ScopeIP = "ip"
)

// Options 是登录失败锁定的运行参数。
type Options struct {
	// Limit 连续失败阈值；<=0 表示关闭登录失败锁定。
	Limit int
	// Lockout 锁定时长，同时作为计数键的 TTL。
	Lockout time.Duration
	// LockIP 是否在达到阈值时同时锁定来源 IP。
	//
	// 关闭后只锁账号：适合同一出口 IP（NAT / 办公网）承载大量合法用户的部署。
	LockIP bool
}

// Status 是一次锁定检查的结果。
type Status struct {
	// Attempts 已累计的失败次数。
	Attempts int
	// Limit 失败阈值。
	Limit int
	// Locked 是否处于锁定状态。
	Locked bool
	// Scope 锁定作用域：account / ip；未锁定时为空。
	Scope string
	// RetryAfter 距自动解锁的剩余时间；未锁定时为 0。
	RetryAfter time.Duration
}

// RemainingAttempts 返回锁定前还剩多少次尝试机会。
//
// 返回 Returns:
//   - remaining (int): 剩余次数，最小为 0。
func (s Status) RemainingAttempts() int {
	if s.Limit <= 0 {
		return 0
	}
	if s.Attempts >= s.Limit {
		return 0
	}
	return s.Limit - s.Attempts
}

// Guard 提供账号与来源 IP 的登录失败锁定。
type Guard struct {
	// store 计数与锁定状态存储（Redis 或进程内缓存）。
	store *cachestore.Store
	// opts 运行参数。
	opts Options
}

// New 创建登录锁定守卫。
//
// 参数 Parameters:
//   - store (*cachestore.Store): 计数存储；为 nil 时使用进程内后端。
//   - opts (Options): 阈值、锁定时长与 IP 锁定开关。
//
// 返回 Returns:
//   - guard (*Guard): 登录锁定守卫。
func New(store *cachestore.Store, opts Options) *Guard {
	if store == nil {
		// 未装配业务缓存（cache.enabled=false）时退化到进程内后端，保证功能可用。
		store = cachestore.New(nil, defaultPrefix, opts.Lockout)
	}
	return &Guard{store: store, opts: opts}
}

// Enabled 返回是否启用登录失败锁定。
//
// 返回 Returns:
//   - enabled (bool): 阈值与锁定时长都有效时为 true。
func (g *Guard) Enabled() bool {
	return g != nil && g.opts.Limit > 0 && g.opts.Lockout > 0
}

// LockoutMinutes 返回锁定时长（分钟），供前端提示「请 N 分钟后重试」。
//
// 返回 Returns:
//   - minutes (int): 锁定时长分钟数。
func (g *Guard) LockoutMinutes() int {
	if g == nil || g.opts.Lockout <= 0 {
		return 0
	}
	return int(g.opts.Lockout / time.Minute)
}

// Inspect 检查账号与来源 IP 的当前锁定状态（只读，不产生副作用）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - username (string): 登录账号。
//   - ip (string): 来源 IP。
//
// 返回 Returns:
//   - status (Status): 锁定状态。
func (g *Guard) Inspect(ctx context.Context, username, ip string) Status {
	status := Status{Limit: g.limit()}
	if !g.Enabled() {
		return status
	}
	// 账号维度优先：锁定时提示更贴近用户输入（同一 IP 上换账号名仍会被 IP 维度拦住）。
	accountAttempts, accountLeft := g.count(ctx, accountKey(username))
	if accountAttempts >= g.opts.Limit {
		status.Attempts, status.Locked, status.Scope, status.RetryAfter =
			accountAttempts, true, ScopeAccount, accountLeft
		return status
	}
	ipAttempts := 0
	if g.opts.LockIP {
		var ipLeft time.Duration
		ipAttempts, ipLeft = g.count(ctx, ipKey(ip))
		if ipAttempts >= g.opts.Limit {
			status.Attempts, status.Locked, status.Scope, status.RetryAfter =
				ipAttempts, true, ScopeIP, ipLeft
			return status
		}
	}
	status.Attempts = max(accountAttempts, ipAttempts)
	return status
}

// Failure 记录一次登录失败，并在达到阈值时进入锁定状态。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - username (string): 登录账号。
//   - ip (string): 来源 IP。
//
// 返回 Returns:
//   - status (Status): 累加后的失败次数与锁定状态。
func (g *Guard) Failure(ctx context.Context, username, ip string) Status {
	if !g.Enabled() {
		return Status{Limit: g.limit()}
	}
	g.bump(ctx, accountKey(username))
	if g.opts.LockIP {
		g.bump(ctx, ipKey(ip))
	}
	return g.Inspect(ctx, username, ip)
}

// Reset 清零账号维度的失败计数（登录成功后调用）。
//
// 来源 IP 维度的计数故意不清零：否则攻击者只要掌握任意一个可用账号，
// 就能用「错 4 次 + 对 1 次」的节奏反复重置 IP 计数，绕过 IP 锁定。
// 该计数只随 TTL 自动衰减，白名单 IP 另由上层豁免。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - username (string): 登录账号。
func (g *Guard) Reset(ctx context.Context, username string) {
	if g == nil {
		return
	}
	if key := accountKey(username); key != "" {
		_ = g.store.Delete(ctx, key)
	}
}

// limit 返回失败阈值（守卫为 nil 时返回 0）。
func (g *Guard) limit() int {
	if g == nil {
		return 0
	}
	return g.opts.Limit
}

// count 读取某个键的计数与剩余 TTL。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - key (string): 缓存键；为空表示该维度不参与统计。
//
// 返回 Returns:
//   - attempts (int): 失败次数。
//   - remaining (time.Duration): 剩余 TTL（键不存在时为 0）。
func (g *Guard) count(ctx context.Context, key string) (int, time.Duration) {
	if key == "" {
		return 0, 0
	}
	var value int64
	if !g.store.Get(ctx, key, &value) || value <= 0 {
		return 0, 0
	}
	return int(value), g.store.TTL(ctx, key)
}

// bump 自增某个键的计数并刷新 TTL。存储故障时返回 0（fail-open，不误锁合法用户）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - key (string): 缓存键；为空表示该维度不参与统计。
//
// 返回 Returns:
//   - attempts (int): 自增后的失败次数。
func (g *Guard) bump(ctx context.Context, key string) int {
	if key == "" {
		return 0
	}
	value, err := g.store.Incr(ctx, key, g.opts.Lockout)
	if err != nil {
		return 0
	}
	return int(value)
}

// accountKey 生成账号维度缓存键（账号名统一转小写，避免大小写绕过）。
//
// 参数 Parameters:
//   - username (string): 登录账号。
//
// 返回 Returns:
//   - key (string): 缓存键；账号为空时返回空串。
func accountKey(username string) string {
	name := strings.ToLower(strings.TrimSpace(username))
	if name == "" {
		return ""
	}
	return accountKeyPrefix + name
}

// ipKey 生成来源 IP 维度缓存键。
//
// 参数 Parameters:
//   - ip (string): 来源 IP。
//
// 返回 Returns:
//   - key (string): 缓存键；IP 为空时返回空串。
func ipKey(ip string) string {
	value := strings.TrimSpace(ip)
	if value == "" {
		return ""
	}
	return ipKeyPrefix + value
}
