package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/util"
	"github.com/company/monitor-webserver/pkg/redisx"
)

// groupSeparators 是自动分组的键名分隔符：键名中首个分隔符（含）之前的部分作为分组名。
var groupSeparators = []string{":", "."}

// valuePreviewLimit 是集合类值最多读取的元素个数（list/set/zset）。
const valuePreviewLimit = 1000

// deleteBatchSize 是清理缓存时的分批删除大小。
const deleteBatchSize = 200

// CustomGroup 是配置里声明的自定义分组。
type CustomGroup struct {
	// Name 展示名。
	Name string
	// Pattern 键匹配模式，如 token:*。
	Pattern string
}

// redisOverview 是 Redis INFO 摘要。
type redisOverview struct {
	// Version Redis 版本。
	Version string
	// UsedMemory 已用内存（可读文本）。
	UsedMemory string
	// Clients 当前连接数。
	Clients int64
	// TotalCommands 已处理命令数。
	TotalCommands int64
	// HitRate 命中率文本。
	HitRate string
	// Keyspace 各库统计。
	Keyspace []dto.CacheKeyspace
}

// RedisSource 提供基于 go-redis 的缓存浏览与清理。
type RedisSource struct {
	// primary 用于 PING / INFO 的主客户端。
	primary *redis.Client
	// clients 各库的客户端（go-redis 客户端绑定固定 DB，避免共享 SELECT 带来的并发问题）。
	clients map[int]*redis.Client
	// databases 参与展示的库序号（升序）。
	databases []int
	// scanLimit 单库最多扫描的键数。
	scanLimit int
	// timeout 单次操作超时。
	timeout time.Duration
	// custom 自定义分组。
	custom []CustomGroup
}

