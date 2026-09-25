package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gptmail/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrAPIRateLimitConflict = errors.New("rate limit settings changed; reload before saving")

func EnsureAPIRateLimitSettings(database *gorm.DB, defaults models.APIRateLimitConfig) (models.APIRateLimitSettings, error) {
	initial := models.APIRateLimitSettings{ID: 1, Revision: 1, Config: defaults, UpdatedBy: "system"}
	if err := database.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
		return models.APIRateLimitSettings{}, err
	}
	return ReadAPIRateLimitSettings(database)
}

func ReadAPIRateLimitSettings(database *gorm.DB) (models.APIRateLimitSettings, error) {
	var settings models.APIRateLimitSettings
	err := database.First(&settings, 1).Error
	return settings, err
}

// SaveAPIRateLimitSettings 将完整配置、版本与审计放在同一事务，失败时不产生部分更新。
func SaveAPIRateLimitSettings(database *gorm.DB, revision int64, config models.APIRateLimitConfig, audit models.AuditLog) (models.APIRateLimitSettings, error) {
	var saved models.APIRateLimitSettings
	err := database.Transaction(func(tx *gorm.DB) error {
		// 先取得写锁，避免 SQLite 两个读事务同时升级写锁时直接返回 SQLITE_BUSY。
		// 条件空更新同时适用于 PostgreSQL；等待前一个写者后重新检查版本。
		lock := tx.Model(&models.APIRateLimitSettings{}).
			Where("id = ? AND revision = ?", 1, revision).
			UpdateColumn("revision", gorm.Expr("revision"))
		if lock.Error != nil {
			return lock.Error
		}
		if lock.RowsAffected != 1 {
			return ErrAPIRateLimitConflict
		}
		var previous models.APIRateLimitSettings
		if err := tx.First(&previous, 1).Error; err != nil {
			return err
		}
		saved = previous
		if previous.Config == config {
			return nil
		}
		saved.Config = config
		saved.Revision++
		saved.UpdatedBy = audit.Actor
		saved.UpdatedAt = time.Now().UTC()
		// Select 保证 false 值也会保存；主键和创建时间不能由更新覆盖。
		result := tx.Clauses(clause.Returning{}).Model(&saved).
			Where("id = ? AND revision = ?", 1, revision).
			Select("*").Omit("id", "created_at").Updates(&saved)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAPIRateLimitConflict
		}
		metadata, err := json.Marshal(struct {
			PreviousRevision int64                     `json:"previous_revision"`
			Revision         int64                     `json:"revision"`
			Before           models.APIRateLimitConfig `json:"before"`
			After            models.APIRateLimitConfig `json:"after"`
		}{previous.Revision, saved.Revision, previous.Config, saved.Config})
		if err != nil {
			return fmt.Errorf("encode rate limit audit: %w", err)
		}
		audit.Metadata = string(metadata)
		audit.CreatedAt = saved.UpdatedAt
		return tx.Create(&audit).Error
	})
	if err != nil {
		return models.APIRateLimitSettings{}, err
	}
	return saved, nil
}
