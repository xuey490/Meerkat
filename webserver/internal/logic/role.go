package logic

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// RoleLogic 负责 sa_system_role 及其菜单/部门关联表的数据读写。
type RoleLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewRoleLogic 创建角色领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*RoleLogic): 角色领域操作实例。
func NewRoleLogic(db *gorm.DB) *RoleLogic { return &RoleLogic{db: db} }

// Page 分页查询角色。
//
// 参数 Parameters:
//   - q (dto.RoleQuery): 查询条件。
//
// 返回 Returns:
//   - rows ([]model.SysRole): 当前页角色。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *RoleLogic) Page(q dto.RoleQuery) ([]model.SysRole, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysRole{})
	if pattern := util.LikeKeyword(q.Keywords); pattern != "" {
		db = db.Where(`(name LIKE ? ESCAPE '\' OR code LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询角色数量失败", err)
	}
	var rows []model.SysRole
	if err := db.Order("sort").Order("id").Offset(util.Offset(page, size)).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询角色失败", err)
	}
	return rows, total, nil
}

// Enabled 返回启用角色，用于下拉选项。
//
// 返回 Returns:
//   - rows ([]model.SysRole): 启用角色列表。
//   - err (error): 查询失败时返回业务错误。
func (l *RoleLogic) Enabled() ([]model.SysRole, error) {
	var rows []model.SysRole
	if err := l.db.Where("status = 1").Order("sort").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询角色失败", err)
	}
	return rows, nil
}

// Get 按主键查询角色。
//
// 参数 Parameters:
//   - id (int64): 角色主键。
//
// 返回 Returns:
//   - row (*model.SysRole): 角色实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *RoleLogic) Get(id int64) (*model.SysRole, error) {
	var row model.SysRole
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "角色不存在", "查询角色失败")
	}
	return &row, nil
}

// CodeExists 判断角色编码是否已被占用（excludeID 用于编辑场景排除自身）。
//
// 参数 Parameters:
//   - code (string): 角色编码。
//   - excludeID (int64): 需要排除的角色主键，0 表示不排除。
//
// 返回 Returns:
//   - exists (bool): 是否已存在。
//   - err (error): 查询失败时返回业务错误。
func (l *RoleLogic) CodeExists(code string, excludeID int64) (bool, error) {
	// 唯一索引不含 delete_time，软删除角色同样占用编码，见 unique_key.go 说明。
	db := l.db.Unscoped().Model(&model.SysRole{}).Where("code = ?", code)
	if excludeID > 0 {
		db = db.Where("id <> ?", excludeID)
	}
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return false, InternalErr("校验角色编码失败", err)
	}
	return count > 0, nil
}

// Create 新增角色并同步自定义数据权限部门。
//
// 参数 Parameters:
//   - row (*model.SysRole): 角色实体（含审计字段）。
//   - deptIDs ([]int64): 自定义数据权限部门（仅 data_scope=5 时生效）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *RoleLogic) Create(row *model.SysRole, deptIDs []int64) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
			return UniqueOrInternal(err, "角色编码已存在", "创建角色失败")
		}
		return replaceRoleDepts(tx, row.ID, row.DataScope, deptIDs)
	})
}

// Update 保存角色并同步自定义数据权限部门。
//
// 参数 Parameters:
//   - row (*model.SysRole): 已修改的角色实体。
//   - deptIDs ([]int64): 自定义数据权限部门。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *RoleLogic) Update(row *model.SysRole, deptIDs []int64) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(row).Error; err != nil {
			return UniqueOrInternal(err, "角色编码已存在", "更新角色失败")
		}
		return replaceRoleDepts(tx, row.ID, row.DataScope, deptIDs)
	})
}

// Delete 软删除角色并清理关联（角色菜单、角色部门、用户角色）。
//
// 参数 Parameters:
//   - ids ([]int64): 角色主键列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *RoleLogic) Delete(ids []int64) error {
	for _, id := range ids {
		if id == 1 {
			return errInvalid("超级管理员角色不允许删除")
		}
	}
	return l.db.Transaction(func(tx *gorm.DB) error {
		// 软删除同时释放角色编码，否则同编码角色无法再次创建。
		if err := softDeleteFreeKey(tx, "sa_system_role", "code", ids); err != nil {
			return InternalErr("删除角色失败", err)
		}
		if err := tx.Where("role_id IN ?", ids).Delete(&model.SysRoleMenu{}).Error; err != nil {
			return InternalErr("清理角色菜单失败", err)
		}
		if err := tx.Where("role_id IN ?", ids).Delete(&model.SysRoleDept{}).Error; err != nil {
			return InternalErr("清理角色部门失败", err)
		}
		// 关联表物理删除，原因同 syncRelations：唯一索引不含 delete_time。
		if err := tx.Unscoped().Where("role_id IN ?", ids).Delete(&model.SysUserRole{}).Error; err != nil {
			return InternalErr("清理用户角色失败", err)
		}
		return nil
	})
}

// MenuIDs 返回角色已授权的菜单 ID。
//
// 参数 Parameters:
//   - roleID (int64): 角色主键。
//
// 返回 Returns:
//   - ids ([]int64): 菜单 ID 列表（非 nil 切片）。
//   - err (error): 查询失败时返回业务错误。
func (l *RoleLogic) MenuIDs(roleID int64) ([]int64, error) {
	ids := util.EmptyInt64Slice()
	if err := l.db.Model(&model.SysRoleMenu{}).Where("role_id = ?", roleID).Pluck("menu_id", &ids).Error; err != nil {
		return nil, InternalErr("查询角色菜单失败", err)
	}
	return ids, nil
}

// DeptIDs 返回角色自定义数据权限的部门 ID。
//
// 参数 Parameters:
//   - roleID (int64): 角色主键。
//
// 返回 Returns:
//   - ids ([]int64): 部门 ID 列表（非 nil 切片）。
//   - err (error): 查询失败时返回业务错误。
func (l *RoleLogic) DeptIDs(roleID int64) ([]int64, error) {
	ids := util.EmptyInt64Slice()
	if err := l.db.Model(&model.SysRoleDept{}).Where("role_id = ?", roleID).Pluck("dept_id", &ids).Error; err != nil {
		return nil, InternalErr("查询角色部门失败", err)
	}
	return ids, nil
}

// RoleIDsOfUser 返回用户已启用角色的主键列表。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//
// 返回 Returns:
//   - ids ([]int64): 角色主键列表（非 nil）。
//   - err (error): 查询失败时返回业务错误。
func (l *RoleLogic) RoleIDsOfUser(userID int64) ([]int64, error) {
	ids := util.EmptyInt64Slice()
	if err := l.db.Model(&model.SysUserRole{}).
		Where("user_id = ? AND status = 1", userID).
		Pluck("role_id", &ids).Error; err != nil {
		return nil, InternalErr("查询角色失败", err)
	}
	return ids, nil
}

// RoleMenuIDs 返回角色集合已授权的菜单 ID（去重前的原始列表）。
//
// 参数 Parameters:
//   - roleIDs ([]int64): 角色主键列表。
//
// 返回 Returns:
//   - ids ([]int64): 菜单主键列表。
//   - err (error): 查询失败时返回业务错误。
func (l *RoleLogic) RoleMenuIDs(roleIDs []int64) ([]int64, error) {
	if len(roleIDs) == 0 {
		return util.EmptyInt64Slice(), nil
	}
	ids := util.EmptyInt64Slice()
	if err := l.db.Model(&model.SysRoleMenu{}).Where("role_id IN ?", roleIDs).Pluck("menu_id", &ids).Error; err != nil {
		return nil, InternalErr("查询角色菜单失败", err)
	}
	return ids, nil
}

// ReplaceMenus 覆盖式保存角色菜单授权，并自动补齐父级链路。
//
// 参数 Parameters:
//   - roleID (int64): 角色主键。
//   - menuIDs ([]int64): 前端提交的菜单 ID 列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *RoleLogic) ReplaceMenus(roleID int64, menuIDs []int64) error {
	finalIDs, err := l.withParentMenus(menuIDs)
	if err != nil {
		return err
	}
	txErr := l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&model.SysRoleMenu{}).Error; err != nil {
			return InternalErr("清理角色菜单失败", err)
		}
		for _, menuID := range finalIDs {
			row := model.SysRoleMenu{RoleID: roleID, MenuID: menuID}
			if err := tx.Create(&row).Error; err != nil {
				return InternalErr("保存角色菜单失败", err)
			}
		}
		return nil
	})
	if txErr != nil {
		return txErr
	}
	// 仅影响列表展示的更新时间，失败不阻断授权结果。
	return l.db.Model(&model.SysRole{}).Where("id = ?", roleID).
		Update("update_time", time.Now()).Error
}

// withParentMenus 补齐菜单 ID 的父级链路并去重（实现见 CompleteParentMenus，与个人菜单授权共用）。
//
// 参数 Parameters:
//   - menuIDs ([]int64): 前端提交的菜单 ID 列表。
//
// 返回 Returns:
//   - ids ([]int64): 补齐父级后的菜单 ID 列表。
//   - err (error): 查询菜单失败时返回业务错误。
func (l *RoleLogic) withParentMenus(menuIDs []int64) ([]int64, error) {
	return CompleteParentMenus(l.db, menuIDs)
}

// replaceRoleDepts 覆盖式写入角色自定义数据权限部门（仅 data_scope=5 保留数据）。
func replaceRoleDepts(tx *gorm.DB, roleID int64, dataScope int, deptIDs []int64) error {
	if err := tx.Where("role_id = ?", roleID).Delete(&model.SysRoleDept{}).Error; err != nil {
		return InternalErr("保存角色数据权限失败", err)
	}
	if dataScope != 5 {
		return nil
	}
	for _, deptID := range deptIDs {
		row := model.SysRoleDept{RoleID: roleID, DeptID: deptID}
		if err := tx.Create(&row).Error; err != nil {
			return InternalErr("保存角色数据权限失败", err)
		}
	}
	return nil
}

// NormalizeSort 收敛排序值，避免前端空值写入 0 导致排序异常。
//
// 参数 Parameters:
//   - sort (int): 前端提交的排序值。
//
// 返回 Returns:
//   - value (int): 合法排序值，<= 0 时返回 100。
func NormalizeSort(sort int) int {
	if sort <= 0 {
		return 100
	}
	return sort
}

// NormalizeDataScope 收敛数据权限范围到 1..5。
//
// 参数 Parameters:
//   - dataScope (int): 前端提交的数据范围。
//
// 返回 Returns:
//   - value (int): 合法范围值，非法时返回 1（全部数据）。
func NormalizeDataScope(dataScope int) int {
	if dataScope < 1 || dataScope > 5 {
		return 1
	}
	return dataScope
}

// ParseDeptIDs 解析角色表单中的自定义部门 ID 列表。
//
// 参数 Parameters:
//   - values ([]string): 前端提交的部门 ID 文本列表。
//
// 返回 Returns:
//   - ids ([]int64): 解析结果。
func ParseDeptIDs(values []string) []int64 {
	return util.ParseIDList(strings.Join(values, ","))
}
