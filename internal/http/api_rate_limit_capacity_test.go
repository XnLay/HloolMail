package httpapi

import (
	"fmt"
	"testing"
	"time"

	"gptmail/internal/models"
	"gptmail/internal/ratelimit"
)

func TestAPIRateLimitSaturatedInstanceDoesNotAllocateSourceBuckets(t *testing.T) {
	f := newRateLimitFixture(t)
	config := unlimitedAPIConfig()
	config.PreAuth.PerIP = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
	config.PreAuth.PerInstance = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
	f.saveConfig(t, config)
	if response := f.apiRequest("GET", "/api/mailboxes", f.plain, "192.0.2.1:5000"); response.Code != 200 {
		t.Fatalf("initial request: %d", response.Code)
	}
	before := f.handler.APIKeyRateLimiter.Len()
	for i := 2; i < 20; i++ {
		response := f.apiRequest("GET", "/api/mailboxes", "invalid-key", fmt.Sprintf("192.0.2.%d:5000", i))
		if response.Code != 429 || response.Header().Get("Retry-After") != "1000" {
			t.Fatalf("saturated request: status=%d retry=%q", response.Code, response.Header().Get("Retry-After"))
		}
	}
	if after := f.handler.APIKeyRateLimiter.Len(); after != before {
		t.Fatalf("saturated instance allocated source buckets: before=%d after=%d", before, after)
	}
	f.assertUsage(t, f.key.ID, 1)
}

func TestAPIRateLimitCapacityDoesNotBlockAdministratorRecovery(t *testing.T) {
	f := newRateLimitFixture(t)
	f.handler.APIKeyRateLimiter = ratelimit.NewWithOptions(ratelimit.Options{MaxEntries: 2, Now: func() time.Time { return f.now }})
	config := unlimitedAPIConfig()
	config.PreAuth.PerIP = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
	f.saveConfig(t, config)
	for i := 1; i <= 2; i++ {
		if response := f.apiRequest("GET", "/api/mailboxes", f.plain, fmt.Sprintf("192.0.2.%d:5000", i)); response.Code != 200 {
			t.Fatalf("filling API capacity: status=%d", response.Code)
		}
	}
	if response := f.apiRequest("GET", "/api/mailboxes", f.plain, "192.0.2.3:5000"); response.Code != 429 {
		t.Fatalf("API capacity overflow: status=%d", response.Code)
	}
	path := "/api/admin/rate-limit-settings"
	loaded := decodeRateSettings(t, perform(f.router, "GET", path, nil, f.adminHeaders))
	config.PreAuth.PerIP.Enabled = false
	saved := decodeRateSettings(t, perform(f.router, "PUT", path, settingsPayload(loaded.Revision, config), f.adminHeaders))
	if saved.Revision != loaded.Revision+1 {
		t.Fatal("administrator could not recover the saturated API policy")
	}
	if response := f.apiRequest("GET", "/api/mailboxes", f.plain, "192.0.2.3:5000"); response.Code != 200 {
		t.Fatalf("recovered API request: status=%d", response.Code)
	}
	f.assertUsage(t, f.key.ID, 3)
}
