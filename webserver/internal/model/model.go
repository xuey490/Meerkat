// Package model 存放 sa_system_* 权限库的数据库模型（GORM 实体）。
//
// 表结构与列名以仓库根目录 database/init.sql 为准，逐字段对应（含 tenant_id 等租户列与
// created_by/updated_by/create_time/update_time/delete_time 审计列）；租户列只写入默认值，
// 系统不实现任何租户功能。
//
// 注意 Naming：GORM 只解析导出的字段（含匿名嵌入字段），未导出的嵌入结构会被整体忽略，
// 导致 create_time/delete_time 等列永不写入，因此 AuditFields 必须导出。
package model

import (
	"time"

	"gorm.io/gorm"
)

// AuditFields 是 sa_system_* 表共有的审计列。
//
// DeleteTime 使用 gorm.DeletedAt：GORM 会自动为查询补上 `delete_time IS NULL`，
// 并在调用 Delete 时写入软删除时间，避免各层重复拼装过滤条件。
type AuditFields struct {
	// CreatedBy 创建人 ID。
	CreatedBy *int64 `gorm:"column:created_by" json:"createdBy,omitempty"`
	// UpdatedBy 更新人 ID。
	UpdatedBy *int64 `gorm:"column:updated_by" json:"updatedBy,omitempty"`
	// CreateTime 创建时间。
	CreateTime *time.Time `gorm:"column:create_time" json:"createTime,omitempty"`
	// UpdateTime 更新时间。
	UpdateTime *time.Time `gorm:"column:update_time" json:"updateTime,omitempty"`
	// DeleteTime 软删除时间，非空表示已删除。
	DeleteTime gorm.DeletedAt `gorm:"column:delete_time;index" json:"-"`
}

