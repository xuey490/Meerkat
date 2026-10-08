package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// codeDemoForbidden 演示模式拦截写操作时返回的业务码。
const codeDemoForbidden = "B0206"

// demoWriteWhitelist 演示模式下始终放行的写操作路径。
//
// 白名单只覆盖「认证本身」的操作：登录、验证码、退出、刷新令牌。
// 其余写操作（含附件上传、用户/角色/菜单等维护动作）在演示模式下都会被拒绝。
var demoWriteWhitelist = []string{
	"/api/v1/auth/login",
	"/api/v1/auth/captcha",
	"/api/v1/auth/logout",
	"/api/v1/auth/refresh-token",
}

// DemoMode 构造演示模式中间件：开启后除白名单外的写操作一律拒绝（403 / B0206）。
//
// 为什么用中间件而不是在各控制器里判断：写操作分散在十几个控制器，
// 中间件按「HTTP 方法 + 路径白名单」统一拦截，既不会漏，也不侵入业务代码。
//
// 参数 Parameters:
//   - enabled (bool): 是否开启演示模式。
//   - extraWhitelist ([]string): 额外放行的路径（前缀匹配，用于按部署情况追加白名单）。
//
// 返回 Returns:
//   - handler (gin.HandlerFunc): Gin 中间件。
func DemoMode(enabled bool, extraWhitelist []string) gin.HandlerFunc {
	whitelist := make([]string, 0, len(demoWriteWhitelist)+len(extraWhitelist))
	whitelist = append(whitelist, demoWriteWhitelist...)
	for _, path := range extraWhitelist {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			whitelist = append(whitelist, trimmed)
		}
	}
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}
		if !IsWriteMethod(c.Request.Method) || matchAnyPath(c.Request.URL.Path, whitelist) {
			c.Next()
			return
		}
		respond(c, http.StatusForbidden, codeDemoForbidden, "演示模式已开启，该操作被禁止")
		c.Abort()
	}
}

// IsWriteMethod 判断请求方法是否为写操作（POST/PUT/PATCH/DELETE）。
//
// 参数 Parameters:
//   - method (string): HTTP 方法。
//
// 返回 Returns:
//   - isWrite (bool): 写操作返回 true。
func IsWriteMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// matchAnyPath 判断路径是否命中白名单（精确匹配或以 `<白名单>/` 开头）。
//
// 用「前缀 + 斜杠」而不是裸前缀，避免 /api/v1/auth/login 意外放行
// /api/v1/auth/login-history 这类同前缀路径。
//
// 参数 Parameters:
//   - path (string): 请求路径。
//   - whitelist ([]string): 白名单路径。
//
// 返回 Returns:
//   - matched (bool): 命中返回 true。
func matchAnyPath(path string, whitelist []string) bool {
	for _, item := range whitelist {
		if path == item || strings.HasPrefix(path, strings.TrimSuffix(item, "/")+"/") {
			return true
		}
	}
	return false
}
