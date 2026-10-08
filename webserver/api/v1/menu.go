package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// MenuAPI 是菜单管理的 HTTP 控制器。
type MenuAPI struct {
	// menus 菜单应用服务。
	menus *service.MenuService
}

// NewMenuAPI 创建菜单控制器。
//
// 参数 Parameters:
//   - menus (*service.MenuService): 菜单应用服务。
//
// 返回 Returns:
//   - api (*MenuAPI): 菜单控制器。
func NewMenuAPI(menus *service.MenuService) *MenuAPI { return &MenuAPI{menus: menus} }

// Register 注册菜单相关路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *MenuAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/menus", h.List)
	group.GET("/menus/routes", h.Routes)
	group.GET("/menus/options", h.Options)
	group.GET("/menus/:id/form", h.Form)
	group.POST("/menus", guard("sys:menu:create"), h.Create)
	group.PUT("/menus/:id", guard("sys:menu:update"), h.Update)
	group.DELETE("/menus/:id", guard("sys:menu:delete"), h.Delete)
}

// List 处理 GET /api/v1/menus：菜单树。
func (h *MenuAPI) List(c *gin.Context) {
	tree, err := h.menus.List(dto.MenuQuery{Keywords: c.Query("keywords"), Scope: c.Query("scope")})
	if err != nil {
		Fail(c, err, "查询菜单失败")
		return
	}
	OK(c, tree)
}

// Routes 处理 GET /api/v1/menus/routes：当前用户可见的动态路由树。
func (h *MenuAPI) Routes(c *gin.Context) {
	routes, err := h.menus.Routes(CurrentUser(c))
	if err != nil {
		Fail(c, err, "查询菜单失败")
		return
	}
	OK(c, routes)
}

// Options 处理 GET /api/v1/menus/options：菜单下拉树。
func (h *MenuAPI) Options(c *gin.Context) {
	tree, err := h.menus.Options(c.Query("onlyParent") == "true")
	if err != nil {
		Fail(c, err, "查询菜单失败")
		return
	}
	OK(c, tree)
}

// Form 处理 GET /api/v1/menus/:id/form：菜单编辑表单。
func (h *MenuAPI) Form(c *gin.Context) {
	form, err := h.menus.Form(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询菜单失败")
		return
	}
	OK(c, form)
}

// Create 处理 POST /api/v1/menus：新增菜单。
func (h *MenuAPI) Create(c *gin.Context) {
	var req dto.MenuForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.menus.Create(req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建菜单失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// Update 处理 PUT /api/v1/menus/:id：更新菜单。
func (h *MenuAPI) Update(c *gin.Context) {
	var req dto.MenuForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.menus.Update(c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新菜单失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// Delete 处理 DELETE /api/v1/menus/:id：删除菜单。
func (h *MenuAPI) Delete(c *gin.Context) {
	if err := h.menus.Delete(c.Param("id")); err != nil {
		Fail(c, err, "删除菜单失败")
		return
	}
	OK(c, nil)
}
