package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// DeptAPI 是部门管理的 HTTP 控制器。
type DeptAPI struct {
	// depts 部门应用服务。
	depts *service.DeptService
}

// NewDeptAPI 创建部门控制器。
//
// 参数 Parameters:
//   - depts (*service.DeptService): 部门应用服务。
//
// 返回 Returns:
//   - api (*DeptAPI): 部门控制器。
func NewDeptAPI(depts *service.DeptService) *DeptAPI { return &DeptAPI{depts: depts} }

// Register 注册部门相关路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *DeptAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/depts", h.List)
	group.GET("/depts/options", h.Options)
	group.GET("/depts/:id/form", h.Form)
	group.POST("/depts", guard("sys:dept:create"), h.Create)
	group.PUT("/depts/:id", guard("sys:dept:update"), h.Update)
	group.DELETE("/depts/:ids", guard("sys:dept:delete"), h.Delete)
}

// List 处理 GET /api/v1/depts：部门树。
func (h *DeptAPI) List(c *gin.Context) {
	tree, err := h.depts.List(dto.DeptQuery{Keywords: c.Query("keywords"), Status: c.Query("status")})
	if err != nil {
		Fail(c, err, "查询部门失败")
		return
	}
	OK(c, tree)
}

// Options 处理 GET /api/v1/depts/options：部门下拉树。
func (h *DeptAPI) Options(c *gin.Context) {
	tree, err := h.depts.Options()
	if err != nil {
		Fail(c, err, "查询部门失败")
		return
	}
	OK(c, tree)
}

// Form 处理 GET /api/v1/depts/:id/form：部门编辑表单。
func (h *DeptAPI) Form(c *gin.Context) {
	form, err := h.depts.Form(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询部门失败")
		return
	}
	OK(c, form)
}

// Create 处理 POST /api/v1/depts：新增部门。
func (h *DeptAPI) Create(c *gin.Context) {
	var req dto.DeptForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.depts.Create(req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建部门失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// Update 处理 PUT /api/v1/depts/:id：更新部门。
func (h *DeptAPI) Update(c *gin.Context) {
	var req dto.DeptForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.depts.Update(c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新部门失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// Delete 处理 DELETE /api/v1/depts/:ids：删除部门。
func (h *DeptAPI) Delete(c *gin.Context) {
	if err := h.depts.Delete(c.Param("ids")); err != nil {
		Fail(c, err, "删除部门失败")
		return
	}
	OK(c, nil)
}
