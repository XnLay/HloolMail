package db

import (
	"testing"
	"time"

	"gptmail/internal/models"

	"gorm.io/gorm"
)

func TestOwnershipMigrationRequiresStableOriginalMailbox(t *testing.T) {
	for _, name := range []string{"original reference", "address only", "missing mailbox", "wrong address", "newer mailbox", "changed owner"} {
		t.Run(name, func(t *testing.T) {
			f := newOwnershipFixture(t, models.DomainModePublic)
			message := f.message("legacy")
			switch name {
			case "address only":
				message.MailboxID, message.DomainID = nil, nil
			case "missing mailbox":
				message.MailboxID = nil
			case "wrong address":
				message.Recipient = "different@" + f.domain.Domain
			case "newer mailbox":
				message.CreatedAt = f.mailbox.CreatedAt.Add(-time.Second)
			case "changed owner":
				other := f.otherOwner(t)
				if err := f.db.Model(&f.mailbox).Update("owner_id", other.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := f.db.Create(&message).Error; err != nil {
				t.Fatal(err)
			}
			if err := AutoMigrate(f.db); err != nil {
				t.Fatal(err)
			}
			f.assertOwner(t, message.ID, name == "original reference")
		})
	}
}

func TestOwnershipMigrationRequiresStableOriginalPrivateDomain(t *testing.T) {
	for _, name := range []string{"original reference", "domain name only", "inconsistent mailbox", "changed owner"} {
		t.Run(name, func(t *testing.T) {
			f := newOwnershipFixture(t, models.DomainModePrivate)
			message := f.message("catch-all")
			message.MailboxID = nil
			message.Recipient = "catch-all@" + f.domain.Domain
			switch name {
			case "domain name only":
				message.DomainID = nil
			case "inconsistent mailbox":
				message.MailboxID = &f.mailbox.ID
			case "changed owner":
				other := f.otherOwner(t)
				if err := f.db.Model(&f.domain).Update("owner_id", other.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := f.db.Create(&message).Error; err != nil {
				t.Fatal(err)
			}
			if err := AutoMigrate(f.db); err != nil {
				t.Fatal(err)
			}
			f.assertOwner(t, message.ID, name == "original reference")
		})
	}
}

func TestOwnershipMigrationPreservesExistingOwner(t *testing.T) {
	f := newOwnershipFixture(t, models.DomainModePublic)
	other := f.otherOwner(t)
	message := f.message("owned")
	message.OwnerID = &other.ID
	if err := f.db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(f.db); err != nil {
		t.Fatal(err)
	}
	var reloaded models.Message
	if err := f.db.First(&reloaded, "id = ?", message.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertUintPtr(t, reloaded.OwnerID, other.ID, "existing owner")
}

func TestOwnershipMigrationRunsOnce(t *testing.T) {
	f := newOwnershipFixture(t, models.DomainModePublic)
	if err := AutoMigrate(f.db); err != nil {
		t.Fatal(err)
	}
	// 后续导入或创建的记录不能在下一次启动时被重新推断归属。
	message := f.message("imported-after-migration")
	if err := f.db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(f.db); err != nil {
		t.Fatal(err)
	}
	f.assertOwner(t, message.ID, false)
	var count int64
	if err := f.db.Table("data_migrations").Where("name = ?", "message_ownership").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ownership migration records = %d, want 1", count)
	}
}

func TestOwnershipMigrationRollsBackDataAndVersion(t *testing.T) {
	f := newOwnershipFixture(t, models.DomainModePrivate)
	mailboxMessage := f.message("mailbox")
	privateMessage := f.message("private")
	privateMessage.MailboxID = nil
	privateMessage.Recipient = "catch-all@" + f.domain.Domain
	if err := f.db.Create(&[]models.Message{mailboxMessage, privateMessage}).Error; err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(f.db); err != nil {
		t.Fatal(err)
	}
	if err := ensureMigrationLedger(f.db, "sqlite", "data_migrations"); err != nil {
		t.Fatal(err)
	}
	// 让第二条归属更新失败，确认第一条更新和迁移版本没有部分提交。
	if err := f.db.Exec("CREATE TRIGGER reject_private_owner BEFORE UPDATE OF owner_id ON messages " +
		"WHEN OLD.id = 'private' BEGIN SELECT RAISE(ABORT, 'injected migration failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := runDataMigrations(f.db); err == nil {
		t.Fatal("expected migration failure")
	}
	f.assertOwner(t, mailboxMessage.ID, false)
	f.assertOwner(t, privateMessage.ID, false)
	var count int64
	if err := f.db.Table("data_migrations").Where("name = ?", "message_ownership").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed migration recorded as applied")
	}
	if err := f.db.Exec("DROP TRIGGER reject_private_owner").Error; err != nil {
		t.Fatal(err)
	}
	if err := runDataMigrations(f.db); err != nil {
		t.Fatal(err)
	}
	f.assertOwner(t, mailboxMessage.ID, true)
	f.assertOwner(t, privateMessage.ID, true)
}

type ownershipFixture struct {
	db      *gorm.DB
	owner   models.User
	domain  models.Domain
	mailbox models.Mailbox
}

func newOwnershipFixture(t *testing.T, mode string) ownershipFixture {
	t.Helper()
	f := ownershipFixture{db: openSQLiteTestDB(t)}
	if err := f.db.AutoMigrate(&models.User{}, &models.Domain{}, &models.Mailbox{}, &models.Message{}); err != nil {
		t.Fatal(err)
	}
	f.owner = models.User{Email: "owner@example.test", PasswordHash: "hash", Role: models.UserRoleUser, Enabled: true}
	if err := f.db.Create(&f.owner).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Hour)
	f.domain = models.Domain{
		Domain: "ownership.test", Mode: mode, OwnerID: &f.owner.ID,
		Active: true, MXVerified: true, CreatedAt: at, UpdatedAt: at,
	}
	if err := f.db.Create(&f.domain).Error; err != nil {
		t.Fatal(err)
	}
	f.mailbox = models.Mailbox{
		Email: "box@ownership.test", OwnerID: f.owner.ID, DomainID: f.domain.ID,
		LocalPart: "box", Host: f.domain.Domain, CreatedAt: at, UpdatedAt: at,
	}
	if err := f.db.Create(&f.mailbox).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func (f ownershipFixture) message(id string) models.Message {
	message := legacyMessage(id, f.mailbox.Email, f.domain.Domain, f.domain.Domain)
	message.DomainID, message.MailboxID = &f.domain.ID, &f.mailbox.ID
	message.CreatedAt = f.mailbox.UpdatedAt.Add(time.Minute)
	return message
}

func (f ownershipFixture) otherOwner(t *testing.T) models.User {
	t.Helper()
	owner := models.User{Email: "other@example.test", PasswordHash: "hash", Role: models.UserRoleUser, Enabled: true}
	if err := f.db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	return owner
}

func (f ownershipFixture) assertOwner(t *testing.T, id string, owned bool) {
	t.Helper()
	var message models.Message
	if err := f.db.First(&message, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if owned {
		assertUintPtr(t, message.OwnerID, f.owner.ID, "owner_id")
	} else if message.OwnerID != nil {
		t.Errorf("unproven message %s assigned to owner %d", id, *message.OwnerID)
	}
}
