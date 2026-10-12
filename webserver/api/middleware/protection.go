package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/util"
)

const protectionCode = "B0210"

type ProtectionConfig struct {
	Enabled           bool
	APIRequests       int
	APIWindow         time.Duration
	LoginRequests     int
	LoginWindow       time.Duration
	Blacklist         []string
	Whitelist         []string
	TrustProxyHeaders bool
}

type requestLimiter struct {
	limit   int
	window  time.Duration
	mu      sync.Mutex
	entries map[string][]time.Time
}

func newRequestLimiter(limit int, window time.Duration) *requestLimiter {
	if limit <= 0 || window <= 0 {
		return nil
	}
	return &requestLimiter{limit: limit, window: window, entries: make(map[string][]time.Time)}
}

func (l *requestLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	history := l.entries[key]
	cutoff := now.Add(-l.window)
	start := 0
	for start < len(history) && history[start].Before(cutoff) {
		start++
	}
	history = history[start:]
	if len(history) >= l.limit {
		l.entries[key] = history
		return false
	}
	l.entries[key] = append(history, now)
	return true
}

type ipMatcher struct {
	ip  net.IP
	net *net.IPNet
}

func newIPMatchers(values []string) []ipMatcher {
	matchers := make([]ipMatcher, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			matchers = append(matchers, ipMatcher{net: network})
			continue
		}
		if parsed := net.ParseIP(value); parsed != nil {
			matchers = append(matchers, ipMatcher{ip: parsed})
		}
	}
	return matchers
}

func (m ipMatcher) matches(ip net.IP) bool {
	if m.net != nil {
		return m.net.Contains(ip)
	}
	return m.ip != nil && m.ip.Equal(ip)
}

// Protection provides blacklist and per-IP request limits.
func Protection(cfg ProtectionConfig) gin.HandlerFunc {
	matchers := newIPMatchers(cfg.Blacklist)
	whitelist := newIPMatchers(cfg.Whitelist)
	apiLimiter := newRequestLimiter(cfg.APIRequests, cfg.APIWindow)
	loginLimiter := newRequestLimiter(cfg.LoginRequests, cfg.LoginWindow)
	return func(c *gin.Context) {
		// 来源 IP 只在这里解析一次（是否信任代理头由配置决定），写入上下文供
		// 控制器与其他中间件统一读取；关闭防护总开关时也照常写入，保证日志口径一致。
		ip := protectionClientIP(c, cfg.TrustProxyHeaders)
		c.Set(util.ContextClientIPKey, ip)
		if !cfg.Enabled {
			c.Next()
			return
		}
		parsed := net.ParseIP(ip)
		exempt := false
		for _, matcher := range whitelist {
			if parsed != nil && matcher.matches(parsed) {
				exempt = true
				break
			}
		}
		if exempt {
			c.Set(util.ContextProtectionExemptKey, true)
			c.Next()
			return
		}
		for _, matcher := range matchers {
			if parsed != nil && matcher.matches(parsed) {
				denyProtection(c, http.StatusForbidden, "请求来源 IP 已被禁止")
				return
			}
		}
		limiter := apiLimiter
		if c.Request.URL.Path == "/api/v1/auth/login" {
			limiter = loginLimiter
		}
		if limiter != nil && !limiter.allow(ip, time.Now()) {
			c.Header("Retry-After", strconv.Itoa(int(limiter.window.Seconds())))
			denyProtection(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
			return
		}
		c.Next()
	}
}

// ProtectionExempt reports whether the request IP is in the protection whitelist.
func ProtectionExempt(c *gin.Context) bool {
	value, ok := c.Get(util.ContextProtectionExemptKey)
	return ok && value == true
}

func protectionClientIP(c *gin.Context, trustProxy bool) string {
	if trustProxy {
		if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-For")); forwarded != "" {
			return strings.TrimSpace(strings.Split(forwarded, ",")[0])
		}
		if realIP := strings.TrimSpace(c.GetHeader("X-Real-IP")); realIP != "" {
			return realIP
		}
	}
	ip, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err == nil {
		return ip
	}
	return strings.TrimSpace(c.Request.RemoteAddr)
}

func denyProtection(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": protectionCode, "msg": message, "data": nil})
	c.Abort()
}
