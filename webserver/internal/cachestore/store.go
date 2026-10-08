// Package cachestore 提供业务侧键值缓存：Redis 可用时写 Redis，
// 否则退化为进程内 TTL 缓存，保证「有没有 Redis」都不影响功能正确性。
//
// 用途：字典、系统配置、IP 归属地等热点数据的读缓存；写操作后按前缀失效。
package cachestore

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// 后端标识。
const (
	// BackendRedis 使用 Redis。
	BackendRedis = "redis"
	// BackendMemory 使用进程内缓存。
	BackendMemory = "memory"
)

// Item 是进程内缓存的一条记录（供缓存管理页展示）。
type Item struct {
	// Key 完整键名（含前缀）。
	Key string
	// Value 序列化后的值。
	Value []byte
	// ExpireAt 过期时间；零值表示永不过期。
	ExpireAt time.Time
}

// entry 是进程内缓存的内部记录。
type entry struct {
	// value 序列化后的值。
	value []byte
	// expireAt 过期时间；零值表示永不过期。
	expireAt time.Time
}

// Store 是业务缓存。
type Store struct {
	// client Redis 客户端；为 nil 时使用进程内缓存。
	client *redis.Client
	// prefix 键前缀，便于在缓存管理页按分组浏览与批量失效。
	prefix string
	// ttl 默认过期时间（秒），<=0 表示永不过期。
	ttl time.Duration
	// mu 保护 items。
	mu sync.RWMutex
	// items 进程内缓存。
	items map[string]entry
}

// New 创建业务缓存。
//
// 参数 Parameters:
//   - client (*redis.Client): Redis 客户端，可为 nil（退化为进程内缓存）。
//   - prefix (string): 键前缀，如 monitor:；为空时按 monitor: 处理。
//   - ttl (time.Duration): 默认过期时间，<=0 表示永不过期。
//
// 返回 Returns:
//   - store (*Store): 业务缓存。
func New(client *redis.Client, prefix string, ttl time.Duration) *Store {
	normalized := strings.TrimSpace(prefix)
	if normalized == "" {
		normalized = "monitor:"
	}
	if !strings.HasSuffix(normalized, ":") {
		normalized += ":"
	}
	return &Store{client: client, prefix: normalized, ttl: ttl, items: make(map[string]entry, 32)}
}

// Backend 返回当前后端类型（redis / memory）。
//
// 返回 Returns:
//   - backend (string): 后端标识。
func (s *Store) Backend() string {
	if s == nil || s.client == nil {
		return BackendMemory
	}
	return BackendRedis
}

// Prefix 返回键前缀。
//
// 返回 Returns:
//   - prefix (string): 键前缀。
func (s *Store) Prefix() string {
	if s == nil {
		return ""
	}
	return s.prefix
}

// fullKey 拼接完整键名。
//
// 参数 Parameters:
//   - key (string): 业务键。
//
// 返回 Returns:
//   - fullKey (string): 带前缀的键名。
func (s *Store) fullKey(key string) string {
	trimmed := strings.TrimLeft(strings.TrimSpace(key), ":")
	return s.prefix + trimmed
}

// Get 读取缓存并反序列化到 dest，命中返回 true。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - key (string): 业务键。
//   - dest (any): 反序列化目标（指针）。
//
// 返回 Returns:
//   - hit (bool): 是否命中。
func (s *Store) Get(ctx context.Context, key string, dest any) bool {
	if s == nil || dest == nil {
		return false
	}
	full := s.fullKey(key)
	var raw []byte
	if s.client != nil {
		value, err := s.client.Get(ctx, full).Bytes()
		if err != nil {
			return false
		}
		raw = value
	} else {
		s.mu.RLock()
		record, ok := s.items[full]
		s.mu.RUnlock()
		if !ok {
			return false
		}
		if !record.expireAt.IsZero() && time.Now().After(record.expireAt) {
			s.mu.Lock()
			delete(s.items, full)
			s.mu.Unlock()
			return false
		}
		raw = record.value
	}
	return json.Unmarshal(raw, dest) == nil
}

