package apiratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"gptmail/internal/config"
	appdb "gptmail/internal/db"
	"gptmail/internal/models"

	"gorm.io/gorm"
)

func serviceTestDB(t testing.TB) *gorm.DB {
	t.Helper()
	database, err := appdb.Open(config.Config{DatabaseDriver: "sqlite", DatabaseURL: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(&models.APIRateLimitSettings{}, &models.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	return database
}

func newTestService(t testing.TB, database *gorm.DB) *Service {
	t.Helper()
	service, err := New(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func settingsAudit() models.AuditLog {
	return models.AuditLog{Category: "security", Severity: "info", Action: "api_rate_limit_settings.update", Actor: "admin@example.test", Target: "1", TargetType: "api_rate_limit_settings", TargetID: "1"}
}

func TestSettingsPersistAndCompareVersions(t *testing.T) {
	exerciseSettingsPersistence(t, serviceTestDB(t))
}

func exerciseSettingsPersistence(t *testing.T, database *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	service := newTestService(t, database)
	initial := service.Snapshot()
	if initial.Revision != 1 || initial.Config != DefaultConfig() {
		t.Fatalf("unexpected initial settings: %+v", initial)
	}
	changed := initial.Config
	changed.PreAuth.PerIP.Enabled = false
	changed.Business.Mail.RequestsPerSecond = 100
	saved, err := service.Save(ctx, initial.Revision, changed, settingsAudit())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 2 || service.Snapshot().Config != changed {
		t.Fatal("saved configuration was not applied")
	}
	restarted := newTestService(t, database)
	if restarted.Snapshot().Config != changed {
		t.Fatal("restart overwrote saved settings or lost false values")
	}
	if _, err := service.Save(ctx, 1, DefaultConfig(), settingsAudit()); !errors.Is(err, appdb.ErrAPIRateLimitConflict) {
		t.Fatalf("stale save = %v", err)
	}
	unchanged, err := service.Save(ctx, 2, changed, settingsAudit())
	if err != nil || unchanged.Revision != 2 || !unchanged.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatalf("no-op save = %+v, %v", unchanged, err)
	}
	var logs []models.AuditLog
	if err := database.Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("audit count = %d, want 1", len(logs))
	}
	var metadata struct {
		Before, After    models.APIRateLimitConfig
		PreviousRevision int64 `json:"previous_revision"`
		Revision         int64
	}
	if err := json.Unmarshal([]byte(logs[0].Metadata), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Before != initial.Config || metadata.After != changed || metadata.PreviousRevision != 1 || metadata.Revision != 2 {
		t.Fatal("audit did not record the committed change")
	}
	if err := service.apply(initial); err != nil {
		t.Fatal(err)
	}
	if service.Snapshot().Revision != 2 {
		t.Fatal("late read reverted configuration")
	}
	copy := service.Snapshot()
	copy.Config.Business.Mail.Enabled = false
	if !service.Snapshot().Config.Business.Mail.Enabled {
		t.Fatal("caller mutated the live snapshot")
	}
}

func TestSettingsAndAuditRollBackTogether(t *testing.T) {
	exerciseSettingsRollback(t, serviceTestDB(t))
}

func exerciseSettingsRollback(t *testing.T, database *gorm.DB) {
	t.Helper()
	service := newTestService(t, database)
	if err := database.Callback().Create().Before("gorm:create").Register("test:reject-rate-audit", func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(errors.New("injected audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	changed := DefaultConfig()
	changed.Business.Mail.Burst = 100
	if _, err := service.Save(context.Background(), 1, changed, settingsAudit()); err == nil {
		t.Fatal("expected audit failure")
	}
	stored, err := appdb.ReadAPIRateLimitSettings(database)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != 1 || stored.Config != DefaultConfig() || service.Snapshot().Config != DefaultConfig() {
		t.Fatal("failed audit left settings partially applied")
	}
}

func TestConcurrentSettingsUpdatesHaveOneWinner(t *testing.T) {
	exerciseConcurrentSettings(t, serviceTestDB(t))
}

func exerciseConcurrentSettings(t *testing.T, database *gorm.DB) {
	t.Helper()
	services := []*Service{newTestService(t, database), newTestService(t, database)}
	results := make(chan error, len(services))
	var workers sync.WaitGroup
	for i, service := range services {
		workers.Go(func() {
			changed := DefaultConfig()
			changed.Business.Mail.Burst = 100 + i
			_, err := service.Save(context.Background(), 1, changed, settingsAudit())
			results <- err
		})
	}
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, appdb.ErrAPIRateLimitConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	for _, service := range services {
		if err := service.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if services[0].Snapshot().Config != services[1].Snapshot().Config {
		t.Fatal("instances did not converge")
	}
}

func TestRefreshKeepsLastValidConfigAndSnapshotDoesNotQuery(t *testing.T) {
	database := serviceTestDB(t)
	service := newTestService(t, database)
	queries := 0
	if err := database.Callback().Query().Before("gorm:query").Register("test:count-settings-queries", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		_ = service.Snapshot()
	}
	if queries != 0 {
		t.Fatal("snapshot queried the database")
	}
	if err := database.Model(&models.APIRateLimitSettings{}).Where("id = 1").Updates(map[string]any{"revision": 2, "config_business_mail_requests_per_second": -1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Refresh(context.Background()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid stored settings = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Refresh(ctx); err == nil {
		t.Fatal("canceled refresh succeeded")
	}
	if service.Snapshot().Revision != 1 || service.Snapshot().Config != DefaultConfig() {
		t.Fatal("refresh failure changed live limits")
	}
	if _, err := New(context.Background(), database); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("startup accepted invalid settings: %v", err)
	}
}

func TestSettingsPollAndStopWithContext(t *testing.T) {
	database := serviceTestDB(t)
	first, second := newTestService(t, database), newTestService(t, database)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { second.Run(ctx); close(done) }()
	changed := DefaultConfig()
	changed.Business.Stats.Burst = 100
	if _, err := first.Save(ctx, 1, changed, settingsAudit()); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(3 * SyncInterval)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for second.Snapshot().Revision != 2 {
		select {
		case <-deadline.C:
			t.Fatal("settings did not synchronize")
		case <-poll.C:
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("synchronizer did not stop")
	}
}

func TestValidateRejectsInvalidParametersEvenWhenDisabled(t *testing.T) {
	for _, rps := range []float64{0, -1, 0.0001, 100001, math.NaN(), math.Inf(1)} {
		config := DefaultConfig()
		config.PreAuth.PerIP.Enabled = false
		config.PreAuth.PerIP.RequestsPerSecond = rps
		if err := Validate(config); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("rate %v accepted: %v", rps, err)
		}
	}
	for _, burst := range []int{0, -1, MaxBurst + 1} {
		config := DefaultConfig()
		config.Business.YYDS.Burst = burst
		if err := Validate(config); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("burst %d accepted: %v", burst, err)
		}
	}
	config := DefaultConfig()
	config.Business.Mail.RequestsPerSecond = MinRequestsPerSecond
	config.Business.Mail.Burst = 1
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
}
