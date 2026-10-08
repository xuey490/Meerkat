package logic

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/permission"
	"github.com/company/monitor-webserver/internal/util"
)

// UserLogic 负责 sa_system_user 及用户角色/岗位/部门关联的数据读写。
type UserLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewUserLogic 创建用户领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*UserLogic): 用户领域操作实例。
func NewUserLogic(db *gorm.DB) *UserLogic { return &UserLogic{db: db} }

// Page 按条件分页查询用户，并按数据权限范围收敛结果。
//
// 参数 Parameters:
//   - q (dto.UserQuery): 查询条件。
//   - scope (permission.Scope): 当前用户的数据权限范围。
//   - currentID (int64): 当前用户主键。
//   - deptIDs ([]int64): 部门过滤（含下级部门），为空表示不过滤。
//
// 返回 Returns:
//   - rows ([]model.SysUser): 当前页用户。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) Page(q dto.UserQuery, scope permission.Scope, currentID int64, deptIDs []int64) ([]model.SysUser, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysUser{})
	if pattern := util.LikeKeyword(q.Keywords); pattern != "" {
		db = db.Where(`(username LIKE ? ESCAPE '\' OR realname LIKE ? ESCAPE '\' OR phone LIKE ? ESCAPE '\')`,
			pattern, pattern, pattern)
	}
	if status := strings.TrimSpace(q.Status); status != "" {
		db = db.Where("status = ?", util.AtoiSafe(status, 1))
	}
	if len(deptIDs) > 0 {
		db = db.Where("dept_id IN ?", deptIDs)
	}
	if start, end := util.ParseTimePair(q.CreateTime); start != nil || end != nil {
		if start != nil {
			db = db.Where("create_time >= ?", *start)
		}
		if end != nil {
			db = db.Where("create_time <= ?", end.Add(24*time.Hour).Add(-time.Second))
		}
	}
	db = permission.ApplyUserScope(db, scope, currentID)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询用户数量失败", err)
	}
	var rows []model.SysUser
	if err := db.Order("id").Offset(util.Offset(page, size)).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询用户失败", err)
	}
	return rows, total, nil
}

// Enabled 返回启用用户，用于下拉选项。
//
// 返回 Returns:
//   - rows ([]model.SysUser): 启用用户列表（仅取必要列）。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) Enabled() ([]model.SysUser, error) {
	var rows []model.SysUser
	if err := l.db.Select("id", "username", "realname").Where("status = 1").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询用户失败", err)
	}
	return rows, nil
}

// ExportRows 返回用于导出 CSV 的用户列表（最多 5000 条）。
//
// 返回 Returns:
//   - rows ([]model.SysUser): 用户列表。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) ExportRows() ([]model.SysUser, error) {
	var rows []model.SysUser
	if err := l.db.Order("id").Limit(5000).Find(&rows).Error; err != nil {
		return nil, InternalErr("导出用户失败", err)
	}
	return rows, nil
}

// Get 按主键查询用户。
//
// 参数 Parameters:
//   - id (int64): 用户主键。
//
// 返回 Returns:
//   - row (*model.SysUser): 用户实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *UserLogic) Get(id int64) (*model.SysUser, error) {
	var row model.SysUser
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "用户不存在", "查询用户失败")
	}
	return &row, nil
}

// UsernameExists 判断用户名是否已被占用（excludeID 用于编辑场景排除自身）。
//
// 参数 Parameters:
//   - username (string): 用户名。
//   - excludeID (int64): 需要排除的用户主键，0 表示不排除。
//
// 返回 Returns:
//   - exists (bool): 是否已存在。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) UsernameExists(username string, excludeID int64) (bool, error) {
	// 唯一索引 ux_sa_system_user_username 不含 delete_time，软删除行同样占用用户名，
	// 因此这里必须带 Unscoped 统计，才能给出「用户名已存在」而不是让写入撞约束报 500。
	db := l.db.Unscoped().Model(&model.SysUser{}).Where("username = ?", username)
	if excludeID > 0 {
		db = db.Where("id <> ?", excludeID)
	}
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return false, InternalErr("校验用户名失败", err)
	}
	return count > 0, nil
}

// RoleIDs 返回用户的角色主键。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//
// 返回 Returns:
//   - ids ([]int64): 角色主键列表（非 nil）。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) RoleIDs(userID int64) ([]int64, error) {
	ids := util.EmptyInt64Slice()
	if err := l.db.Model(&model.SysUserRole{}).Where("user_id = ?", userID).Pluck("role_id", &ids).Error; err != nil {
		return nil, InternalErr("查询用户角色失败", err)
	}
	return ids, nil
}

