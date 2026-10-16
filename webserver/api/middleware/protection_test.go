package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/util"
)

func protectionEngine(cfg ProtectionConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Protection(cfg))
	engine.Any("/api", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.Any("/api/v1/auth/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return engine
}

func protectionRequest(engine *gin.Engine, path, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = ip + ":1234"
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder
}

func TestProtectionBlacklistSupportsCIDR(t *testing.T) {
	engine := protectionEngine(ProtectionConfig{
		Enabled:   true,
		Blacklist: []string{"192.0.2.0/24"},
	})
	response := protectionRequest(engine, "/api", "192.0.2.10")
	if response.Code != http.StatusForbidden {
		t.Fatalf("blacklisted IP should be rejected, got %d", response.Code)
	}
}

func TestProtectionSeparatesLoginAndAPILimits(t *testing.T) {
	engine := protectionEngine(ProtectionConfig{
		Enabled:       true,
		APIRequests:   1,
		APIWindow:     time.Minute,
		LoginRequests: 2,
		LoginWindow:   time.Minute,
	})
	if response := protectionRequest(engine, "/api", "198.51.100.1"); response.Code != http.StatusNoContent {
		t.Fatalf("first API request should pass, got %d", response.Code)
	}
	if response := protectionRequest(engine, "/api", "198.51.100.1"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("second API request should be limited, got %d", response.Code)
	}
	if response := protectionRequest(engine, "/api/v1/auth/login", "198.51.100.1"); response.Code != http.StatusNoContent {
		t.Fatalf("login limiter should have an independent quota, got %d", response.Code)
	}
}

// protectionResolvedIP 返回中间件写入上下文的来源 IP。
func protectionResolvedIP(cfg ProtectionConfig, remoteAddr string, headers map[string]string) string {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Protection(cfg))
	engine.GET("/api", func(c *gin.Context) { c.String(http.StatusOK, c.GetString(util.ContextClientIPKey)) })
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.RemoteAddr = remoteAddr
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder.Body.String()
}

func TestProtectionTrustsForwardedHeadersWhenEnabled(t *testing.T) {
	cfg := ProtectionConfig{TrustProxyHeaders: true}
	// 反向代理追加的 X-Forwarded-For：首个是原始客户端，后续是代理链。
	ip := protectionResolvedIP(cfg, "127.0.0.1:1234", map[string]string{
		"X-Forwarded-For": "203.0.113.7, 10.0.0.9",
	})
	if ip != "203.0.113.7" {
		t.Fatalf("X-Forwarded-For should win when proxy headers are trusted, got %q", ip)
	}
	// 只有 X-Real-IP 时回退到该头。
	ip = protectionResolvedIP(cfg, "127.0.0.1:1234", map[string]string{"X-Real-IP": "203.0.113.8"})
	if ip != "203.0.113.8" {
		t.Fatalf("X-Real-IP should be used when X-Forwarded-For is absent, got %q", ip)
	}
}

func TestProtectionIgnoresForwardedHeadersWhenDisabled(t *testing.T) {
	// 直连部署：客户端伪造代理头不能改变来源 IP，否则限流与锁定可被绕过。
	ip := protectionResolvedIP(ProtectionConfig{}, "198.51.100.20:1234", map[string]string{
		"X-Forwarded-For": "203.0.113.7",
		"X-Real-IP":       "203.0.113.8",
	})
	if ip != "198.51.100.20" {
		t.Fatalf("forged proxy headers must be ignored, got %q", ip)
	}
}

func TestProtectionWhitelistBypassesLimits(t *testing.T) {
	engine := protectionEngine(ProtectionConfig{
		Enabled:       true,
		APIRequests:   1,
		APIWindow:     time.Minute,
		LoginRequests: 1,
		LoginWindow:   time.Minute,
		Whitelist:     []string{"198.51.100.10"},
	})
	for i := 0; i < 3; i++ {
		if response := protectionRequest(engine, "/api", "198.51.100.10"); response.Code != http.StatusNoContent {
			t.Fatalf("whitelisted API request should pass, got %d", response.Code)
		}
		if response := protectionRequest(engine, "/api/v1/auth/login", "198.51.100.10"); response.Code != http.StatusNoContent {
			t.Fatalf("whitelisted login request should pass, got %d", response.Code)
		}
	}
}
