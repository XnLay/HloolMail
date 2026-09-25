package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gptmail/internal/apiratelimit"
	"gptmail/internal/apispec"
	"gptmail/internal/models"
)

func unlimitedAPIConfig() models.APIRateLimitConfig {
	config := apiratelimit.DefaultConfig()
	for _, rule := range []*models.APIRateLimitRule{
		&config.PreAuth.PerIP, &config.PreAuth.PerInstance, &config.Business.Mail,
		&config.Business.GenerateEmail, &config.Business.AvailableDomains, &config.Business.Stats, &config.Business.YYDS,
	} {
		rule.Enabled = false
	}
	return config
}

func TestAPIRateLimitPoliciesUpdateExistingBucketsBeforeQuota(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		status             int
		rule               func(*models.APIRateLimitConfig) *models.APIRateLimitRule
	}{
		{"IP", "GET", "/api/mailboxes", 200, func(c *models.APIRateLimitConfig) *models.APIRateLimitRule { return &c.PreAuth.PerIP }},
		{"instance", "GET", "/api/mailboxes", 200, func(c *models.APIRateLimitConfig) *models.APIRateLimitRule { return &c.PreAuth.PerInstance }},
		{"mail", "GET", "/api/mailboxes", 200, func(c *models.APIRateLimitConfig) *models.APIRateLimitRule { return &c.Business.Mail }},
		{"generate", "POST", "/api/generate-email", 400, func(c *models.APIRateLimitConfig) *models.APIRateLimitRule { return &c.Business.GenerateEmail }},
		{"domains", "GET", "/api/domains/available", 200, func(c *models.APIRateLimitConfig) *models.APIRateLimitRule { return &c.Business.AvailableDomains }},
		{"stats", "GET", "/api/stats", 200, func(c *models.APIRateLimitConfig) *models.APIRateLimitRule { return &c.Business.Stats }},
		{"YYDS", "GET", "/yyds/v1/domains", 200, func(c *models.APIRateLimitConfig) *models.APIRateLimitRule { return &c.Business.YYDS }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRateLimitFixture(t)
			if err := f.handler.DB.Create(&models.APIInterfaceSettings{ID: 1, YYDSCompatibilityEnabled: true}).Error; err != nil {
				t.Fatal(err)
			}
			config := unlimitedAPIConfig()
			rule := tc.rule(&config)
			*rule = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
			f.saveConfig(t, config)
			request := func(want int, retry string) {
				t.Helper()
				response := f.apiRequest(tc.method, tc.path, f.plain, "")
				if response.Code != want || response.Header().Get("Retry-After") != retry {
					t.Fatalf("status=%d retry=%q, want %d/%q: %s", response.Code, response.Header().Get("Retry-After"), want, retry, response.Body.String())
				}
				if want == 429 && !strings.Contains(response.Body.String(), "rate limit exceeded") {
					t.Fatal("rate error envelope changed")
				}
			}
			request(tc.status, "")
			request(429, "1000")
			f.assertUsage(t, f.key.ID, 1)
			rule.RequestsPerSecond, rule.Burst = 10, 2
			f.saveConfig(t, config)
			request(429, "1") // 调高参数不会立即补满已耗尽的桶。
			f.now = f.now.Add(100 * time.Millisecond)
			request(tc.status, "")
			f.assertUsage(t, f.key.ID, 2)
			rule.Enabled = false
			f.saveConfig(t, config)
			request(tc.status, "")
			request(tc.status, "")
			rule.Enabled = true
			f.saveConfig(t, config)
			request(429, "1")
			f.assertUsage(t, f.key.ID, 4)
		})
	}
}

