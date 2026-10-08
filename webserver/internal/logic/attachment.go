package logic

import (
	"strings"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// AttachmentLogic 负责 sa_system_category 与 sa_system_attachment 的数据读写。
type AttachmentLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewAttachmentLogic 创建附件领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*AttachmentLogic): 附件领域操作实例。
func NewAttachmentLogic(db *gorm.DB) *AttachmentLogic { return &AttachmentLogic{db: db} }

// Categories 返回全部未删除的分类（按 sort、id 升序）。
//
// 返回 Returns:
//   - rows ([]model.SysCategory): 分类列表。
//   - err (error): 查询失败时返回业务错误。
func (l *AttachmentLogic) Categories() ([]model.SysCategory, error) {
	var rows []model.SysCategory
	if err := l.db.Order("sort").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询附件分类失败", err)
	}
	return rows, nil
}

// Category 按主键查询分类。
//
// 参数 Parameters:
//   - id (int64): 分类主键。
//
// 返回 Returns:
//   - row (*model.SysCategory): 分类实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *AttachmentLogic) Category(id int64) (*model.SysCategory, error) {
	var row model.SysCategory
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "附件分类不存在", "查询附件分类失败")
	}
	return &row, nil
}

// CategoryLevel 计算挂在 parentID 下的子分类 level 前缀。
//
// 约定与 init.sql 一致：根分类 level 为 "0,"，其子分类为 "0,1,"（1 为父分类主键）。
//
// 参数 Parameters:
//   - parentID (int64): 父分类主键，0 表示根。
//
// 返回 Returns:
//   - level (string): level 前缀。
//   - err (error): 父分类不存在时返回业务错误。
func (l *AttachmentLogic) CategoryLevel(parentID int64) (string, error) {
	if parentID <= 0 {
		return "0,", nil
	}
	parent, err := l.Category(parentID)
	if err != nil {
		return "", err
	}
	return parent.Level + util.TextID(parent.ID) + ",", nil
}

// CreateCategory 新增分类，level 由父分类推导。
//
// 参数 Parameters:
//   - row (*model.SysCategory): 分类实体（Level 由本方法填充）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *AttachmentLogic) CreateCategory(row *model.SysCategory) error {
	level, err := l.CategoryLevel(row.ParentID)
	if err != nil {
		return err
	}
	row.Level = level
	if err := l.db.Create(row).Error; err != nil {
		return InternalErr("创建附件分类失败", err)
	}
	return nil
}

// UpdateCategory 保存分类；父分类变化时同步改写整棵子树的 level。
//
// 参数 Parameters:
//   - row (*model.SysCategory): 已修改的分类实体，Level 需为改动前的值。
//   - parentChanged (bool): 父分类是否发生变化。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *AttachmentLogic) UpdateCategory(row *model.SysCategory, parentChanged bool) error {
	oldPrefix := row.Level + util.TextID(row.ID) + ","
	newLevel, err := l.CategoryLevel(row.ParentID)
	if err != nil {
		return err
	}
	row.Level = newLevel
	newPrefix := newLevel + util.TextID(row.ID) + ","
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(row).Error; err != nil {
			return InternalErr("更新附件分类失败", err)
		}
		if !parentChanged || oldPrefix == newPrefix {
			return nil
		}
		// 子树 level 均为 <oldPrefix> 开头，替换前缀即可整体迁移。
		statement := "UPDATE sa_system_category SET level = ? || substr(level, ?) WHERE level LIKE ? || '%'"
		if err := tx.Exec(statement, newPrefix, len(oldPrefix)+1, oldPrefix).Error; err != nil {
			return InternalErr("同步子分类层级失败", err)
		}
		return nil
	})
}

// CategoryChildCount 统计子分类数量。
//
// 参数 Parameters:
//   - id (int64): 分类主键。
//
// 返回 Returns:
//   - count (int64): 子分类数量。
//   - err (error): 查询失败时返回业务错误。
func (l *AttachmentLogic) CategoryChildCount(id int64) (int64, error) {
	var count int64
	if err := l.db.Model(&model.SysCategory{}).Where("parent_id = ?", id).Count(&count).Error; err != nil {
		return 0, InternalErr("查询子分类失败", err)
	}
	return count, nil
}

// CategoryAttachmentCount 统计分类下（含子分类）的附件数量。
//
// 参数 Parameters:
//   - row (*model.SysCategory): 分类实体。
//
// 返回 Returns:
//   - count (int64): 附件数量。
//   - err (error): 查询失败时返回业务错误。
func (l *AttachmentLogic) CategoryAttachmentCount(row *model.SysCategory) (int64, error) {
	prefix := row.Level + util.TextID(row.ID) + ","
	var count int64
	err := l.db.Model(&model.SysAttachment{}).
		Where("category_id IN (?)", l.db.Model(&model.SysCategory{}).
			Select("id").
			Where("id = ? OR level LIKE ?", row.ID, prefix+"%")).
		Count(&count).Error
	if err != nil {
		return 0, InternalErr("查询分类附件失败", err)
	}
	return count, nil
}

