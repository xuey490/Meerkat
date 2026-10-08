package logic

import (
	"strings"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// PostLogic 负责 sa_system_post 与 sa_system_user_post 的数据读写。
type PostLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewPostLogic 创建岗位领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*PostLogic): 岗位领域操作实例。
func NewPostLogic(db *gorm.DB) *PostLogic { return &PostLogic{db: db} }

// Page 分页查询岗位。
//
// 参数 Parameters:
//   - q (dto.PostQuery): 查询条件（关键字、状态、分页）。
//
// 返回 Returns:
//   - rows ([]model.SysPost): 当前页岗位。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *PostLogic) Page(q dto.PostQuery) ([]model.SysPost, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysPost{})
	if pattern := util.LikeKeyword(q.Keywords); pattern != "" {
		db = db.Where(`(name LIKE ? ESCAPE '\' OR code LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	if status := strings.TrimSpace(q.Status); status != "" {
		db = db.Where("status = ?", util.AtoiSafe(status, 1))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询岗位数量失败", err)
	}
	var rows []model.SysPost
	if err := db.Order("sort").Order("id").Offset(util.Offset(page, size)).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询岗位失败", err)
	}
	return rows, total, nil
}

// Options 返回启用状态的岗位，用于前端下拉。
//
// 返回 Returns:
//   - rows ([]model.SysPost): 按 sort、id 排序的岗位。
//   - err (error): 查询失败时返回业务错误。
func (l *PostLogic) Options() ([]model.SysPost, error) {
	var rows []model.SysPost
	if err := l.db.Where("status = 1").Order("sort").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询岗位失败", err)
	}
	return rows, nil
}

// Get 按主键查询岗位。
//
// 参数 Parameters:
//   - id (int64): 岗位主键。
//
// 返回 Returns:
//   - row (*model.SysPost): 岗位实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *PostLogic) Get(id int64) (*model.SysPost, error) {
	var row model.SysPost
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "岗位不存在", "查询岗位失败")
	}
	return &row, nil
}

// Create 新增岗位。
//
// 参数 Parameters:
//   - row (*model.SysPost): 岗位实体（含审计字段）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *PostLogic) Create(row *model.SysPost) error {
	if err := l.db.Create(row).Error; err != nil {
		return InternalErr("创建岗位失败", err)
	}
	return nil
}

// Update 保存岗位。
//
// 参数 Parameters:
//   - row (*model.SysPost): 已修改的岗位实体。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *PostLogic) Update(row *model.SysPost) error {
	if err := l.db.Save(row).Error; err != nil {
		return InternalErr("更新岗位失败", err)
	}
	return nil
}

// Delete 软删除岗位，并清理用户岗位关联（同一事务内完成）。
//
// 参数 Parameters:
//   - ids ([]int64): 岗位主键列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *PostLogic) Delete(ids []int64) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", ids).Delete(&model.SysPost{}).Error; err != nil {
			return InternalErr("删除岗位失败", err)
		}
		// 关联表物理删除，避免软删除行长期滞留。
		if err := tx.Unscoped().Where("post_id IN ?", ids).Delete(&model.SysUserPost{}).Error; err != nil {
			return InternalErr("清理用户岗位失败", err)
		}
		return nil
	})
}
