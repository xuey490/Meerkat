package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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
