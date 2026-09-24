package httpapi

import (
	"net/http"
	"time"

	"gptmail/internal/db"
	"gptmail/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *Handler) createMailboxWithAccounting(ownerID uint, d *models.Domain, email, local, host string, requireWildcard bool, actor *requestActor, share *pendingMailboxShare) (models.Mailbox, *models.ShareLink, error) {
	var mailbox models.Mailbox
	var link *models.ShareLink
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var freshDomain models.Domain
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&freshDomain, "id = ?", d.ID).Error; err != nil {
			return err
		}
		if requireWildcard {
			if !freshDomain.IsWildcardReady() {
				return httpStatusError{Status: http.StatusBadRequest, Message: "domain wildcard MX is not enabled"}
			}
			if err := ensureSubdomainHostAvailable(tx, host, &freshDomain); err != nil {
				return err
			}
		} else if !freshDomain.IsRootMailboxReady() {
			return httpStatusError{Status: http.StatusBadRequest, Message: "domain is not active or MX verified"}
		}
		if freshDomain.Mode == models.DomainModePrivate {
			if freshDomain.OwnerID == nil || *freshDomain.OwnerID != ownerID {
				return httpStatusError{Status: http.StatusForbidden, Message: "该域名是私有域名，只有域名的所有者才能使用"}
			}
		}
		mailbox = models.Mailbox{
			OwnerID:   ownerID,
			Email:     email,
			LocalPart: local,
			Host:      host,
			DomainID:  freshDomain.ID,
		}
		if err := tx.Create(&mailbox).Error; err != nil {
			return err
		}
		if err := h.applyMailboxAccounting(tx, ownerID, freshDomain, actor); err != nil {
			return err
		}
		if share != nil {
			createdLink := models.ShareLink{
				OwnerID:       mailbox.OwnerID,
				TokenHash:     share.TokenHash,
				TokenPrefix:   share.TokenPrefix,
				ResourceType:  models.ShareResourceTypeMailbox,
				MailboxID:     &mailbox.ID,
				AccessKeyHash: share.AccessKeyHash,
				ExpiresAt:     share.ExpiresAt,
			}
			if err := tx.Create(&createdLink).Error; err != nil {
				return err
			}
			link = &createdLink
		}
		*d = freshDomain
		return nil
	})
	if err != nil {
		return models.Mailbox{}, nil, err
	}
	return mailbox, link, nil
}

func (h *Handler) applyMailboxAccounting(tx *gorm.DB, userID uint, d models.Domain, actor *requestActor) error {
	if d.Mode == models.DomainModePublic {
		settings, err := db.EnsureSystemQuotaSettings(tx)
		if err != nil {
			return err
		}
		if !actor.isAdmin() {
			if err := h.enforcePublicMailboxRules(tx, userID, d, settings); err != nil {
				return err
			}
		}
		if err := incrementDomainMailboxCount(tx, d.ID, d, actor, settings); err != nil {
			return err
		}
		return incrementUserPublicMailboxCount(tx, userID, settings, !actor.isAdmin())
	}
	if err := tx.Model(&models.Domain{}).Where("id = ?", d.ID).Update("mailbox_created_count", gorm.Expr("mailbox_created_count + 1")).Error; err != nil {
		return err
	}
	return tx.Model(&models.User{}).Where("id = ?", userID).Update("private_mailbox_created", gorm.Expr("private_mailbox_created + 1")).Error
}

func (h *Handler) enforcePublicMailboxRules(tx *gorm.DB, userID uint, d models.Domain, settings *models.SystemQuotaSettings) error {
	if settings.RequirePublicDomainForQuota {
		hasPublicDomain, err := hasMailboxCapablePublicDomain(tx, userID)
		if err != nil {
			return err
		}
		if !hasPublicDomain {
			return httpStatusError{Status: http.StatusForbidden, Message: "需要先上传并验证公开域名后才可创建公开邮箱"}
		}
	}
	return nil
}

