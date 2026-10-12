package util

// ContextUserKey 是鉴权中间件写入 gin.Context 的当前用户键名。
//
// 中间件（api/middleware）写入、控制器（api/v1）读取，常量放在本包以避免两个包互相依赖。
const ContextUserKey = "monitor.currentUser"

// ContextSessionKey 是会话编号在 gin.Context 中的键（JWT 载荷 sid）。
const ContextSessionKey = "monitor.sessionID"

// ContextOperatorKey 是操作日志中间件读取操作人信息的键名。
const ContextOperatorKey = "monitor.operator"

// ContextProtectionExemptKey marks a request from the configured protection whitelist.
const ContextProtectionExemptKey = "monitor.protectionExempt"

// ContextClientIPKey 是防护中间件解析出的客户端 IP。
//
// 是否信任 X-Forwarded-For / X-Real-IP 由 protection.trust_proxy_headers 决定，
// 因此来源 IP 只解析一次并写入上下文，供限流、登录日志与操作日志统一读取，
// 避免各处口径不一致（直连部署时客户端伪造该头即可绕开 IP 维度限流与锁定）。
const ContextClientIPKey = "monitor.clientIP"