func TestAPIRateLimitIPRejectionDoesNotSpendInstanceTokens(t *testing.T) {
	f := newRateLimitFixture(t)
	second, plain, err := f.handler.APIKeys.CreateFor(&f.owner.ID, "second", 1000, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	config := unlimitedAPIConfig()
	config.PreAuth.PerIP = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
	config.PreAuth.PerInstance = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 2}
	f.saveConfig(t, config)
	for _, request := range []struct {
		key, peer string
		status    int
	}{
		{f.plain, "192.0.2.1:5000", 200},
		{plain, "192.0.2.1:5001", 429},
		{plain, "192.0.2.2:5000", 200},
		{f.plain, "192.0.2.3:5000", 429},
	} {
		response := f.apiRequest("GET", "/api/mailboxes", request.key, request.peer)
		if response.Code != request.status {
			t.Fatalf("peer %s status=%d, want %d", request.peer, response.Code, request.status)
		}
	}
	f.assertUsage(t, f.key.ID, 1)
	f.assertUsage(t, second.ID, 1)
}

func TestAPIRateLimitIdentityAndRouteTemplateScope(t *testing.T) {
	f := newRateLimitFixture(t)
	second, plain, err := f.handler.APIKeys.CreateFor(&f.owner.ID, "second", 1000, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	config := unlimitedAPIConfig()
	config.Business.Mail = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
	f.saveConfig(t, config)
	for _, request := range []struct {
		method, path, key, peer string
		status                  int
	}{
		{"GET", "/api/mailboxes?page=1", f.plain, "192.0.2.1:5000", 200},
		{"GET", "/api/mailboxes?page=2", f.plain, "192.0.2.2:5000", 429},
		{"GET", "/api/mailboxes", plain, "192.0.2.1:5000", 200},
		{"GET", "/api/mailboxes/stats", f.plain, "", 200},
		{"GET", "/api/mailboxes/stats?limit=1", f.plain, "", 429},
		{"GET", "/api/email/100", f.plain, "", 404},
		{"GET", "/api/email/101", f.plain, "", 429},
		{"DELETE", "/api/email/100", f.plain, "", 404},
	} {
		response := f.apiRequest(request.method, request.path, request.key, request.peer)
		if response.Code != request.status {
			t.Fatalf("%s %s=%d, want %d: %s", request.method, request.path, response.Code, request.status, response.Body.String())
		}
	}
	f.assertUsage(t, f.key.ID, 4)
	f.assertUsage(t, second.ID, 1)
}

func TestAPIRateLimitIngressProtectsInvalidCredentials(t *testing.T) {
	for _, state := range []string{"invalid", "disabled key", "expired key", "disabled owner"} {
		t.Run(state, func(t *testing.T) {
			f := newRateLimitFixture(t)
			config := unlimitedAPIConfig()
			config.PreAuth.PerIP = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
			f.saveConfig(t, config)
			key, want := f.plain, 403
			switch state {
			case "invalid":
				key, want = "invalid-key", 401
			case "disabled key":
				if err := f.handler.DB.Model(f.key).Update("enabled", false).Error; err != nil {
					t.Fatal(err)
				}
			case "expired key":
				if err := f.handler.DB.Model(f.key).Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
					t.Fatal(err)
				}
			case "disabled owner":
				if err := f.handler.DB.Model(&f.owner).Update("enabled", false).Error; err != nil {
					t.Fatal(err)
				}
			}
			if response := f.apiRequest("GET", "/api/mailboxes", key, ""); response.Code != want {
				t.Fatalf("auth response=%d: %s", response.Code, response.Body.String())
			}
			if response := f.apiRequest("GET", "/api/mailboxes", key, ""); response.Code != 429 {
				t.Fatalf("repeated invalid attempt=%d", response.Code)
			}
			f.assertUsage(t, f.key.ID, 0)
		})
	}
}

func TestAPIRateLimitQuotaAndSessionProtectionRemainIndependent(t *testing.T) {
	f := newRateLimitFixture(t)
	f.saveConfig(t, unlimitedAPIConfig())
	if err := f.handler.DB.Model(f.key).Update("daily_limit", 1).Error; err != nil {
		t.Fatal(err)
	}
	if response := f.apiRequest("GET", "/api/mailboxes", f.plain, ""); response.Code != 200 {
		t.Fatalf("first request=%d", response.Code)
	}
	response := f.apiRequest("GET", "/api/mailboxes", f.plain, "")
	if response.Code != 429 || response.Header().Get("Retry-After") != "" || strings.Contains(response.Body.String(), "rate limit exceeded") {
		t.Fatalf("quota rejection changed: %d %s", response.Code, response.Body.String())
	}
	f.assertUsage(t, f.key.ID, 1)
	for i := 0; i < 21; i++ {
		response := perform(f.router, "GET", "/api/mailboxes", nil, f.adminHeaders)
		want := 200
		if i == 20 {
			want = 429
		}
		if response.Code != want {
			t.Fatalf("session request %d=%d, want %d", i, response.Code, want)
		}
	}
}

