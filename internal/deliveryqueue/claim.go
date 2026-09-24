package deliveryqueue

import (
	"time"

	"gorm.io/gorm"
)

// Claimed 限定一次领取的所有权；同一 worker 重新领取后，旧时间戳也不能再匹配。
// 未领取记录的 locked_at 为 NULL，不会匹配等值条件。
func Claimed(query *gorm.DB, id, workerID string, lockedAt *time.Time) *gorm.DB {
	return query.Where("id = ? AND locked_by = ? AND locked_at = ?", id, workerID, lockedAt)
}
