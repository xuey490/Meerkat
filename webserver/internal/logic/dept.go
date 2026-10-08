package logic

import (
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// DeptLogic 负责 sa_system_dept 与 sa_system_role_dept 的部门侧读写。
type DeptLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewDeptLogic 创建部门领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*DeptLogic): 部门领域操作实例。
func NewDeptLogic(db *gorm.DB) *DeptLogic { return &DeptLogic{db: db} }

// List 按条件返回部门列表（前端使用树形表格，不做分页）。
//
// 参数 Parameters:
//   - q (dto.DeptQuery): 查询条件（关键字、状态）。
//
// 返回 Returns:
//   - rows ([]model.SysDept): 部门列表。
//   - err (error): 查询失败时返回业务错误。
func (l *DeptLogic) List(q dto.DeptQuery) ([]model.SysDept, error) {
	db := l.db.Model(&model.SysDept{})
	if pattern := util.LikeKeyword(q.Keywords); pattern != "" {
		db = db.Where(`(name LIKE ? ESCAPE '\' OR code LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	if status := strings.TrimSpace(q.Status); status != "" {
		db = db.Where("status = ?", util.AtoiSafe(status, 1))
	}
	var rows []model.SysDept
	if err := db.Order("sort").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询部门失败", err)
	}
	return rows, nil
}

// Enabled 返回启用状态的部门，用于下拉树。
//
// 返回 Returns:
//   - rows ([]model.SysDept): 部门列表。
//   - err (error): 查询失败时返回业务错误。
func (l *DeptLogic) Enabled() ([]model.SysDept, error) {
	var rows []model.SysDept
	if err := l.db.Where("status = 1").Order("sort").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询部门失败", err)
	}
	return rows, nil
}

// Get 按主键查询部门。
//
// 参数 Parameters:
//   - id (int64): 部门主键。
//
// 返回 Returns:
//   - row (*model.SysDept): 部门实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *DeptLogic) Get(id int64) (*model.SysDept, error) {
	var row model.SysDept
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "部门不存在", "查询部门失败")
	}
	return &row, nil
}

// Level 计算指定父部门下的祖级列表。
//
// 参数 Parameters:
//   - parentID (int64): 父部门主键；0 表示根节点。
//
// 返回 Returns:
//   - level (string): 形如 "0,1,3," 的祖级列表。
//   - err (error): 父部门查询失败时返回业务错误。
func (l *DeptLogic) Level(parentID int64) (string, error) {
	if parentID <= 0 {
		return "0,", nil
	}
	var parent model.SysDept
	if err := l.db.First(&parent, "id = ?", parentID).Error; err != nil {
		return "", TranslateDBError(err, "上级部门不存在", "查询上级部门失败")
	}
	return parent.Level + strconv.FormatInt(parent.ID, 10) + ",", nil
}

// Create 新增部门。
//
// 参数 Parameters:
//   - row (*model.SysDept): 部门实体（含审计字段与 level）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *DeptLogic) Create(row *model.SysDept) error {
	if err := l.db.Create(row).Error; err != nil {
		return InternalErr("创建部门失败", err)
	}
	return nil
}

// Update 保存部门；当祖级列表变化时同步重写子孙部门。
//
// 参数 Parameters:
//   - row (*model.SysDept): 已修改的部门实体。
//   - oldLevel (string): 变更前的祖级列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *DeptLogic) Update(row *model.SysDept, oldLevel string) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(row).Error; err != nil {
			return InternalErr("更新部门失败", err)
		}
		if oldLevel == row.Level {
			return nil
		}
		oldPrefix := oldLevel + strconv.FormatInt(row.ID, 10) + ","
		newPrefix := row.Level + strconv.FormatInt(row.ID, 10) + ","
		var children []model.SysDept
		if err := tx.Where("level LIKE ?", oldPrefix+"%").Find(&children).Error; err != nil {
			return InternalErr("同步下级部门失败", err)
		}
		for index := range children {
			child := children[index]
			updated := strings.Replace(child.Level, oldPrefix, newPrefix, 1)
			if updated == child.Level {
				continue
			}
			if err := tx.Model(&model.SysDept{}).Where("id = ?", child.ID).Update("level", updated).Error; err != nil {
				return InternalErr("同步下级部门失败", err)
			}
		}
		return nil
	})
}

// Delete 删除部门，存在子部门或归属用户时拒绝。
//
// 参数 Parameters:
//   - ids ([]int64): 部门主键列表。
//
// 返回 Returns:
//   - err (error): 校验不通过或写入失败时返回业务错误。
func (l *DeptLogic) Delete(ids []int64) error {
	for _, id := range ids {
		var childCount int64
		if err := l.db.Model(&model.SysDept{}).Where("parent_id = ?", id).Count(&childCount).Error; err != nil {
			return InternalErr("校验子部门失败", err)
		}
		if childCount > 0 {
			return errInvalid("存在下级部门，无法删除")
		}
		var userCount int64
		if err := l.db.Model(&model.SysUser{}).Where("dept_id = ?", id).Count(&userCount).Error; err != nil {
			return InternalErr("校验部门用户失败", err)
		}
		if userCount > 0 {
			return errInvalid("部门下存在用户，无法删除")
		}
	}
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", ids).Delete(&model.SysDept{}).Error; err != nil {
			return InternalErr("删除部门失败", err)
		}
		if err := tx.Where("dept_id IN ?", ids).Delete(&model.SysRoleDept{}).Error; err != nil {
			return InternalErr("清理角色部门失败", err)
		}
		return nil
	})
}

// WithDescendants 返回给定部门及其全部子孙部门的 ID 集合。
//
// 参数 Parameters:
//   - ids ([]int64): 起始部门 ID 列表。
//
// 返回 Returns:
//   - result ([]int64): 含子孙的部门 ID 列表（去重）。
//   - err (error): 查询失败时返回业务错误。
func (l *DeptLogic) WithDescendants(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return util.EmptyInt64Slice(), nil
	}
	var rows []model.SysDept
	if err := l.db.Select("id", "level").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询部门失败", err)
	}
	levels := make(map[int64]string, len(rows))
	for _, row := range rows {
		levels[row.ID] = row.Level
	}
	result := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
		if levels[id] == "" {
			continue
		}
		prefix := levels[id] + strconv.FormatInt(id, 10) + ","
		for _, row := range rows {
			if row.ID == id || seen[row.ID] {
				continue
			}
			if strings.HasPrefix(row.Level, prefix) {
				seen[row.ID] = true
				result = append(result, row.ID)
			}
		}
	}
	return result, nil
}