func TestAPIRateLimitUsesTrustedProxyBoundary(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(map[bool]string{false: "untrusted", true: "trusted"}[trusted], func(t *testing.T) {
			f := newRateLimitFixture(t)
			if trusted {
				f.handler.Config.TrustedProxies = []string{"127.0.0.1"}
				f.router = NewRouter(f.handler)
			}
			config := unlimitedAPIConfig()
			config.PreAuth.PerIP = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
			f.saveConfig(t, config)
			for i, forwarded := range []string{"192.0.2.1", "192.0.2.2"} {
				request := httptest.NewRequestWithContext(context.Background(), "GET", "/api/mailboxes", nil)
				request.RemoteAddr = "127.0.0.1:5000"
				request.Header.Set("X-Forwarded-For", forwarded)
				request.Header.Set("X-API-Key", f.plain)
				response := httptest.NewRecorder()
				f.router.ServeHTTP(response, request)
				want := 200
				if i == 1 && !trusted {
					want = 429
				}
				if response.Code != want {
					t.Fatalf("forwarded %s=%d, want %d", forwarded, response.Code, want)
				}
			}
		})
	}
}

func TestAPIRateLimitCORSExposesRetryAndAllowsPUT(t *testing.T) {
	f := newRateLimitFixture(t)
	f.handler.Config.AllowedOrigin = "https://client.example.test"
	headers := map[string]string{"Origin": "https://client.example.test", "Access-Control-Request-Method": "PUT"}
	preflight := perform(f.router, "OPTIONS", "/api/admin/rate-limit-settings", nil, headers)
	if preflight.Code != 204 || !strings.Contains(preflight.Header().Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatalf("PUT preflight rejected: %d %v", preflight.Code, preflight.Header())
	}
	config := unlimitedAPIConfig()
	config.PreAuth.PerIP = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
	f.saveConfig(t, config)
	headers["X-API-Key"] = f.plain
	perform(f.router, "GET", "/api/mailboxes", nil, headers)
	response := perform(f.router, "GET", "/api/mailboxes", nil, headers)
	if response.Code != 429 || response.Header().Get("Access-Control-Expose-Headers") != "Retry-After" || response.Header().Get("Retry-After") != "1000" {
		t.Fatalf("browser cannot read retry delay: %d %v", response.Code, response.Header())
	}
}

func TestAPIRateLimitCatalogCoversAutomationRoutes(t *testing.T) {
	f := newRateLimitFixture(t)
	for _, operation := range apispec.AutomationOperations() {
		key := operation.Method + ":" + operation.DisplayPath()
		if _, ok := f.handler.apiRateLimitProfiles[key]; !ok {
			t.Errorf("OpenAPI automation operation has no rate policy: %s", key)
		}
	}
	if f.handler.apiRateLimitProfiles["GET:/api/mailboxes/stats"] != apiratelimit.Mail {
		t.Error("mailbox stats missing mail policy")
	}
	for _, route := range f.handler.yydsAutomationRoutes() {
		if f.handler.apiRateLimitProfiles[route.method+":/yyds/v1"+route.path] != apiratelimit.YYDS {
			t.Errorf("YYDS route missing policy: %s %s", route.method, route.path)
		}
	}
	for route := range f.handler.apiRateLimitProfiles {
		path := strings.SplitN(route, ":", 2)[1]
		if isSessionOnlyWebPath(path) || isSessionOnlyManagementPath(path) {
			t.Errorf("session-only route is configurable: %s", route)
		}
	}
}
