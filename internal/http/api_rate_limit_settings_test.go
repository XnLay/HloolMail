package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gptmail/internal/apiratelimit"
	"gptmail/internal/auth"
	"gptmail/internal/config"
	"gptmail/internal/domain"
	"gptmail/internal/models"
	"gptmail/internal/ratelimit"
)

type rateLimitFixture struct {
	handler      *Handler
	router       http.Handler
	owner        models.User
	key          *models.APIKey
	plain        string
	adminHeaders map[string]string
	now          time.Time
}

func newRateLimitFixture(t *testing.T) *rateLimitFixture {
	t.Helper()
	database := httpTestDB(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	hash, err := auth.HashSecret("password123")
	if err != nil {
		t.Fatal(err)
	}
	f := &rateLimitFixture{
		owner: models.User{Email: "rate-admin@example.test", PasswordHash: hash, Role: models.UserRoleAdmin, Enabled: true, EmailVerified: true},
		now:   time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
	}
	if err := database.Create(&f.owner).Error; err != nil {
		t.Fatal(err)
	}
	f.handler = &Handler{
		DB:                database,
		Config:            config.Config{FrontendDist: t.TempDir(), PublicBaseURL: "http://example.com", SessionSecret: "rate-limit-test-session"},
		APIKeys:           auth.APIKeyService{DB: database},
		Sessions:          auth.NewSessionService("rate-limit-test-session", database),
		Resolver:          domain.Resolver{DB: database},
		RateLimiter:       ratelimit.NewWithOptions(ratelimit.Options{Now: func() time.Time { return f.now }}),
		APIKeyRateLimiter: ratelimit.NewWithOptions(ratelimit.Options{Now: func() time.Time { return f.now }}),
	}
	f.key, f.plain, err = f.handler.APIKeys.CreateFor(&f.owner.ID, "rate-limit-test", 1000, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.router = NewRouter(f.handler)
	f.adminHeaders = cookieHeaders(loginShareTestUser(t, f.router, f.owner.Email).Result().Cookies())
	f.adminHeaders["Origin"] = "http://example.com"
	return f
}

func (f *rateLimitFixture) saveConfig(t *testing.T, config models.APIRateLimitConfig) {
	t.Helper()
	_, err := f.handler.APIRateLimits.Save(context.Background(), f.handler.APIRateLimits.Snapshot().Revision, config,
		newAuditLog("api_rate_limit_settings.update", f.owner.Email, "1", ""))
	if err != nil {
		t.Fatal(err)
	}
}

func (f *rateLimitFixture) apiRequest(method, path, key, peer string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	request.Header.Set("X-API-Key", key)
	if peer != "" {
		request.RemoteAddr = peer
	}
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

func (f *rateLimitFixture) assertUsage(t *testing.T, keyID uint, want int64) {
	t.Helper()
	var key models.APIKey
	if err := f.handler.DB.First(&key, keyID).Error; err != nil {
		t.Fatal(err)
	}
	var logs int64
	if err := f.handler.DB.Model(&models.APIUsageLog{}).Where("api_key_id = ?", keyID).Count(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if key.UsedToday != want || key.TotalUsed != want || logs != want {
		t.Fatalf("usage: daily=%d total=%d logs=%d, want %d", key.UsedToday, key.TotalUsed, logs, want)
	}
}

func settingsPayload(revision int64, config models.APIRateLimitConfig) map[string]any {
	return map[string]any{"revision": revision, "config": config}
}

func decodeRateSettings(t *testing.T, response *httptest.ResponseRecorder) apiRateLimitSettingsResponse {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("settings status=%d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data apiRateLimitSettingsResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func TestAPIRateLimitSettingsManagementContract(t *testing.T) {
	f := newRateLimitFixture(t)
	path := "/api/admin/rate-limit-settings"
	initial := decodeRateSettings(t, perform(f.router, http.MethodGet, path, nil, f.adminHeaders))
	if initial.Revision != 1 || initial.AppliedRevision != 1 || initial.Config != apiratelimit.DefaultConfig() || initial.Defaults != initial.Config {
		t.Fatalf("unexpected defaults: %+v", initial)
	}
	if initial.Constraints.MinRequestsPerSecond != apiratelimit.MinRequestsPerSecond || initial.Constraints.MaxBurst != apiratelimit.MaxBurst {
		t.Fatal("response constraints differ from domain validation")
	}
	changed := initial.Config
	changed.PreAuth.PerIP.Enabled = false
	changed.Business.Mail.RequestsPerSecond = 100
	saved := decodeRateSettings(t, perform(f.router, http.MethodPut, path, settingsPayload(1, changed), f.adminHeaders))
	if saved.Revision != 2 || saved.AppliedRevision != 2 || saved.Config != changed || saved.UpdatedBy != f.owner.Email {
		t.Fatalf("saved settings not applied: %+v", saved)
	}
	unchanged := decodeRateSettings(t, perform(f.router, http.MethodPut, path, settingsPayload(2, changed), f.adminHeaders))
	if unchanged.Revision != 2 || !unchanged.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatal("no-op save changed metadata")
	}
	stale := perform(f.router, http.MethodPut, path, settingsPayload(1, initial.Config), f.adminHeaders)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update=%d: %s", stale.Code, stale.Body.String())
	}
	loaded := decodeRateSettings(t, perform(f.router, http.MethodGet, path, nil, f.adminHeaders))
	if loaded.Config != changed || loaded.Revision != 2 {
		t.Fatal("conflict changed stored configuration")
	}
	var logs []models.AuditLog
	if err := f.handler.DB.Where("action = ?", "api_rate_limit_settings.update").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Actor != f.owner.Email || logs[0].Category != auditCategorySecurity || logs[0].TargetType != "api_rate_limit_settings" {
		t.Fatalf("unexpected settings audits: %+v", logs)
	}
	f.assertUsage(t, f.key.ID, 0)
}

func TestAPIRateLimitSettingsRequireAdminAndSameOrigin(t *testing.T) {
	f := newRateLimitFixture(t)
	f.handler.Config.AllowLegacyAdminToken = true
	f.handler.Config.AdminToken = "legacy-token"
	user := createShareTestUser(t, f.handler.DB, "rate-user@example.test")
	if err := f.handler.DB.Model(&user).Update("email_verified", true).Error; err != nil {
		t.Fatal(err)
	}
	userHeaders := cookieHeaders(loginShareTestUser(t, f.router, user.Email).Result().Cookies())
	for name, headers := range map[string]map[string]string{
		"anonymous":        nil,
		"ordinary session": userHeaders,
		"API key":          {"X-API-Key": f.plain},
		"legacy token":     {"X-Admin-Token": "legacy-token", "Authorization": "Bearer legacy-token"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				response := perform(f.router, method, "/api/admin/rate-limit-settings", settingsPayload(1, apiratelimit.DefaultConfig()), headers)
				if response.Code != http.StatusForbidden {
					t.Fatalf("%s=%d: %s", method, response.Code, response.Body.String())
				}
			}
		})
	}
	f.adminHeaders["Origin"] = "https://other.example.test"
	response := perform(f.router, http.MethodPut, "/api/admin/rate-limit-settings", settingsPayload(1, apiratelimit.DefaultConfig()), f.adminHeaders)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-site write=%d: %s", response.Code, response.Body.String())
	}
	f.adminHeaders["X-Admin-Token"] = "legacy-token"
	response = perform(f.router, http.MethodPut, "/api/admin/rate-limit-settings", settingsPayload(1, apiratelimit.DefaultConfig()), f.adminHeaders)
	if response.Code != http.StatusForbidden {
		t.Fatalf("legacy token bypassed same-origin settings write: %d", response.Code)
	}
	delete(f.adminHeaders, "X-Admin-Token")
	f.adminHeaders["Origin"] = "http://example.com"
	minimal := apiratelimit.DefaultConfig()
	minimal.PreAuth.PerIP = models.APIRateLimitRule{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
	minimal.PreAuth.PerInstance = minimal.PreAuth.PerIP
	minimal.Business.Mail = minimal.PreAuth.PerIP
	f.saveConfig(t, minimal)
	f.apiRequest(http.MethodGet, "/api/mailboxes", f.plain, "")
	if response := f.apiRequest(http.MethodGet, "/api/mailboxes", f.plain, ""); response.Code != http.StatusTooManyRequests {
		t.Fatalf("minimal API rate=%d", response.Code)
	}
	loaded := decodeRateSettings(t, perform(f.router, http.MethodGet, "/api/admin/rate-limit-settings", nil, f.adminHeaders))
	decodeRateSettings(t, perform(f.router, http.MethodPut, "/api/admin/rate-limit-settings", settingsPayload(loaded.Revision, loaded.Defaults), f.adminHeaders))
}

func TestAPIRateLimitSettingsRejectIncompleteOrInvalidJSON(t *testing.T) {
	f := newRateLimitFixture(t)
	validBytes, err := json.Marshal(settingsPayload(1, apiratelimit.DefaultConfig()))
	if err != nil {
		t.Fatal(err)
	}
	valid := string(validBytes)
	for name, body := range map[string]string{
		"empty":            "",
		"null":             "null",
		"missing config":   `{"revision":1}`,
		"missing revision": strings.Replace(valid, `,"revision":1`, "", 1),
		"missing enabled":  strings.Replace(valid, `"enabled":true,`, "", 1),
		"null enabled":     strings.Replace(valid, `"enabled":true`, `"enabled":null`, 1),
		"missing rate":     strings.Replace(valid, `"requests_per_second":5,`, "", 1),
		"missing burst":    strings.Replace(valid, `,"burst":20`, "", 1),
		"unknown field":    strings.Replace(valid, `"enabled":true`, `"extra":true,"enabled":true`, 1),
		"zero rate":        strings.Replace(valid, `"requests_per_second":5`, `"requests_per_second":0`, 1),
		"negative rate":    strings.Replace(valid, `"requests_per_second":5`, `"requests_per_second":-1`, 1),
		"oversized rate":   strings.Replace(valid, `"requests_per_second":5`, `"requests_per_second":100001`, 1),
		"string rate":      strings.Replace(valid, `"requests_per_second":5`, `"requests_per_second":"5"`, 1),
		"overflow":         strings.Replace(valid, `"requests_per_second":5`, `"requests_per_second":1e999`, 1),
		"fractional burst": strings.Replace(valid, `"burst":20`, `"burst":1.5`, 1),
		"oversized burst":  strings.Replace(valid, `"burst":20`, `"burst":100001`, 1),
		"zero burst":       strings.Replace(valid, `"burst":20`, `"burst":0`, 1),
		"trailing JSON":    valid + "{}",
		"oversized body":   strings.Repeat(" ", 17<<10) + valid,
	} {
		t.Run(name, func(t *testing.T) {
			f.now = f.now.Add(time.Minute)
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/admin/rate-limit-settings", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			for key, value := range f.adminHeaders {
				request.Header.Set(key, value)
			}
			response := httptest.NewRecorder()
			f.router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid input=%d: %s", response.Code, response.Body.String())
			}
		})
	}
	if f.handler.APIRateLimits.Snapshot().Revision != 1 {
		t.Fatal("invalid input changed live settings")
	}
}
