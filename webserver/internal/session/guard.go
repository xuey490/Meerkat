// Package session 维护被强制下线的会话（JWT 中的 sid）清单，
// 使「强退」在 access token 到期前也能立即生效。
//
// 标记先落进程内集合（同实例立即生效），Redis 可用时再写一份带 TTL 的标记，
// 供多实例共享与重启后保留。
package session

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/company/monitor-webserver/internal/cachestore"
)

// revokeKeyPrefix 是缓存中的会话失效标记前缀。
const revokeKeyPrefix = "session:revoked:"

// maxLocalEntries 是进程内标记数量上限，超出时先清理过期项。
const maxLocalEntries = 1024

// Guard 记录被强退的会话。
type Guard struct {
	// mu 保护 revoked。
	mu sync.RWMutex
	// revoked 会话 ID 到失效截止时间的映射。
	revoked map[string]time.Time
	// store 业务缓存（Redis 或进程内），可为 nil。
	store *cachestore.Store
}

// NewGuard 创建会话失效登记器。
//
// 参数 Parameters:
//   - store (*cachestore.Store): 业务缓存，可为 nil（仅进程内标记）。
//
// 返回 Returns:
//   - guard (*Guard): 会话失效登记器。
func NewGuard(store *cachestore.Store) *Guard {
	return &Guard{revoked: make(map[string]time.Time, 16), store: store}
}

// Revoke 登记会话失效。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - sid (string): 会话 ID（刷新令牌主键）。
//   - ttl (time.Duration): 标记保留时间，一般取 access token 剩余有效期；<=0 时按 1 小时处理。
func (g *Guard) Revoke(ctx context.Context, sid string, ttl time.Duration) {
	trimmed := strings.TrimSpace(sid)
	if g == nil || trimmed == "" {
		return
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	g.mu.Lock()
	if len(g.revoked) >= maxLocalEntries {
		g.pruneLocked()
	}
	g.revoked[trimmed] = time.Now().Add(ttl)
	g.mu.Unlock()
	if g.store != nil {
		_ = g.store.SetWithTTL(ctx, revokeKeyPrefix+trimmed, true, ttl)
	}
}

// Revoked 判断会话是否已被强退。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - sid (string): 会话 ID。
//
// 返回 Returns:
//   - revoked (bool): 已失效时为 true。
func (g *Guard) Revoked(ctx context.Context, sid string) bool {
	trimmed := strings.TrimSpace(sid)
	if g == nil || trimmed == "" {
		return false
	}
	g.mu.RLock()
	expireAt, exists := g.revoked[trimmed]
	g.mu.RUnlock()
	if exists {
		if time.Now().Before(expireAt) {
			return true
		}
		g.mu.Lock()
		delete(g.revoked, trimmed)
		g.mu.Unlock()
	}
	if g.store == nil {
		return false
	}
	var flag bool
	return g.store.Get(ctx, revokeKeyPrefix+trimmed, &flag) && flag
}

// pruneLocked 清理已过期的进程内标记（调用方需持有写锁）。
func (g *Guard) pruneLocked() {
	now := time.Now()
	for sid, expireAt := range g.revoked {
		if now.After(expireAt) {
			delete(g.revoked, sid)
		}
	}
}