// PostIDs 返回用户的岗位主键。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//
// 返回 Returns:
//   - ids ([]int64): 岗位主键列表（非 nil）。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) PostIDs(userID int64) ([]int64, error) {
	ids := util.EmptyInt64Slice()
	if err := l.db.Model(&model.SysUserPost{}).Where("user_id = ?", userID).Pluck("post_id", &ids).Error; err != nil {
		return nil, InternalErr("查询用户岗位失败", err)
	}
	return ids, nil
}

// DeptNames 返回部门 ID 到名称的映射（批量展示，避免逐行查询）。
//
// 返回 Returns:
//   - names (map[int64]string): 部门名称映射。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) DeptNames() (map[int64]string, error) {
	var rows []model.SysDept
	if err := l.db.Select("id", "name").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询部门失败", err)
	}
	names := make(map[int64]string, len(rows))
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}

// DeptIDByName 返回部门名称到主键的映射（导入时按名称匹配）。
//
// 返回 Returns:
//   - ids (map[string]int64): 部门名称映射。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) DeptIDByName() (map[string]int64, error) {
	var rows []model.SysDept
	if err := l.db.Select("id", "name").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询部门失败", err)
	}
	ids := make(map[string]int64, len(rows))
	for _, row := range rows {
		ids[strings.TrimSpace(row.Name)] = row.ID
	}
	return ids, nil
}

// RoleNames 返回用户 ID 到角色名称串（逗号分隔）的映射。
//
// 参数 Parameters:
//   - userIDs ([]int64): 用户主键列表。
//
// 返回 Returns:
//   - names (map[int64]string): 用户 ID 到角色名称的映射。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) RoleNames(userIDs []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(userIDs))
	if len(userIDs) == 0 {
		return names, nil
	}
	var rows []struct {
		UserID int64
		Name   string
	}
	err := l.db.Table("sa_system_user_role AS ur").
		Select("ur.user_id AS user_id, r.name AS name").
		Joins("JOIN sa_system_role AS r ON r.id = ur.role_id AND r.delete_time IS NULL").
		Where("ur.user_id IN ? AND ur.status = 1", userIDs).
		Order("r.sort").
		Scan(&rows).Error
	if err != nil {
		return nil, InternalErr("查询角色失败", err)
	}
	grouped := make(map[int64][]string, len(userIDs))
	for _, row := range rows {
		grouped[row.UserID] = append(grouped[row.UserID], row.Name)
	}
	for _, userID := range userIDs {
		names[userID] = strings.Join(grouped[userID], ",")
	}
	return names, nil
}

// Create 新增用户并覆盖式保存角色/岗位关联。
//
// 参数 Parameters:
//   - row (*model.SysUser): 用户实体（含密码哈希与审计字段）。
//   - roleIDs ([]int64): 角色主键列表。
//   - postIDs ([]int64): 岗位主键列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *UserLogic) Create(row *model.SysUser, roleIDs, postIDs []int64) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
			return UniqueOrInternal(err, "用户名已存在", "创建用户失败")
		}
		return syncRelations(tx, row.ID, roleIDs, postIDs, row.AuditFields)
	})
}

// Update 保存用户基础信息并覆盖式保存角色/岗位关联。
//
// 参数 Parameters:
//   - row (*model.SysUser): 已修改的用户实体。
//   - roleIDs ([]int64): 角色主键列表（nil 表示不修改岗位关联；空切片表示清空）。
//   - postIDs ([]int64): 岗位主键列表；nil 表示保留原岗位关联。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *UserLogic) Update(row *model.SysUser, roleIDs, postIDs []int64) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(row).Error; err != nil {
			return UniqueOrInternal(err, "用户名已存在", "更新用户失败")
		}
		return syncRelations(tx, row.ID, roleIDs, postIDs, row.AuditFields)
	})
}

// MenuIDs 返回用户的个人菜单授权（sa_system_user_menu）。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//
// 返回 Returns:
//   - ids ([]int64): 菜单 ID 列表（非 nil 切片）。
//   - err (error): 查询失败时返回业务错误。
func (l *UserLogic) MenuIDs(userID int64) ([]int64, error) {
	ids := util.EmptyInt64Slice()
	if err := l.db.Model(&model.SysUserMenu{}).Where("user_id = ?", userID).Pluck("menu_id", &ids).Error; err != nil {
		return nil, InternalErr("查询用户菜单失败", err)
	}
	return ids, nil
}

