package mailstore

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gptmail/internal/config"
	appdb "gptmail/internal/db"
	"gptmail/internal/models"

	"gorm.io/gorm"
)

func TestDeleteMailboxScopesOwnerAndProtectsRecreatedAddress(t *testing.T) {
	database, mailbox := mailboxFixture(t)
	if _, err := DeleteMailbox(database, mailbox.OwnerID+1, mailbox.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign owner deletion = %v, want not found", err)
	}
	deleted, err := DeleteMailbox(database, mailbox.OwnerID, mailbox.ID)
	if err != nil || deleted != 1 {
		t.Fatalf("delete = %d, %v; want 1, nil", deleted, err)
	}
	assertRemaining(t, database, 0)
	var delivery models.WebhookDelivery
	if err := database.First(&delivery, "id = ?", "delivery").Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(delivery.PayloadJSON, "secret") || delivery.Status != models.WebhookDeliveryStatusFailed {
		t.Fatalf("deleted message delivery was not redacted: status=%s payload=%s", delivery.Status, delivery.PayloadJSON)
	}

	recreated := mailbox
	recreated.ID = 0
	if err := database.Create(&recreated).Error; err != nil {
		t.Fatal(err)
	}
	message := models.Message{
		ID: "new-message", Recipient: recreated.Email, OwnerID: &recreated.OwnerID,
		MailboxID: &recreated.ID, DomainID: &recreated.DomainID, ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := database.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	// 旧删除请求只针对旧 ID，不能根据地址把后来创建的资源一起清掉。
	if _, err := DeleteMailbox(database, mailbox.OwnerID, mailbox.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale deletion = %v, want not found", err)
	}
	var count int64
	if err := database.Model(&models.Message{}).Where("id = ?", message.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("recreated mailbox lost its message: count=%d err=%v", count, err)
	}
}

func TestDeleteMailboxRollsBackAllDependents(t *testing.T) {
	database, mailbox := mailboxFixture(t)
	if err := database.Callback().Delete().Before("gorm:delete").Register("test:reject-mailbox-delete", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "mailboxes" {
			tx.AddError(errors.New("injected mailbox delete failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if deleted, err := DeleteMailbox(database, mailbox.OwnerID, mailbox.ID); err == nil || deleted != 0 {
		t.Fatalf("delete = %d, %v; want rollback", deleted, err)
	}
	assertRemaining(t, database, 1)
	var delivery models.WebhookDelivery
	if err := database.First(&delivery, "id = ?", "delivery").Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(delivery.PayloadJSON, "secret") || delivery.Status != models.WebhookDeliveryStatusPending {
		t.Fatal("delivery redaction escaped the rolled-back transaction")
	}
}

func mailboxFixture(t *testing.T) (*gorm.DB, models.Mailbox) {
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
	if err := appdb.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	owner := models.User{Email: "owner@example.test", PasswordHash: "hash", Role: models.UserRoleUser, Enabled: true}
	if err := database.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	domain := models.Domain{Domain: "example.test", Mode: models.DomainModePublic, Active: true, MXVerified: true}
	if err := database.Create(&domain).Error; err != nil {
		t.Fatal(err)
	}
	mailbox := models.Mailbox{OwnerID: owner.ID, DomainID: domain.ID, Email: "box@example.test", LocalPart: "box", Host: domain.Domain}
	if err := database.Create(&mailbox).Error; err != nil {
		t.Fatal(err)
	}
	message := models.Message{ID: "message", Recipient: mailbox.Email, OwnerID: &owner.ID, MailboxID: &mailbox.ID, DomainID: &domain.ID, ExpiresAt: time.Now().Add(time.Hour)}
	attachment := models.MessageAttachment{ID: "attachment", MessageID: message.ID, Sequence: 1, Filename: "note.txt"}
	share := models.ShareLink{OwnerID: owner.ID, MailboxID: &mailbox.ID, ResourceType: models.ShareResourceTypeMailbox, TokenHash: "hash", TokenPrefix: "share"}
	for _, record := range []any{&message, &attachment, &share} {
		if err := database.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	log := models.ShareLinkAccessLog{ShareLinkID: share.ID, OwnerID: owner.ID, ResourceType: models.ShareResourceTypeMailbox, MailboxID: &mailbox.ID, IP: "127.0.0.1"}
	endpoint := models.WebhookEndpoint{OwnerID: owner.ID, Name: "hook", URL: "https://example.test/hook", Secret: "test", SecretPreview: "test", EventsJSON: "[]"}
	for _, record := range []any{&log, &endpoint} {
		if err := database.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	delivery := models.WebhookDelivery{
		ID: "delivery", OwnerID: owner.ID, EndpointID: endpoint.ID, MessageID: message.ID,
		EventType: models.WebhookEventMessageReceived, PayloadJSON: "{\"message\":\"secret\"}",
		DedupKey: "delivery", Status: models.WebhookDeliveryStatusPending,
	}
	if err := database.Create(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	return database, mailbox
}

func assertRemaining(t *testing.T, database *gorm.DB, want int64) {
	t.Helper()
	for _, model := range []any{&models.Mailbox{}, &models.Message{}, &models.MessageAttachment{}, &models.ShareLink{}, &models.ShareLinkAccessLog{}} {
		var count int64
		if err := database.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Errorf("%T count=%d, want %d", model, count, want)
		}
	}
}
