package v1

import (
	"encoding/csv"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// UserAPI 是用户管理的 HTTP 控制器。
type UserAPI struct {
	// users 用户应用服务。
	users *service.UserService
}

// NewUserAPI 创建用户控制器。
//
// 参数 Parameters:
//   - users (*service.UserService): 用户应用服务。
//
// 返回 Returns:
//   - api (*UserAPI): 用户控制器。
func NewUserAPI(users *service.UserService) *UserAPI { return &UserAPI{users: users} }

// Register 注册用户相关路由。
//
// 注册顺序与旧路由保持一致：先注册静态路径（profile/password/options/template/export），
// 再注册带 :id 的动态路径，避免 Gin 路由树冲突。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *UserAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/users", h.List)
	group.GET("/users/options", h.Options)
	group.GET("/users/template", h.Template)
	group.GET("/users/export", h.Export)
	group.GET("/users/:id/form", h.Form)
	group.GET("/users/:id/menu-ids", h.MenuIDs)

	group.GET("/users/profile", h.Profile)
	group.PUT("/users/profile", h.UpdateProfile)
	group.PUT("/users/password", h.ChangePassword)

	// 短信/邮件未接入，统一返回明确的业务提示而不是静默失败。
	group.POST("/users/mobile/code", h.FeatureUnavailable)
	group.POST("/users/email/code", h.FeatureUnavailable)
	group.PUT("/users/mobile", h.FeatureUnavailable)
	group.DELETE("/users/mobile", h.FeatureUnavailable)
	group.PUT("/users/email", h.FeatureUnavailable)
	group.DELETE("/users/email", h.FeatureUnavailable)

	group.POST("/users", guard("sys:user:create"), h.Create)
	group.PUT("/users/:id", guard("sys:user:update"), h.Update)
	group.PUT("/users/:id/menus", guard("sys:user:assign-menu"), h.ReplaceMenus)
	group.PUT("/users/:id/password/reset", guard("sys:user:reset-password"), h.ResetPassword)
	group.DELETE("/users/:ids", guard("sys:user:delete"), h.Delete)
	group.POST("/users/import", guard("sys:user:import"), h.Import)
}

// List 处理 GET /api/v1/users：用户分页。
func (h *UserAPI) List(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.users.Page(dto.UserQuery{
		PageQuery:  dto.PageQuery{PageNum: page, PageSize: size},
		Keywords:   c.Query("keywords"),
		Status:     c.Query("status"),
		DeptID:     c.Query("deptId"),
		CreateTime: c.QueryArray("createTime"),
	}, CurrentUser(c))
	if err != nil {
		Fail(c, err, "查询用户失败")
		return
	}
	OK(c, result)
}

// Options 处理 GET /api/v1/users/options：用户下拉。
func (h *UserAPI) Options(c *gin.Context) {
	options, err := h.users.Options()
	if err != nil {
		Fail(c, err, "查询用户失败")
		return
	}
	OK(c, options)
}

// Form 处理 GET /api/v1/users/:id/form：用户编辑表单。
func (h *UserAPI) Form(c *gin.Context) {
	form, err := h.users.Form(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询用户失败")
		return
	}
	OK(c, form)
}