func incrementDomainMailboxCount(tx *gorm.DB, domainID uint, d models.Domain, actor *requestActor, settings *models.SystemQuotaSettings) error {
	isOwner := false
	if ownerID, ok := actor.ownerID(); ok {
		isOwner = d.OwnerID != nil && *d.OwnerID == ownerID
	}
	query := tx.Model(&models.Domain{}).Where("id = ?", domainID)
	if d.Mode == models.DomainModePublic && !actor.isAdmin() && !isOwner && settings.PublicDomainMailboxLimit > 0 {
		query = query.Where("mailbox_created_count < ?", settings.PublicDomainMailboxLimit)
	}
	result := query.Update("mailbox_created_count", gorm.Expr("mailbox_created_count + 1"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return httpStatusError{Status: http.StatusForbidden, Message: "该公开域名邮箱创建已达上限，仅域名所有者可使用"}
	}
	return nil
}

func incrementUserPublicMailboxCount(tx *gorm.DB, userID uint, settings *models.SystemQuotaSettings, enforceDailyLimit bool) error {
	today := time.Now().Format("2006-01-02")
	query := tx.Model(&models.User{}).Where("id = ? AND enabled = ?", userID, true)
	if enforceDailyLimit {
		if settings.UserDailyPublicMailboxLimit > 0 {
			query = query.Where("public_mailbox_date <> ? OR public_mailbox_today < ?", today, settings.UserDailyPublicMailboxLimit)
		}
		query = query.Where("daily_limit = 0 OR public_mailbox_date <> ? OR public_mailbox_today < daily_limit", today)
	}
	result := query.Updates(map[string]interface{}{
		"public_mailbox_created": gorm.Expr("public_mailbox_created + 1"),
		"public_mailbox_today":   gorm.Expr("CASE WHEN public_mailbox_date = ? THEN public_mailbox_today + 1 ELSE 1 END", today),
		"public_mailbox_date":    today,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return httpStatusError{Status: http.StatusTooManyRequests, Message: "公开邮箱每日创建额度已用完"}
	}
	return nil
}

func hasMailboxCapablePublicDomain(tx *gorm.DB, userID uint) (bool, error) {
	var count int64
	if err := ownerRootReadyPublicDomainQuery(tx.Model(&models.Domain{}), userID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func hasRootReadyPublicDomain(tx *gorm.DB, userID uint) (bool, error) {
	return hasMailboxCapablePublicDomain(tx, userID)
}

func (h *Handler) filterAvailablePublicDomains(domains []models.Domain, actor *requestActor) ([]models.Domain, string, error) {
	settings, err := db.EnsureSystemQuotaSettings(h.DB)
	if err != nil {
		return nil, "", err
	}
	if actor != nil && !actor.isAdmin() && settings.RequirePublicDomainForQuota {
		ownerID, ok := actor.ownerID()
		if !ok {
			return []models.Domain{}, "public mailbox creation requires an owned active public domain with MX or wildcard MX verified", nil
		}
		hasPublicDomain, err := hasMailboxCapablePublicDomain(h.DB, ownerID)
		if err != nil {
			return nil, "", err
		}
		if !hasPublicDomain {
			return []models.Domain{}, "public mailbox creation requires an owned active public domain with MX or wildcard MX verified", nil
		}
	}
	if settings.PublicDomainMailboxLimit <= 0 {
		return domains, "", nil
	}
	var ownerID uint
	if actor != nil {
		if id, ok := actor.ownerID(); ok {
			ownerID = id
		}
	}
	out := make([]models.Domain, 0, len(domains))
	for _, d := range domains {
		isOwner := d.OwnerID != nil && *d.OwnerID == ownerID
		if actor != nil && (actor.isAdmin() || isOwner) || d.MailboxCreatedCount < settings.PublicDomainMailboxLimit {
			out = append(out, d)
		}
	}
	return out, "", nil
}
