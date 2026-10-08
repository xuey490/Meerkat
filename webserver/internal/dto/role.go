package dto

import "github.com/company/monitor-webserver/internal/model"

// RoleItem 对齐前端 RoleItem（角色分页列表项）。
type RoleItem struct {
	// ID 角色 ID。
	ID string `json:"id"`
	// Code 角色编码。
	Code string `json:"code"`
	// Name 角色名称。
	Name string `json:"name"`
	// Sort 排序。
	Sort int `json:"sort"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `json:"status"`
	// DataScope 数据权限范围。
	DataScope int `json:"dataScope"`
	// DataScopeLabel 数据权限中文标签。
	DataScopeLabel string `json:"dataScopeLabel"`
	// Remark 备注。
	Remark string `json:"remark"`
	// UpdateTime 修改时间。
	UpdateTime string `json:"updateTime"`
}

// RoleForm 对齐前端 RoleForm（新增/编辑角色表单）。
type RoleForm struct {
	// ID 角色 ID。
	ID string `json:"id"`
	// Code 角色编码。
	Code string `json:"code"`
	// Name 角色名称。
	Name string `json:"name"`
	// Sort 排序。
	Sort int `json:"sort"`
	// Status 状态。
	Status int `json:"status"`
	// DataScope 数据权限范围。
	DataScope int `json:"dataScope"`
	// DeptIDs 自定义数据权限部门集合（dataScope=5 时生效）；兼容数字与字符串元素。
	DeptIDs FlexStrings `json:"deptIds"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// RoleQuery 是角色分页查询参数。
type RoleQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Keywords 关键字（角色名称/编码）。
	Keywords string `json:"keywords" form:"keywords"`
}

// MenuIDsForm 是角色菜单授权请求体。
type MenuIDsForm struct {
	// MenuIDs 菜单 ID 集合。
	MenuIDs []string `json:"menuIds"`
}

// NewRoleItem 把角色实体转换为前端列表项。
//
// 参数 Parameters:
//   - role (*model.SysRole): 角色实体。
//
// 返回 Returns:
//   - item (RoleItem): 前端角色列表项。
func NewRoleItem(role *model.SysRole) RoleItem {
	return RoleItem{
		ID:             textID(role.ID),
		Code:           role.Code,
		Name:           role.Name,
		Sort:           role.Sort,
		Status:         role.Status,
		DataScope:      role.DataScope,
		DataScopeLabel: DataScopeLabels[role.DataScope],
		Remark:         textValue(role.Remark),
		UpdateTime:     timeText(role.UpdateTime),
	}
}
