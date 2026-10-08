package logic

import (
	"strings"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// MenuLogic 负责 sa_system_menu 与 sa_system_role_menu 的菜单侧读写。
type MenuLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewMenuLogic 创建菜单领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*MenuLogic): 菜单领域操作实例。
func NewMenuLogic(db *gorm.DB) *MenuLogic { return &MenuLogic{db: db} }

// List 按关键字返回全部菜单（前端使用树形表格，不做分页）。
//
// 参数 Parameters:
//   - q (dto.MenuQuery): 查询条件。
//
// 返回 Returns:
//   - rows ([]model.SysMenu): 菜单列表。
//   - err (error): 查询失败时返回业务错误。
func (l *MenuLogic) List(q dto.MenuQuery) ([]model.SysMenu, error) {
	db := l.db.Model(&model.SysMenu{})
	if pattern := util.LikeKeyword(q.Keywords); pattern != "" {
		db = db.Where(`(name LIKE ? ESCAPE '\' OR slug LIKE ? ESCAPE '\' OR path LIKE ? ESCAPE '\')`,
			pattern, pattern, pattern)
	}
	var rows []model.SysMenu
	if err := db.Order("sort").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询菜单失败", err)
	}
	return rows, nil
}

// Options 返回启用菜单，用于下拉树；onlyParent 为 true 时只返回目录/菜单/外链。
//
// 参数 Parameters:
//   - onlyParent (bool): 是否只返回可选作上级的节点（排除按钮）。
//
// 返回 Returns:
//   - rows ([]model.SysMenu): 菜单列表。
//   - err (error): 查询失败时返回业务错误。
func (l *MenuLogic) Options(onlyParent bool) ([]model.SysMenu, error) {
	db := l.db.Model(&model.SysMenu{}).Where("status = 1")
	if onlyParent {
		db = db.Where("type IN ?", []int{1, 2, 4})
	}
	var rows []model.SysMenu
	if err := db.Order("sort").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询菜单失败", err)
	}
	return rows, nil
}

// Get 按主键查询菜单。
//
// 参数 Parameters:
//   - id (int64): 菜单主键。
//
// 返回 Returns:
//   - row (*model.SysMenu): 菜单实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *MenuLogic) Get(id int64) (*model.SysMenu, error) {
	var row model.SysMenu
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "菜单不存在", "查询菜单失败")
	}
	return &row, nil
}

// Create 新增菜单。
//
// 参数 Parameters:
//   - row (*model.SysMenu): 菜单实体（含审计字段）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *MenuLogic) Create(row *model.SysMenu) error {
	if err := l.db.Create(row).Error; err != nil {
		return InternalErr("创建菜单失败", err)
	}
	return nil
}

// Update 保存菜单。
//
// 参数 Parameters:
//   - row (*model.SysMenu): 已修改的菜单实体。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *MenuLogic) Update(row *model.SysMenu) error {
	if err := l.db.Save(row).Error; err != nil {
		return InternalErr("更新菜单失败", err)
	}
	return nil
}

// Delete 删除菜单（存在子菜单时拒绝），并清理角色菜单授权。
//
// 参数 Parameters:
//   - ids ([]int64): 菜单主键列表。
//
// 返回 Returns:
//   - err (error): 校验不通过或写入失败时返回业务错误。
func (l *MenuLogic) Delete(ids []int64) error {
	var childCount int64
	if err := l.db.Model(&model.SysMenu{}).Where("parent_id IN ?", ids).Count(&childCount).Error; err != nil {
		return InternalErr("校验子菜单失败", err)
	}
	if childCount > 0 {
		return errInvalid("存在子菜单，无法删除")
	}
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", ids).Delete(&model.SysMenu{}).Error; err != nil {
			return InternalErr("删除菜单失败", err)
		}
		if err := tx.Where("menu_id IN ?", ids).Delete(&model.SysRoleMenu{}).Error; err != nil {
			return InternalErr("清理角色菜单失败", err)
		}
		// 个人菜单授权一并清理，避免删除菜单后 sa_system_user_menu 残留孤儿行。
		if err := tx.Unscoped().Where("menu_id IN ?", ids).Delete(&model.SysUserMenu{}).Error; err != nil {
			return InternalErr("清理用户菜单失败", err)
		}
		return nil
	})
}

