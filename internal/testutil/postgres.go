package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"gptmail/internal/config"
	"gptmail/internal/db"

	"gorm.io/gorm"
)

// Postgres 仅在显式指定测试库时运行，每个用例使用独立 schema 并在结束后回收。
func Postgres(t testing.TB) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HLOOLMAIL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set HLOOLMAIL_TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("HLOOLMAIL_TEST_POSTGRES_DSN must be a PostgreSQL URL")
	}
	control, err := db.Open(config.Config{DatabaseDriver: "postgres", DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	controlSQL, err := control.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = controlSQL.Close() })
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "rate_limit_test_" + hex.EncodeToString(random[:])
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 标识符完全由固定前缀和随机十六进制组成，不接收外部 SQL 片段。
	if err := control.WithContext(ctx).Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := control.WithContext(ctx).Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Errorf("cleanup PostgreSQL test schema: %v", err)
		}
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := db.Open(config.Config{DatabaseDriver: "postgres", DatabaseURL: parsed.String()})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return database
}
