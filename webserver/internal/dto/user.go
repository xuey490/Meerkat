package dto

import (
	"strings"

	"github.com/company/monitor-webserver/internal/model"
)

// UserInfo 对齐前端 UserInfo（GET /api/v1/users/me 的返回体）。
type UserInfo struct {
	// UserID 用户 ID。
	UserID string `json:"userId"`
	// Username 用户账号。
	Username string `json:"username"`
	// Nickname 用户昵称。
	Nickname string `json:"nickname"`
	// Avatar 头像地址。
	Avatar string `json:"avatar"`
	// Roles 角色标识集合。
	Roles []string `json:"roles"`
	// Perms 权限标识集合。
	Perms []string `json:"perms"`
	// IsSuperAdmin 是否超级管理员。
	IsSuperAdmin bool `json:"isSuperAdmin"`
}

// UserItem 对齐前端 UserItem（用户分页列表项）。
type UserItem struct {
	// ID 用户 ID。
	ID string `json:"id"`
	// Username 用户账号。
	Username string `json:"username"`
	// Nickname 用户昵称。
	Nickname string `json:"nickname"`
	// Avatar 头像地址。
	Avatar string `json:"avatar"`
	// Gender 性别：0 未知 / 1 男 / 2 女。
	Gender int `json:"gender"`
	// Mobile 手机号。
	Mobile string `json:"mobile"`
	// Email 邮箱。
	Email string `json:"email"`
	// DeptName 部门名称。
	DeptName string `json:"deptName"`
	// RoleNames 角色名称，多个以英文逗号分隔。
	RoleNames string `json:"roleNames"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `json:"status"`
	// IsSuper 是否超级管理员：1 是 / 0 否（前端据此隐藏「菜单设置」按钮）。
	IsSuper int `json:"isSuper"`
	// CreateTime 创建时间。
	CreateTime string `json:"createTime"`
}

// UserForm 对齐前端 UserForm（新增/编辑用户表单，同时用于表单回显）。
type UserForm struct {
	// ID 用户 ID。
	ID string `json:"id"`
	// Username 用户账号。
	Username string `json:"username"`
	// Nickname 用户昵称。
	Nickname string `json:"nickname"`
	// Avatar 头像地址。
	Avatar string `json:"avatar"`
	// DeptID 部门 ID。
	DeptID string `json:"deptId"`
	// Gender 性别：0 未知 / 1 男 / 2 女；字典下拉提交字符串 "1"，用 FlexInt 兼容。
	Gender FlexInt `json:"gender"`
	// Mobile 手机号。
	Mobile string `json:"mobile"`
	// Email 邮箱。
	Email string `json:"email"`
	// RoleIDs 角色 ID 集合；前端类型为 number[]，用 FlexStrings 兼容数字与字符串。
	RoleIDs FlexStrings `json:"roleIds"`
	// Status 状态：1 正常 / 0 禁用。
	Status FlexInt `json:"status"`
	// PostIDs 岗位 ID 集合。
	PostIDs FlexStrings `json:"postIds"`
	// Remark 备注。
	Remark string `json:"remark"`
	// Password 初始密码，仅在新增请求中出现。
	Password string `json:"password,omitempty"`
}

// UserProfile 对齐前端 UserProfileDetail（个人中心资料）。
type UserProfile struct {
	// ID 用户 ID。
	ID string `json:"id"`
	// Username 用户账号。
	Username string `json:"username"`
	// Nickname 用户昵称。
	Nickname string `json:"nickname"`
	// Avatar 头像地址。
	Avatar string `json:"avatar"`
	// Gender 性别。
	Gender int `json:"gender"`
	// Mobile 手机号。
	Mobile string `json:"mobile"`
	// Email 邮箱。
	Email string `json:"email"`
	// DeptName 部门名称。
	DeptName string `json:"deptName"`
	// RoleNames 角色名称。
	RoleNames string `json:"roleNames"`
	// CreateTime 创建时间。
	CreateTime string `json:"createTime"`
}

// UserProfileForm 对齐前端 UserProfileForm（个人资料修改）。
type UserProfileForm struct {
	// Nickname 昵称。
	Nickname string `json:"nickname"`
	// Avatar 头像地址。
	Avatar string `json:"avatar"`
	// Gender 性别；字典下拉提交字符串，用 FlexInt 兼容。
	Gender *FlexInt `json:"gender"`
}

// PasswordChangeForm 对齐前端 PasswordChangeForm（个人中心改密）。
type PasswordChangeForm struct {
	// OldPassword 原密码。
	OldPassword string `json:"oldPassword"`
	// NewPassword 新密码。
	NewPassword string `json:"newPassword"`
	// ConfirmPassword 确认新密码。
	ConfirmPassword string `json:"confirmPassword"`
}

// UserQuery 是用户分页查询参数。
type UserQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Keywords 关键字（账号/昵称/手机号）。
	Keywords string `json:"keywords" form:"keywords"`
	// Status 状态过滤。
	Status string `json:"status" form:"status"`
	// DeptID 部门过滤。
	DeptID string `json:"deptId" form:"deptId"`
	// CreateTime 创建时间范围（开始,结束）。
	CreateTime []string `json:"createTime" form:"createTime"`
}

// UserImportForm 是用户导入请求（multipart 文件由 api 层解析后传入）。
type UserImportForm struct {
	// FileName 上传文件名。
	FileName string
	// Content 文件内容。
	Content []byte
}

// NewUserItem 把用户实体转换为前端列表项。
//
// 参数 Parameters:
//   - user (*model.SysUser): 用户实体。
//   - deptName (string): 部门名称，可为空。
//   - roleNames (string): 角色名称，多个以英文逗号分隔。
//
// 返回 Returns:
//   - item (UserItem): 前端用户列表项。
func NewUserItem(user *model.SysUser, deptName, roleNames string) UserItem {
	return UserItem{
		ID:         textID(user.ID),
		Username:   user.Username,
		Nickname:   user.Realname,
		Avatar:     user.Avatar,
		Gender:     GenderFromDB(user.Gender),
		Mobile:     textValue(user.Phone),
		Email:      textValue(user.Email),
		DeptName:   deptName,
		RoleNames:  roleNames,
		Status:     user.Status,
		IsSuper:    user.IsSuper,
		CreateTime: timeText(user.CreateTime),
	}
}

// NewUserProfile 把用户实体转换为个人中心资料。
//
// 参数 Parameters:
//   - user (*model.SysUser): 用户实体。
//   - deptName (string): 部门名称。
//   - roleNames (string): 角色名称。
//
// 返回 Returns:
//   - profile (UserProfile): 个人中心资料。
func NewUserProfile(user *model.SysUser, deptName, roleNames string) UserProfile {
	return UserProfile{
		ID:         textID(user.ID),
		Username:   user.Username,
		Nickname:   user.Realname,
		Avatar:     user.Avatar,
		Gender:     GenderFromDB(user.Gender),
		Mobile:     textValue(user.Phone),
		Email:      textValue(user.Email),
		DeptName:   deptName,
		RoleNames:  roleNames,
		CreateTime: timeText(user.CreateTime),
	}
}

// GenderFromDB 把 sa_system_user.gender（varchar）转换为前端数字枚举。
//
// 参数 Parameters:
//   - value (string): 数据库中的性别值（"0"/"1"/"2"/空）。
//
// 返回 Returns:
//   - gender (int): 0 未知 / 1 男 / 2 女；无法解析时返回 0。
func GenderFromDB(value string) int {
	switch strings.TrimSpace(value) {
	case "1":
		return 1
	case "2":
		return 2
	default:
		return 0
	}
}

// GenderToDB 把前端数字性别转换为数据库字符串值。
//
// 参数 Parameters:
//   - value (int): 0 未知 / 1 男 / 2 女。
//
// 返回 Returns:
//   - gender (string): 数据库字符串值。
func GenderToDB(value int) string {
	switch value {
	case 1:
		return "1"
	case 2:
		return "2"
	default:
		return "0"
	}
}
