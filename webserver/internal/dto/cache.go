package dto

// CacheGroup 是缓存分组（键前缀 / 进程内缓存分区）。
type CacheGroup struct {
	// ID 分组稳定标识：<source>|<db>|<pattern>，请求时原样回传。
	ID string `json:"id"`
	// Source 来源：redis（Redis）或 memory（进程内缓存）。
	Source string `json:"source"`
	// Name 分组名（键前缀，如 sys_config: / apscheduler.）。
	Name string `json:"name"`
	// Label 展示名。
	Label string `json:"label"`
	// DB Redis 库序号；进程内缓存为 -1。
	DB int `json:"db"`
	// Pattern Redis 键匹配模式；进程内缓存为空串。
	Pattern string `json:"pattern"`
	// Count 当前键数量。
	Count int64 `json:"count"`
	// Cleanable 是否允许清理（进程内缓存与 Redis 均可清理，预留给只读分组）。
	Cleanable bool `json:"cleanable"`
}

// CacheItem 是缓存键列表项。
type CacheItem struct {
	// Key 键名。
	Key string `json:"key"`
	// Type Redis 数据类型；进程内缓存固定为 memory。
	Type string `json:"type"`
	// TTL 剩余有效期（秒）：-1 永久，-2 已失效。
	TTL int64 `json:"ttl"`
	// Size 占用字节数；无法获取时为 0。
	Size int64 `json:"size"`
}

// CacheDetail 是缓存键详情。
type CacheDetail struct {
	// Key 键名。
	Key string `json:"key"`
	// Type Redis 数据类型；进程内缓存固定为 memory。
	Type string `json:"type"`
	// TTL 剩余有效期（秒）：-1 永久，-2 已失效。
	TTL int64 `json:"ttl"`
	// Size 占用字节数。
	Size int64 `json:"size"`
	// Value 值内容（JSON 文本或原文）。
	Value string `json:"value"`
	// Truncated 内容是否被截断。
	Truncated bool `json:"truncated"`
}

// CacheKeyspace 是 Redis 单个库的键统计。
type CacheKeyspace struct {
	// DB 库序号。
	DB int `json:"db"`
	// Keys 键数量。
	Keys int64 `json:"keys"`
	// Expires 设置了过期时间的键数量。
	Expires int64 `json:"expires"`
}

// CacheOverview 是缓存总览（连接状态与运行指标）。
type CacheOverview struct {
	// Enabled 是否配置了 Redis。
	Enabled bool `json:"enabled"`
	// Connected 是否连接成功。
	Connected bool `json:"connected"`
	// Addr Redis 地址。
	Addr string `json:"addr"`
	// Version Redis 版本。
	Version string `json:"version"`
	// UsedMemory 已用内存（可读文本）。
	UsedMemory string `json:"usedMemory"`
	// Clients 当前连接数。
	Clients int64 `json:"clients"`
	// HitRate 命中率（百分比文本）。
	HitRate string `json:"hitRate"`
	// TotalCommands 已处理命令数。
	TotalCommands int64 `json:"totalCommands"`
	// Keyspace 各库键统计。
	Keyspace []CacheKeyspace `json:"keyspace"`
	// MemoryGroups 进程内缓存分组数量。
	MemoryGroups int `json:"memoryGroups"`
	// Message 未启用或连接失败时的提示。
	Message string `json:"message"`
}

// CacheKeyQuery 是缓存键列表查询条件。
type CacheKeyQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Group 分组 ID。
	Group string `json:"group"`
	// Keywords 键名模糊搜索。
	Keywords string `json:"keywords"`
}