// DeleteCategory 软删除分类。
//
// 参数 Parameters:
//   - id (int64): 分类主键。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *AttachmentLogic) DeleteCategory(id int64) error {
	if err := l.db.Where("id = ?", id).Delete(&model.SysCategory{}).Error; err != nil {
		return InternalErr("删除附件分类失败", err)
	}
	return nil
}

// AttachmentPage 分页查询附件。
//
// 参数 Parameters:
//   - q (dto.AttachmentQuery): 查询条件。
//
// 返回 Returns:
//   - rows ([]model.SysAttachment): 当前页附件。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *AttachmentLogic) AttachmentPage(q dto.AttachmentQuery) ([]model.SysAttachment, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysAttachment{})
	if ids := util.ParseIDList(q.CategoryID); len(ids) > 0 {
		db = db.Where("category_id = ?", ids[0])
	}
	if pattern := util.LikeKeyword(q.Keywords); pattern != "" {
		db = db.Where(`(origin_name LIKE ? ESCAPE '\' OR remark LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询附件失败", err)
	}
	var rows []model.SysAttachment
	if err := db.Order("create_time DESC").Order("id DESC").
		Limit(size).Offset(util.Offset(page, size)).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询附件失败", err)
	}
	return rows, total, nil
}

// CategoryNameMap 返回分类主键到名称的映射。
//
// 参数 Parameters:
//   - ids ([]int64): 分类主键列表。
//
// 返回 Returns:
//   - names (map[int64]string): 分类名称映射。
//   - err (error): 查询失败时返回业务错误。
func (l *AttachmentLogic) CategoryNameMap(ids []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	var rows []model.SysCategory
	if err := l.db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, InternalErr("查询附件分类失败", err)
	}
	for _, row := range rows {
		names[row.ID] = row.CategoryName
	}
	return names, nil
}

// Attachment 按主键查询附件。
//
// 参数 Parameters:
//   - id (int64): 附件主键。
//
// 返回 Returns:
//   - row (*model.SysAttachment): 附件实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *AttachmentLogic) Attachment(id int64) (*model.SysAttachment, error) {
	var row model.SysAttachment
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "附件不存在", "查询附件失败")
	}
	return &row, nil
}

// AttachmentByPath 按存储路径或访问地址查询附件。
//
// 参数 Parameters:
//   - pathText (string): storage_path 或 url。
//
// 返回 Returns:
//   - row (*model.SysAttachment): 附件实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *AttachmentLogic) AttachmentByPath(pathText string) (*model.SysAttachment, error) {
	trimmed := strings.TrimSpace(pathText)
	if trimmed == "" {
		return nil, NotFoundErr("附件不存在")
	}
	var row model.SysAttachment
	// 前端可能提交绝对 URL（含 http://host），这里只比对路径部分。
	if index := strings.Index(trimmed, "/upload/"); index > 0 {
		trimmed = trimmed[index:]
	}
	storagePath := strings.TrimPrefix(trimmed, "/")
	if err := l.db.Where("url = ? OR storage_path = ?", trimmed, storagePath).First(&row).Error; err != nil {
		return nil, TranslateDBError(err, "附件不存在", "查询附件失败")
	}
	return &row, nil
}

// CreateAttachment 新增附件记录。
//
// 参数 Parameters:
//   - row (*model.SysAttachment): 附件实体（含审计字段）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *AttachmentLogic) CreateAttachment(row *model.SysAttachment) error {
	if err := l.db.Create(row).Error; err != nil {
		return InternalErr("保存附件记录失败", err)
	}
	return nil
}

// UpdateAttachment 保存附件记录。
//
// 参数 Parameters:
//   - row (*model.SysAttachment): 已修改的附件实体。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *AttachmentLogic) UpdateAttachment(row *model.SysAttachment) error {
	if err := l.db.Save(row).Error; err != nil {
		return InternalErr("更新附件失败", err)
	}
	return nil
}

// DeleteAttachment 软删除附件记录。
//
// 参数 Parameters:
//   - id (int64): 附件主键。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *AttachmentLogic) DeleteAttachment(id int64) error {
	if err := l.db.Where("id = ?", id).Delete(&model.SysAttachment{}).Error; err != nil {
		return InternalErr("删除附件失败", err)
	}
	return nil
}
