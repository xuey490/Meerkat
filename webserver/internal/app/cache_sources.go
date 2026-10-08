package app

import (
	"encoding/json"
	"strconv"

	"github.com/company/monitor-webserver/internal/cache"
	"github.com/company/monitor-webserver/internal/cachestore"
	"github.com/company/monitor-webserver/internal/captcha"
	"github.com/company/monitor-webserver/internal/permission"
	"github.com/company/monitor-webserver/internal/util"
)

// permissionCacheSource 把权限快照缓存适配为缓存管理模块的数据来源。
//
// 放在装配根（app）而不是 permission 包：permission 不需要反向依赖缓存管理，
// 由 app 负责把「能力」拼成「功能」。
type permissionCacheSource struct {
	// perms 权限服务。
	perms *permission.Service
}

// GroupName 返回分组名。
//
// 返回 Returns:
//   - name (string): 分组名。
func (s permissionCacheSource) GroupName() string { return "permission" }

// GroupLabel 返回分组展示名。
//
// 返回 Returns:
//   - label (string): 展示名。
func (s permissionCacheSource) GroupLabel() string {
	return "权限快照（进程内缓存，TTL 60s）"
}

// Entries 返回权限缓存快照。
//
// 返回 Returns:
//   - entries ([]cache.MemoryEntry): 缓存记录。
func (s permissionCacheSource) Entries() []cache.MemoryEntry {
	rows := s.perms.Snapshot()
	entries := make([]cache.MemoryEntry, 0, len(rows))
	for _, row := range rows {
		expireAt := row.ExpireAt
		entries = append(entries, cache.MemoryEntry{
			Key: util.TextID(row.UserID),
			Value: map[string]any{
				"userId":    util.TextID(row.UserID),
				"roles":     row.Roles,
				"permCount": row.PermCount,
				"isSuper":   row.IsSuper,
				"expireAt":  expireAt.Format("2006-01-02 15:04:05"),
			},
			ExpireAt: &expireAt,
		})
	}
	return entries
}

// Delete 按用户 ID 清理权限缓存。
//
// 参数 Parameters:
//   - keys ([]string): 用户 ID 文本列表。
//
// 返回 Returns:
//   - deleted (int): 实际清理条数。
func (s permissionCacheSource) Delete(keys []string) int {
	deleted := 0
	for _, key := range keys {
		userID, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			continue
		}
		s.perms.Invalidate(userID)
		deleted++
	}
	return deleted
}

// captchaCacheSource 把登录验证码仓储适配为缓存管理模块的数据来源。
type captchaCacheSource struct {
	// store 验证码仓储，可为 nil（验证码未启用时）。
	store *captcha.Store
}

// GroupName 返回分组名。
//
// 返回 Returns:
//   - name (string): 分组名。
func (s captchaCacheSource) GroupName() string { return "captcha" }

// GroupLabel 返回分组展示名。
//
// 返回 Returns:
//   - label (string): 展示名。
func (s captchaCacheSource) GroupLabel() string { return "登录验证码（进程内缓存）" }

// Entries 返回验证码快照。
//
// 返回 Returns:
//   - entries ([]cache.MemoryEntry): 缓存记录。
func (s captchaCacheSource) Entries() []cache.MemoryEntry {
	if s.store == nil {
		return nil
	}
	rows := s.store.Snapshot()
	entries := make([]cache.MemoryEntry, 0, len(rows))
	for _, row := range rows {
		expireAt := row.ExpiresAt
		entries = append(entries, cache.MemoryEntry{
			Key: row.ID,
			Value: map[string]any{
				"captchaId": row.ID,
				"code":      row.Text,
				"expiresAt": expireAt.Format("2006-01-02 15:04:05"),
			},
			ExpireAt: &expireAt,
		})
	}
	return entries
}

// Delete 按验证码 ID 清理缓存。
//
// 参数 Parameters:
//   - keys ([]string): 验证码 ID 列表。
//
// 返回 Returns:
//   - deleted (int): 实际清理条数。
func (s captchaCacheSource) Delete(keys []string) int {
	if s.store == nil {
		return 0
	}
	return s.store.Delete(keys)
}

// businessCacheSource 把业务缓存（字典 / 配置 / IP 归属地）适配为缓存管理来源。
//
// 仅在该缓存退化为进程内后端时挂载：走 Redis 时这些键已由 Redis 来源展示，
// 重复挂载会出现同名分组。
type businessCacheSource struct {
	// store 业务缓存。
	store *cachestore.Store
}

// GroupName 返回分组名。
//
// 返回 Returns:
//   - name (string): 分组名。
func (s businessCacheSource) GroupName() string { return "business" }

// GroupLabel 返回分组展示名。
//
// 返回 Returns:
//   - label (string): 展示名。
func (s businessCacheSource) GroupLabel() string {
	return "业务缓存（字典/配置/归属地，进程内）"
}

// Entries 返回业务缓存快照。
//
// 返回 Returns:
//   - entries ([]cache.MemoryEntry): 缓存记录。
func (s businessCacheSource) Entries() []cache.MemoryEntry {
	items := s.store.Entries()
	entries := make([]cache.MemoryEntry, 0, len(items))
	for _, item := range items {
		var value any
		if err := json.Unmarshal(item.Value, &value); err != nil {
			value = string(item.Value)
		}
		entry := cache.MemoryEntry{Key: item.Key, Value: value}
		if !item.ExpireAt.IsZero() {
			expireAt := item.ExpireAt
			entry.ExpireAt = &expireAt
		}
		entries = append(entries, entry)
	}
	return entries
}

// Delete 按完整键名清理业务缓存。
//
// 参数 Parameters:
//   - keys ([]string): 完整键名。
//
// 返回 Returns:
//   - deleted (int): 实际清理条数。
func (s businessCacheSource) Delete(keys []string) int { return s.store.DeleteKeys(keys) }

// newCacheSources 组装进程内缓存来源。
//
// 参数 Parameters:
//   - perms (*permission.Service): 权限服务，可为 nil。
//   - captchaStore (*captcha.Store): 验证码仓储，可为 nil。
//
// 返回 Returns:
//   - sources ([]cache.MemorySource): 缓存来源列表。
func newCacheSources(perms *permission.Service, captchaStore *captcha.Store) []cache.MemorySource {
	sources := make([]cache.MemorySource, 0, 2)
	if perms != nil {
		sources = append(sources, permissionCacheSource{perms: perms})
	}
	if captchaStore != nil {
		sources = append(sources, captchaCacheSource{store: captchaStore})
	}
	return sources
}
