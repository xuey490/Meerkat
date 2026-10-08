package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// codeCSRFRejected 跨站请求被拒时返回的业务码。
const codeCSRFRejected = "B0205"

// CSRFConfig 控制跨站请求伪造（XSRF）防护策略。
type CSRFConfig struct {
	// Enabled 是否启用校验。
	Enabled bool
	// AllowOrigins 明确放行的来源（scheme://host[:port]）；留空表示只校验同源与回环地址。
	// 其中的 "*" 不参与放行——通配会让校验失去意义，需要跨源写入时请显式列出来源。
	AllowOrigins []string
}

// CSRF 构造跨站请求伪造防护中间件（Origin / Referer 校验）。
//
// 为什么这样实现：本系统的凭证是 Authorization 头里的 Bearer 令牌，不是 Cookie，
// 浏览器不会「自动携带」，因此不存在传统 Cookie-CSRF 的自动提交面；残留风险是
// 第三方页面诱导浏览器发起带 Origin 的跨站写请求。按 gin 生态的通用做法，
// 对写方法校验 Origin（缺失时回落 Referer）即可覆盖：
//
//   - 无 Origin / Referer：视为非浏览器调用（curl、服务间调用），放行；
//   - Origin 为 null（file://、沙箱 iframe）：拒绝；
//   - Origin 命中 allow_origins、与请求 Host 同源、或双方都是本机回环地址：放行；
//   - 其余：403 / B0205，并打印一条 warn 便于排查（含 origin 与 host）。
//
// 关于「双方都是回环地址」：前端 dev server 常在 http://127.0.0.1:3000，
// Vite 代理默认 changeOrigin: true，转发到后端时 Host 变成 localhost:8090，
// 两者不同源但同为本机；若不放行，本地开发的所有写操作都会被误判为跨站。
// 外网来源不属于回环地址，仍会被拦截，生产安全性不受影响。
//
// 参数 Parameters:
//   - cfg (CSRFConfig): 校验配置。
//
// 返回 Returns:
//   - handler (gin.HandlerFunc): Gin 中间件。
func CSRF(cfg CSRFConfig) gin.HandlerFunc {
	allowed := make([]string, 0, len(cfg.AllowOrigins))
	for _, origin := range cfg.AllowOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			// 显式写 "*"：明确表示「不要校验」（例如确认部署拓扑下无 CSRF 面）。
			// 注意与「留空」区分：留空仍按同源 + 回环地址判定。
			return func(c *gin.Context) { c.Next() }
		}
		allowed = append(allowed, strings.TrimSuffix(strings.ToLower(trimmed), "/"))
	}
	return func(c *gin.Context) {
		if !cfg.Enabled || !IsWriteMethod(c.Request.Method) {
			c.Next()
			return
		}
		origin := normalOrigin(c.GetHeader("Origin"))
		if origin == "" {
			origin = normalOrigin(c.GetHeader("Referer"))
		}
		if origin == "" {
			c.Next()
			return
		}
		if origin == "null" {
			rejectCrossSite(c, origin)
			return
		}
		if originTrusted(origin, c, allowed) {
			c.Next()
			return
		}
		rejectCrossSite(c, origin)
	}
}

// rejectCrossSite 记录一条可排查的告警并写出 403。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - origin (string): 归一化后的来源。
func rejectCrossSite(c *gin.Context, origin string) {
	slog.Warn("跨站请求被拒绝",
		"origin", origin,
		"host", requestHost(c),
		"method", c.Request.Method,
		"path", c.Request.URL.Path)
	respond(c, http.StatusForbidden, codeCSRFRejected, "跨站请求被拒绝")
	c.Abort()
}

// originTrusted 判断来源是否可信。
//
// 参数 Parameters:
//   - origin (string): 已归一化的来源（小写）。
//   - c (*gin.Context): 当前请求上下文。
//   - allowed ([]string): 放行来源列表（已归一化）。
//
// 返回 Returns:
//   - trusted (bool): 可信返回 true。
func originTrusted(origin string, c *gin.Context, allowed []string) bool {
	if containsOrigin(allowed, origin) {
		return true
	}
	host := requestHost(c)
	if sameHost(origin, host) {
		return true
	}
	// 本地开发：前端 dev server 与后端不同端口/不同回环写法，但都在本机。
	return isLoopbackHost(origin) && isLoopbackName(host)
}

// requestHost 返回请求的「站点 Host」：优先取反向代理透传的 X-Forwarded-Host，
// 便于代理改写 Host 的部署（如 nginx、Vite dev proxy）也能正确同源判定。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//
// 返回 Returns:
//   - host (string): 站点 Host。
func requestHost(c *gin.Context) string {
	if forwarded := c.GetHeader("X-Forwarded-Host"); forwarded != "" {
		if index := strings.IndexByte(forwarded, ','); index > 0 {
			return strings.TrimSpace(forwarded[:index])
		}
		return strings.TrimSpace(forwarded)
	}
	return c.Request.Host
}

// normalOrigin 把 Origin / Referer 统一归一化为 scheme://host[:port]。
//
// 参数 Parameters:
//   - raw (string): 原始头部值。
//
// 返回 Returns:
//   - origin (string): 归一化结果；为空或不可解析时返回空串。
func normalOrigin(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, "null") {
		return "null"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

// containsOrigin 判断来源是否在放行列表中。
//
// 参数 Parameters:
//   - allowed ([]string): 已归一化的放行列表（小写）。
//   - origin (string): 已归一化的来源（小写）。
//
// 返回 Returns:
//   - matched (bool): 命中返回 true。
func containsOrigin(allowed []string, origin string) bool {
	for _, item := range allowed {
		if item == origin {
			return true
		}
	}
	return false
}

// sameHost 判断来源与站点 Host 是否同源（同 host:port）。
//
// 参数 Parameters:
//   - origin (string): 归一化来源，如 http://localhost:3000。
//   - host (string): 站点 Host，如 localhost:3000。
//
// 返回 Returns:
//   - same (bool): 同源返回 true。
func sameHost(origin, host string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, strings.TrimSpace(host))
}

// isLoopbackHost 判断归一化来源是否指向本机（localhost / 127.0.0.0/8 / ::1）。
//
// 参数 Parameters:
//   - origin (string): 归一化来源。
//
// 返回 Returns:
//   - loopback (bool): 本机返回 true。
func isLoopbackHost(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return isLoopbackName(parsed.Host)
}

// isLoopbackName 判断 host[:port] 是否为本机地址。
//
// 参数 Parameters:
//   - host (string): 形如 127.0.0.1:3000、localhost:8090、[::1]:8090。
//
// 返回 Returns:
//   - loopback (bool): 本机返回 true。
func isLoopbackName(host string) bool {
	name := strings.ToLower(splitHost(host))
	if name == "localhost" || name == "::1" {
		return true
	}
	if ip := net.ParseIP(name); ip != nil {
		return ip.IsLoopback()
	}
	return strings.HasPrefix(name, "127.")
}

// splitHost 去掉 host 里的端口与 IPv6 方括号。
//
// 参数 Parameters:
//   - host (string): 形如 example.com:8080、[::1]:8090、example.com。
//
// 返回 Returns:
//   - name (string): 主机名或 IP。
func splitHost(host string) string {
	trimmed := strings.TrimSpace(host)
	if name, _, err := net.SplitHostPort(trimmed); err == nil {
		return strings.Trim(name, "[]")
	}
	return strings.Trim(trimmed, "[]")
}
