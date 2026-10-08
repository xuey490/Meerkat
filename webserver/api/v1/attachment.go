package v1

import (
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// AttachmentAPI 是附件分类与附件文件的 HTTP 控制器。
type AttachmentAPI struct {
	// attachments 附件应用服务。
	attachments *service.AttachmentService
}

// NewAttachmentAPI 创建附件控制器。
//
// 参数 Parameters:
//   - attachments (*service.AttachmentService): 附件应用服务。
//
// 返回 Returns:
//   - api (*AttachmentAPI): 附件控制器。
func NewAttachmentAPI(attachments *service.AttachmentService) *AttachmentAPI {
	return &AttachmentAPI{attachments: attachments}
}

// Register 注册附件相关路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *AttachmentAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/attachment-categories", guard("sys:attachment:list"), h.CategoryTree)
	group.POST("/attachment-categories", guard("sys:attachment:category:create"), h.CategoryCreate)
	group.PUT("/attachment-categories/:id", guard("sys:attachment:category:update"), h.CategoryUpdate)
	group.DELETE("/attachment-categories/:id", guard("sys:attachment:category:delete"), h.CategoryDelete)

	group.GET("/attachments", guard("sys:attachment:list"), h.List)
	group.POST("/attachments", guard("sys:attachment:upload"), h.Upload)
	group.PUT("/attachments/:id", guard("sys:attachment:update"), h.Update)
	group.DELETE("/attachments/:id", guard("sys:attachment:delete"), h.Delete)

	// 通用文件接口：与前端模板自带的 FileAPI（SingleImageUpload / FileUpload 等组件）
	// 契约一致。这里只要求登录，不绑定附件管理权限——个人中心头像上传同样走该接口。
	group.POST("/files", h.UploadFile)
	group.DELETE("/files", h.DeleteFile)
}

// CategoryTree 处理 GET /api/v1/attachment-categories：附件分类树。
func (h *AttachmentAPI) CategoryTree(c *gin.Context) {
	tree, err := h.attachments.CategoryTree()
	if err != nil {
		Fail(c, err, "查询附件分类失败")
		return
	}
	OK(c, tree)
}

// CategoryCreate 处理 POST /api/v1/attachment-categories：新增附件分类。
func (h *AttachmentAPI) CategoryCreate(c *gin.Context) {
	var req dto.CategoryForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.attachments.CreateCategory(req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建附件分类失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// CategoryUpdate 处理 PUT /api/v1/attachment-categories/:id：更新附件分类。
func (h *AttachmentAPI) CategoryUpdate(c *gin.Context) {
	var req dto.CategoryForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.attachments.UpdateCategory(c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新附件分类失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// CategoryDelete 处理 DELETE /api/v1/attachment-categories/:id：删除附件分类。
func (h *AttachmentAPI) CategoryDelete(c *gin.Context) {
	if err := h.attachments.DeleteCategory(c.Param("id")); err != nil {
		Fail(c, err, "删除附件分类失败")
		return
	}
	OK(c, nil)
}

// List 处理 GET /api/v1/attachments：附件分页。
func (h *AttachmentAPI) List(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.attachments.Page(dto.AttachmentQuery{
		PageQuery:  dto.PageQuery{PageNum: page, PageSize: size},
		CategoryID: c.Query("categoryId"),
		Keywords:   c.Query("keywords"),
	})
	if err != nil {
		Fail(c, err, "查询附件失败")
		return
	}
	OK(c, result)
}

// Upload 处理 POST /api/v1/attachments：上传附件到指定分类。
func (h *AttachmentAPI) Upload(c *gin.Context) {
	fileHeader, err := h.prepareUpload(c)
	if err != nil {
		Fail(c, err, "上传附件失败")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		BadRequest(c, "读取上传文件失败")
		return
	}
	defer func() { _ = file.Close() }()

	item, err := h.attachments.Upload(c.PostForm("categoryId"), fileHeader.Filename, fileHeader.Size, file, CurrentUser(c))
	if err != nil {
		Fail(c, err, "上传附件失败")
		return
	}
	Created(c, item)
}

// Update 处理 PUT /api/v1/attachments/:id：重命名 / 调整分类 / 备注。
func (h *AttachmentAPI) Update(c *gin.Context) {
	var req dto.AttachmentForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.attachments.Update(c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新附件失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// Delete 处理 DELETE /api/v1/attachments/:id：删除附件。
func (h *AttachmentAPI) Delete(c *gin.Context) {
	if err := h.attachments.Delete(c.Param("id")); err != nil {
		Fail(c, err, "删除附件失败")
		return
	}
	OK(c, nil)
}

// UploadFile 处理 POST /api/v1/files：通用文件上传（返回 FileInfo，供上传组件直接使用）。
func (h *AttachmentAPI) UploadFile(c *gin.Context) {
	fileHeader, err := h.prepareUpload(c)
	if err != nil {
		Fail(c, err, "上传文件失败")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		BadRequest(c, "读取上传文件失败")
		return
	}
	defer func() { _ = file.Close() }()

	item, err := h.attachments.Upload(c.PostForm("categoryId"), fileHeader.Filename, fileHeader.Size, file, CurrentUser(c))
	if err != nil {
		Fail(c, err, "上传文件失败")
		return
	}
	OK(c, dto.FileInfo{Name: item.OriginName, URL: item.URL})
}

// DeleteFile 处理 DELETE /api/v1/files?filePath=...：按访问地址删除文件记录与物理文件。
func (h *AttachmentAPI) DeleteFile(c *gin.Context) {
	filePath := c.Query("filePath")
	if filePath == "" {
		BadRequest(c, "缺少文件路径")
		return
	}
	if err := h.attachments.DeleteByPath(filePath); err != nil {
		Fail(c, err, "删除文件失败")
		return
	}
	OK(c, nil)
}

// prepareUpload 解析上传表单并校验单文件大小。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文（multipart/form-data）。
//
// 返回 Returns:
//   - header (*multipart.FileHeader): 文件头信息。
//   - err (error): 缺少文件或超出大小上限时返回业务错误。
func (h *AttachmentAPI) prepareUpload(c *gin.Context) (*multipart.FileHeader, error) {
	maxBytes := h.attachments.MaxBytes()
	if maxBytes > 0 {
		// 预留 1MB 余量给表单中的其它字段，超限时读取阶段即失败。
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+1024*1024)
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return nil, apperr.Invalid("请选择要上传的文件")
	}
	if maxBytes > 0 && fileHeader.Size > maxBytes {
		return nil, apperr.Invalid("文件大小超出上限")
	}
	return fileHeader, nil
}
