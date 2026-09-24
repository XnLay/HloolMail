package mailstore

import (
	"time"

	"gptmail/internal/models"
	"gptmail/internal/webhook"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeleteMailbox 原子删除指定 owner 的邮箱及邮件；重复请求不能影响同地址的新邮箱。
func DeleteMailbox(database *gorm.DB, ownerID, mailboxID uint) (int64, error) {
	var deleted int64
	err := database.Transaction(func(tx *gorm.DB) error {
		var mailbox models.Mailbox
		// 在事务内重新核验资源身份，避免使用已删除或已变更的外部快照。
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&mailbox, "id = ? AND owner_id = ?", mailboxID, ownerID).Error; err != nil {
			return err
		}
		if err := DeleteMailboxShares(tx, []uint{mailbox.ID}); err != nil {
			return err
		}
		scope := tx.Model(&models.Message{}).Where(
			"mailbox_id = ? OR (owner_id = ? AND recipient = ?) OR (owner_id IS NULL AND recipient = ?)",
			mailbox.ID, mailbox.OwnerID, mailbox.Email, mailbox.Email,
		)
		count, err := DeleteMessages(tx, scope, time.Now().UTC(), webhook.RedactionReasonMailboxDeleted)
		if err != nil {
			return err
		}
		if err := tx.Delete(&mailbox).Error; err != nil {
			return err
		}
		deleted = count
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}
