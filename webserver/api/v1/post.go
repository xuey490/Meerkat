package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// PostAPI 是岗位管理的 HTTP 控制器。
type PostAPI struct {
	// posts 岗位应用服务。
	posts *service.PostService
}

// NewPostAPI 创建岗位控制器。
//
// 参数 Parameters:
//   - posts (*service.PostService): 岗位应用服务。
//
// 返回 Returns:
//   - api (*PostAPI): 岗位控制器。
func NewPostAPI(posts *service.PostService) *PostAPI { return &PostAPI{posts: posts} }

// Register 注册岗位相关路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *PostAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/posts", h.List)
	group.GET("/posts/options", h.Options)
	group.GET("/posts/:id/form", h.Form)
	group.POST("/posts", guard("sys:post:create"), h.Create)
	group.PUT("/posts/:id", guard("sys:post:update"), h.Update)
	group.DELETE("/posts/:ids", guard("sys:post:delete"), h.Delete)
}

// List 处理 GET /api/v1/posts：岗位分页。
func (h *PostAPI) List(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.posts.Page(dto.PostQuery{
		PageQuery: dto.PageQuery{PageNum: page, PageSize: size},
		Keywords:  c.Query("keywords"),
		Status:    c.Query("status"),
	})
	if err != nil {
		Fail(c, err, "查询岗位失败")
		return
	}
	OK(c, result)
}

// Options 处理 GET /api/v1/posts/options：岗位下拉。
func (h *PostAPI) Options(c *gin.Context) {
	options, err := h.posts.Options()
	if err != nil {
		Fail(c, err, "查询岗位失败")
		return
	}
	OK(c, options)
}

// Form 处理 GET /api/v1/posts/:id/form：岗位编辑表单。
func (h *PostAPI) Form(c *gin.Context) {
	form, err := h.posts.Form(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询岗位失败")
		return
	}
	OK(c, form)
}

// Create 处理 POST /api/v1/posts：新增岗位。
func (h *PostAPI) Create(c *gin.Context) {
	var req dto.PostForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.posts.Create(req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建岗位失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// Update 处理 PUT /api/v1/posts/:id：更新岗位。
func (h *PostAPI) Update(c *gin.Context) {
	var req dto.PostForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.posts.Update(c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新岗位失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// Delete 处理 DELETE /api/v1/posts/:ids：删除岗位。
func (h *PostAPI) Delete(c *gin.Context) {
	if err := h.posts.Delete(c.Param("ids")); err != nil {
		Fail(c, err, "删除岗位失败")
		return
	}
	OK(c, nil)
}
