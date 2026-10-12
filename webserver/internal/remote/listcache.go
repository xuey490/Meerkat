package remote

import (
	"sync"
	"time"
)

// 列表缓存的两个 TTL。
//
// 它们的职责是「防抖」而不是「保鲜」：界面每次打开抽屉都会请求一次，
// TTL 只用来挡掉「连点刷新」「抽屉快速开合」这类重复请求。
//
// 因此刻意设得很短——设长了只会换来「在终端里刚重启完、抽屉还显示旧状态」的误读。
// 真正保证新鲜度的是另外两条：前端每次打开都会真的请求一次，以及本包里的写操作
// 成功后主动失效（见 InvalidateTargetLists）。
const (
	// serviceListTTL 服务列表的防抖窗口。
	serviceListTTL = 15 * time.Second
	// processListTTL 进程列表的防抖窗口。
	//
	// 比服务更短：进程变化远比服务频繁，一条命令就可能起落几个进程。
	processListTTL = 8 * time.Second
)

// listCacheEntry 是缓存里的一条列表与其写入时刻。
type listCacheEntry[T any] struct {
	// data 列表数据。与 get 返回的切片共享底层数组，写入后不得再修改。
	data []T
	// at 写入时刻。
	at time.Time
}

// listCache 是「按主机缓存一份列表」的最小实现。
//
// 它有两个刻意的约束：
//
//  1. 只缓存成功结果。错误一律不写缓存——否则一次网络抖动会被"记住"整个 TTL，
//     表现为目标机明明在线、界面却持续报错。
//  2. get 返回的切片与缓存共享底层数组。调用方**不得再修改它**（尤其不能再排序），
//     否则就是对共享切片的并发写。排序、截断这类整理动作必须在 put 之前做完。
type listCache[T any] struct {
	// ttl 有效期；<=0 表示不缓存（get 永远未命中）。
	ttl time.Duration
	// mu 保护 entries。读取路径上有并发（多个用户的请求），必须加锁。
	mu sync.Mutex
	// entries 主机标识到缓存项的映射。
	entries map[string]listCacheEntry[T]
}

// newListCache 创建缓存。
//
// 参数 Parameters:
//   - ttl (time.Duration): 有效期；<=0 时退化为「不缓存」。
//
// 返回 Returns:
//   - cache (*listCache[T]): 缓存实例。
func newListCache[T any](ttl time.Duration) *listCache[T] {
	return &listCache[T]{ttl: ttl, entries: make(map[string]listCacheEntry[T], 16)}
}

// get 取缓存。
//
// 参数 Parameters:
//   - key (string): 主机标识。
//
// 返回 Returns:
//   - data ([]T): 列表数据；未命中时为 nil。
//   - found (bool): 命中且未过期时为 true。
func (c *listCache[T]) get(key string) ([]T, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, found := c.entries[key]
	if !found || time.Since(entry.at) >= c.ttl {
		return nil, false
	}
	return entry.data, true
}

// put 写入缓存，并顺手回收所有过期项。
//
// 遍历成本可以忽略（机器数量有限），换来的是「已被删除的主机不会长期占位」——
// 否则这份 map 会随历史上出现过的机器数单调增长。
//
// 参数 Parameters:
//   - key (string): 主机标识。
//   - data ([]T): 列表数据；调用方在传入后不得再修改它。
func (c *listCache[T]) put(key string, data []T) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = listCacheEntry[T]{data: data, at: time.Now()}
	for k, entry := range c.entries {
		if time.Since(entry.at) >= c.ttl {
			delete(c.entries, k)
		}
	}
}

// invalidate 丢弃某台主机的缓存。
//
// 参数 Parameters:
//   - key (string): 主机标识。
func (c *listCache[T]) invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// InvalidateTargetLists 丢弃某台主机的服务与进程列表缓存。
//
// 为什么两者总是**成对**失效：这两个列表在目标机上是耦合的——
// 杀掉服务的主进程会让 systemd 把该服务标成 failed（服务列表变了），
// 重启服务会让主进程号变化（进程列表也变了）。只失效一半必然留下一个陈旧列表。
//
// 谁必须调用它：本包里每一条会改动目标机状态的操作。目前有三处——
// OperateService（启停/重启/自启）、RestartProcess（systemctl restart）、
// KillProcess（kill -9 / taskkill）。新增写入路径时不要漏。
//
// 刻意导出：API 层在别的写入路径（例如 SFTP 写入 .service 文件）也能主动失效。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
func (s *Service) InvalidateTargetLists(agentID string) {
	s.serviceCache.invalidate(agentID)
	s.processCache.invalidate(agentID)
}