// Create 处理 POST /api/v1/users：新增用户。
func (h *UserAPI) Create(c *gin.Context) {
	var req dto.UserForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.users.Create(req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建用户失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// Update 处理 PUT /api/v1/users/:id：更新用户。
func (h *UserAPI) Update(c *gin.Context) {
	var req dto.UserForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.users.Update(c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新用户失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// ResetPassword 处理 PUT /api/v1/users/:id/password/reset：重置密码。
func (h *UserAPI) ResetPassword(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	if !BindJSON(c, &req) {
		return
	}
	if err := h.users.ResetPassword(c.Param("id"), req.Password, CurrentUser(c)); err != nil {
		Fail(c, err, "重置密码失败")
		return
	}
	OK(c, nil)
}

// Delete 处理 DELETE /api/v1/users/:ids：删除用户。
func (h *UserAPI) Delete(c *gin.Context) {
	if err := h.users.Delete(c.Param("ids"), CurrentUser(c)); err != nil {
		Fail(c, err, "删除用户失败")
		return
	}
	OK(c, nil)
}

// Export 处理 GET /api/v1/users/export：导出用户 CSV。
func (h *UserAPI) Export(c *gin.Context) {
	filename, records, err := h.users.Export()
	if err != nil {
		Fail(c, err, "导出用户失败")
		return
	}
	WriteCSV(c, filename, records)
}

// Template 处理 GET /api/v1/users/template：下载导入模板。
func (h *UserAPI) Template(c *gin.Context) {
	filename, records := h.users.Template()
	WriteCSV(c, filename, records)
}

// Import 处理 POST /api/v1/users/import：按 CSV 模板导入用户。
func (h *UserAPI) Import(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		BadRequest(c, "请选择要导入的文件")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		Fail(c, err, "读取文件失败")
		return
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(file)
	if err != nil {
		Fail(c, err, "读取文件失败")
		return
	}
	result, err := h.users.Import(content)
	if err != nil {
		Fail(c, err, "导入用户失败")
		return
	}
	OK(c, gin.H{
		"code":         CodeSuccess,
		"validCount":   result.ValidCount,
		"invalidCount": result.InvalidCount,
		"messageList":  result.MessageList,
	})
}

// FeatureUnavailable 处理未接入的短信/邮件相关接口。
func (h *UserAPI) FeatureUnavailable(c *gin.Context) {
	write(c, http.StatusOK, "A0400", "当前部署未接入短信/邮件服务，暂不支持该操作", nil)
}

// Profile 处理 GET /api/v1/users/profile：当前用户资料。
func (h *UserAPI) Profile(c *gin.Context) {
	profile, err := h.users.Profile(CurrentUser(c))
	if err != nil {
		Fail(c, err, "查询个人资料失败")
		return
	}
	OK(c, profile)
}

// UpdateProfile 处理 PUT /api/v1/users/profile：修改个人资料。
func (h *UserAPI) UpdateProfile(c *gin.Context) {
	var req dto.UserProfileForm
	if !BindJSON(c, &req) {
		return
	}
	if err := h.users.UpdateProfile(req, CurrentUser(c)); err != nil {
		Fail(c, err, "更新个人资料失败")
		return
	}
	OK(c, nil)
}

// ChangePassword 处理 PUT /api/v1/users/password：修改当前用户密码。
func (h *UserAPI) ChangePassword(c *gin.Context) {
	var req dto.PasswordChangeForm
	if !BindJSON(c, &req) {
		return
	}
	if err := h.users.ChangePassword(req, CurrentUser(c)); err != nil {
		Fail(c, err, "修改密码失败")
		return
	}
	OK(c, nil)
}

// WriteCSV 以附件形式输出 CSV（带 UTF-8 BOM，保证 Excel 正确识别中文）。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - filename (string): 下载文件名。
//   - records ([][]string): CSV 内容。
func WriteCSV(c *gin.Context, filename string, records [][]string) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Header("Cache-Control", "no-store")
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(c.Writer)
	for _, record := range records {
		if err := writer.Write(record); err != nil {
			return
		}
	}
	writer.Flush()
}

// MenuIDs 处理 GET /api/v1/users/:id/menu-ids：用户个人菜单授权（弹窗回显用）。
func (h *UserAPI) MenuIDs(c *gin.Context) {
	ids, err := h.users.MenuIDs(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询用户菜单失败")
		return
	}
	OK(c, ids)
}

// ReplaceMenus 处理 PUT /api/v1/users/:id/menus：覆盖式保存用户个人菜单授权。
//
// 请求体为菜单 ID 数组（与前端 UserAPI.updateUserMenus 的契约一致）。
func (h *UserAPI) ReplaceMenus(c *gin.Context) {
	var menuIDs []int64
	if err := c.ShouldBindJSON(&menuIDs); err != nil {
		BadRequest(c, "请求参数格式错误")
		return
	}
	if err := h.users.ReplaceMenus(c.Param("id"), menuIDs, CurrentUser(c)); err != nil {
		Fail(c, err, "保存用户菜单失败")
		return
	}
	OK(c, nil)
}
