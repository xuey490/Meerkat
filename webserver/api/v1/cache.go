package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// CacheAPI 是缓存管理的 HTTP 控制器。
type CacheAPI struct {
	// caches 缓存应用服务。
	caches *service.CacheService
}

// NewCacheAPI 创建缓存控制器。
//
// 参数 Parameters:
//   - caches (*service.CacheService): 缓存应用服务。
//
// 返回 Returns:
//   - api (*CacheAPI): 缓存控制器。
func NewCacheAPI(caches *service.CacheService) *CacheAPI { return &CacheAPI{caches: caches} }

// Register 注册缓存管理路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *CacheAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/cache/overview", guard("sys:cache:list"), h.Overview)
	group.GET("/cache/groups", guard("sys:cache:list"), h.Groups)
	group.GET("/cache/keys", guard("sys:cache:list"), h.Keys)
	group.GET("/cache/value", guard("sys:cache:list"), h.Value)
	group.DELETE("/cache/keys", guard("sys:cache:clear"), h.ClearKeys)
	group.DELETE("/cache/groups", guard("sys:cache:clear"), h.ClearGroup)
}

// Overview 处理 GET /api/v1/cache/overview：缓存总览与连接状态。
func (h *CacheAPI) Overview(c *gin.Context) {
	overview, err := h.caches.Overview(c.Request.Context())
	if err != nil {
		Fail(c, err, "查询缓存总览失败")
		return
	}
	OK(c, overview)
}

// Groups 处理 GET /api/v1/cache/groups：缓存分组列表。
func (h *CacheAPI) Groups(c *gin.Context) {
	groups, err := h.caches.Groups(c.Request.Context())
	if err != nil {
		Fail(c, err, "查询缓存分组失败")
		return
	}
	OK(c, groups)
}

// Keys 处理 GET /api/v1/cache/keys：分组下的键列表。
func (h *CacheAPI) Keys(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.caches.Keys(c.Request.Context(), dto.CacheKeyQuery{
		PageQuery: dto.PageQuery{PageNum: page, PageSize: size},
		Group:     c.Query("group"),
		Keywords:  c.Query("keywords"),
	})
	if err != nil {
		Fail(c, err, "查询缓存键失败")
		return
	}
	OK(c, result)
}

// Value 处理 GET /api/v1/cache/value：缓存键详情。
func (h *CacheAPI) Value(c *gin.Context) {
	detail, err := h.caches.Value(c.Request.Context(), c.Query("group"), c.Query("key"))
	if err != nil {
		Fail(c, err, "查询缓存值失败")
		return
	}
	OK(c, detail)
}

// ClearKeys 处理 DELETE /api/v1/cache/keys?group=&keys=a,b：清理指定键。
func (h *CacheAPI) ClearKeys(c *gin.Context) {
	group := c.Query("group")
	keysText := c.Query("keys")
	deleted, err := h.caches.ClearKeys(c.Request.Context(), group, keysText)
	if err != nil {
		Fail(c, err, "清理缓存失败")
		return
	}
	OK(c, gin.H{"deleted": deleted})
}

// ClearGroup 处理 DELETE /api/v1/cache/groups?group=：清理整个分组。
func (h *CacheAPI) ClearGroup(c *gin.Context) {
	deleted, err := h.caches.ClearGroup(c.Request.Context(), c.Query("group"))
	if err != nil {
		Fail(c, err, "清理缓存分组失败")
		return
	}
	OK(c, gin.H{"deleted": deleted})
}
