package apiratelimit

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gptmail/internal/config"
	appdb "gptmail/internal/db"
	"gptmail/internal/models"

	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"
)

func TestConcurrentSQLiteConnectionsHaveOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	services := make([]*Service, 2)
	for i := range services {
		database, err := appdb.Open(config.Config{DatabaseDriver: "sqlite", DatabaseURL: path})
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
		services[i] = newTestService(t, database)
		// 放大两个进程都已读到旧版本、尚未执行写入的竞争窗口。
		if err := database.Callback().Query().After("gorm:query").Register("test:slow-settings-read", func(tx *gorm.DB) {
			if tx.Statement.Table == "api_rate_limit_settings" {
				time.Sleep(10 * time.Millisecond)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, len(services))
	for i, service := range services {
		go func() {
			<-start
			config := DefaultConfig()
			config.Business.Mail.Burst = 100 + i
			_, err := service.Save(context.Background(), 1, config, settingsAudit())
			results <- err
		}()
	}
	close(start)
	wins, conflicts := 0, 0
	for range services {
		err := <-results
		if err == nil {
			wins++
		} else if errors.Is(err, appdb.ErrAPIRateLimitConflict) {
			conflicts++
		} else {
			t.Errorf("concurrent SQLite save: %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d, want 1 each", wins, conflicts)
	}
}

func TestConcurrentSnapshotPublicationsKeepHighestRevision(t *testing.T) {
	service := newTestService(t, serviceTestDB(t))
	initial := service.Snapshot()
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Go(func() {
			for i := 1; i <= 100; i++ {
				settings := initial
				settings.Revision = int64(worker*100 + i + 1)
				settings.Config.Business.Mail.Burst = int(settings.Revision)
				if err := service.apply(settings); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	if got := service.Snapshot(); got.Revision != 801 || got.Config.Business.Mail.Burst != 801 {
		t.Fatalf("publication regressed: %+v", got)
	}
	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range metrics {
		if metric.GetName() == "api_rate_limit_applied_revision" {
			if metric.Metric[0].GetGauge().GetValue() != 801 {
				t.Fatal("revision metric disagrees with the live snapshot")
			}
			return
		}
	}
	t.Fatal("applied revision metric missing")
}

func TestDatabaseOutagePreservesRunningConfigButRejectsStartup(t *testing.T) {
	database := serviceTestDB(t)
	service := newTestService(t, database)
	initial := service.Snapshot()
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.Refresh(context.Background()); err == nil {
		t.Fatal("refresh succeeded after database disconnect")
	}
	if service.Snapshot() != initial {
		t.Fatal("database outage changed the running policy")
	}
	if _, err := New(context.Background(), database); err == nil {
		t.Fatal("startup accepted an unavailable database")
	}
}
