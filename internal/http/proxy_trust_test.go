package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gptmail/internal/config"

	"github.com/gin-gonic/gin"
)

func TestRateLimitIgnoresUntrustedForwardedIP(t *testing.T) {
	handler := &Handler{DB: httpTestDB(t), Config: config.Config{DevMode: true, FrontendDist: t.TempDir()}}
	router := NewRouter(handler)
	// 零补充速率使同一来源只有一次额度，避免测试依赖执行速度。
	router.GET("/test-ip-limit", handler.perIPRateLimit(0, 1), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	for i, forwarded := range []string{"203.0.113.10", "203.0.113.11"} {
		request := httptest.NewRequest(http.MethodGet, "/test-ip-limit", nil)
		request.RemoteAddr = "198.51.100.20:12345"
		request.Header.Set("X-Forwarded-For", forwarded)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		expected := http.StatusNoContent
		if i > 0 {
			expected = http.StatusTooManyRequests
		}
		if response.Code != expected {
			t.Errorf("same peer changed X-Forwarded-For: status=%d, want %d", response.Code, expected)
		}
	}
}

func TestClientIPUsesOnlyConfiguredProxyChain(t *testing.T) {
	for _, tc := range []struct {
		name, remote, forwarded, want string
		trusted                       []string
	}{
		{"direct", "198.51.100.20:12345", "203.0.113.7", "198.51.100.20", nil},
		{"trusted", "127.0.0.1:12345", "203.0.113.7", "203.0.113.7", []string{"127.0.0.1"}},
		{"untrusted peer", "198.51.100.20:12345", "203.0.113.7", "198.51.100.20", []string{"127.0.0.1"}},
		{"trusted chain", "127.0.0.1:12345", "203.0.113.7, 10.1.0.9", "203.0.113.7", []string{"127.0.0.1", "10.0.0.0/8"}},
		{"untrusted hop", "127.0.0.1:12345", "203.0.113.250, 198.51.100.80, 10.1.0.9", "198.51.100.80", []string{"127.0.0.1", "10.0.0.0/8"}},
		{"ipv6", "[::1]:12345", "2001:db8::9", "2001:db8::9", []string{"::1/128"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{DB: httpTestDB(t), Config: config.Config{TrustedProxies: tc.trusted, FrontendDist: t.TempDir()}}
			router := NewRouter(h)
			router.GET("/test-client-ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
			request := httptest.NewRequest(http.MethodGet, "/test-client-ip", nil)
			request.RemoteAddr = tc.remote
			request.Header.Set("X-Forwarded-For", tc.forwarded)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Body.String() != tc.want {
				t.Fatalf("client IP = %q, want %q", response.Body.String(), tc.want)
			}
		})
	}
}