// NewRedisSource 创建 Redis 缓存来源；每个配置的库使用独立的 go-redis 客户端。
//
// 参数 Parameters:
//   - cfg (redisx.Config): Redis 连接配置（复用 pkg/redisx 组件）。
//   - databases ([]int): 需要展示的库序号。
//   - scanLimit (int): 单库最多扫描的键数，<=0 时取 5000。
//   - timeout (time.Duration): 单次操作超时，<=0 时取 5s。
//   - custom ([]CustomGroup): 自定义分组。
//
// 返回 Returns:
//   - source (*RedisSource): Redis 缓存来源。
func NewRedisSource(cfg redisx.Config, databases []int, scanLimit int, timeout time.Duration, custom []CustomGroup) *RedisSource {
	if scanLimit <= 0 {
		scanLimit = 5000
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	clients := make(map[int]*redis.Client, len(databases)+1)
	unique := make([]int, 0, len(databases)+1)
	for _, db := range databases {
		if db < 0 {
			continue
		}
		if _, exists := clients[db]; exists {
			continue
		}
		dbConfig := cfg
		dbConfig.DB = db
		clients[db] = redisx.NewClient(dbConfig)
		unique = append(unique, db)
	}
	if _, exists := clients[cfg.DB]; !exists {
		clients[cfg.DB] = redisx.NewClient(cfg)
		unique = append(unique, cfg.DB)
	}
	sort.Ints(unique)
	return &RedisSource{
		primary:   redisx.NewClient(cfg),
		clients:   clients,
		databases: unique,
		scanLimit: scanLimit,
		timeout:   timeout,
		custom:    custom,
	}
}

// Close 关闭全部 Redis 客户端。
func (s *RedisSource) Close() {
	if s == nil {
		return
	}
	if s.primary != nil {
		_ = s.primary.Close()
	}
	for _, client := range s.clients {
		_ = client.Close()
	}
}

// Addr 返回 Redis 地址。
//
// 返回 Returns:
//   - addr (string): Redis 地址。
func (s *RedisSource) Addr() string {
	if s == nil || s.primary == nil {
		return ""
	}
	return s.primary.Options().Addr
}

// Overview 通过 PING + INFO 汇总 Redis 状态。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//
// 返回 Returns:
//   - overview (redisOverview): INFO 摘要。
//   - err (error): 连接或命令失败时返回原始错误。
func (s *RedisSource) Overview(ctx context.Context) (redisOverview, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if err := s.primary.Ping(ctx).Err(); err != nil {
		return redisOverview{}, err
	}
	text, err := s.primary.Info(ctx).Result()
	if err != nil {
		return redisOverview{}, err
	}
	sections := parseInfo(text)
	overview := redisOverview{
		Version:       sections["redis_version"],
		UsedMemory:    sections["used_memory_human"],
		Clients:       atoi64(sections["connected_clients"]),
		TotalCommands: atoi64(sections["total_commands_processed"]),
		HitRate:       hitRate(atoi64(sections["keyspace_hits"]), atoi64(sections["keyspace_misses"])),
		Keyspace:      parseKeyspace(sections),
	}
	return overview, nil
}

// Groups 扫描各库并按前缀聚合出分组。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//
// 返回 Returns:
//   - groups ([]dto.CacheGroup): 分组列表（自定义分组在前，其余按数量倒序）。
//   - err (error): 扫描失败时返回原始错误。
func (s *RedisSource) Groups(ctx context.Context) ([]dto.CacheGroup, error) {
	groups := make([]dto.CacheGroup, 0, len(s.custom)+8)
	for _, db := range s.databases {
		keys, err := s.scanAll(ctx, db)
		if err != nil {
			return nil, err
		}
		groups = append(groups, s.buildGroups(db, keys)...)
	}
	return groups, nil
}

// buildGroups 按自定义分组与键前缀聚合分组。
//
// 参数 Parameters:
//   - db (int): 库序号。
//   - keys ([]string): 该库扫描到的键。
//
// 返回 Returns:
//   - groups ([]dto.CacheGroup): 分组列表。
func (s *RedisSource) buildGroups(db int, keys []string) []dto.CacheGroup {
	groups := make([]dto.CacheGroup, 0, len(s.custom)+4)
	matched := make(map[string]bool, len(keys))
	for _, custom := range s.custom {
		count := int64(0)
		for _, key := range keys {
			if matchPattern(custom.Pattern, key) {
				count++
				matched[key] = true
			}
		}
		groups = append(groups, dto.CacheGroup{
			ID:        groupID(SourceRedis, db, custom.Pattern),
			Source:    SourceRedis,
			Name:      custom.Pattern,
			Label:     custom.Name,
			DB:        db,
			Pattern:   custom.Pattern,
			Count:     count,
			Cleanable: true,
		})
	}
	counts := make(map[string]int64, 8)
	for _, key := range keys {
		if matched[key] {
			continue
		}
		counts[groupNameOf(key)]++
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		groups = append(groups, dto.CacheGroup{
			ID:        groupID(SourceRedis, db, name+"*"),
			Source:    SourceRedis,
			Name:      name,
			Label:     name,
			DB:        db,
			Pattern:   name + "*",
			Count:     counts[name],
			Cleanable: true,
		})
	}
	return groups
}

// Keys 分页返回某分组下的键。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - db (int): 库序号。
//   - pattern (string): 分组模式。
//   - keyword (string): 键名关键字。
//   - page (int): 页码。
//   - size (int): 每页数量。
//
// 返回 Returns:
//   - items ([]dto.CacheItem): 当前页键列表（含类型与 TTL）。
//   - total (int64): 满足条件的键总数。
//   - err (error): 扫描失败时返回原始错误。
func (s *RedisSource) Keys(ctx context.Context, db int, pattern, keyword string, page, size int) ([]dto.CacheItem, int64, error) {
	client := s.client(db)
	if client == nil {
		return nil, 0, apperr.Invalid("未配置该 Redis 库：" + strconv.Itoa(db))
	}
	keys, err := s.scanAll(ctx, db)
	if err != nil {
		return nil, 0, err
	}
	matched := make([]string, 0, len(keys))
	for _, key := range keys {
		if !matchPattern(pattern, key) {
			continue
		}
		if keyword != "" && !strings.Contains(key, keyword) {
			continue
		}
		matched = append(matched, key)
	}
	sort.Strings(matched)
	total := int64(len(matched))
	start := util.Offset(page, size)
	if start > len(matched) {
		start = len(matched)
	}
	end := start + size
	if end > len(matched) {
		end = len(matched)
	}
	pageKeys := matched[start:end]
	if len(pageKeys) == 0 {
		return []dto.CacheItem{}, total, nil
	}
	return s.describeKeys(ctx, client, pageKeys), total, nil
}

// describeKeys 用流水线批量查询键的类型与 TTL。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - client (*redis.Client): 库客户端。
//   - keys ([]string): 键列表。
//
// 返回 Returns:
//   - items ([]dto.CacheItem): 键列表项。
func (s *RedisSource) describeKeys(ctx context.Context, client *redis.Client, keys []string) []dto.CacheItem {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	pipe := client.Pipeline()
	types := make([]*redis.StatusCmd, 0, len(keys))
	ttls := make([]*redis.DurationCmd, 0, len(keys))
	for _, key := range keys {
		types = append(types, pipe.Type(ctx, key))
		ttls = append(ttls, pipe.TTL(ctx, key))
	}
	// 流水线执行失败时降级为「未知类型」，不阻断列表展示。
	_, _ = pipe.Exec(ctx)
	items := make([]dto.CacheItem, 0, len(keys))
	for index, key := range keys {
		itemType := "unknown"
		if value, err := types[index].Result(); err == nil {
			itemType = value
		}
		ttl := int64(-1)
		if value, err := ttls[index].Result(); err == nil {
			ttl = ttlOf(value)
		}
		items = append(items, dto.CacheItem{Key: key, Type: itemType, TTL: ttl})
	}
	return items
}

// Value 返回单个键的类型、TTL、大小与值。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - db (int): 库序号。
//   - pattern (string): 分组模式，用于校验键归属。
//   - key (string): 键名。
//
// 返回 Returns:
//   - detail (dto.CacheDetail): 键详情。
//   - err (error): 键不存在或读取失败时返回业务错误。
func (s *RedisSource) Value(ctx context.Context, db int, pattern, key string) (dto.CacheDetail, error) {
	client := s.client(db)
	if client == nil {
		return dto.CacheDetail{}, apperr.Invalid("未配置该 Redis 库：" + strconv.Itoa(db))
	}
	if !matchPattern(pattern, key) {
		return dto.CacheDetail{}, apperr.Invalid("键不属于当前分组")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	itemType, err := client.Type(ctx, key).Result()
	if err != nil {
		return dto.CacheDetail{}, apperr.Internal("读取缓存类型失败", err)
	}
	if itemType == "none" {
		return dto.CacheDetail{}, apperr.NotFound("缓存键不存在或已过期")
	}
	detail := dto.CacheDetail{Key: key, Type: itemType, TTL: -1}
	if ttl, err := client.TTL(ctx, key).Result(); err == nil {
		detail.TTL = ttlOf(ttl)
	}
	detail.Size = s.sizeOf(ctx, client, key, itemType)
	value, truncated, err := s.readValue(ctx, client, key, itemType)
	if err != nil {
		return dto.CacheDetail{}, err
	}
	detail.Value = value
	detail.Truncated = truncated
	return detail, nil
}

// sizeOf 获取键占用字节数，优先 MEMORY USAGE，失败时按类型长度估算。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - client (*redis.Client): 库客户端。
//   - key (string): 键名。
//   - itemType (string): 键类型。
//
// 返回 Returns:
//   - size (int64): 字节数；无法获取时为 0。
func (s *RedisSource) sizeOf(ctx context.Context, client *redis.Client, key, itemType string) int64 {
	if size, err := client.MemoryUsage(ctx, key).Result(); err == nil {
		return size
	}
	switch itemType {
	case "string":
		if length, err := client.StrLen(ctx, key).Result(); err == nil {
			return length
		}
	case "list":
		if length, err := client.LLen(ctx, key).Result(); err == nil {
			return length
		}
	case "hash":
		if length, err := client.HLen(ctx, key).Result(); err == nil {
			return length
		}
	case "set":
		if length, err := client.SCard(ctx, key).Result(); err == nil {
			return length
		}
	case "zset":
		if length, err := client.ZCard(ctx, key).Result(); err == nil {
			return length
		}
	}
	return 0
}

// readValue 按类型读取键值并序列化为文本。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - client (*redis.Client): 库客户端。
//   - key (string): 键名。
//   - itemType (string): 键类型。
//
// 返回 Returns:
//   - value (string): 值文本。
//   - truncated (bool): 是否截断。
//   - err (error): 读取失败时返回业务错误。
func (s *RedisSource) readValue(ctx context.Context, client *redis.Client, key, itemType string) (string, bool, error) {
	truncate := func(text string) (string, bool) {
		if len(text) > maxValueBytes {
			return text[:maxValueBytes], true
		}
		return text, false
	}
	switch itemType {
	case "string":
		raw, err := client.Get(ctx, key).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return "", false, apperr.Internal("读取缓存值失败", err)
		}
		// JSON 内容做缩进美化，便于核对结构化缓存。
		if json.Valid([]byte(raw)) {
			var decoded any
			if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
				if pretty, err := json.MarshalIndent(decoded, "", "  "); err == nil {
					text, truncated := truncate(string(pretty))
					return text, truncated, nil
				}
			}
		}
		text, truncated := truncate(raw)
		return text, truncated, nil
	case "list":
		values, err := client.LRange(ctx, key, 0, valuePreviewLimit-1).Result()
		if err != nil {
			return "", false, apperr.Internal("读取缓存值失败", err)
		}
		return encodeSlice(values)
	case "set":
		values, err := client.SMembers(ctx, key).Result()
		if err != nil {
			return "", false, apperr.Internal("读取缓存值失败", err)
		}
		sort.Strings(values)
		if len(values) > valuePreviewLimit {
			values = values[:valuePreviewLimit]
		}
		return encodeSlice(values)
	case "hash":
		values, err := client.HGetAll(ctx, key).Result()
		if err != nil {
			return "", false, apperr.Internal("读取缓存值失败", err)
		}
		encoded, marshalErr := json.MarshalIndent(values, "", "  ")
		if marshalErr != nil {
			return "", false, apperr.Internal("序列化缓存值失败", marshalErr)
		}
		text, truncated := truncate(string(encoded))
		return text, truncated, nil
	case "zset":
		values, err := client.ZRangeWithScores(ctx, key, 0, valuePreviewLimit-1).Result()
		if err != nil {
			return "", false, apperr.Internal("读取缓存值失败", err)
		}
		rows := make([]map[string]any, 0, len(values))
		for _, value := range values {
			rows = append(rows, map[string]any{"member": value.Member, "score": value.Score})
		}
		encoded, marshalErr := json.MarshalIndent(rows, "", "  ")
		if marshalErr != nil {
			return "", false, apperr.Internal("序列化缓存值失败", marshalErr)
		}
		text, truncated := truncate(string(encoded))
		return text, truncated, nil
	default:
		return fmt.Sprintf("<%s 类型暂不支持在线预览>", itemType), false, nil
	}
}

