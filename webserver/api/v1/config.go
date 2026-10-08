package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// ConfigAPI 是系统配置的 HTTP 控制器。
type ConfigAPI struct {
	// configs 配置应用服务。
	configs *service.ConfigService
}

// NewConfigAPI 创建系统配置控制器。
//
// 参数 Parameters:
//   - configs (*service.ConfigService): 配置应用服务。
//
// 返回 Returns:
//   - api (*ConfigAPI): 配置控制器。
func NewConfigAPI(configs *service.ConfigService) *ConfigAPI { return &ConfigAPI{configs: configs} }

// Register 注册系统配置相关路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *ConfigAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/configs", h.List)
	group.GET("/configs/:id/form", h.Form)
	group.POST("/configs", guard("sys:config:create"), h.Create)
	// 注意：refresh 必须声明在 /configs/:id 之前，保持与旧路由一致的匹配语义。
	group.PUT("/configs/refresh", guard("sys:config:refresh"), h.RefreshCache)
	group.PUT("/configs/:id", guard("sys:config:update"), h.Update)
	group.DELETE("/configs/:id", guard("sys:config:delete"), h.Delete)
}

// List 处理 GET /api/v1/configs：配置分页。
func (h *ConfigAPI) List(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.configs.Page(dto.ConfigQuery{
		PageQuery: dto.PageQuery{PageNum: page, PageSize: size},
		Keywords:  c.Query("keywords"),
	})
	if err != nil {
		Fail(c, err, "查询配置失败")
		return
	}
	OK(c, result)
}

// Form 处理 GET /api/v1/configs/:id/form：配置编辑表单。
func (h *ConfigAPI) Form(c *gin.Context) {
	form, err := h.configs.Form(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询配置失败")
		return
	}
	OK(c, form)
}

// Create 处理 POST /api/v1/configs：新增配置。
func (h *ConfigAPI) Create(c *gin.Context) {
	var req dto.ConfigForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.configs.Create(req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建配置失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// Update 处理 PUT /api/v1/configs/:id：更新配置。
func (h *ConfigAPI) Update(c *gin.Context) {
	var req dto.ConfigForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.configs.Update(c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新配置失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// Delete 处理 DELETE /api/v1/configs/:id：删除配置。
func (h *ConfigAPI) Delete(c *gin.Context) {
	if err := h.configs.Delete(c.Param("id")); err != nil {
		Fail(c, err, "删除配置失败")
		return
	}
	OK(c, nil)
}

// RefreshCache 处理 PUT /api/v1/configs/refresh：刷新配置缓存。
func (h *ConfigAPI) RefreshCache(c *gin.Context) {
	data, err := h.configs.RefreshCache()
	if err != nil {
		Fail(c, err, "刷新配置缓存失败")
		return
	}
	OK(c, data)
}
