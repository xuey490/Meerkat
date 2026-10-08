package v1

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/remote"
)

// DangerousCommandAPI 是高危命令规则管理的 HTTP 控制器。
type DangerousCommandAPI struct {
	remote *remote.Service
}

// NewDangerousCommandAPI 创建高危命令规则控制器。
func NewDangerousCommandAPI(service *remote.Service) *DangerousCommandAPI {
	return &DangerousCommandAPI{remote: service}
}

// Register 注册高危命令规则 REST 路由。
func (h *DangerousCommandAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/monitor/dangerous-commands", guard("monitor:dangerous-command:list"), h.List)
	group.GET("/monitor/dangerous-commands/:id", guard("monitor:dangerous-command:list"), h.Get)
	group.POST("/monitor/dangerous-commands", guard("monitor:dangerous-command:create"), h.Create)
	group.PUT("/monitor/dangerous-commands/:id", guard("monitor:dangerous-command:update"), h.Update)
	group.DELETE("/monitor/dangerous-commands/:id", guard("monitor:dangerous-command:delete"), h.Delete)
	group.PATCH("/monitor/dangerous-commands/:id/toggle", guard("monitor:dangerous-command:update"), h.Toggle)
}

// List 处理 GET /api/v1/monitor/dangerous-commands：分页列表。
func (h *DangerousCommandAPI) List(c *gin.Context) {
	page, size := PageParams(c)
	keyword := c.Query("keyword")
	rows, total, err := h.remote.ListDangerousCommands(page, size, keyword)
	if err != nil {
		Fail(c, err, "查询失败")
		return
	}
	OK(c, gin.H{"list": rows, "total": total})
}

// Get 处理 GET /api/v1/monitor/dangerous-commands/:id：单条详情。
func (h *DangerousCommandAPI) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "ID 无效")
		return
	}
	row, err := h.remote.GetDangerousCommand(uint(id))
	if err != nil {
		Fail(c, err, "查询失败")
		return
	}
	OK(c, row)
}

// Create 处理 POST /api/v1/monitor/dangerous-commands：新增。
func (h *DangerousCommandAPI) Create(c *gin.Context) {
	var form remote.DangerousCommand
	if !BindJSON(c, &form) {
		return
	}
	if strings.TrimSpace(form.Name) == "" {
		BadRequest(c, "规则名称不能为空")
		return
	}
	if strings.TrimSpace(form.Pattern) == "" {
		BadRequest(c, "匹配内容不能为空")
		return
	}
	form.MatchType = normalizeMatchType(form.MatchType)
	if form.MatchType == "" {
		BadRequest(c, "匹配类型仅支持 exact / prefix / regex")
		return
	}
	if err := h.remote.CreateDangerousCommand(&form); err != nil {
		Fail(c, err, "创建失败")
		return
	}
	Created(c, form)
}

// Update 处理 PUT /api/v1/monitor/dangerous-commands/:id：修改。
func (h *DangerousCommandAPI) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "ID 无效")
		return
	}
	var form remote.DangerousCommand
	if !BindJSON(c, &form) {
		return
	}
	if strings.TrimSpace(form.Name) == "" {
		BadRequest(c, "规则名称不能为空")
		return
	}
	if strings.TrimSpace(form.Pattern) == "" {
		BadRequest(c, "匹配内容不能为空")
		return
	}
	form.MatchType = normalizeMatchType(form.MatchType)
	if form.MatchType == "" {
		BadRequest(c, "匹配类型仅支持 exact / prefix / regex")
		return
	}
	form.ID = uint(id)
	form.UpdatedAt = time.Now()
	if err := h.remote.UpdateDangerousCommand(&form); err != nil {
		Fail(c, err, "更新失败")
		return
	}
	OK(c, nil)
}

// Delete 处理 DELETE /api/v1/monitor/dangerous-commands/:id：删除。
func (h *DangerousCommandAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "ID 无效")
		return
	}
	if err := h.remote.DeleteDangerousCommand(uint(id)); err != nil {
		Fail(c, err, "删除失败")
		return
	}
	OK(c, nil)
}

// Toggle 处理 PATCH /api/v1/monitor/dangerous-commands/:id/toggle：启用/禁用。
func (h *DangerousCommandAPI) Toggle(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "ID 无效")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !BindJSON(c, &body) {
		return
	}
	if err := h.remote.ToggleDangerousCommand(uint(id), body.Enabled); err != nil {
		Fail(c, err, "状态更新失败")
		return
	}
	OK(c, nil)
}

func normalizeMatchType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "exact":
		return "exact"
	case "prefix":
		return "prefix"
	case "regex":
		return "regex"
	default:
		return ""
	}
}