// ClearKeys 删除分组内的指定键（会校验键确实属于该分组）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - db (int): 库序号。
//   - pattern (string): 分组模式。
//   - keys ([]string): 待删除的键。
//
// 返回 Returns:
//   - deleted (int64): 实际删除数量。
//   - err (error): 校验或执行失败时返回业务错误。
func (s *RedisSource) ClearKeys(ctx context.Context, db int, pattern string, keys []string) (int64, error) {
	client := s.client(db)
	if client == nil {
		return 0, apperr.Invalid("未配置该 Redis 库：" + strconv.Itoa(db))
	}
	targets := make([]string, 0, len(keys))
	for _, key := range keys {
		if !matchPattern(pattern, key) {
			return 0, apperr.Invalid("存在不属于当前分组的键：" + key)
		}
		targets = append(targets, key)
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var deleted int64
	for start := 0; start < len(targets); start += deleteBatchSize {
		end := start + deleteBatchSize
		if end > len(targets) {
			end = len(targets)
		}
		count, err := client.Del(ctx, targets[start:end]...).Result()
		if err != nil {
			return deleted, apperr.Internal("清理缓存失败", err)
		}
		deleted += count
	}
	return deleted, nil
}

// ClearGroup 清理整个分组（按模式扫描后批量删除）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - db (int): 库序号。
//   - pattern (string): 分组模式。
//
// 返回 Returns:
//   - deleted (int64): 实际删除数量。
//   - err (error): 扫描或执行失败时返回业务错误。
func (s *RedisSource) ClearGroup(ctx context.Context, db int, pattern string) (int64, error) {
	keys, err := s.scanAll(ctx, db)
	if err != nil {
		return 0, err
	}
	targets := make([]string, 0, len(keys))
	for _, key := range keys {
		if matchPattern(pattern, key) {
			targets = append(targets, key)
		}
	}
	if len(targets) == 0 {
		return 0, nil
	}
	return s.ClearKeys(ctx, db, pattern, targets)
}

// scanAll 扫描指定库的键；超过 scanLimit 时截断。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - db (int): 库序号。
//
// 返回 Returns:
//   - keys ([]string): 键列表。
//   - err (error): 扫描失败时返回原始错误。
func (s *RedisSource) scanAll(ctx context.Context, db int) ([]string, error) {
	client := s.client(db)
	if client == nil {
		return nil, apperr.Invalid("未配置该 Redis 库：" + strconv.Itoa(db))
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	keys := make([]string, 0, 64)
	var cursor uint64
	for {
		batch, next, err := client.Scan(ctx, cursor, "*", 500).Result()
		if err != nil {
			return nil, apperr.Internal("扫描 Redis 键失败", err)
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 || len(keys) >= s.scanLimit {
			break
		}
	}
	if len(keys) > s.scanLimit {
		keys = keys[:s.scanLimit]
	}
	return keys, nil
}

// client 返回指定库的客户端。
//
// 参数 Parameters:
//   - db (int): 库序号。
//
// 返回 Returns:
//   - client (*redis.Client): 库客户端；未配置时返回 nil。
func (s *RedisSource) client(db int) *redis.Client {
	if s == nil {
		return nil
	}
	return s.clients[db]
}

// encodeSlice 把字符串切片序列化为展示文本。
//
// 参数 Parameters:
//   - values ([]string): 值列表。
//
// 返回 Returns:
//   - text (string): JSON 文本。
//   - truncated (bool): 是否截断。
//   - err (error): 序列化失败时返回业务错误。
func encodeSlice(values []string) (string, bool, error) {
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return "", false, apperr.Internal("序列化缓存值失败", err)
	}
	if len(encoded) > maxValueBytes {
		return string(encoded[:maxValueBytes]), true, nil
	}
	return string(encoded), false, nil
}

// groupNameOf 提取键的分组名（首个 ":" 或 "." 及其之前的部分）。
//
// 参数 Parameters:
//   - key (string): 键名。
//
// 返回 Returns:
//   - name (string): 分组名；无分隔符时返回键名本身。
func groupNameOf(key string) string {
	index := -1
	for _, separator := range groupSeparators {
		if found := strings.Index(key, separator); found >= 0 && (index < 0 || found < index) {
			index = found
		}
	}
	if index < 0 {
		return key
	}
	return key[:index+1]
}

// parseInfo 把 INFO 输出解析为键值表。
//
// 参数 Parameters:
//   - text (string): INFO 原始文本。
//
// 返回 Returns:
//   - sections (map[string]string): 字段表（含 dbN 行）。
func parseInfo(text string) map[string]string {
	sections := make(map[string]string, 64)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		index := strings.Index(line, ":")
		if index <= 0 {
			continue
		}
		sections[line[:index]] = line[index+1:]
	}
	return sections
}

// parseKeyspace 解析 INFO keyspace 中的 dbN 行。
//
// 参数 Parameters:
//   - sections (map[string]string): INFO 字段表。
//
// 返回 Returns:
//   - rows ([]dto.CacheKeyspace): 各库统计（按库序号升序）。
func parseKeyspace(sections map[string]string) []dto.CacheKeyspace {
	rows := make([]dto.CacheKeyspace, 0, 4)
	for field, value := range sections {
		if !strings.HasPrefix(field, "db") {
			continue
		}
		db, err := strconv.Atoi(strings.TrimPrefix(field, "db"))
		if err != nil {
			continue
		}
		row := dto.CacheKeyspace{DB: db}
		for _, pair := range strings.Split(value, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) != 2 {
				continue
			}
			switch parts[0] {
			case "keys":
				row.Keys = atoi64(parts[1])
			case "expires":
				row.Expires = atoi64(parts[1])
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].DB < rows[j].DB })
	return rows
}

// hitRate 计算命中率文本。
//
// 参数 Parameters:
//   - hits (int64): 命中次数。
//   - misses (int64): 未命中次数。
//
// 返回 Returns:
//   - text (string): 形如 98.32% 的文本；无数据时返回 "-"。
func hitRate(hits, misses int64) string {
	total := hits + misses
	if total <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(hits)*100/float64(total), 'f', 2, 64) + "%"
}

// ttlOf 把 time.Duration 形式的 TTL 转换为秒：go-redis 以 -1 表示永久，-2 表示不存在。
//
// 参数 Parameters:
//   - value (time.Duration): TTL。
//
// 返回 Returns:
//   - ttl (int64): 秒数（保留 -1 / -2 语义）。
func ttlOf(value time.Duration) int64 {
	if value < 0 {
		return int64(value)
	}
	seconds := int64(value.Seconds())
	if seconds == 0 && value > 0 {
		return 1
	}
	return seconds
}

// atoi64 解析十进制整数，失败时返回 0。
//
// 参数 Parameters:
//   - text (string): 数字文本。
//
// 返回 Returns:
//   - value (int64): 解析结果。
func atoi64(text string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}
