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

// RoleService 提供角色管理的应用服务。
type RoleService struct {
	// roles 角色领域操作。
	roles *logic.RoleLogic
	// perms 权限缓存，角色或授权变更后需要整体失效。
	perms *permission.Service
}

// NewRoleService 创建角色应用服务。
//
// 参数 Parameters:
//   - roles (*logic.RoleLogic): 角色领域操作。
//   - perms (*permission.Service): 权限缓存服务。
//
// 返回 Returns:
//   - service (*RoleService): 角色应用服务。
func NewRoleService(roles *logic.RoleLogic, perms *permission.Service) *RoleService {
	return &RoleService{roles: roles, perms: perms}
}

// Page 分页查询角色。
//
// 参数 Parameters:
//   - q (dto.RoleQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.RoleItem]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *RoleService) Page(q dto.RoleQuery) (dto.PageResult[dto.RoleItem], error) {
	rows, total, err := s.roles.Page(q)
	if err != nil {
		return dto.PageResult[dto.RoleItem]{}, err
	}
	list := make([]dto.RoleItem, 0, len(rows))
	for index := range rows {
		list = append(list, dto.NewRoleItem(&rows[index]))
	}
	return dto.PageResult[dto.RoleItem]{List: list, Total: total}, nil
}

// Options 返回角色下拉选项。
//
// 参数 Parameters:
//   - useCode (bool): true 时选项值使用角色编码，false 时使用角色主键。
//
// 返回 Returns:
//   - options ([]dto.OptionItem): 角色选项。
//   - err (error): 查询失败时返回业务错误。
func (s *RoleService) Options(useCode bool) ([]dto.OptionItem, error) {
	rows, err := s.roles.Enabled()
	if err != nil {
		return nil, err
	}
	options := make([]dto.OptionItem, 0, len(rows))
	for index := range rows {
		row := rows[index]
		value := util.TextID(row.ID)
		if useCode {
			value = row.Code
		}
		options = append(options, dto.OptionItem{Value: value, Label: row.Name})
	}
	return options, nil
}

// Form 返回角色编辑表单（含自定义数据权限部门）。
//
// 参数 Parameters:
//   - idText (string): 角色 ID 文本。
//
// 返回 Returns:
//   - form (dto.RoleForm): 表单数据。
//   - err (error): ID 非法或角色不存在时返回业务错误。
func (s *RoleService) Form(idText string) (dto.RoleForm, error) {
	id, err := requiredID(idText, "角色 ID 无效")
	if err != nil {
		return dto.RoleForm{}, err
	}
	row, err := s.roles.Get(id)
	if err != nil {
		return dto.RoleForm{}, err
	}
	deptIDs, err := s.roles.DeptIDs(row.ID)
	if err != nil {
		return dto.RoleForm{}, err
	}
	return dto.RoleForm{
		ID:        util.TextID(row.ID),
		Code:      row.Code,
		Name:      row.Name,
		Sort:      row.Sort,
		Status:    row.Status,
		DataScope: row.DataScope,
		DeptIDs:   util.TextIDs(deptIDs),
		Remark:    util.TextValue(row.Remark),
	}, nil
}

// Create 新增角色。
//
// 参数 Parameters:
//   - req (dto.RoleForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新角色 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *RoleService) Create(req dto.RoleForm, operator *model.SysUser) (string, error) {
	code := strings.TrimSpace(req.Code)
	name := strings.TrimSpace(req.Name)
	if code == "" || name == "" {
		return "", apperr.Invalid("角色名称与编码不能为空")
	}
	exists, err := s.roles.CodeExists(code, 0)
	if err != nil {
		return "", err
	}
	if exists {
		return "", apperr.Conflict("角色编码已存在")
	}
	row := model.SysRole{
		Name:        name,
		Code:        code,
		Level:       1,
		Sort:        logic.NormalizeSort(req.Sort),
		Status:      util.NormalizeStatus(req.Status),
		DataScope:   logic.NormalizeDataScope(req.DataScope),
		Remark:      util.NonEmptyPtr(req.Remark),
		AuditFields: logic.Stamp(operator),
	}
	if err := s.roles.Create(&row, req.DeptIDs.IDs()); err != nil {
		return "", err
	}
	s.perms.InvalidateAll()
	return util.TextID(row.ID), nil
}

// Update 更新角色与其数据权限部门。
//
// 参数 Parameters:
//   - idText (string): 角色 ID 文本。
//   - req (dto.RoleForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 角色 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *RoleService) Update(idText string, req dto.RoleForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "角色 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.roles.Get(id)
	if err != nil {
		return "", err
	}
	if code := strings.TrimSpace(req.Code); code != "" && code != row.Code {
		exists, err := s.roles.CodeExists(code, row.ID)
		if err != nil {
			return "", err
		}
		if exists {
			return "", apperr.Conflict("角色编码已存在")
		}
		row.Code = code
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		row.Name = name
	}
	row.Sort = logic.NormalizeSort(req.Sort)
	row.Status = util.NormalizeStatus(req.Status)
	row.DataScope = logic.NormalizeDataScope(req.DataScope)
	row.Remark = util.NonEmptyPtr(req.Remark)
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.roles.Update(row, req.DeptIDs.IDs()); err != nil {
		return "", err
	}
	s.perms.InvalidateAll()
	return util.TextID(row.ID), nil
}

// Delete 删除角色。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的角色 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *RoleService) Delete(idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("角色 ID 无效")
	}
	if err := s.roles.Delete(ids); err != nil {
		return err
	}
	s.perms.InvalidateAll()
	return nil
}

// MenuIDs 返回角色已授权的菜单 ID。
//
// 参数 Parameters:
//   - idText (string): 角色 ID 文本。
//
// 返回 Returns:
//   - ids ([]string): 菜单 ID 文本列表（非 nil）。
//   - err (error): ID 非法或查询失败时返回业务错误。
func (s *RoleService) MenuIDs(idText string) ([]string, error) {
	id, err := requiredID(idText, "角色 ID 无效")
	if err != nil {
		return nil, err
	}
	ids, err := s.roles.MenuIDs(id)
	if err != nil {
		return nil, err
	}
	return util.TextIDs(ids), nil
}

// DeptIDs 返回角色自定义数据权限的部门 ID。
//
// 参数 Parameters:
//   - idText (string): 角色 ID 文本。
//
// 返回 Returns:
//   - ids ([]string): 部门 ID 文本列表（非 nil）。
//   - err (error): ID 非法或查询失败时返回业务错误。
func (s *RoleService) DeptIDs(idText string) ([]string, error) {
	id, err := requiredID(idText, "角色 ID 无效")
	if err != nil {
		return nil, err
	}
	ids, err := s.roles.DeptIDs(id)
	if err != nil {
		return nil, err
	}
	return util.TextIDs(ids), nil
}

// ReplaceMenus 覆盖式保存角色菜单授权。
//
// 参数 Parameters:
//   - idText (string): 角色 ID 文本。
//   - menuIDs ([]int64): 前端提交的菜单 ID 列表。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *RoleService) ReplaceMenus(idText string, menuIDs []int64) error {
	id, err := requiredID(idText, "角色 ID 无效")
	if err != nil {
		return err
	}
	if err := s.roles.ReplaceMenus(id, menuIDs); err != nil {
		return err
	}
	s.perms.InvalidateAll()
	return nil
}
