package dto

// OnlineUser 是在线用户（登录会话）列表项，对齐前端在线用户视图。
type OnlineUser struct {
	// ID 会话编号（刷新令牌主键 / JWT 的 sid）。
	ID string `json:"id"`
	// Username 登录账号。
	Username string `json:"username"`
	// Nickname 用户昵称（sa_system_user.realname）。
	Nickname string `json:"nickname"`
	// DeptName 所属部门。
	DeptName string `json:"deptName"`
	// IP 主机地址。
	IP string `json:"ip"`
	// IPLocation 登录地点。
	IPLocation string `json:"ipLocation"`
	// Device 设备类型。
	Device string `json:"device"`
	// OS 操作系统。
	OS string `json:"os"`
	// Browser 浏览器。
	Browser string `json:"browser"`
	// LoginTime 登录时间（会话创建时间）。
	LoginTime string `json:"loginTime"`
	// LastActiveTime 最近活跃时间。
	LastActiveTime string `json:"lastActiveTime"`
	// ExpiresTime 会话过期时间。
	ExpiresTime string `json:"expiresTime"`
}

// OnlineQuery 是在线用户查询条件。
type OnlineQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Keywords 登录账号 / IP 模糊搜索。
	Keywords string `json:"keywords"`
	// Username 精确匹配登录账号。
	Username string `json:"username"`
	// IP 精确匹配主机地址。
	IP string `json:"ip"`
}

// OnlineOverview 是在线用户概览（顶部统计用）。
type OnlineOverview struct {
	// Total 当前在线会话数。
	Total int64 `json:"total"`
	// UserTotal 去重后的在线用户数。
	UserTotal int64 `json:"userTotal"`
}
