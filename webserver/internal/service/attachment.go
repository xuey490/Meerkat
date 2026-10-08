package service

import (
	"io"
	"log/slog"
	"strings"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// UploadConfig 是附件本地存储的运行期配置。
type UploadConfig struct {
	// Dir 上传根目录（相对进程工作目录，如 upload）。
	Dir string
	// URLPrefix 静态访问前缀（如 /upload）。
	URLPrefix string
	// MaxSizeMB 单文件大小上限（MB），<=0 表示不限制。
	MaxSizeMB int
}

// AttachmentService 提供附件分类与附件文件的应用服务。
type AttachmentService struct {
	// attachments 附件领域操作。
	attachments *logic.AttachmentLogic
	// cfg 上传配置。
	cfg UploadConfig
}

// NewAttachmentService 创建附件应用服务。
//
// 参数 Parameters:
//   - attachments (*logic.AttachmentLogic): 附件领域操作。
//   - cfg (UploadConfig): 上传配置。
//
// 返回 Returns:
//   - service (*AttachmentService): 附件应用服务。
func NewAttachmentService(attachments *logic.AttachmentLogic, cfg UploadConfig) *AttachmentService {
	if strings.TrimSpace(cfg.Dir) == "" {
		cfg.Dir = "upload"
	}
	if strings.TrimSpace(cfg.URLPrefix) == "" {
		cfg.URLPrefix = "/upload"
	}
	return &AttachmentService{attachments: attachments, cfg: cfg}
}

// MaxBytes 返回单文件大小上限（字节）。
//
// 返回 Returns:
//   - bytes (int64): 上限字节数；0 表示不限制。
func (s *AttachmentService) MaxBytes() int64 {
	if s.cfg.MaxSizeMB <= 0 {
		return 0
	}
	return int64(s.cfg.MaxSizeMB) * 1024 * 1024
}

// CategoryTree 返回附件分类树（按 sort、id 排序）。
//
// 返回 Returns:
//   - items ([]dto.CategoryItem): 根节点列表。
//   - err (error): 查询失败时返回业务错误。
func (s *AttachmentService) CategoryTree() ([]dto.CategoryItem, error) {
	rows, err := s.attachments.Categories()
	if err != nil {
		return nil, err
	}
	items := make(map[int64]*dto.CategoryItem, len(rows))
	parents := make(map[int64]int64, len(rows))
	children := make(map[int64][]int64, len(rows))
	rootIDs := make([]int64, 0, 8)
	for index := range rows {
		item := dto.NewCategoryItem(&rows[index])
		items[rows[index].ID] = &item
		parents[rows[index].ID] = rows[index].ParentID
	}
	for index := range rows {
		id := rows[index].ID
		parentID := parents[id]
		// 父分类已被删除的孤立节点降级为根，避免整棵子树在界面上消失。
		if parentID == 0 || parentID == id {
			rootIDs = append(rootIDs, id)
			continue
		}
		if _, ok := items[parentID]; !ok {
			rootIDs = append(rootIDs, id)
			continue
		}
		children[parentID] = append(children[parentID], id)
	}
	// 递归装配，保证深层子节点不会因装配顺序丢失。
	var build func(id int64) dto.CategoryItem
	build = func(id int64) dto.CategoryItem {
		item := *items[id]
		for _, childID := range children[id] {
			item.Children = append(item.Children, build(childID))
		}
		return item
	}
	roots := make([]dto.CategoryItem, 0, len(rootIDs))
	for _, id := range rootIDs {
		roots = append(roots, build(id))
	}
	return roots, nil
}

// CreateCategory 新增附件分类。
//
// 参数 Parameters:
//   - req (dto.CategoryForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新分类 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *AttachmentService) CreateCategory(req dto.CategoryForm, operator *model.SysUser) (string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return "", apperr.Invalid("分类名称不能为空")
	}
	parentID := firstID(req.ParentID)
	row := model.SysCategory{
		ParentID:     parentID,
		CategoryName: name,
		Sort:         req.Sort,
		Status:       util.NormalizeStatus(int(req.Status)),
		Remark:       strings.TrimSpace(req.Remark),
		AuditFields:  logic.Stamp(operator),
	}
	if err := s.attachments.CreateCategory(&row); err != nil {
		return "", err
	}
	return util.TextID(row.ID), nil
}