// ApplyForm 把前端菜单表单写入实体（新增与编辑共用）。
//
// 参数 Parameters:
//   - menu (*model.SysMenu): 目标实体，可能已存在数据。
//   - req (*dto.MenuForm): 前端提交的表单。
func ApplyForm(menu *model.SysMenu, req *dto.MenuForm) {
	menu.Name = strings.TrimSpace(req.Name)
	menu.Type = dto.MenuTypeToDB(req.Type)
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = strings.TrimSpace(req.RoutePath)
	}
	menu.Path = util.NonEmptyPtr(path)
	menu.Code = util.NonEmptyPtr(req.RouteName)
	menu.Slug = util.NonEmptyPtr(req.Perm)
	menu.LinkURL = util.NonEmptyPtr(req.ExternalURL)
	menu.Redirect = util.NonEmptyPtr(req.Redirect)
	menu.Icon = util.NonEmptyPtr(req.Icon)
	menu.Sort = NormalizeSort(req.Sort)
	menu.Method = util.NonEmptyPtr(req.Method)
	if menu.Type == 1 {
		// 目录固定使用布局组件，避免前端动态路由回退到 404。
		menu.Component = util.NonEmptyPtr("Layout")
	} else {
		menu.Component = util.NonEmptyPtr(req.Component)
	}
	menu.ParentID = int64(0)
	if parentIDs := util.ParseIDList(req.ParentID); len(parentIDs) > 0 {
		menu.ParentID = parentIDs[0]
	}
	menu.IsHidden = dto.HiddenFromVisible(req.Visible)
	menu.IsKeepAlive = util.BoolFlag(int(req.KeepAlive) != 0)
	menu.IsAlwaysShow = util.BoolFlag(int(req.AlwaysShow) != 0)
	menu.Params = util.NonEmptyPtr(dto.EncodeMenuParams(req.Params))
	menu.Status = util.NormalizeStatus(req.Status)
}

// Routable 返回全部可路由的启用菜单（目录 / 菜单 / 外链），用于超级管理员的动态菜单。
//
// 返回 Returns:
//   - rows ([]model.SysMenu): 菜单列表。
//   - err (error): 查询失败时返回业务错误。
func (l *MenuLogic) Routable() ([]model.SysMenu, error) {
	var rows []model.SysMenu
	if err := l.db.Where("status = 1 AND type IN ?", []int{1, 2, 4}).Order("sort").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询菜单失败", err)
	}
	return rows, nil
}

// RoutableByIDs 返回指定菜单 ID 中可路由的启用菜单。
//
// 参数 Parameters:
//   - ids ([]int64): 菜单主键列表。
//
// 返回 Returns:
//   - rows ([]model.SysMenu): 菜单列表。
//   - err (error): 查询失败时返回业务错误。
func (l *MenuLogic) RoutableByIDs(ids []int64) ([]model.SysMenu, error) {
	var rows []model.SysMenu
	if err := l.db.Where("id IN ? AND status = 1 AND type IN ?", ids, []int{1, 2, 4}).
		Order("sort").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询菜单失败", err)
	}
	return rows, nil
}

// CompleteParentMenus 补齐菜单 ID 的父级链路并去重（角色菜单与个人菜单授权共用）。
//
// 为什么必须补父级：侧边栏按「目录 → 菜单 → 按钮」渲染，只保存叶子节点会让父级目录缺失，
// 整棵子树都不会出现在菜单里；补齐后即使只勾了按钮，目录链路也是完整的。
//
// 参数 Parameters:
//   - db (*gorm.DB): 数据库会话（可传事务）。
//   - menuIDs ([]int64): 前端提交的菜单 ID 列表。
//
// 返回 Returns:
//   - ids ([]int64): 补齐父级后的菜单 ID 列表（非 nil）。
//   - err (error): 查询菜单失败时返回业务错误。
func CompleteParentMenus(db *gorm.DB, menuIDs []int64) ([]int64, error) {
	if len(menuIDs) == 0 {
		return []int64{}, nil
	}
	var rows []model.SysMenu
	if err := db.Select("id", "parent_id").Find(&rows).Error; err != nil {
		return nil, InternalErr("校验菜单失败", err)
	}
	parents := make(map[int64]int64, len(rows))
	for _, row := range rows {
		parents[row.ID] = row.ParentID
	}
	result := make([]int64, 0, len(menuIDs))
	seen := map[int64]bool{}
	for _, id := range menuIDs {
		for current := id; current > 0 && !seen[current]; {
			seen[current] = true
			result = append(result, current)
			parent, exists := parents[current]
			if !exists || parent == current {
				break
			}
			current = parent
		}
	}
	return result, nil
}
