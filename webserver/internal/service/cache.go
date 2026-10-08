package service

import (
	"context"
	"strings"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/cache"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/util"
)

// CacheService 提供缓存管理的应用服务。
type CacheService struct {
	// caches 缓存管理器（聚合 Redis 与进程内缓存）。
	caches *cache.Manager
}

// NewCacheService 创建缓存应用服务。
//
// 参数 Parameters:
//   - caches (*cache.Manager): 缓存管理器，可为 nil（未装配时全部接口返回未启用提示）。
//
// 返回 Returns:
//   - service (*CacheService): 缓存应用服务。
func NewCacheService(caches *cache.Manager) *CacheService {
	return &CacheService{caches: caches}
}

// Overview 返回缓存总览（连接状态与运行指标）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//
// 返回 Returns:
//   - overview (dto.CacheOverview): 总览数据。
//   - err (error): 未装配缓存管理时返回业务错误。
func (s *CacheService) Overview(ctx context.Context) (dto.CacheOverview, error) {
	if s.caches == nil {
		return dto.CacheOverview{}, apperr.Invalid("缓存管理未启用")
	}
	return s.caches.Overview(ctx), nil
}

// Groups 返回全部缓存分组。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//
// 返回 Returns:
//   - groups ([]dto.CacheGroup): 分组列表。
//   - err (error): 未装配缓存管理时返回业务错误。
func (s *CacheService) Groups(ctx context.Context) ([]dto.CacheGroup, error) {
	if s.caches == nil {
		return nil, apperr.Invalid("缓存管理未启用")
	}
	return s.caches.Groups(ctx)
}

// Keys 分页查询分组下的缓存键。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - q (dto.CacheKeyQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.CacheItem]): 分页结果。
//   - err (error): 未装配缓存管理或分组无效时返回业务错误。
func (s *CacheService) Keys(ctx context.Context, q dto.CacheKeyQuery) (dto.PageResult[dto.CacheItem], error) {
	if s.caches == nil {
		return dto.PageResult[dto.CacheItem]{}, apperr.Invalid("缓存管理未启用")
	}
	if strings.TrimSpace(q.Group) == "" {
		return dto.PageResult[dto.CacheItem]{}, apperr.Invalid("请先选择缓存分组")
	}
	return s.caches.Keys(ctx, q)
}

// Value 返回缓存键详情。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - group (string): 分组 ID。
//   - key (string): 键名。
//
// 返回 Returns:
//   - detail (dto.CacheDetail): 键详情。
//   - err (error): 未装配缓存管理或键不存在时返回业务错误。
func (s *CacheService) Value(ctx context.Context, group, key string) (dto.CacheDetail, error) {
	if s.caches == nil {
		return dto.CacheDetail{}, apperr.Invalid("缓存管理未启用")
	}
	if strings.TrimSpace(group) == "" {
		return dto.CacheDetail{}, apperr.Invalid("请先选择缓存分组")
	}
	return s.caches.Value(ctx, group, key)
}

// ClearKeys 清理分组下的指定键（keysText 支持逗号分隔的多个键）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - group (string): 分组 ID。
//   - keysText (string): 逗号分隔的键名。
//
// 返回 Returns:
//   - deleted (int64): 实际清理数量。
//   - err (error): 未装配缓存管理或校验失败时返回业务错误。
func (s *CacheService) ClearKeys(ctx context.Context, group, keysText string) (int64, error) {
	if s.caches == nil {
		return 0, apperr.Invalid("缓存管理未启用")
	}
	if strings.TrimSpace(group) == "" {
		return 0, apperr.Invalid("请先选择缓存分组")
	}
	keys := make([]string, 0, 4)
	for _, part := range strings.Split(keysText, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	if len(keys) == 0 {
		return 0, apperr.Invalid("请选择要清理的缓存键")
	}
	return s.caches.ClearKeys(ctx, group, keys)
}

// ClearGroup 清理整个分组。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - group (string): 分组 ID。
//
// 返回 Returns:
//   - deleted (int64): 实际清理数量。
//   - err (error): 未装配缓存管理或校验失败时返回业务错误。
func (s *CacheService) ClearGroup(ctx context.Context, group string) (int64, error) {
	if s.caches == nil {
		return 0, apperr.Invalid("缓存管理未启用")
	}
	if strings.TrimSpace(group) == "" {
		return 0, apperr.Invalid("请先选择缓存分组")
	}
	return s.caches.ClearGroup(ctx, group)
}

// ParseCacheKeys 把逗号分隔的键名拆分为列表（供控制器记录操作日志使用）。
//
// 参数 Parameters:
//   - keysText (string): 逗号分隔的键名。
//
// 返回 Returns:
//   - keys ([]string): 键名列表。
func ParseCacheKeys(keysText string) []string {
	keys := make([]string, 0, 4)
	for _, part := range strings.Split(keysText, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	return keys
}

// CacheKeyPreview 返回键名列表的展示文本（最多 3 个，其余省略）。
//
// 参数 Parameters:
//   - keys ([]string): 键名列表。
//
// 返回 Returns:
//   - text (string): 展示文本。
func CacheKeyPreview(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	limit := len(keys)
	if limit > 3 {
		limit = 3
	}
	text := strings.Join(keys[:limit], ",")
	if len(keys) > limit {
		text += " 等 " + util.TextID(int64(len(keys))) + " 个键"
	}
	return text
}