// UpdateCategory 更新附件分类（含父级调整与子树层级同步）。
//
// 参数 Parameters:
//   - idText (string): 分类 ID 文本。
//   - req (dto.CategoryForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 分类 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *AttachmentService) UpdateCategory(idText string, req dto.CategoryForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "分类 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.attachments.Category(id)
	if err != nil {
		return "", err
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		row.CategoryName = name
	}
	row.Sort = req.Sort
	row.Status = util.NormalizeStatus(int(req.Status))
	row.Remark = strings.TrimSpace(req.Remark)

	parentChanged := false
	if req.ParentID != "" {
		parentID := firstID(req.ParentID)
		if parentID != row.ParentID {
			if err := s.ensureCategoryParent(row, parentID); err != nil {
				return "", err
			}
			row.ParentID = parentID
			parentChanged = true
		}
	}
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.attachments.UpdateCategory(row, parentChanged); err != nil {
		return "", err
	}
	return util.TextID(row.ID), nil
}

// DeleteCategory 删除附件分类（要求分类下既无子分类也无附件）。
//
// 参数 Parameters:
//   - idText (string): 分类 ID 文本。
//
// 返回 Returns:
//   - err (error): 校验或写入失败时返回业务错误。
func (s *AttachmentService) DeleteCategory(idText string) error {
	id, err := requiredID(idText, "分类 ID 无效")
	if err != nil {
		return err
	}
	row, err := s.attachments.Category(id)
	if err != nil {
		return err
	}
	childCount, err := s.attachments.CategoryChildCount(id)
	if err != nil {
		return err
	}
	if childCount > 0 {
		return apperr.Conflict("请先删除子分类")
	}
	attachmentCount, err := s.attachments.CategoryAttachmentCount(row)
	if err != nil {
		return err
	}
	if attachmentCount > 0 {
		return apperr.Conflict("该分类下还有附件，请先移走或删除")
	}
	return s.attachments.DeleteCategory(id)
}

// ensureCategoryParent 校验新的父分类是否合法。
//
// 参数 Parameters:
//   - row (*model.SysCategory): 待调整的分类。
//   - parentID (int64): 目标父分类主键，0 表示根。
//
// 返回 Returns:
//   - err (error): 非法父分类时返回业务错误。
func (s *AttachmentService) ensureCategoryParent(row *model.SysCategory, parentID int64) error {
	if parentID <= 0 {
		return nil
	}
	if parentID == row.ID {
		return apperr.Invalid("不能把分类挂到自己下面")
	}
	parent, err := s.attachments.Category(parentID)
	if err != nil {
		return err
	}
	// 目标是自己的后代时，其 level 必然以「自己的 level + 自己的 ID + ,」开头。
	if strings.HasPrefix(parent.Level, row.Level+util.TextID(row.ID)+",") {
		return apperr.Invalid("不能把分类移动到自己的子分类下")
	}
	return nil
}

// Page 分页查询附件。
//
// 参数 Parameters:
//   - q (dto.AttachmentQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.AttachmentItem]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *AttachmentService) Page(q dto.AttachmentQuery) (dto.PageResult[dto.AttachmentItem], error) {
	rows, total, err := s.attachments.AttachmentPage(q)
	if err != nil {
		return dto.PageResult[dto.AttachmentItem]{}, err
	}
	categoryIDs := make([]int64, 0, len(rows))
	for index := range rows {
		categoryIDs = append(categoryIDs, rows[index].CategoryID)
	}
	names, err := s.attachments.CategoryNameMap(categoryIDs)
	if err != nil {
		return dto.PageResult[dto.AttachmentItem]{}, err
	}
	list := make([]dto.AttachmentItem, 0, len(rows))
	for index := range rows {
		list = append(list, dto.NewAttachmentItem(&rows[index], names[rows[index].CategoryID]))
	}
	return dto.PageResult[dto.AttachmentItem]{List: list, Total: total}, nil
}