// Set 写入缓存（使用默认 TTL）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - key (string): 业务键。
//   - value (any): 待缓存的值。
//
// 返回 Returns:
//   - err (error): 序列化或写入失败时返回错误（调用方通常忽略，仅作为降级信号）。
func (s *Store) Set(ctx context.Context, key string, value any) error {
	return s.SetWithTTL(ctx, key, value, s.ttl)
}

// SetWithTTL 按指定 TTL 写入缓存。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - key (string): 业务键。
//   - value (any): 待缓存的值。
//   - ttl (time.Duration): 过期时间，<=0 时使用默认 TTL。
//
// 返回 Returns:
//   - err (error): 序列化或写入失败时返回错误。
func (s *Store) SetWithTTL(ctx context.Context, key string, value any, ttl time.Duration) error {
	if s == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = s.ttl
	}
	full := s.fullKey(key)
	if s.client != nil {
		return s.client.Set(ctx, full, raw, ttl).Err()
	}
	record := entry{value: raw}
	if ttl > 0 {
		record.expireAt = time.Now().Add(ttl)
	}
	s.mu.Lock()
	s.items[full] = record
	s.mu.Unlock()
	return nil
}

// Delete 删除指定键。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - keys (...string): 业务键。
//
// 返回 Returns:
//   - err (error): 删除失败时返回错误。
func (s *Store) Delete(ctx context.Context, keys ...string) error {
	if s == nil || len(keys) == 0 {
		return nil
	}
	full := make([]string, 0, len(keys))
	for _, key := range keys {
		full = append(full, s.fullKey(key))
	}
	if s.client != nil {
		return s.client.Del(ctx, full...).Err()
	}
	s.mu.Lock()
	for _, key := range full {
		delete(s.items, key)
	}
	s.mu.Unlock()
	return nil
}

// DeletePrefix 删除某前缀下的全部键（用于字典/配置整体失效），返回删除数量。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - keyPrefix (string): 业务键前缀（不含全局前缀）。
//
// 返回 Returns:
//   - deleted (int64): 删除数量。
//   - err (error): 删除失败时返回错误。
func (s *Store) DeletePrefix(ctx context.Context, keyPrefix string) (int64, error) {
	if s == nil {
		return 0, nil
	}
	full := s.fullKey(keyPrefix)
	if s.client != nil {
		// SCAN 分批删除，避免 KEYS 阻塞 Redis。
		var deleted int64
		var cursor uint64
		for {
			keys, next, err := s.client.Scan(ctx, cursor, full+"*", 200).Result()
			if err != nil {
				return deleted, err
			}
			if len(keys) > 0 {
				count, err := s.client.Del(ctx, keys...).Result()
				if err != nil {
					return deleted, err
				}
				deleted += count
			}
			cursor = next
			if cursor == 0 {
				return deleted, nil
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var deleted int64
	for key := range s.items {
		if strings.HasPrefix(key, full) {
			delete(s.items, key)
			deleted++
		}
	}
	return deleted, nil
}

// Entries 返回进程内缓存快照（Redis 后端时返回空）。
//
// 返回 Returns:
//   - items ([]Item): 缓存记录。
func (s *Store) Entries() []Item {
	if s == nil || s.client != nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	items := make([]Item, 0, len(s.items))
	for key, record := range s.items {
		if !record.expireAt.IsZero() && now.After(record.expireAt) {
			continue
		}
		items = append(items, Item{Key: key, Value: record.value, ExpireAt: record.expireAt})
	}
	return items
}

// DeleteKeys 按完整键名删除进程内缓存记录（供缓存管理页清理），返回删除数量。
//
// 参数 Parameters:
//   - keys ([]string): 完整键名。
//
// 返回 Returns:
//   - deleted (int): 删除数量。
func (s *Store) DeleteKeys(keys []string) int {
	if s == nil || s.client != nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for _, key := range keys {
		if _, exists := s.items[key]; exists {
			delete(s.items, key)
			deleted++
		}
	}
	return deleted
}