// ReplaceMenus 覆盖式保存用户的个人菜单授权，并自动补齐父级链路。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//   - menuIDs ([]int64): 前端提交的菜单 ID 列表。
//   - audit (model.AuditFields): 关联行审计字段（取自当前操作人）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *UserLogic) ReplaceMenus(userID int64, menuIDs []int64, audit model.AuditFields) error {
	finalIDs, err := CompleteParentMenus(l.db, menuIDs)
	if err != nil {
		return err
	}
	// created_by / updated_by 为 NOT NULL DEFAULT 0，必须显式给值，否则 GORM 写入的 NULL 触发约束失败。
	relationAudit := model.AuditFields{
		CreatedBy:  ActorID(nil),
		UpdatedBy:  ActorID(nil),
		CreateTime: util.NowPtr(),
		UpdateTime: util.NowPtr(),
	}
	if audit.CreatedBy != nil {
		relationAudit.CreatedBy = audit.CreatedBy
	}
	if audit.UpdatedBy != nil {
		relationAudit.UpdatedBy = audit.UpdatedBy
	}
	txErr := l.db.Transaction(func(tx *gorm.DB) error {
		// 物理删除：唯一索引是 (user_id, menu_id, tenant_id)，不含 delete_time，
		// 若按软删除处理，旧行仍占位，重新授权同一菜单会撞唯一约束。
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&model.SysUserMenu{}).Error; err != nil {
			return InternalErr("清理用户菜单失败", err)
		}
		for _, menuID := range finalIDs {
			row := model.SysUserMenu{
				UserID:      userID,
				MenuID:      menuID,
				TenantID:    0,
				Status:      1,
				IsShow:      1,
				IsCreate:    1,
				IsUpdate:    1,
				IsDelete:    1,
				AuditFields: relationAudit,
			}
			if err := tx.Create(&row).Error; err != nil {
				return InternalErr("保存用户菜单失败", err)
			}
		}
		return nil
	})
	if txErr != nil {
		return txErr
	}
	// 仅影响列表展示的更新时间，失败不阻断授权结果。
	return l.db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("update_time", time.Now()).Error
}

// ResetPassword 重置用户密码并吊销其全部刷新令牌。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//   - hash (string): bcrypt 密码哈希。
//   - operatorID (int64): 操作人主键。
//
// 返回 Returns:
//   - err (error): 用户不存在时返回 NotFound，写入失败时返回 Internal。
func (l *UserLogic) ResetPassword(userID int64, hash string, operatorID int64) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		result := tx.Model(&model.SysUser{}).Where("id = ?", userID).Updates(map[string]any{
			"password":    hash,
			"updated_by":  operatorID,
			"update_time": now,
		})
		if result.Error != nil {
			return InternalErr("重置密码失败", result.Error)
		}
		if result.RowsAffected == 0 {
			return NotFoundErr("用户不存在")
		}
		// 密码已变更，吊销该用户所有刷新令牌，强制重新登录。
		if err := tx.Model(&model.SysRefreshToken{}).
			Where("user_id = ? AND revoked_at IS NULL", userID).
			Update("revoked_at", now).Error; err != nil {
			return InternalErr("吊销令牌失败", err)
		}
		return nil
	})
}

// ChangePassword 修改用户密码并吊销其刷新令牌。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//   - hash (string): bcrypt 密码哈希。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *UserLogic) ChangePassword(userID int64, hash string) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&model.SysUser{}).Where("id = ?", userID).Updates(map[string]any{
			"password":    hash,
			"update_time": now,
		}).Error; err != nil {
			return InternalErr("修改密码失败", err)
		}
		// 修改密码后吊销本人刷新令牌，避免旧令牌继续换取访问令牌。
		if err := tx.Model(&model.SysRefreshToken{}).
			Where("user_id = ? AND revoked_at IS NULL", userID).
			Update("revoked_at", now).Error; err != nil {
			return InternalErr("吊销令牌失败", err)
		}
		return nil
	})
}

// UpdateProfile 修改用户的昵称、头像与性别。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//   - nickname (string): 昵称，空串表示不修改。
//   - avatar (string): 头像地址。
//   - gender (string): 数据库性别值。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *UserLogic) UpdateProfile(userID int64, nickname, avatar, gender string) error {
	updates := map[string]any{
		"gender":      gender,
		"avatar":      avatar,
		"update_time": time.Now(),
	}
	if strings.TrimSpace(nickname) != "" {
		updates["realname"] = strings.TrimSpace(nickname)
	}
	if err := l.db.Model(&model.SysUser{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return InternalErr("更新个人资料失败", err)
	}
	return nil
}

