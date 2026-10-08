// Package cache 提供缓存管理能力：把 Redis 与进程内缓存聚合成统一的分组/键视图，
// 支持查看键值、查看 TTL 与清理缓存（单个键、整组）。
//
// 分组约定：Redis 分组名取键名前缀（首个 ":" 或 "." 及其之前的部分），
// 例如 sys_config:site_name 属于分组 sys_config:，token:user_sessions:1 属于分组 token:。
// 也可以在配置里用 redis.groups 自定义展示分组（name + pattern）。
package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/util"
)

// SourceRedis / SourceMemory 是缓存分组来源标识。
const (
	// SourceRedis 表示 Redis 中的键。
	SourceRedis = "redis"
	// SourceMemory 表示进程内缓存。
	SourceMemory = "memory"
)

// maxValueBytes 是键值详情的最大返回长度，超出部分截断。
const maxValueBytes = 64 * 1024

// MemoryEntry 是进程内缓存的一条记录。
type MemoryEntry struct {
	// Key 键名。
	Key string
	// Value 值，任意结构，展示时序列化为 JSON。
	Value any
	// ExpireAt 过期时间；nil 表示永不过期。
	ExpireAt *time.Time
}

// MemorySource 暴露一组进程内缓存，供缓存管理页展示与清理。
type MemorySource interface {
	// GroupName 分组名（键前缀）。
	GroupName() string
	// GroupLabel 分组展示名。
	GroupLabel() string
	// Entries 返回当前全部记录快照。
	Entries() []MemoryEntry
	// Delete 按键删除，返回实际删除条数。
	Delete(keys []string) int
}

// Manager 聚合 Redis 与进程内缓存两组来源。
type Manager struct {
	// redis Redis 来源；nil 表示未启用。
	redis *RedisSource
	// memory 进程内缓存来源集合。
	memory []MemorySource
}

// NewManager 创建缓存管理器。
//
// 参数 Parameters:
//   - redis (*RedisSource): Redis 来源，可为 nil。
//   - sources (...MemorySource): 进程内缓存来源。
//
// 返回 Returns:
//   - manager (*Manager): 缓存管理器。
func NewManager(redis *RedisSource, sources ...MemorySource) *Manager {
	return &Manager{redis: redis, memory: sources}
}

// RedisEnabled 返回是否配置了 Redis。
//
// 返回 Returns:
//   - enabled (bool): 是否启用 Redis。
func (m *Manager) RedisEnabled() bool { return m != nil && m.redis != nil }

// Close 释放 Redis 连接。
func (m *Manager) Close() {
	if m != nil && m.redis != nil {
		m.redis.Close()
	}
}

// Overview 返回缓存总览；Redis 未启用或不可用时通过 Message 说明原因，不返回错误。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//
// 返回 Returns:
//   - overview (dto.CacheOverview): 连接状态与统计。
func (m *Manager) Overview(ctx context.Context) dto.CacheOverview {
	overview := dto.CacheOverview{Enabled: m.RedisEnabled(), MemoryGroups: len(m.memory)}
	if !overview.Enabled {
		overview.Message = "未启用 Redis（configs/webserver.yaml 的 redis.enabled=false），当前仅展示进程内缓存"
		return overview
	}
	overview.Addr = m.redis.Addr()
	info, err := m.redis.Overview(ctx)
	if err != nil {
		overview.Message = "Redis 连接失败：" + err.Error()
		return overview
	}
	overview.Connected = true
	overview.Version = info.Version
	overview.UsedMemory = info.UsedMemory
	overview.Clients = info.Clients
	overview.TotalCommands = info.TotalCommands
	overview.HitRate = info.HitRate
	overview.Keyspace = info.Keyspace
	return overview
}

// Groups 返回全部分组（进程内缓存 + Redis）；Redis 不可用时只返回进程内缓存。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//
// 返回 Returns:
//   - groups ([]dto.CacheGroup): 分组列表。
//   - err (error): 查询失败时返回业务错误。
func (m *Manager) Groups(ctx context.Context) ([]dto.CacheGroup, error) {
	groups := make([]dto.CacheGroup, 0, len(m.memory)+8)
	for _, source := range m.memory {
		count := len(source.Entries())
		groups = append(groups, dto.CacheGroup{
			ID:        groupID(SourceMemory, -1, source.GroupName()),
			Source:    SourceMemory,
			Name:      source.GroupName(),
			Label:     source.GroupLabel(),
			DB:        -1,
			Count:     int64(count),
			Cleanable: true,
		})
	}
	if m.redis == nil {
		return groups, nil
	}
	redisGroups, err := m.redis.Groups(ctx)
	if err != nil {
		// Redis 不可用时不阻断页面：进程内缓存仍可查看，总览接口会给出失败原因。
		slog.Warn("查询 Redis 缓存分组失败", "error", err)
		return groups, nil
	}
	return append(groups, redisGroups...), nil
}

