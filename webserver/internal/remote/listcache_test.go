package remote

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestListCacheHitWithinTTL 验证 TTL 内命中、且只命中写入过的那台主机。
func TestListCacheHitWithinTTL(t *testing.T) {
	cache := newListCache[ServiceUnit](time.Minute)
	cache.put("vm1", []ServiceUnit{{Unit: "nginx.service"}})

	units, found := cache.get("vm1")
	if !found || len(units) != 1 || units[0].Unit != "nginx.service" {
		t.Fatalf("TTL 内应命中，实际 found=%v units=%#v", found, units)
	}
	if _, found := cache.get("vm2"); found {
		t.Error("未写入过的主机不应命中")
	}
}

// TestListCacheExpires 验证超过 TTL 后不再命中。
func TestListCacheExpires(t *testing.T) {
	cache := newListCache[ServiceUnit](20 * time.Millisecond)
	cache.put("vm1", []ServiceUnit{{Unit: "nginx.service"}})

	time.Sleep(60 * time.Millisecond)
	if _, found := cache.get("vm1"); found {
		t.Error("超过 TTL 后不应命中")
	}
}

// TestListCacheDisabled 验证 ttl<=0 时彻底不缓存。
//
// 这个开关必须是"真能关掉"：它是不认可缓存策略时的退路。
func TestListCacheDisabled(t *testing.T) {
	cache := newListCache[ServiceUnit](0)
	cache.put("vm1", []ServiceUnit{{Unit: "nginx.service"}})
	if _, found := cache.get("vm1"); found {
		t.Error("ttl<=0 时不应命中任何缓存")
	}
}

// TestListCacheInvalidate 验证失效后不再命中。
func TestListCacheInvalidate(t *testing.T) {
	cache := newListCache[ProcessInfo](time.Minute)
	cache.put("vm1", []ProcessInfo{{PID: 1}})
	cache.invalidate("vm1")
	if _, found := cache.get("vm1"); found {
		t.Error("失效后不应命中")
	}
}

// TestListCacheSweepsExpired 验证 put 会顺手回收过期项。
//
// 不回收的话，这份 map 会随历史上出现过的机器数单调增长。
func TestListCacheSweepsExpired(t *testing.T) {
	cache := newListCache[ServiceUnit](20 * time.Millisecond)
	cache.put("old", []ServiceUnit{{Unit: "old.service"}})

	time.Sleep(60 * time.Millisecond)
	cache.put("new", []ServiceUnit{{Unit: "new.service"}})

	cache.mu.Lock()
	_, oldStillThere := cache.entries["old"]
	total := len(cache.entries)
	cache.mu.Unlock()

	if oldStillThere {
		t.Error("过期的旧主机条目应被回收")
	}
	if total != 1 {
		t.Errorf("回收后应只剩 1 条，实际 %d", total)
	}
}

// TestListCacheConcurrent 验证并发读写无数据竞争（配合 -race 运行）。
func TestListCacheConcurrent(t *testing.T) {
	cache := newListCache[ProcessInfo](time.Minute)
	var wait sync.WaitGroup
	for seed := 0; seed < 8; seed++ {
		wait.Add(1)
		go func(seed int) {
			defer wait.Done()
			key := "vm" + strconv.Itoa(seed%3)
			for round := 0; round < 200; round++ {
				cache.put(key, []ProcessInfo{{PID: round}})
				cache.get(key)
				cache.invalidate(key)
			}
		}(seed)
	}
	wait.Wait()
}

// TestPrepareProcessList 验证列表整理：按内存降序 + 超限截断。
//
// 这条断言锁住的是并发安全的前置条件——整理必须在数据进入缓存（被多个请求共享）
// 之前完成，否则命中缓存后再排序就是对同一底层数组的并发写。
func TestPrepareProcessList(t *testing.T) {
	raw := []ProcessInfo{
		{PID: 1, RSSBytes: 100},
		{PID: 2, RSSBytes: 900},
		{PID: 3, RSSBytes: 500},
	}
	prepared := prepareProcessList(raw)
	if len(prepared) != 3 {
		t.Fatalf("未超上限时不应截断，实际 %d 条", len(prepared))
	}
	if prepared[0].PID != 2 || prepared[1].PID != 3 || prepared[2].PID != 1 {
		t.Errorf("应按内存降序，实际 %#v", prepared)
	}

	// 超过上限时截断，且保留的是内存占用最高的那些。
	large := make([]ProcessInfo, 0, processListLimit+10)
	for index := 0; index < processListLimit+10; index++ {
		large = append(large, ProcessInfo{PID: index, RSSBytes: uint64(index)})
	}
	truncated := prepareProcessList(large)
	if len(truncated) != processListLimit {
		t.Fatalf("应截断到 %d 条，实际 %d", processListLimit, len(truncated))
	}
	if truncated[0].RSSBytes != uint64(processListLimit+9) {
		t.Errorf("截断后应保留内存最高的一条，实际首条 RSS=%d", truncated[0].RSSBytes)
	}
}
