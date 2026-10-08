// Package middleware 提供 Gin 中间件：登录鉴权、权限校验、操作日志与访问日志。
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/permission"
	"github.com/company/monitor-webserver/internal/service"
	"github.com/company/monitor-webserver/internal/util"
)

// 鉴权失败与越权的响应（与迁移前保持一致）。
const (
	// codeTokenInvalid 访问令牌失效。
	codeTokenInvalid = "A0230"
	// codeDenied 权限不足。
	codeDenied = "A0301"
)

// respond 写出统一响应壳（中间件不依赖 api/v1 包，避免循环引用）。
func respond(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"code": code, "msg": message, "data": nil})
}

// RequireAuth 校验 Authorization 头中的访问令牌并加载当前用户。
//
// 校验通过后把用户与会话编号写入上下文；失败时写出 A0230，前端会尝试刷新令牌。
// 会话编号（sid）用于「强退」：被强制下线的会话即使 access token 未过期也立即失效。
//
// 参数 Parameters:
//   - auth (*service.AuthService): 认证服务（解析令牌并加载用户）。
//
// 返回 Returns:
//   - handler (gin.HandlerFunc): Gin 中间件。
func RequireAuth(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			respond(c, http.StatusUnauthorized, codeTokenInvalid, "登录已过期")
			c.Abort()
			return
		}
		user, sid, err := auth.UserFromToken(header[len(prefix):])
		if err != nil || user == nil {
			respond(c, http.StatusUnauthorized, codeTokenInvalid, "登录已过期")
			c.Abort()
			return
		}
		if sid != "" && auth.SessionRevoked(c.Request.Context(), sid) {
			respond(c, http.StatusUnauthorized, codeTokenInvalid, "该会话已被强制下线")
			c.Abort()
			return
		}
		c.Set(util.ContextUserKey, user)
		c.Set(util.ContextSessionKey, sid)
		c.Next()
	}
}

// RequirePerm 构造权限校验中间件：超级管理员直通，其余按用户权限集合判定。
//
// 参数 Parameters:
//   - perms (*permission.Service): 权限服务。
//   - perm (string): 权限标识（sa_system_menu.slug），如 sys:user:create。
//
// 返回 Returns:
//   - handler (gin.HandlerFunc): 校验失败时返回 403 / A0301。
func RequirePerm(perms *permission.Service, perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := currentUser(c)
		if user == nil {
			// 未经过 RequireAuth（例如路由漏挂鉴权）时按未登录处理，避免越权。
			respond(c, http.StatusUnauthorized, codeTokenInvalid, "登录已过期")
			c.Abort()
			return
		}
		if entry := perms.Of(user); entry.Allows(perm) {
			c.Next()
			return
		}
		respond(c, http.StatusForbidden, codeDenied, "权限不足")
		c.Abort()
	}
}

// currentUser 从上下文取出当前登录用户。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//
// 返回 Returns:
//   - user (*model.SysUser): 登录用户；未鉴权时返回 nil。
func currentUser(c *gin.Context) *model.SysUser {
	value, exists := c.Get(util.ContextUserKey)
	if !exists {
		return nil
	}
	user, ok := value.(*model.SysUser)
	if !ok {
		return nil
	}
	return user
}