// Delete 软删除用户并清理角色/岗位关联。
//
// 参数 Parameters:
//   - ids ([]int64): 用户主键列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *UserLogic) Delete(ids []int64) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		// 软删除同时释放用户名，否则同名账号会被已删除的行永久占用。
		if err := softDeleteFreeKey(tx, "sa_system_user", "username", ids); err != nil {
			return InternalErr("删除用户失败", err)
		}
		if err := tx.Unscoped().Where("user_id IN ?", ids).Delete(&model.SysUserRole{}).Error; err != nil {
			return InternalErr("清理用户角色失败", err)
		}
		if err := tx.Unscoped().Where("user_id IN ?", ids).Delete(&model.SysUserPost{}).Error; err != nil {
			return InternalErr("清理用户岗位失败", err)
		}
		if err := tx.Unscoped().Where("user_id IN ?", ids).Delete(&model.SysUserDept{}).Error; err != nil {
			return InternalErr("清理用户部门失败", err)
		}
		if err := tx.Unscoped().Where("user_id IN ?", ids).Delete(&model.SysUserMenu{}).Error; err != nil {
			return InternalErr("清理用户菜单失败", err)
		}
		// 删除后立即失效刷新令牌，避免已删除账号继续换取新的访问令牌。
		if err := tx.Where("user_id IN ?", ids).Delete(&model.SysRefreshToken{}).Error; err != nil {
			return InternalErr("清理用户令牌失败", err)
		}
		return nil
	})
}

// CreateImported 保存一条 CSV 导入的用户记录。
//
// 参数 Parameters:
//   - row (*model.SysUser): 用户实体（含密码哈希）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *UserLogic) CreateImported(row *model.SysUser) error {
	if err := l.db.Create(row).Error; err != nil {
		return InternalErr("写入失败", err)
	}
	return nil
}

// syncRelations 覆盖式保存用户的角色与岗位关联（postIDs 为 nil 表示保留原岗位）。
//
// 参数 Parameters:
//   - tx (*gorm.DB): 事务会话。
//   - userID (int64): 用户主键。
//   - roleIDs ([]int64): 角色主键列表。
//   - postIDs ([]int64): 岗位主键列表；nil 表示不修改岗位关联。
//   - audit (model.AuditFields): 关联行的审计字段，取自用户行，避免 NOT NULL 列写入 NULL。
func syncRelations(tx *gorm.DB, userID int64, roleIDs, postIDs []int64, audit model.AuditFields) error {
	// 关联表使用物理删除：sa_system_user_role 的唯一索引是 (user_id, role_id)，
	// 不含 delete_time，若按软删除处理，旧行仍占位，重新授权同一角色会撞唯一约束。
	if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&model.SysUserRole{}).Error; err != nil {
		return InternalErr("保存用户关联失败", err)
	}
	// 关联表（sa_system_user_post / sa_system_user_tenant）的 created_by、updated_by
	// 为 NOT NULL DEFAULT 0，必须显式给出数值，否则 GORM 插入的 NULL 会触发约束失败。
	relationAudit := model.AuditFields{
		CreatedBy:  ActorID(nil),
		UpdatedBy:  ActorID(nil),
		CreateTime: util.NowPtr(),
		UpdateTime: util.NowPtr(),
	}
	if audit.CreatedBy != nil {
		relationAudit.CreatedBy = audit.CreatedBy
	}
	if audit.UpdatedBy != nil {
		relationAudit.UpdatedBy = audit.UpdatedBy
	}
	for _, roleID := range roleIDs {
		row := model.SysUserRole{UserID: userID, RoleID: roleID, Status: 1, AuditFields: relationAudit}
		if err := tx.Create(&row).Error; err != nil {
			return InternalErr("保存用户关联失败", err)
		}
	}
	if postIDs == nil {
		return nil
	}
	if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&model.SysUserPost{}).Error; err != nil {
		return InternalErr("保存用户关联失败", err)
	}
	for _, postID := range postIDs {
		row := model.SysUserPost{UserID: userID, PostID: postID, Status: 1, AuditFields: relationAudit}
		if err := tx.Create(&row).Error; err != nil {
			return InternalErr("保存用户关联失败", err)
		}
	}
	return nil
}

// InvalidUserID 构造用户 ID 非法的业务错误。
//
// 返回 Returns:
//   - err (*apperr.Error): 参数错误。
func InvalidUserID() *apperr.Error { return apperr.Invalid("用户 ID 无效") }
