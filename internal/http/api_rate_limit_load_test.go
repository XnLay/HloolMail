package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"gptmail/internal/apiratelimit"
	"gptmail/internal/auth"
	"gptmail/internal/config"
	appdb "gptmail/internal/db"
	"gptmail/internal/domain"
	"gptmail/internal/models"
	"gptmail/internal/ratelimit"
	"gptmail/internal/testutil"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type rateLimitLoadResult struct {
	Scenario           string  `json:"scenario"`
	Concurrency        int     `json:"concurrency"`
	AuthenticationPath string  `json:"authentication_path"`
	StoredHashCost     int     `json:"stored_bcrypt_cost"`
	Seconds            float64 `json:"seconds"`
	Successes          int     `json:"successes"`
	RateLimited        int     `json:"rate_limited"`
	Unexpected         int     `json:"unexpected"`
	SuccessRPS         float64 `json:"success_rps"`
	RejectedRPS        float64 `json:"rejected_rps"`
	SuccessP95MS       float64 `json:"success_p95_ms"`
	SuccessP99MS       float64 `json:"success_p99_ms"`
	PoolMaxConnections int     `json:"pool_max_connections"`
	PoolWaits          int64   `json:"pool_waits"`
	PoolWaitMS         float64 `json:"pool_wait_ms"`
	SettingsQueries    int64   `json:"settings_queries"`
}

// 独立压测使用真实 PostgreSQL 与 HTTP；密钥按 key_value 索引认证，不执行逐请求 bcrypt 比较。
func TestPostgresAPIRateLimitLoad(t *testing.T) {
	if os.Getenv("HLOOLMAIL_RUN_RATE_LIMIT_LOAD") != "1" {
		t.Skip("set HLOOLMAIL_RUN_RATE_LIMIT_LOAD=1 to run the isolated load test")
	}
	database := testutil.Postgres(t)
	if err := appdb.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	restore := auth.SetHashCostForTesting(12)
	defer restore()
	owner := models.User{Email: "load@example.test", Role: models.UserRoleUser, Enabled: true, EmailVerified: true}
	if err := database.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	keys := auth.APIKeyService{DB: database}
	key, plain, err := keys.CreateFor(&owner.ID, "isolated load test", 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if key.KeyValue != plain {
		t.Fatal("load test requires the key_value authentication path")
	}
	cost, err := bcrypt.Cost([]byte(key.KeyHash))
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{
		DB: database, Config: config.Config{FrontendDist: t.TempDir()}, APIKeys: keys,
		Sessions: auth.NewSessionService("load-test-session", database),
		Resolver: domain.Resolver{DB: database}, RateLimiter: ratelimit.NewWithOptions(ratelimit.Options{}),
		APIKeyRateLimiter: ratelimit.NewWithOptions(ratelimit.Options{}),
	}
	server := httptest.NewServer(NewRouter(handler))
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Second
	var settingsQueries atomic.Int64
	if err := database.Callback().Query().Before("gorm:query").Register("test:rate-settings-queries", func(tx *gorm.DB) {
		if tx.Statement.Table == "api_rate_limit_settings" {
			settingsQueries.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"defaults", "raised"} {
		policy := apiratelimit.DefaultConfig()
		if name == "raised" {
			for _, rule := range []*models.APIRateLimitRule{&policy.PreAuth.PerIP, &policy.PreAuth.PerInstance, &policy.Business.Mail} {
				rule.RequestsPerSecond, rule.Burst = 1000, 1000
			}
		}
		if _, err := handler.APIRateLimits.Save(context.Background(), handler.APIRateLimits.Snapshot().Revision, policy, newAuditLog("api_rate_limit_settings.update", "load-test", "1", "")); err != nil {
			t.Fatal(err)
		}
		settingsQueries.Store(0)
		before := sqlDB.Stats()
		result := runRateLimitLoad(client, server.URL+"/api/mailboxes", plain, 8, 6*time.Second)
		after := sqlDB.Stats()
		result.Scenario, result.StoredHashCost = name, cost
		result.AuthenticationPath = "key_value_index"
		result.PoolMaxConnections, result.PoolWaits = after.MaxOpenConnections, after.WaitCount-before.WaitCount
		result.PoolWaitMS = float64(after.WaitDuration-before.WaitDuration) / float64(time.Millisecond)
		result.SettingsQueries = settingsQueries.Load()
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("RATE_LIMIT_LOAD %s", encoded)
		if result.Successes == 0 || result.Unexpected != 0 || result.SettingsQueries != 0 {
			t.Fatalf("load test failed: %s", encoded)
		}
	}
}

func runRateLimitLoad(client *http.Client, endpoint, key string, concurrency int, duration time.Duration) rateLimitLoadResult {
	type workerResult struct {
		successLatency       []time.Duration
		rejected, unexpected int
	}
	results := make(chan workerResult, concurrency)
	start := time.Now()
	deadline := start.Add(duration)
	for i := 0; i < concurrency; i++ {
		go func() {
			result := workerResult{}
			for time.Now().Before(deadline) {
				request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
				if err != nil {
					result.unexpected++
					break
				}
				request.Header.Set("X-API-Key", key)
				began := time.Now()
				response, err := client.Do(request)
				if err != nil {
					result.unexpected++
					continue
				}
				_, readErr := io.Copy(io.Discard, response.Body)
				closeErr := response.Body.Close()
				if readErr != nil || closeErr != nil {
					result.unexpected++
					continue
				}
				switch response.StatusCode {
				case http.StatusOK:
					result.successLatency = append(result.successLatency, time.Since(began))
				case http.StatusTooManyRequests:
					result.rejected++
				default:
					result.unexpected++
				}
			}
			results <- result
		}()
	}
	result := rateLimitLoadResult{Concurrency: concurrency}
	var latencies []time.Duration
	for i := 0; i < concurrency; i++ {
		worker := <-results
		latencies = append(latencies, worker.successLatency...)
		result.RateLimited += worker.rejected
		result.Unexpected += worker.unexpected
	}
	result.Seconds = time.Since(start).Seconds()
	result.Successes = len(latencies)
	result.SuccessRPS = float64(result.Successes) / result.Seconds
	result.RejectedRPS = float64(result.RateLimited) / result.Seconds
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	if len(latencies) > 0 {
		result.SuccessP95MS = float64(latencies[(len(latencies)-1)*95/100]) / float64(time.Millisecond)
		result.SuccessP99MS = float64(latencies[(len(latencies)-1)*99/100]) / float64(time.Millisecond)
	}
	return result
}