// Upload 保存上传文件并写入附件记录。
//
// 参数 Parameters:
//   - categoryText (string): 目标分类 ID 文本，空表示未分类。
//   - filename (string): 客户端原始文件名。
//   - size (int64): 文件字节数；<=0 时按内容长度计算。
//   - src (io.Reader): 上传内容。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - item (dto.AttachmentItem): 新增的附件。
//   - err (error): 校验、落盘或写库失败时返回业务错误。
func (s *AttachmentService) Upload(categoryText, filename string, size int64, src io.Reader, operator *model.SysUser) (dto.AttachmentItem, error) {
	maxBytes := s.MaxBytes()
	if maxBytes > 0 && size > maxBytes {
		return dto.AttachmentItem{}, apperr.Invalid("文件大小不能超过 " + util.TextID(int64(s.cfg.MaxSizeMB)) + "MB")
	}
	categoryID := firstID(categoryText)
	categoryName := ""
	if categoryID > 0 {
		category, err := s.attachments.Category(categoryID)
		if err != nil {
			return dto.AttachmentItem{}, apperr.Invalid("附件分类不存在")
		}
		categoryName = category.CategoryName
	}
	originName := util.SafeFileName(filename)
	stored, err := util.SaveLocalFile(s.cfg.Dir, s.cfg.URLPrefix, originName, src)
	if err != nil {
		return dto.AttachmentItem{}, apperr.Internal("保存附件失败", err)
	}
	row := model.SysAttachment{
		CategoryID:  categoryID,
		StorageMode: 1,
		OriginName:  util.NonEmptyPtr(originName),
		ObjectName:  stored.ObjectName,
		Hash:        util.NonEmptyPtr(stored.Hash),
		MimeType:    util.NonEmptyPtr(stored.MimeType),
		StoragePath: stored.StoragePath,
		Suffix:      util.NonEmptyPtr(stored.Suffix),
		SizeByte:    util.Int64Ptr(stored.SizeByte),
		SizeInfo:    util.NonEmptyPtr(stored.SizeInfo),
		URL:         stored.URL,
		AuditFields: logic.Stamp(operator),
	}
	if err := s.attachments.CreateAttachment(&row); err != nil {
		// 写库失败时回滚已落盘的文件，避免产生无主文件。
		if removeErr := util.RemoveLocalFile(s.cfg.Dir, stored.StoragePath); removeErr != nil {
			slog.Warn("回滚附件文件失败", "path", stored.StoragePath, "err", removeErr)
		}
		return dto.AttachmentItem{}, err
	}
	return dto.NewAttachmentItem(&row, categoryName), nil
}

// Update 修改附件信息（重命名 / 调整分类 / 备注）。
//
// 参数 Parameters:
//   - idText (string): 附件 ID 文本。
//   - req (dto.AttachmentForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 附件 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *AttachmentService) Update(idText string, req dto.AttachmentForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "附件 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.attachments.Attachment(id)
	if err != nil {
		return "", err
	}
	if name := strings.TrimSpace(req.OriginName); name != "" {
		row.OriginName = util.NonEmptyPtr(util.SafeFileName(name))
	}
	if req.CategoryID != "" {
		categoryID := firstID(req.CategoryID)
		if categoryID > 0 {
			if _, err := s.attachments.Category(categoryID); err != nil {
				return "", apperr.Invalid("附件分类不存在")
			}
		}
		row.CategoryID = categoryID
	}
	row.Remark = util.NonEmptyPtr(strings.TrimSpace(req.Remark))
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.attachments.UpdateAttachment(row); err != nil {
		return "", err
	}
	return util.TextID(row.ID), nil
}

// Delete 删除附件（软删除记录并移除物理文件）。
//
// 参数 Parameters:
//   - idText (string): 附件 ID 文本。
//
// 返回 Returns:
//   - err (error): 校验或写入失败时返回业务错误。
func (s *AttachmentService) Delete(idText string) error {
	id, err := requiredID(idText, "附件 ID 无效")
	if err != nil {
		return err
	}
	row, err := s.attachments.Attachment(id)
	if err != nil {
		return err
	}
	if err := s.attachments.DeleteAttachment(id); err != nil {
		return err
	}
	if removeErr := util.RemoveLocalFile(s.cfg.Dir, row.StoragePath); removeErr != nil {
		// 文件已被手工删除或占用时不影响业务结果，仅记录告警。
		slog.Warn("删除附件文件失败", "path", row.StoragePath, "err", removeErr)
	}
	return nil
}

// DeleteByPath 按存储路径 / 访问地址删除附件，供通用文件接口（/api/v1/files）使用。
//
// 参数 Parameters:
//   - pathText (string): storage_path 或 url。
//
// 返回 Returns:
//   - err (error): 附件不存在或写入失败时返回业务错误。
func (s *AttachmentService) DeleteByPath(pathText string) error {
	row, err := s.attachments.AttachmentByPath(pathText)
	if err != nil {
		return err
	}
	return s.Delete(util.TextID(row.ID))
}

// firstID 取 ID 文本中的首个主键，无法解析时返回 0（表示未分类 / 根分类）。
//
// 参数 Parameters:
//   - raw (string): ID 文本。
//
// 返回 Returns:
//   - id (int64): 首个主键，解析失败为 0。
func firstID(raw string) int64 {
	if ids := util.ParseIDList(raw); len(ids) > 0 {
		return ids[0]
	}
	return 0
}
