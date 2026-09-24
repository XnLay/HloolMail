package smtpserver

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gptmail/internal/config"
	"gptmail/internal/events"
	"gptmail/internal/models"
	"gptmail/internal/webhook"

	"gorm.io/gorm"
)

func TestDataCommitsRecipientsAtomically(t *testing.T) {
	session, database := newSMTPTestSession(t, config.Config{
		MaxMessageBytes: 1 << 20, MessageRetention: time.Hour, WebhooksEnabled: true,
	})
	first := session.recipients[0]
	second := models.Mailbox{
		OwnerID: first.OwnerID, Email: "second@example.test",
		LocalPart: "second", Host: "example.test", DomainID: first.Domain.ID,
	}
	if err := database.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	if err := session.Rcpt(second.Email, nil); err != nil {
		t.Fatal(err)
	}
	eventTypes, err := webhook.EventsJSON([]string{models.WebhookEventMessageReceived})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := models.WebhookEndpoint{
		OwnerID: first.OwnerID, Name: "atomic-delivery", URL: "https://example.test/hook",
		Secret: "test-secret", SecretPreview: "test", Enabled: true,
		EventsJSON: eventTypes, Scope: models.WebhookScopeAll,
	}
	if err := database.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	session.service.Hub = events.NewHub()
	stream, cancel := session.service.Hub.Subscribe(first.Parts.Recipient)
	defer cancel()

	// 在第二个收件人落库时失败，检验前一人的邮件及所有附属数据是否一起回滚。
	failSecond := true
	if err := database.Callback().Create().Before("gorm:create").Register("test:second-recipient-failure", func(tx *gorm.DB) {
		if msg, ok := tx.Statement.Dest.(*models.Message); ok && failSecond && msg.Recipient == second.Email {
			tx.AddError(errors.New("injected second recipient insert failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	raw := strings.Join([]string{
		"From: sender@example.test", "Subject: atomic batch", "MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=outer", "", "--outer",
		"Content-Type: text/plain", "", "hello", "--outer",
		"Content-Type: text/plain; name=note.txt",
		"Content-Disposition: attachment; filename=note.txt", "", "attachment",
		"--outer--", "",
	}, "\r\n")
	if err := session.Data(strings.NewReader(raw)); err == nil {
		t.Fatal("expected SMTP failure for the entire DATA command")
	}
	assertBatchCounts(t, database, 0)
	select {
	case <-stream:
		t.Error("rolled-back message was published to the inbox stream")
	default:
	}

	failSecond = false
	if err := session.Data(strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	assertBatchCounts(t, database, 2)
	select {
	case <-stream:
	default:
		t.Error("committed message was not published to the inbox stream")
	}
}

func assertBatchCounts(t *testing.T, database *gorm.DB, want int64) {
	t.Helper()
	for _, model := range []any{&models.Message{}, &models.MessageAttachment{}, &models.WebhookDelivery{}} {
		var count int64
		if err := database.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Errorf("%T count = %d, want %d", model, count, want)
		}
	}
	var received int64
	if err := database.Model(&models.MessageDailyStat{}).Select("COALESCE(SUM(message_count), 0)").Scan(&received).Error; err != nil {
		t.Fatal(err)
	}
	if received != want {
		t.Errorf("received count = %d, want %d", received, want)
	}
}
