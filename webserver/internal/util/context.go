package util

// ContextUserKey 是鉴权中间件写入 gin.Context 的当前用户键名。
//
// 中间件（api/middleware）写入、控制器（api/v1）读取，常量放在本包以避免两个包互相依赖。
const ContextUserKey = "monitor.currentUser"

// ContextSessionKey 是会话编号在 gin.Context 中的键（JWT 载荷 sid）。
const ContextSessionKey = "monitor.sessionID"

// ContextOperatorKey 是操作日志中间件读取操作人信息的键名。
const ContextOperatorKey = "monitor.operator"
