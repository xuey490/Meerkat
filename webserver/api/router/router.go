// Package router 负责把控制器与中间件装配成完整的 Gin 引擎。
//
// 装配规则：
//   - /healthz 与 /api/v1/auth/{captcha,login,refresh-token} 为公开接口；
//   - 其余 /api/v1/* 先过 RequireAuth（登录态），写操作再按权限标识过 RequirePerm；
//   - 全局中间件顺序：SecurityHeaders → RequestID → CORS → XSS → XSRF → 演示模式
//     → 访问日志 → 操作日志。跨域与请求 ID 复用 pkg/middleware 的原版组件。
package router

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/api/middleware"
	v1 "github.com/company/monitor-webserver/api/v1"
	"github.com/company/monitor-webserver/internal/permission"
	"github.com/company/monitor-webserver/internal/service"
	pkgmiddleware "github.com/company/monitor-webserver/pkg/middleware"
)

// Deps 是装配路由所需的依赖集合。
type Deps struct {
	// Auth 认证控制器。
	Auth *v1.AuthAPI
	// Users 用户控制器。
	Users *v1.UserAPI
	// Roles 角色控制器。
	Roles *v1.RoleAPI
	// Depts 部门控制器。
	Depts *v1.DeptAPI
	// Menus 菜单控制器。
	Menus *v1.MenuAPI
	// Posts 岗位控制器。
	Posts *v1.PostAPI
	// Dicts 字典控制器。
	Dicts *v1.DictAPI
	// Configs 系统配置控制器。
	Configs *v1.ConfigAPI
	// Notices 公告控制器。
	Notices *v1.NoticeAPI
	// Logs 日志控制器。
	Logs *v1.LogAPI
	// Attachments 附件控制器（含通用文件上传）。
	Attachments *v1.AttachmentAPI
	// Caches 缓存管理控制器。
	Caches *v1.CacheAPI
	// Online 在线用户控制器。
	Online *v1.OnlineAPI
	// Monitor 监控控制器。
	Monitor *v1.MonitorAPI
	// Remote 远程运维控制器（SSH/RDP/操作审计）。
	Remote *v1.RemoteAPI
	// DangerousCommand 高危命令规则控制器。
	DangerousCommand *v1.DangerousCommandAPI
	// Job 计划任务控制器。
	Job *v1.JobAPI
	// JobLog 计划任务日志控制器。
	JobLog *v1.JobLogAPI
	// UploadDir 附件本地存储目录；为空时不挂载静态访问。
	UploadDir string
	// UploadURLPrefix 附件静态访问前缀，默认为 /upload。
	UploadURLPrefix string
	// AuthService 认证服务（鉴权中间件解析令牌使用）。
	AuthService *service.AuthService
	// Perms 权限服务（权限中间件使用）。
	Perms *permission.Service
	// OperateLog 操作日志中间件（由 app 层注入，内部持有数据库会话）。
	OperateLog gin.HandlerFunc
	// AccessLog 是否开启访问日志。
	AccessLog bool
	// Print 访问日志输出函数。
	Print func(string)
	// CORSConfig 跨域策略（复用 pkg/middleware.CORS）。
	CORSConfig pkgmiddleware.CORSConfig
	// CSRFConfig 跨站请求伪造防护配置（Origin / Referer 校验）。
	CSRFConfig middleware.CSRFConfig
	// XSSEnabled 是否启用 XSS 清洗中间件。
	XSSEnabled bool
	// DemoEnabled 是否开启演示模式（除白名单外的写操作一律拒绝）。
	DemoEnabled bool
	// DemoWhitelist 演示模式额外放行的路径。
	DemoWhitelist []string
}

// New 装配并返回 Gin 引擎。
//
// 参数 Parameters:
//   - deps (Deps): 控制器与中间件依赖。
//
// 返回 Returns:
//   - engine (*gin.Engine): 已完成中间件与路由注册的 Gin 引擎。
func New(deps Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(middleware.SecurityHeaders())
	// 请求关联 ID：贯穿访问日志、操作日志与响应头，便于排查单次请求。
	engine.Use(pkgmiddleware.RequestID())
	// 跨域：复用 pkg/middleware 的原版实现（含预检处理）。
	engine.Use(pkgmiddleware.CORS(deps.CORSConfig))
	if deps.XSSEnabled {
		engine.Use(middleware.XSS())
	}
	// XSRF：写方法校验 Origin / Referer；本系统用 Bearer 令牌，无 Cookie 自动携带面。
	engine.Use(middleware.CSRF(deps.CSRFConfig))
	// 演示模式：放在鉴权之前，直接拦掉写操作，避免无谓的令牌解析与审计写入。
	engine.Use(middleware.DemoMode(deps.DemoEnabled, deps.DemoWhitelist))
	engine.Use(middleware.AccessLog(deps.AccessLog, deps.Print))
	if deps.OperateLog != nil {
		engine.Use(deps.OperateLog)
	}

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": "00000", "msg": "操作成功",
			"data": gin.H{"status": "ok", "time": time.Now().UTC()}})
	})

	api := engine.Group("/api/v1")

	// 公开接口：验证码、登录、刷新令牌。
	if deps.Auth != nil {
		deps.Auth.RegisterPublic(api)
	}

	// 需登录接口。
	authed := api.Group("")
	authed.Use(middleware.RequireAuth(deps.AuthService))

	guard := func(perm string) gin.HandlerFunc {
		return middleware.RequirePerm(deps.Perms, perm)
	}

	if deps.Auth != nil {
		deps.Auth.RegisterAuthed(authed)
	}
	for _, register := range []func(*gin.RouterGroup, v1.Guard){
		deps.Users.Register, deps.Roles.Register, deps.Depts.Register, deps.Menus.Register,
		deps.Posts.Register, deps.Dicts.Register, deps.Configs.Register, deps.Notices.Register,
		deps.Logs.Register, deps.Attachments.Register, deps.Caches.Register,
		deps.Online.Register, deps.Monitor.Register, deps.Remote.Register,
		deps.DangerousCommand.Register, deps.Job.Register, deps.JobLog.Register,
	} {
		if register == nil {
			continue
		}
		register(authed, guard)
	}

	// SSH 终端 WebSocket：浏览器握手时无法携带 Authorization 头，改由一次性
	// ticket 鉴权（先 POST /monitor/ssh/ticket 换取），因此挂在全局 engine 层。
	if deps.Remote != nil {
		engine.GET("/api/v1/monitor/ssh/ws", deps.Remote.SSHWebSocket)
	}

	// 附件静态访问：/upload/... 直出本地文件，供 <img>/<a> 等直接引用。
	// 该路径不做登录校验（浏览器加载图片不会带 Authorization 头），配合
	// SecurityHeaders 的 nosniff 限制内容嗅探；上传目录本身不含可执行内容。
	if deps.UploadDir != "" {
		prefix := deps.UploadURLPrefix
		if prefix == "" {
			prefix = "/upload"
		}
		engine.Static(prefix, deps.UploadDir)
	}
	return engine
}
