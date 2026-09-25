package apiratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"gptmail/internal/db"
	"gptmail/internal/models"
	"gptmail/internal/observability"

	"gorm.io/gorm"
)

const SyncInterval = 5 * time.Second

type Service struct {
	database  *gorm.DB
	publishMu sync.Mutex
	current   atomic.Pointer[models.APIRateLimitSettings]
}

func New(ctx context.Context, database *gorm.DB) (*Service, error) {
	settings, err := db.EnsureAPIRateLimitSettings(database.WithContext(ctx), DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("initialize API rate limits: %w", err)
	}
	service := &Service{database: database}
	if err := service.apply(settings); err != nil {
		return nil, err
	}
	return service, nil
}

// Snapshot 返回值拷贝，调用方不能修改已发布的配置，热路径不读取数据库。
func (s *Service) Snapshot() models.APIRateLimitSettings {
	return *s.current.Load()
}

func (s *Service) Refresh(ctx context.Context) error {
	settings, err := db.ReadAPIRateLimitSettings(s.database.WithContext(ctx))
	if err == nil {
		err = s.apply(settings)
	}
	observability.ObserveAPIRateLimitSync(err == nil)
	return err
}

func (s *Service) Save(ctx context.Context, revision int64, config models.APIRateLimitConfig, audit models.AuditLog) (models.APIRateLimitSettings, error) {
	if err := Validate(config); err != nil {
		return models.APIRateLimitSettings{}, err
	}
	settings, err := db.SaveAPIRateLimitSettings(s.database.WithContext(ctx), revision, config, audit)
	if err != nil {
		return models.APIRateLimitSettings{}, err
	}
	if err := s.apply(settings); err != nil {
		return models.APIRateLimitSettings{}, err
	}
	return settings, nil
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := s.Refresh(refreshCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Warn("API rate limit settings refresh failed; retaining last valid configuration", "error", err)
			}
		}
	}
}

func (s *Service) apply(settings models.APIRateLimitSettings) error {
	if settings.Revision < 1 {
		return fmt.Errorf("%w: revision must be positive", ErrInvalidConfig)
	}
	if err := Validate(settings.Config); err != nil {
		return err
	}
	// 仅发布者串行，热路径仍为原子读取；配置与指标以相同顺序推进版本。
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	current := s.current.Load()
	if current != nil && current.Revision >= settings.Revision {
		return nil
	}
	s.current.Store(&settings)
	observability.SetAPIRateLimitRevision(settings.Revision)
	return nil
}
