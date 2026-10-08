package service

import (
	"strings"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/permission"
	"github.com/company/monitor-webserver/internal/util"
)

// MenuService 提供菜单管理的应用服务。
type MenuService struct {
	// menus 菜单领域操作。
	menus *logic.MenuLogic
	// roles 角色领域操作（动态菜单需要按用户角色过滤）。
	roles *logic.RoleLogic
	// users 用户领域操作（动态菜单需要合并用户个人菜单）。
	users *logic.UserLogic
	// perms 权限缓存，菜单变更后需要整体失效。
	perms *permission.Service
}

// NewMenuService 创建菜单应用服务。
//
// 参数 Parameters:
//   - menus (*logic.MenuLogic): 菜单领域操作。
//   - roles (*logic.RoleLogic): 角色领域操作。
//   - users (*logic.UserLogic): 用户领域操作。
//   - perms (*permission.Service): 权限缓存服务。
//
// 返回 Returns:
//   - service (*MenuService): 菜单应用服务。
func NewMenuService(menus *logic.MenuLogic, roles *logic.RoleLogic, users *logic.UserLogic, perms *permission.Service) *MenuService {
	return &MenuService{menus: menus, roles: roles, users: users, perms: perms}
}

// List 返回菜单树。
//
// 参数 Parameters:
//   - q (dto.MenuQuery): 查询条件。
//
// 返回 Returns:
//   - tree ([]dto.MenuItem): 菜单树。
//   - err (error): 查询失败时返回业务错误。
func (s *MenuService) List(q dto.MenuQuery) ([]dto.MenuItem, error) {
	rows, err := s.menus.List(q)
	if err != nil {
		return nil, err
	}
	return dto.BuildMenuTree(rows), nil
}

// Options 返回菜单下拉树。
//
// 参数 Parameters:
//   - onlyParent (bool): 是否只返回可选作上级的节点。
//
// 返回 Returns:
//   - tree ([]dto.OptionItem): 选项树。
//   - err (error): 查询失败时返回业务错误。
func (s *MenuService) Options(onlyParent bool) ([]dto.OptionItem, error) {
	rows, err := s.menus.Options(onlyParent)
	if err != nil {
		return nil, err
	}
	items := make([]dto.OptionItem, 0, len(rows))
	parents := make(map[string]string, len(rows))
	for index := range rows {
		row := rows[index]
		value := util.TextID(row.ID)
		items = append(items, dto.OptionItem{Value: value, Label: row.Name})
		if row.ParentID > 0 {
			parents[value] = util.TextID(row.ParentID)
		}
	}
	return dto.BuildOptionTree(items, parents), nil
}

// Form 返回菜单编辑表单。
//
// 参数 Parameters:
//   - idText (string): 菜单 ID 文本。
//
// 返回 Returns:
//   - form (dto.MenuForm): 表单数据。
//   - err (error): ID 非法或菜单不存在时返回业务错误。
func (s *MenuService) Form(idText string) (dto.MenuForm, error) {
	id, err := requiredID(idText, "菜单 ID 无效")
	if err != nil {
		return dto.MenuForm{}, err
	}
	row, err := s.menus.Get(id)
	if err != nil {
		return dto.MenuForm{}, err
	}
	return dto.NewMenuForm(row), nil
}

// Routes 返回当前用户可见的动态路由树（数据库驱动）。
//
// 数据来源：sa_system_menu（status=1、type∈{1目录,2菜单,4外链}）。
// 超级管理员（is_super=1 或 id=1）可见全部菜单；普通用户的菜单 = 角色菜单
// （sa_system_role_menu，经用户角色关联）∪ 个人菜单（sa_system_user_menu），
// 两者合并去重后再按 ID 过滤菜单表；都没有时返回空数组，前端渲染空侧边栏。
//
// 参数 Parameters:
//   - operator (*model.SysUser): 当前登录用户。
//
// 返回 Returns:
//   - routes ([]dto.RouteItem): 前端路由树。
//   - err (error): 未登录或查询失败时返回业务错误。
func (s *MenuService) Routes(operator *model.SysUser) ([]dto.RouteItem, error) {
	if operator == nil {
		return nil, apperr.Unauthorized("登录已过期")
	}
	entry := s.perms.Of(operator)
	if entry.IsSuper {
		rows, err := s.menus.Routable()
		if err != nil {
			return nil, err
		}
		return dto.BuildRouteTree(rows), nil
	}
	roleIDs, err := s.roles.RoleIDsOfUser(operator.ID)
	if err != nil {
		return nil, err
	}
	roleMenuIDs, err := s.roles.RoleMenuIDs(roleIDs)
	if err != nil {
		return nil, err
	}
	// 个人菜单：即使用户没有任何角色，也可能被单独授权了菜单。
	personalMenuIDs, err := s.users.MenuIDs(operator.ID)
	if err != nil {
		return nil, err
	}
	menuIDs := mergeIDs(roleMenuIDs, personalMenuIDs)
	if len(menuIDs) == 0 {
		return []dto.RouteItem{}, nil
	}
	rows, err := s.menus.RoutableByIDs(menuIDs)
	if err != nil {
		return nil, err
	}
	return dto.BuildRouteTree(rows), nil
}

// mergeIDs 合并多个菜单 ID 列表并去重（保持首次出现顺序，便于定位与断言）。
//
// 参数 Parameters:
//   - lists (...[]int64): 待合并的 ID 列表。
//
// 返回 Returns:
//   - ids ([]int64): 去重后的 ID 列表。
func mergeIDs(lists ...[]int64) []int64 {
	total := 0
	for _, list := range lists {
		total += len(list)
	}
	merged := make([]int64, 0, total)
	seen := make(map[int64]bool, total)
	for _, list := range lists {
		for _, id := range list {
			if id <= 0 || seen[id] {
				continue
			}
			seen[id] = true
			merged = append(merged, id)
		}
	}
	return merged
}

// Create 新增菜单。
//
// 参数 Parameters:
//   - req (dto.MenuForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新菜单 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *MenuService) Create(req dto.MenuForm, operator *model.SysUser) (string, error) {
	if strings.TrimSpace(req.Name) == "" {
		return "", apperr.Invalid("菜单名称不能为空")
	}
	row := model.SysMenu{}
	logic.ApplyForm(&row, &req)
	row.AuditFields = logic.Stamp(operator)
	if err := s.menus.Create(&row); err != nil {
		return "", err
	}
	s.perms.InvalidateAll()
	return util.TextID(row.ID), nil
}

// Update 更新菜单。
//
// 参数 Parameters:
//   - idText (string): 菜单 ID 文本。
//   - req (dto.MenuForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 菜单 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *MenuService) Update(idText string, req dto.MenuForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "菜单 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.menus.Get(id)
	if err != nil {
		return "", err
	}
	if parentID := optionalID(req.ParentID); parentID == row.ID {
		return "", apperr.Invalid("上级菜单不能是自身")
	}
	logic.ApplyForm(row, &req)
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.menus.Update(row); err != nil {
		return "", err
	}
	s.perms.InvalidateAll()
	return util.TextID(row.ID), nil
}

// Delete 删除菜单。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的菜单 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *MenuService) Delete(idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("菜单 ID 无效")
	}
	if err := s.menus.Delete(ids); err != nil {
		return err
	}
	s.perms.InvalidateAll()
	return nil
}
