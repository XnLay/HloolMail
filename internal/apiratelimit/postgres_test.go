package apiratelimit

import (
	"context"
	"sync"
	"testing"

	appdb "gptmail/internal/db"
	"gptmail/internal/models"
	"gptmail/internal/testutil"
)

func TestPostgresSettingsIntegration(t *testing.T) {
	for name, exercise := range map[string]func(*testing.T, *Service){
		"persistence":      func(t *testing.T, service *Service) { exerciseSettingsPersistence(t, service.database) },
		"concurrent saves": func(t *testing.T, service *Service) { exerciseConcurrentSettings(t, service.database) },
		"audit rollback":   func(t *testing.T, service *Service) { exerciseSettingsRollback(t, service.database) },
	} {
		t.Run(name, func(t *testing.T) {
			database := testutil.Postgres(t)
			if err := database.AutoMigrate(&models.APIRateLimitSettings{}, &models.AuditLog{}); err != nil {
				t.Fatal(err)
			}
			exercise(t, newTestService(t, database))
		})
	}
}

func TestPostgresSettingsConcurrentInitializationAndProductionMigration(t *testing.T) {
	database := testutil.Postgres(t)
	if err := appdb.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < cap(results); i++ {
		workers.Go(func() { _, err := New(context.Background(), database); results <- err })
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := database.Model(&models.APIRateLimitSettings{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent startup created %d settings rows", count)
	}
	settings, err := appdb.ReadAPIRateLimitSettings(database)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Revision != 1 || settings.Config != DefaultConfig() {
		t.Fatal("startup defaults differ from canonical configuration")
	}
}
