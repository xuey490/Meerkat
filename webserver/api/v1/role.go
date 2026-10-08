package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// RoleAPI 是角色管理的 HTTP 控制器。
type RoleAPI struct {
	// roles 角色应用服务。
	roles *service.RoleService
}

// NewRoleAPI 创建角色控制器。
//
// 参数 Parameters:
//   - roles (*service.RoleService): 角色应用服务。
//
// 返回 Returns:
//   - api (*RoleAPI): 角色控制器。
func NewRoleAPI(roles *service.RoleService) *RoleAPI { return &RoleAPI{roles: roles} }

// Register 注册角色相关路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *RoleAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/roles", h.List)
	group.GET("/roles/options", h.Options)
	group.GET("/roles/code-options", h.CodeOptions)
	group.GET("/roles/:id/form", h.Form)
	group.GET("/roles/:id/menu-ids", h.MenuIDs)
	group.GET("/roles/:id/dept-ids", h.DeptIDs)
	group.POST("/roles", guard("sys:role:create"), h.Create)
	group.PUT("/roles/:id", guard("sys:role:update"), h.Update)
	group.PUT("/roles/:id/menus", guard("sys:role:assign-menu"), h.ReplaceMenus)
	group.DELETE("/roles/:ids", guard("sys:role:delete"), h.Delete)
}

// List 处理 GET /api/v1/roles：角色分页。
func (h *RoleAPI) List(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.roles.Page(dto.RoleQuery{
		PageQuery: dto.PageQuery{PageNum: page, PageSize: size},
		Keywords:  c.Query("keywords"),
	})
	if err != nil {
		Fail(c, err, "查询角色失败")
		return
	}
	OK(c, result)
}

// Options 处理 GET /api/v1/roles/options：角色下拉（值为角色 ID）。
func (h *RoleAPI) Options(c *gin.Context) {
	options, err := h.roles.Options(false)
	if err != nil {
		Fail(c, err, "查询角色失败")
		return
	}
	OK(c, options)
}

// CodeOptions 处理 GET /api/v1/roles/code-options：角色下拉（值为角色编码）。
func (h *RoleAPI) CodeOptions(c *gin.Context) {
	options, err := h.roles.Options(true)
	if err != nil {
		Fail(c, err, "查询角色失败")
		return
	}
	OK(c, options)
}

// Form 处理 GET /api/v1/roles/:id/form：角色编辑表单。
func (h *RoleAPI) Form(c *gin.Context) {
	form, err := h.roles.Form(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询角色失败")
		return
	}
	OK(c, form)
}

// MenuIDs 处理 GET /api/v1/roles/:id/menu-ids：角色已授权菜单。
func (h *RoleAPI) MenuIDs(c *gin.Context) {
	ids, err := h.roles.MenuIDs(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询角色菜单失败")
		return
	}
	OK(c, ids)
}

// DeptIDs 处理 GET /api/v1/roles/:id/dept-ids：角色自定义数据权限部门。
func (h *RoleAPI) DeptIDs(c *gin.Context) {
	ids, err := h.roles.DeptIDs(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询角色部门失败")
		return
	}
	OK(c, ids)
}

// Create 处理 POST /api/v1/roles：新增角色。
func (h *RoleAPI) Create(c *gin.Context) {
	var req dto.RoleForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.roles.Create(req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建角色失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// Update 处理 PUT /api/v1/roles/:id：更新角色。
func (h *RoleAPI) Update(c *gin.Context) {
	var req dto.RoleForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.roles.Update(c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新角色失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// ReplaceMenus 处理 PUT /api/v1/roles/:id/menus：覆盖式保存角色菜单授权。
//
// 请求体为菜单 ID 数组（与前端 RoleAPI.updateRoleMenus 的契约一致）。
func (h *RoleAPI) ReplaceMenus(c *gin.Context) {
	var menuIDs []int64
	if err := c.ShouldBindJSON(&menuIDs); err != nil {
		BadRequest(c, "请求参数格式错误")
		return
	}
	if err := h.roles.ReplaceMenus(c.Param("id"), menuIDs); err != nil {
		Fail(c, err, "保存角色菜单失败")
		return
	}
	OK(c, nil)
}

// Delete 处理 DELETE /api/v1/roles/:ids：删除角色。
func (h *RoleAPI) Delete(c *gin.Context) {
	if err := h.roles.Delete(c.Param("ids")); err != nil {
		Fail(c, err, "删除角色失败")
		return
	}
	OK(c, nil)
}