// Keys 分页查询某个分组下的缓存键。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - q (dto.CacheKeyQuery): 查询条件（分组 ID、分页、关键字）。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.CacheItem]): 分页结果。
//   - err (error): 分组无效或查询失败时返回业务错误。
func (m *Manager) Keys(ctx context.Context, q dto.CacheKeyQuery) (dto.PageResult[dto.CacheItem], error) {
	source, db, pattern, err := parseGroupID(q.Group)
	if err != nil {
		return dto.PageResult[dto.CacheItem]{}, err
	}
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	keyword := strings.TrimSpace(q.Keywords)

	if source == SourceMemory {
		items := m.memoryItems(pattern, keyword)
		total := int64(len(items))
		start := util.Offset(page, size)
		if start > len(items) {
			start = len(items)
		}
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		return dto.PageResult[dto.CacheItem]{List: items[start:end], Total: total}, nil
	}
	if m.redis == nil {
		return dto.PageResult[dto.CacheItem]{}, apperr.Invalid("未启用 Redis")
	}
	items, total, err := m.redis.Keys(ctx, db, pattern, keyword, page, size)
	if err != nil {
		return dto.PageResult[dto.CacheItem]{}, err
	}
	return dto.PageResult[dto.CacheItem]{List: items, Total: total}, nil
}

// Value 返回缓存键详情。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - groupIDText (string): 分组 ID。
//   - key (string): 键名。
//
// 返回 Returns:
//   - detail (dto.CacheDetail): 键详情。
//   - err (error): 键不存在或查询失败时返回业务错误。
func (m *Manager) Value(ctx context.Context, groupIDText, key string) (dto.CacheDetail, error) {
	source, db, pattern, err := parseGroupID(groupIDText)
	if err != nil {
		return dto.CacheDetail{}, err
	}
	if strings.TrimSpace(key) == "" {
		return dto.CacheDetail{}, apperr.Invalid("键名不能为空")
	}
	if source == SourceMemory {
		return m.memoryValue(pattern, key)
	}
	if m.redis == nil {
		return dto.CacheDetail{}, apperr.Invalid("未启用 Redis")
	}
	return m.redis.Value(ctx, db, pattern, key)
}

// ClearKeys 清理分组下的指定键。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - groupIDText (string): 分组 ID。
//   - keys ([]string): 待删除的键名。
//
// 返回 Returns:
//   - deleted (int64): 实际删除数量。
//   - err (error): 校验或执行失败时返回业务错误。
func (m *Manager) ClearKeys(ctx context.Context, groupIDText string, keys []string) (int64, error) {
	source, db, pattern, err := parseGroupID(groupIDText)
	if err != nil {
		return 0, err
	}
	cleaned := make([]string, 0, len(keys))
	for _, key := range keys {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return 0, apperr.Invalid("请选择要清理的缓存键")
	}
	if source == SourceMemory {
		// 进程内缓存按分组名定位来源，只删除本分组内的键。
		found := m.memorySource(pattern)
		if found == nil {
			return 0, apperr.Invalid("缓存分组不存在")
		}
		return int64(found.Delete(cleaned)), nil
	}
	if m.redis == nil {
		return 0, apperr.Invalid("未启用 Redis")
	}
	return m.redis.ClearKeys(ctx, db, pattern, cleaned)
}

// ClearGroup 清理整个分组。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - groupIDText (string): 分组 ID。
//
// 返回 Returns:
//   - deleted (int64): 实际删除数量。
//   - err (error): 校验或执行失败时返回业务错误。
func (m *Manager) ClearGroup(ctx context.Context, groupIDText string) (int64, error) {
	source, db, pattern, err := parseGroupID(groupIDText)
	if err != nil {
		return 0, err
	}
	if source == SourceMemory {
		found := m.memorySource(pattern)
		if found == nil {
			return 0, apperr.Invalid("缓存分组不存在")
		}
		entries := found.Entries()
		keys := make([]string, 0, len(entries))
		for _, entry := range entries {
			keys = append(keys, entry.Key)
		}
		if len(keys) == 0 {
			return 0, nil
		}
		return int64(found.Delete(keys)), nil
	}
	if m.redis == nil {
		return 0, apperr.Invalid("未启用 Redis")
	}
	return m.redis.ClearGroup(ctx, db, pattern)
}

