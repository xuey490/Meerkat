package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// NoticeAPI 是通知公告的 HTTP 控制器。
type NoticeAPI struct {
	// notices 公告应用服务。
	notices *service.NoticeService
}

// NewNoticeAPI 创建公告控制器。
//
// 参数 Parameters:
//   - notices (*service.NoticeService): 公告应用服务。
//
// 返回 Returns:
//   - api (*NoticeAPI): 公告控制器。
func NewNoticeAPI(notices *service.NoticeService) *NoticeAPI { return &NoticeAPI{notices: notices} }

// Register 注册公告相关路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *NoticeAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/notices", h.List)
	group.GET("/notices/my", h.MyList)
	group.GET("/notices/:id/form", h.Form)
	group.GET("/notices/:id/detail", h.Detail)
	group.POST("/notices", guard("sys:notice:create"), h.Create)
	group.PUT("/notices/read-all", h.ReadAll)
	group.PUT("/notices/:id", guard("sys:notice:update"), h.Update)
	group.PUT("/notices/:id/publish", guard("sys:notice:publish"), h.Publish)
	group.PUT("/notices/:id/revoke", guard("sys:notice:revoke"), h.Revoke)
	group.DELETE("/notices/:ids", guard("sys:notice:delete"), h.Delete)
}

// query 从查询参数构造公告查询 DTO。
func (h *NoticeAPI) query(c *gin.Context) dto.NoticeQuery {
	page, size := PageParams(c)
	return dto.NoticeQuery{
		PageQuery:     dto.PageQuery{PageNum: page, PageSize: size},
		Title:         c.Query("title"),
		Keywords:      c.Query("keywords"),
		PublishStatus: c.Query("publishStatus"),
		Type:          c.Query("type"),
	}
}

// List 处理 GET /api/v1/notices：公告分页（管理端）。
func (h *NoticeAPI) List(c *gin.Context) {
	result, err := h.notices.Page(h.query(c))
	if err != nil {
		Fail(c, err, "查询公告失败")
		return
	}
	OK(c, result)
}

// MyList 处理 GET /api/v1/notices/my：我的通知分页。
func (h *NoticeAPI) MyList(c *gin.Context) {
	user := CurrentUser(c)
	var userID int64
	if user != nil {
		userID = user.ID
	}
	result, err := h.notices.MyPage(h.query(c), userID)
	if err != nil {
		Fail(c, err, "查询公告失败")
		return
	}
	OK(c, result)
}

// Form 处理 GET /api/v1/notices/:id/form：公告编辑表单。
func (h *NoticeAPI) Form(c *gin.Context) {
	form, err := h.notices.Form(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询公告失败")
		return
	}
	OK(c, form)
}

// Detail 处理 GET /api/v1/notices/:id/detail：公告详情。
func (h *NoticeAPI) Detail(c *gin.Context) {
	detail, err := h.notices.Detail(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询公告失败")
		return
	}
	OK(c, detail)
}

// Create 处理 POST /api/v1/notices：新增公告。
func (h *NoticeAPI) Create(c *gin.Context) {
	var req dto.NoticeForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.notices.Create(req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建公告失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// Update 处理 PUT /api/v1/notices/:id：更新公告。
func (h *NoticeAPI) Update(c *gin.Context) {
	var req dto.NoticeForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.notices.Update(c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新公告失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// Publish 处理 PUT /api/v1/notices/:id/publish：发布公告。
func (h *NoticeAPI) Publish(c *gin.Context) {
	if err := h.notices.Publish(c.Param("id"), CurrentUser(c)); err != nil {
		Fail(c, err, "发布公告失败")
		return
	}
	OK(c, nil)
}

// Revoke 处理 PUT /api/v1/notices/:id/revoke：撤回公告。
func (h *NoticeAPI) Revoke(c *gin.Context) {
	if err := h.notices.Revoke(c.Param("id"), CurrentUser(c)); err != nil {
		Fail(c, err, "撤回公告失败")
		return
	}
	OK(c, nil)
}

// ReadAll 处理 PUT /api/v1/notices/read-all：全部标记已读。
func (h *NoticeAPI) ReadAll(c *gin.Context) {
	user := CurrentUser(c)
	var userID int64
	if user != nil {
		userID = user.ID
	}
	if err := h.notices.ReadAll(userID); err != nil {
		Fail(c, err, "标记已读失败")
		return
	}
	OK(c, nil)
}

// Delete 处理 DELETE /api/v1/notices/:ids：删除公告。
func (h *NoticeAPI) Delete(c *gin.Context) {
	if err := h.notices.Delete(c.Param("ids")); err != nil {
		Fail(c, err, "删除公告失败")
		return
	}
	OK(c, nil)
}
