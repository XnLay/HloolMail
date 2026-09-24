package mailstore

import (
	"time"

	"gptmail/internal/models"
	"gptmail/internal/webhook"

	"gorm.io/gorm"
)

// DeleteMessages 在调用方的事务中统一删除邮件、附件和分享，并脱敏待投递事件。
// scope 必须由同一事务构建并已限定调用方有权处理的邮件。
func DeleteMessages(tx, scope *gorm.DB, at time.Time, reason string) (int64, error) {
	scope = scope.Session(&gorm.Session{})
	if err := webhook.RedactMessageDeliveriesForQuery(tx, scope.Session(&gorm.Session{}), at, reason); err != nil {
		return 0, err
	}
	ids := scope.Session(&gorm.Session{}).Select("id")
	shares := tx.Model(&models.ShareLink{}).
		Where("resource_type = ? AND message_id IN (?)", models.ShareResourceTypeMessage, ids)
	if err := deleteShares(tx, shares); err != nil {
		return 0, err
	}
	if err := tx.Where("message_id IN (?)", ids).Delete(&models.MessageAttachment{}).Error; err != nil {
		return 0, err
	}
	result := scope.Unscoped().Delete(&models.Message{})
	return result.RowsAffected, result.Error
}

// DeleteMailboxShares 与邮箱或域名的删除共用调用方事务。
func DeleteMailboxShares(tx *gorm.DB, mailboxIDs []uint) error {
	if len(mailboxIDs) == 0 {
		return nil
	}
	return deleteShares(tx, tx.Model(&models.ShareLink{}).
		Where("resource_type = ? AND mailbox_id IN ?", models.ShareResourceTypeMailbox, mailboxIDs))
}

func deleteShares(tx, scope *gorm.DB) error {
	ids := scope.Session(&gorm.Session{}).Select("id")
	if err := tx.Where("share_link_id IN (?)", ids).Delete(&models.ShareLinkAccessLog{}).Error; err != nil {
		return err
	}
	return scope.Delete(&models.ShareLink{}).Error
}