// memoryItems 把进程内缓存记录转换为列表项。
//
// 参数 Parameters:
//   - groupName (string): 分组名（即来源的 GroupName）。
//   - keyword (string): 键名关键字。
//
// 返回 Returns:
//   - items ([]dto.CacheItem): 已排序的列表项。
func (m *Manager) memoryItems(groupName, keyword string) []dto.CacheItem {
	source := m.memorySource(groupName)
	if source == nil {
		return nil
	}
	items := make([]dto.CacheItem, 0, 16)
	for _, entry := range source.Entries() {
		if keyword != "" && !strings.Contains(entry.Key, keyword) {
			continue
		}
		items = append(items, dto.CacheItem{
			Key:  entry.Key,
			Type: SourceMemory,
			TTL:  ttlSeconds(entry.ExpireAt),
			Size: int64(len(marshalValue(entry.Value))),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items
}

// memoryValue 返回进程内缓存单条记录的详情。
//
// 参数 Parameters:
//   - groupName (string): 分组名。
//   - key (string): 键名。
//
// 返回 Returns:
//   - detail (dto.CacheDetail): 键详情。
//   - err (error): 键不存在时返回 NotFound 业务错误。
func (m *Manager) memoryValue(groupName, key string) (dto.CacheDetail, error) {
	source := m.memorySource(groupName)
	if source == nil {
		return dto.CacheDetail{}, apperr.Invalid("缓存分组不存在")
	}
	for _, entry := range source.Entries() {
		if entry.Key != key {
			continue
		}
		value := marshalValue(entry.Value)
		detail := dto.CacheDetail{
			Key:  entry.Key,
			Type: SourceMemory,
			TTL:  ttlSeconds(entry.ExpireAt),
			Size: int64(len(value)),
		}
		if len(value) > maxValueBytes {
			detail.Value = value[:maxValueBytes]
			detail.Truncated = true
			return detail, nil
		}
		detail.Value = value
		return detail, nil
	}
	return dto.CacheDetail{}, apperr.NotFound("缓存键不存在或已过期")
}

// memorySource 按分组名查找进程内缓存来源。
//
// 参数 Parameters:
//   - groupName (string): 分组名。
//
// 返回 Returns:
//   - source (MemorySource): 命中的来源；未命中返回 nil。
func (m *Manager) memorySource(groupName string) MemorySource {
	for _, source := range m.memory {
		if source.GroupName() == groupName {
			return source
		}
	}
	return nil
}

// groupID 拼接分组 ID：<source>|<db>|<pattern>。
//
// 参数 Parameters:
//   - source (string): 来源标识。
//   - db (int): Redis 库序号，进程内缓存传 -1。
//   - pattern (string): 分组名 / 键匹配模式。
//
// 返回 Returns:
//   - id (string): 分组 ID。
func groupID(source string, db int, pattern string) string {
	return source + "|" + strconv.Itoa(db) + "|" + pattern
}

// parseGroupID 解析分组 ID。
//
// 参数 Parameters:
//   - id (string): 分组 ID。
//
// 返回 Returns:
//   - source (string): 来源标识。
//   - db (int): Redis 库序号。
//   - pattern (string): 分组名 / 键匹配模式。
//   - err (error): 格式非法时返回参数错误。
func parseGroupID(id string) (string, int, string, error) {
	parts := strings.SplitN(strings.TrimSpace(id), "|", 3)
	if len(parts) != 3 || parts[0] == "" {
		return "", 0, "", apperr.Invalid("缓存分组标识无效")
	}
	db := -1
	if parts[0] == SourceRedis {
		parsed, err := strconv.Atoi(parts[1])
		if err != nil || parsed < 0 {
			return "", 0, "", apperr.Invalid("缓存分组标识无效")
		}
		db = parsed
	}
	if parts[2] == "" {
		return "", 0, "", apperr.Invalid("缓存分组标识无效")
	}
	return parts[0], db, parts[2], nil
}

// matchPattern 判断键是否匹配分组模式；模式以 * 结尾时按前缀匹配。
//
// 参数 Parameters:
//   - pattern (string): 分组模式（分组名本身或 name* 形式）。
//   - key (string): 键名。
//
// 返回 Returns:
//   - matched (bool): 是否匹配。
func matchPattern(pattern, key string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(key, strings.TrimSuffix(pattern, "*"))
	}
	return key == pattern
}

// ttlSeconds 把过期时间转换为剩余秒数：-1 表示永久。
//
// 参数 Parameters:
//   - expireAt (*time.Time): 过期时间。
//
// 返回 Returns:
//   - ttl (int64): 剩余秒数。
func ttlSeconds(expireAt *time.Time) int64 {
	if expireAt == nil {
		return -1
	}
	remaining := int64(time.Until(*expireAt).Seconds())
	if remaining < 0 {
		return -1
	}
	return remaining
}

// marshalValue 把任意值序列化为便于阅读的 JSON 文本。
//
// 参数 Parameters:
//   - value (any): 任意值。
//
// 返回 Returns:
//   - text (string): JSON 文本；序列化失败时返回占位文本。
func marshalValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "<无法序列化的值>"
	}
	return string(encoded)
}