// SysUser 对应 sa_system_user 用户表。
//
// init.sql 中 realname 即前端契约的 nickname、phone 即 mobile，
// 字段名不一致的部分在 dto 层统一转换。
type SysUser struct {
	// ID 用户主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Username 用户账号，全局唯一。
	Username string `gorm:"column:username;size:64;uniqueIndex" json:"username"`
	// Password bcrypt 密码哈希。
	Password string `gorm:"column:password;size:255" json:"-"`
	// Realname 用户昵称（前端 nickname）。
	Realname string `gorm:"column:realname;size:64" json:"realname"`
	// Gender 性别，数据库存字符串 "0"/"1"/"2"。
	Gender string `gorm:"column:gender;size:10;default:0" json:"gender"`
	// Avatar 头像地址。
	Avatar string `gorm:"column:avatar;size:255" json:"avatar"`
	// Email 邮箱。
	Email *string `gorm:"column:email;size:128" json:"email"`
	// Phone 手机号（前端 mobile）。
	Phone *string `gorm:"column:phone;size:20" json:"phone"`
	// Signed 个性签名。
	Signed *string `gorm:"column:signed;size:255" json:"signed"`
	// Dashboard 工作台标识。
	Dashboard string `gorm:"column:dashboard;size:255;default:work" json:"dashboard"`
	// DeptID 主归属部门。
	DeptID *int64 `gorm:"column:dept_id" json:"deptId"`
	// IsSuper 是否超级管理员：1 是（跳过权限校验）/ 0 否。
	IsSuper int `gorm:"column:is_super;default:0" json:"isSuper"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `gorm:"column:status;default:1" json:"status"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// LoginTime 最后登录时间。
	LoginTime *time.Time `gorm:"column:login_time" json:"loginTime"`
	// LoginIP 最后登录 IP。
	LoginIP *string `gorm:"column:login_ip;size:45" json:"loginIp"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_user 表名。
func (SysUser) TableName() string { return "sa_system_user" }

// SysRole 对应 sa_system_role 角色表。
//
// DataScope 语义：1 全部 / 2 本部门及下属 / 3 本部门 / 4 仅本人 / 5 自定义部门。
type SysRole struct {
	// ID 角色主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// ParentID 父角色 ID，0 为顶级。
	ParentID int64 `gorm:"column:parent_id;default:0" json:"parentId"`
	// Name 角色名称。
	Name string `gorm:"column:name;size:64" json:"name"`
	// Code 角色标识，全局唯一。
	Code string `gorm:"column:code;size:64;uniqueIndex" json:"code"`
	// Level 角色级别，用于行政控制。
	Level int `gorm:"column:level;default:1" json:"level"`
	// DataScope 数据权限范围。
	DataScope int `gorm:"column:data_scope;default:1" json:"dataScope"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// Sort 排序。
	Sort int `gorm:"column:sort;default:100" json:"sort"`
	// TenantID 租户列，仅写入默认值 0。
	TenantID int64 `gorm:"column:tenant_id;not null;default:0" json:"tenantId"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `gorm:"column:status;default:1" json:"status"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_role 表名。
func (SysRole) TableName() string { return "sa_system_role" }

// SysDept 对应 sa_system_dept 部门表。
//
// Level 保存祖级列表（形如 "0,1,3,"），用于按前缀查询子孙部门。
type SysDept struct {
	// ID 部门主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// ParentID 父部门 ID，0 为根节点。
	ParentID int64 `gorm:"column:parent_id;default:0" json:"parentId"`
	// Name 部门名称。
	Name string `gorm:"column:name;size:64" json:"name"`
	// Code 部门编码。
	Code *string `gorm:"column:code;size:64" json:"code"`
	// LeaderID 部门负责人用户 ID。
	LeaderID *int64 `gorm:"column:leader_id" json:"leaderId"`
	// Level 祖级列表。
	Level string `gorm:"column:level;size:255;default:''" json:"level"`
	// TenantID 租户列，仅写入默认值 0。
	TenantID int64 `gorm:"column:tenant_id;not null;default:0" json:"tenantId"`
	// Sort 排序，数字越小越靠前。
	Sort int `gorm:"column:sort;default:0" json:"sort"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `gorm:"column:status;default:1" json:"status"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_dept 表名。
func (SysDept) TableName() string { return "sa_system_dept" }

// SysMenu 对应 sa_system_menu 菜单表。
//
// Type 语义：1 目录 / 2 菜单 / 3 按钮（API）/ 4 外链；IsHidden、IsKeepAlive 等
// 使用 init.sql 的 1 是 / 2 否 标记；Code 为前端 routeName，Slug 为权限标识。
type SysMenu struct {
	// ID 菜单主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// ParentID 父菜单 ID，0 为顶级。
	ParentID int64 `gorm:"column:parent_id;default:0" json:"parentId"`
	// Name 菜单名称。
	Name string `gorm:"column:name;size:64" json:"name"`
	// Code 组件名（前端 routeName）。
	Code *string `gorm:"column:code;size:64" json:"code"`
	// Slug 权限标识，如 user:list。
	Slug *string `gorm:"column:slug;size:100" json:"slug"`
	// Type 菜单类型。
	Type int `gorm:"column:type;not null;default:1" json:"type"`
	// Path 路由地址。
	Path *string `gorm:"column:path;size:255" json:"path"`
	// Component 前端组件路径。
	Component *string `gorm:"column:component;size:255" json:"component"`
	// Method 请求方式（按钮类菜单使用）。
	Method *string `gorm:"column:method;size:10" json:"method"`
	// Icon 图标。
	Icon *string `gorm:"column:icon;size:64" json:"icon"`
	// Sort 排序。
	Sort int `gorm:"column:sort;default:100" json:"sort"`
	// LinkURL 外部链接。
	LinkURL *string `gorm:"column:link_url;size:255" json:"linkUrl"`
	// IsIframe 是否 iframe：1 是 / 2 否。
	IsIframe int `gorm:"column:is_iframe;default:2" json:"isIframe"`
	// IsKeepAlive 是否缓存：1 是 / 2 否。
	IsKeepAlive int `gorm:"column:is_keep_alive;default:2" json:"isKeepAlive"`
	// IsHidden 是否隐藏：1 是 / 2 否。
	IsHidden int `gorm:"column:is_hidden;default:2" json:"isHidden"`
	// IsFixedTab 是否固定标签页：1 是 / 2 否。
	IsFixedTab int `gorm:"column:is_fixed_tab;default:2" json:"isFixedTab"`
	// IsFullPage 是否全屏：1 是 / 2 否。
	IsFullPage int `gorm:"column:is_full_page;default:2" json:"isFullPage"`
	// IsAlwaysShow 目录是否始终显示：1 是 / 2 否（扩展列）。
	IsAlwaysShow int `gorm:"column:is_always_show;default:2" json:"isAlwaysShow"`
	// Redirect 跳转路径（扩展列）。
	Redirect *string `gorm:"column:redirect;size:255" json:"redirect"`
	// Params 路由参数 JSON（扩展列）。
	Params *string `gorm:"column:params" json:"params"`
	// GenerateID 代码生成标识。
	GenerateID int `gorm:"column:generate_id;default:0" json:"generateId"`
	// GenerateKey 代码生成键。
	GenerateKey *string `gorm:"column:generate_key;size:255" json:"generateKey"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `gorm:"column:status;default:1" json:"status"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_menu 表名。
func (SysMenu) TableName() string { return "sa_system_menu" }

// SysPost 对应 sa_system_post 岗位表。
type SysPost struct {
	// ID 岗位主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Name 岗位名称。
	Name *string `gorm:"column:name;size:50" json:"name"`
	// Code 岗位编码。
	Code *string `gorm:"column:code;size:100" json:"code"`
	// Sort 排序。
	Sort int `gorm:"column:sort;default:0" json:"sort"`
	// Status 状态：1 正常 / 0 停用。
	Status int `gorm:"column:status;default:1" json:"status"`
	// TenantID 租户列，仅写入默认值 0。
	TenantID int64 `gorm:"column:tenant_id;not null;default:0" json:"tenantId"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_post 表名。
func (SysPost) TableName() string { return "sa_system_post" }

// SysUserRole 对应 sa_system_user_role 用户角色关联表。
type SysUserRole struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// UserID 用户 ID。
	UserID int64 `gorm:"column:user_id;not null" json:"userId"`
	// RoleID 角色 ID。
	RoleID int64 `gorm:"column:role_id;not null" json:"roleId"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `gorm:"column:status;not null;default:1" json:"status"`
	// TenantID 租户上下文 ID。
	TenantID int64 `gorm:"column:tenant_id;not null;default:0" json:"tenantId"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_user_role 表名。
func (SysUserRole) TableName() string { return "sa_system_user_role" }

// SysRoleMenu 对应 sa_system_role_menu 角色菜单关联表。
type SysRoleMenu struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// RoleID 角色 ID。
	RoleID int64 `gorm:"column:role_id;not null" json:"roleId"`
	// MenuID 菜单 ID。
	MenuID int64 `gorm:"column:menu_id;not null" json:"menuId"`
}

// TableName 返回 sa_system_role_menu 表名。
func (SysRoleMenu) TableName() string { return "sa_system_role_menu" }

// SysUserMenu 对应 sa_system_user_menu 用户菜单授权表（个人菜单，按用户单独授权的菜单）。
type SysUserMenu struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// UserID 用户 ID。
	UserID int64 `gorm:"column:user_id;not null" json:"userId"`
	// MenuID 菜单 ID。
	MenuID int64 `gorm:"column:menu_id;not null" json:"menuId"`
	// TenantID 租户列，仅写入默认值 0。
	TenantID int64 `gorm:"column:tenant_id;not null;default:0" json:"tenantId"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `gorm:"column:status;not null;default:1" json:"status"`
	// IsShow 原始库的「可查看」位；本系统按菜单勾选统一写 1。
	IsShow int `gorm:"column:is_show;not null;default:1" json:"isShow"`
	// IsCreate 原始库的「可新增」位；本系统统一写 1。
	IsCreate int `gorm:"column:is_create;not null;default:1" json:"isCreate"`
	// IsUpdate 原始库的「可修改」位；本系统统一写 1。
	IsUpdate int `gorm:"column:is_update;not null;default:1" json:"isUpdate"`
	// IsDelete 原始库的「可删除」位；本系统统一写 1。
	IsDelete int `gorm:"column:is_delete;not null;default:1" json:"isDelete"`
	// AuditFields 审计列（含软删除时间）。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_user_menu 表名。
func (SysUserMenu) TableName() string { return "sa_system_user_menu" }

// SysRoleDept 对应 sa_system_role_dept 角色部门关联表（data_scope=5 自定义范围）。
type SysRoleDept struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// RoleID 角色 ID。
	RoleID int64 `gorm:"column:role_id;not null" json:"roleId"`
	// DeptID 部门 ID。
	DeptID int64 `gorm:"column:dept_id;not null" json:"deptId"`
}

// TableName 返回 sa_system_role_dept 表名。
func (SysRoleDept) TableName() string { return "sa_system_role_dept" }

// SysUserDept 对应 sa_system_user_dept 用户部门关联表。
type SysUserDept struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// TenantID 租户 ID。
	TenantID int64 `gorm:"column:tenant_id;not null;default:0" json:"tenantId"`
	// UserID 用户 ID。
	UserID int64 `gorm:"column:user_id;not null" json:"userId"`
	// DeptID 部门 ID。
	DeptID int64 `gorm:"column:dept_id;not null" json:"deptId"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_user_dept 表名。
func (SysUserDept) TableName() string { return "sa_system_user_dept" }

// SysUserPost 对应 sa_system_user_post 用户岗位关联表。
type SysUserPost struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// UserID 用户 ID。
	UserID int64 `gorm:"column:user_id;not null" json:"userId"`
	// PostID 岗位 ID。
	PostID int64 `gorm:"column:post_id;not null" json:"postId"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `gorm:"column:status;not null;default:1" json:"status"`
	// TenantID 租户列，仅写入默认值 0。
	TenantID int64 `gorm:"column:tenant_id;not null;default:0" json:"tenantId"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_user_post 表名。
func (SysUserPost) TableName() string { return "sa_system_user_post" }

// SysDictType 对应 sa_system_dict_type 字典类型表。
type SysDictType struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Name 字典名称。
	Name *string `gorm:"column:name;size:50" json:"name"`
	// Code 字典标识。
	Code *string `gorm:"column:code;size:100;uniqueIndex" json:"code"`
	// Status 状态：1 正常 / 0 停用。
	Status int `gorm:"column:status;default:1" json:"status"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_dict_type 表名。
func (SysDictType) TableName() string { return "sa_system_dict_type" }

// SysDictData 对应 sa_system_dict_data 字典数据表。
//
// Code 保存字典标识（如 gender），TagType 保存前端标签样式短码（N/P/S/W/I/D）。
type SysDictData struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// TypeID 字典类型 ID。
	TypeID *int64 `gorm:"column:type_id" json:"typeId"`
	// Label 字典标签。
	Label *string `gorm:"column:label;size:50" json:"label"`
	// Value 字典值。
	Value *string `gorm:"column:value;size:100" json:"value"`
	// Color 标签颜色。
	Color *string `gorm:"column:color;size:50" json:"color"`
	// Code 字典标识。
	Code *string `gorm:"column:code;size:100;index" json:"code"`
	// TagType 标签样式短码（扩展列）。
	TagType *string `gorm:"column:tag_type;size:10" json:"tagType"`
	// Sort 排序。
	Sort int `gorm:"column:sort;default:0" json:"sort"`
	// Status 状态：1 正常 / 0 停用。
	Status int `gorm:"column:status;default:1" json:"status"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_dict_data 表名。
func (SysDictData) TableName() string { return "sa_system_dict_data" }

// SysConfigGroup 对应 sa_system_config_group 配置分组表。
type SysConfigGroup struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Name 分组名称。
	Name *string `gorm:"column:name;size:50" json:"name"`
	// Code 分组编码。
	Code *string `gorm:"column:code;size:100" json:"code"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_config_group 表名。
func (SysConfigGroup) TableName() string { return "sa_system_config_group" }

// SysConfig 对应 sa_system_config 系统配置表。
//
// key / value 为 init.sql 中的列名，SQLite 下由 GORM 自动加引号避免与关键字冲突。
type SysConfig struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// GroupID 配置分组 ID。
	GroupID *int64 `gorm:"column:group_id" json:"groupId"`
	// Key 配置键，全局唯一。
	Key string `gorm:"column:key;size:32;uniqueIndex" json:"configKey"`
	// Value 配置值。
	Value *string `gorm:"column:value" json:"value"`
	// Name 配置名称。
	Name *string `gorm:"column:name;size:255" json:"name"`
	// InputType 输入控件类型。
	InputType *string `gorm:"column:input_type;size:32" json:"inputType"`
	// ConfigSelectData 下拉候选数据。
	ConfigSelectData *string `gorm:"column:config_select_data;size:500" json:"configSelectData"`
	// Sort 排序。
	Sort int `gorm:"column:sort;default:0" json:"sort"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_config 表名。
func (SysConfig) TableName() string { return "sa_system_config" }

// SysNotice 对应 sa_system_notice 通知公告表（含前端契约所需的扩展列）。
type SysNotice struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Title 公告标题。
	Title string `gorm:"column:title;size:100;not null;default:''" json:"title"`
	// Type 公告类型。
	Type int `gorm:"column:type;not null;default:1" json:"type"`
	// Content 公告内容。
	Content *string `gorm:"column:content" json:"content"`
	// Status 状态：1 正常 / 0 停用。
	Status int `gorm:"column:status;not null;default:1" json:"status"`
	// Level 通知等级（扩展列）。
	Level *string `gorm:"column:level;size:20" json:"level"`
	// PublishStatus 发布状态：0 草稿 / 1 已发布 / 2 已撤回（扩展列）。
	PublishStatus int `gorm:"column:publish_status;not null;default:0" json:"publishStatus"`
	// TargetType 通告目标类型（扩展列）。
	TargetType int `gorm:"column:target_type;not null;default:1" json:"targetType"`
	// TargetUsers 目标用户 ID 列表，逗号分隔（扩展列）。
	TargetUsers *string `gorm:"column:target_users" json:"targetUsers"`
	// PublisherID 发布人 ID（扩展列）。
	PublisherID *int64 `gorm:"column:publisher_id" json:"publisherId"`
	// PublishTime 发布时间（扩展列）。
	PublishTime *time.Time `gorm:"column:publish_time" json:"publishTime"`
	// RevokeTime 撤回时间（扩展列）。
	RevokeTime *time.Time `gorm:"column:revoke_time" json:"revokeTime"`
	// Remark 备注。
	Remark string `gorm:"column:remark;size:255;not null;default:''" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_notice 表名。
func (SysNotice) TableName() string { return "sa_system_notice" }

// SysNoticeRead 对应 sa_system_notice_read 公告已读记录表（扩展表）。
type SysNoticeRead struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// NoticeID 公告 ID。
	NoticeID int64 `gorm:"column:notice_id;not null" json:"noticeId"`
	// UserID 用户 ID。
	UserID int64 `gorm:"column:user_id;not null" json:"userId"`
	// ReadTime 已读时间。
	ReadTime *time.Time `gorm:"column:read_time" json:"readTime"`
	// CreateTime 创建时间。
	CreateTime *time.Time `gorm:"column:create_time" json:"createTime"`
}

// TableName 返回 sa_system_notice_read 表名。
func (SysNoticeRead) TableName() string { return "sa_system_notice_read" }

// SysLoginLog 对应 sa_system_login_log 登录日志表。
//
// Status 语义沿用 init.sql：1 成功 / 2 失败；退出登录也记一条（message = 退出成功）。
type SysLoginLog struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Username 用户名。
	Username *string `gorm:"column:username;size:20" json:"username"`
	// IP 登录 IP。
	IP *string `gorm:"column:ip;size:45" json:"ip"`
	// IPLocation IP 归属地。
	IPLocation *string `gorm:"column:ip_location;size:255" json:"ipLocation"`
	// OS 操作系统。
	OS *string `gorm:"column:os;size:50" json:"os"`
	// Browser 浏览器。
	Browser *string `gorm:"column:browser;size:50" json:"browser"`
	// Status 登录状态：1 成功 / 2 失败。
	Status int `gorm:"column:status;default:1" json:"status"`
	// Message 提示消息。
	Message *string `gorm:"column:message;size:50" json:"message"`
	// LoginTime 登录时间。
	LoginTime *time.Time `gorm:"column:login_time" json:"loginTime"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_login_log 表名。
func (SysLoginLog) TableName() string { return "sa_system_login_log" }

// SysOperLog 对应 sa_system_oper_log 操作日志表（含前端契约所需的扩展列）。
type SysOperLog struct {
	// ID 主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Username 操作人账号。
	Username *string `gorm:"column:username;size:20" json:"username"`
	// App 应用/模块名。
	App *string `gorm:"column:app;size:50" json:"app"`
	// Method 请求方式。
	Method *string `gorm:"column:method;size:20" json:"method"`
	// Router 请求路由。
	Router *string `gorm:"column:router;size:500" json:"router"`
	// ServiceName 业务标题（由操作日志中间件按路由推断）。
	ServiceName *string `gorm:"column:service_name;size:30" json:"serviceName"`
	// IP 请求 IP。
	IP *string `gorm:"column:ip;size:45" json:"ip"`
	// IPLocation IP 归属地。
	IPLocation *string `gorm:"column:ip_location;size:255" json:"ipLocation"`
	// RequestData 请求数据（已脱敏，最长 2KB）。
	RequestData *string `gorm:"column:request_data" json:"requestData"`
	// Duration 耗时（毫秒文本）。
	Duration *string `gorm:"column:duration;size:20" json:"duration"`
	// ActionType 操作类型：create/update/delete/...（扩展列）。
	ActionType *string `gorm:"column:action_type;size:20" json:"actionType"`
	// OperatorID 操作人 ID（扩展列）。
	OperatorID *int64 `gorm:"column:operator_id" json:"operatorId"`
	// Device 设备类型（扩展列）。
	Device *string `gorm:"column:device;size:50" json:"device"`
	// Browser 浏览器（扩展列）。
	Browser *string `gorm:"column:browser;size:50" json:"browser"`
	// OS 操作系统（扩展列）。
	OS *string `gorm:"column:os;size:50" json:"os"`
	// Status 执行状态：1 成功 / 0 失败（扩展列）。
	Status *int `gorm:"column:status" json:"status"`
	// ErrorMsg 错误信息（扩展列）。
	ErrorMsg *string `gorm:"column:error_msg;size:500" json:"errorMsg"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_oper_log 表名。
func (SysOperLog) TableName() string { return "sa_system_oper_log" }

// SysRefreshToken 对应 sa_system_refresh_token 刷新令牌表。
//
// 一条未吊销且未过期的记录即一个「在线会话」：主键是会话编号（JWT 的 sid），
// 因此在线用户列表直接以本表为数据源，强退即把 revoked_at 置为当前时间。
type SysRefreshToken struct {
	// ID 令牌主键（UUID），即会话编号。
	ID string `gorm:"column:id;primaryKey;size:64" json:"id"`
	// UserID 所属用户。
	UserID int64 `gorm:"column:user_id;not null" json:"userId"`
	// TokenHash 令牌 SHA-256 指纹。
	TokenHash string `gorm:"column:token_hash;size:128" json:"-"`
	// ExpiresAt 过期时间。
	ExpiresAt time.Time `gorm:"column:expires_at" json:"expiresAt"`
	// RevokedAt 吊销时间。
	RevokedAt *time.Time `gorm:"column:revoked_at" json:"revokedAt"`
	// CreatedAt 创建时间（登录时间）。
	CreatedAt *time.Time `gorm:"column:created_at" json:"createdAt"`
	// IP 登录 IP（[扩展]）。
	IP *string `gorm:"column:ip;size:45" json:"ip"`
	// IPLocation 登录地点（[扩展]）。
	IPLocation *string `gorm:"column:ip_location;size:255" json:"ipLocation"`
	// Device 设备类型（[扩展]）。
	Device *string `gorm:"column:device;size:32" json:"device"`
	// OS 操作系统（[扩展]）。
	OS *string `gorm:"column:os;size:50" json:"os"`
	// Browser 浏览器（[扩展]）。
	Browser *string `gorm:"column:browser;size:50" json:"browser"`
	// LastActiveAt 最近活跃时间（[扩展]）。
	LastActiveAt *time.Time `gorm:"column:last_active_at" json:"lastActiveAt"`
}

// TableName 返回 sa_system_refresh_token 表名。
func (SysRefreshToken) TableName() string { return "sa_system_refresh_token" }

// SysCategory 对应 sa_system_category 附件分类表。
type SysCategory struct {
	// ID 分类主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// ParentID 父分类主键，0 表示根分类。
	ParentID int64 `gorm:"column:parent_id;not null;default:0" json:"parentId"`
	// Level 组集关系（祖先链，如 "0,1,"），用于按分类树筛选。
	Level string `gorm:"column:level;size:255" json:"level"`
	// CategoryName 分类名称。
	CategoryName string `gorm:"column:category_name;size:100;not null;default:''" json:"categoryName"`
	// Sort 排序值。
	Sort int `gorm:"column:sort;not null;default:0" json:"sort"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `gorm:"column:status;default:1" json:"status"`
	// Remark 备注。
	Remark string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列（含软删除时间）。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_category 表名。
func (SysCategory) TableName() string { return "sa_system_category" }

// SysAttachment 对应 sa_system_attachment 附件表。
type SysAttachment struct {
	// ID 附件主键。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// CategoryID 所属分类主键，0 表示未分类。
	CategoryID int64 `gorm:"column:category_id;default:0" json:"categoryId"`
	// StorageMode 存储模式：1 本地（当前仅实现本地）。
	StorageMode int `gorm:"column:storage_mode;default:1" json:"storageMode"`
	// OriginName 原文件名（用户可见名，重命名改此列）。
	OriginName *string `gorm:"column:origin_name;size:255" json:"originName"`
	// ObjectName 存储对象名（落盘文件名）。
	ObjectName string `gorm:"column:object_name;size:255;not null" json:"objectName"`
	// Hash 文件内容 MD5。
	Hash *string `gorm:"column:hash;size:64" json:"hash"`
	// MimeType 资源类型。
	MimeType *string `gorm:"column:mime_type;size:255" json:"mimeType"`
	// StoragePath 相对上传根目录的存储路径（upload/2026/10/06/x.jpg）。
	StoragePath string `gorm:"column:storage_path;size:512;not null" json:"storagePath"`
	// Suffix 文件后缀（不含点）。
	Suffix *string `gorm:"column:suffix;size:10" json:"suffix"`
	// SizeByte 字节数。
	SizeByte *int64 `gorm:"column:size_byte" json:"sizeByte"`
	// SizeInfo 可读文件大小（如 8.57KB）。
	SizeInfo *string `gorm:"column:size_info;size:50" json:"sizeInfo"`
	// URL 访问地址（/upload/2026/10/06/x.jpg）。
	URL string `gorm:"column:url;size:1024;not null" json:"url"`
	// Remark 备注。
	Remark *string `gorm:"column:remark;size:255" json:"remark"`
	// AuditFields 审计列（含软删除时间）。
	AuditFields `gorm:"embedded"`
}

// TableName 返回 sa_system_attachment 表名。
func (SysAttachment) TableName() string { return "sa_system_attachment" }
