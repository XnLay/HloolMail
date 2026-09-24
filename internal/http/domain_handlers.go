package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	domaindb "gptmail/internal/domain"
	"gptmail/internal/mailstore"
	"gptmail/internal/models"
	"gptmail/internal/webhook"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type domainPatchInput struct {
	Active          *bool  `json:"active"`
	MXVerified      *bool  `json:"mx_verified"`
	WildcardEnabled *bool  `json:"wildcard_enabled"`
	Mode            string `json:"mode"`
}

func applyDomainPatch(d *models.Domain, input domainPatchInput, allowAdminFields bool) {
	if allowAdminFields && input.Active != nil {
		d.Active = *input.Active
	}
	if allowAdminFields && input.MXVerified != nil {
		d.MXVerified = *input.MXVerified
	}
	if input.WildcardEnabled != nil {
		d.WildcardRequested = *input.WildcardEnabled
		if !*input.WildcardEnabled {
			d.WildcardEnabled = false
		}
	}
	if input.Mode == models.DomainModePublic || input.Mode == models.DomainModePrivate {
		d.Mode = input.Mode
	}
	applyDomainVerificationLifecycle(d, time.Now(), false)
}

func (h *Handler) requestDomain(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	var input struct {
		Domain          string `json:"domain"`
		Mode            string `json:"mode"`
		WildcardEnabled *bool  `json:"wildcard_enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid json")
		return
	}
	mode := input.Mode
	if mode != models.DomainModePublic && mode != models.DomainModePrivate {
		mode = models.DomainModePrivate
	}
	wildcard := true
	if input.WildcardEnabled != nil {
		wildcard = *input.WildcardEnabled
	}
	d, dns, err := h.upsertDomain(input.Domain, mode, wildcard, &user.ID, user.Email)
	if err != nil {
		if errors.Is(err, domaindb.ErrVerificationToken) {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	created(c, gin.H{"domain": domainWithCount(h.DB, *d), "dns": dns})
}

func (h *Handler) batchRequestDomain(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	var input struct {
		Domains []struct {
			Raw      string `json:"raw"`
			Domain   string `json:"domain"`
			Wildcard bool   `json:"wildcard"`
		} `json:"domains"`
		Mode string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid json")
		return
	}
	mode := input.Mode
	if mode != models.DomainModePublic && mode != models.DomainModePrivate {
		mode = models.DomainModePrivate
	}
	if len(input.Domains) > 50 {
		input.Domains = input.Domains[:50]
	}
	type batchDomainItemResult struct {
		Raw          string                    `json:"raw"`
		Domain       string                    `json:"domain"`
		Status       string                    `json:"status"`
		DomainRecord *domainDTO                `json:"domain_record,omitempty"`
		DNS          *domaindb.DNSInstructions `json:"dns,omitempty"`
		Error        string                    `json:"error,omitempty"`
	}
	results := make([]batchDomainItemResult, 0, len(input.Domains))
	for _, item := range input.Domains {
		raw := strings.TrimSpace(item.Raw)
		candidate := strings.TrimSpace(item.Domain)
		if candidate == "" {
			candidate = raw
		}
		domainName := domaindb.NormalizeDomain(candidate)
		wildcard := item.Wildcard || domainWantsWildcard(raw) || domainWantsWildcard(candidate)
		result := batchDomainItemResult{
			Raw:    raw,
			Domain: domainName,
		}
		if domainName == "" || !strings.Contains(domainName, ".") {
			result.Status = "invalid"
			result.Error = "valid domain required"
			results = append(results, result)
			continue
		}
		var existing models.Domain
		lookupErr := h.DB.Where("domain = ?", domainName).First(&existing).Error
		if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			result.Status = "error"
			result.Error = lookupErr.Error()
			results = append(results, result)
			continue
		}
		if lookupErr == nil && existing.OwnerID != nil && *existing.OwnerID != user.ID {
			result.Status = "owned_by_other"
			result.Error = "domain is already owned by another user"
			results = append(results, result)
			continue
		}
		d, dns, err := h.upsertDomain(domainName, mode, wildcard, &user.ID, user.Email)
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		status := "created"
		if lookupErr == nil {
			status = "already_exists"
		}
		dto := domainWithCount(h.DB, *d)
		result.Status = status
		result.DomainRecord = &dto
		result.DNS = &dns
		results = append(results, result)
		h.scheduleDomainMXAutoRetry(*d)
	}
	created(c, gin.H{"results": results})
}

func (h *Handler) scheduleDomainMXAutoRetry(d models.Domain) {
	if !d.IsWaitingVerification() {
		return
	}
	if d.FirstVerifiedAt != nil || d.PendingDeleteAt == nil {
		return
	}
	now := time.Now()
	until := *d.PendingDeleteAt
	if !until.After(now) {
		return
	}
	if err := h.DB.Model(&models.Domain{}).Where("id = ? AND active = ?", d.ID, true).Updates(map[string]interface{}{
		"mx_auto_retry_enabled":    true,
		"mx_auto_retry_started_at": now,
		"mx_auto_retry_until":      until,
		"mx_auto_retry_next_at":    now,
		"mx_auto_retry_last_at":    nil,
		"mx_auto_retry_count":      0,
	}).Error; err != nil {
		return
	}
}

func (h *Handler) checkMX(c *gin.Context) {
	if _, loggedIn := h.requireLogin(c); !loggedIn {
		return
	}
	var input struct {
		Domain string `json:"domain"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid json")
		return
	}
	var d models.Domain
	if err := h.DB.Where("domain = ?", domaindb.NormalizeDomain(input.Domain)).First(&d).Error; err != nil {
		fail(c, http.StatusNotFound, "domain not found")
		return
	}
	if !h.canManageDomain(c, d) {
		fail(c, http.StatusForbidden, "domain access denied")
		return
	}
	result, err := h.DNSChecker.Check(c.Request.Context(), d.Domain)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, result)
}

func (h *Handler) listDomains(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	var domains []models.Domain
	query := h.DB.Model(&models.Domain{}).Where("owner_id = ?", user.ID).Order("domain asc")
	if err := query.Find(&domains).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, webDomainsWithCounts(h.DB, domains))
}

type availableDomainsResponse struct {
	Domains                 []string             `json:"domains"`
	PublicDomains           []availableDomainDTO `json:"public_domains"`
	PrivateDomains          []availableDomainDTO `json:"private_domains"`
	PublicUnavailableReason string               `json:"public_unavailable_reason,omitempty"`
}

func (h *Handler) availableDomains(c *gin.Context) {
	actor, allowed := h.requireActor(c)
	if !allowed {
		return
	}
	var publicDomains []models.Domain
	if err := publicMailboxCapableDomainQuery(h.DB.Order("domain asc")).Find(&publicDomains).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	publicDomains, publicUnavailableReason, err := h.filterAvailablePublicDomains(publicDomains, actor)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	legacyPublicDomains := rootReadyDomains(publicDomains)
	ownerID, hasOwner := actor.ownerID()
	if !hasOwner {
		ok(c, availableDomainsResponse{
			Domains:                 domainNames(legacyPublicDomains),
			PublicDomains:           availableDomainDTOsWithCountsOrEmpty(h.DB, publicDomains),
			PrivateDomains:          []availableDomainDTO{},
			PublicUnavailableReason: publicUnavailableReason,
		})
		return
	}
	privateQuery := privateMailboxCapableDomainQuery(h.DB.Order("domain asc")).Where("owner_id = ?", ownerID)
	var privateDomains []models.Domain
	if err := privateQuery.Find(&privateDomains).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, availableDomainsResponse{
		Domains:                 domainNames(legacyPublicDomains),
		PublicDomains:           availableDomainDTOsWithCountsOrEmpty(h.DB, publicDomains),
		PrivateDomains:          availableDomainDTOsWithCountsOrEmpty(h.DB, privateDomains),
		PublicUnavailableReason: publicUnavailableReason,
	})
}

func (h *Handler) getDomain(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusNotFound, "domain not found")
		return
	}
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	actor := &requestActor{User: user}
	var d models.Domain
	if err := h.DB.First(&d, "id = ?", id).Error; err != nil {
		fail(c, http.StatusNotFound, "domain not found")
		return
	}
	if !h.canViewDomain(actor, d) {
		fail(c, http.StatusForbidden, "domain access denied")
		return
	}
	ok(c, domainWithCount(h.DB, d))
}

func (h *Handler) patchDomain(c *gin.Context) {
	if _, loggedIn := h.requireLogin(c); !loggedIn {
		return
	}
	var d models.Domain
	if err := h.DB.First(&d, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "domain not found")
		return
	}
	if !h.canManageDomain(c, d) {
		fail(c, http.StatusForbidden, "domain access denied")
		return
	}
	var input domainPatchInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid json")
		return
	}
	applyDomainPatch(&d, input, false)
	if err := h.DB.Save(&d).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("domain.patch", actor(c), d.Domain, "")
	ok(c, d)
}

func (h *Handler) setDomainMXAutoRetry(c *gin.Context) {
	var d models.Domain
	if err := h.DB.First(&d, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "domain not found")
		return
	}
	if !h.canManageDomain(c, d) {
		fail(c, http.StatusForbidden, "domain access denied")
		return
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid json")
		return
	}
	now := time.Now()
	if input.Enabled {
		if d.HasMailboxCapability() {
			fail(c, http.StatusBadRequest, "domain is already verified")
			return
		}
		var until time.Time
		if d.FirstVerifiedAt == nil {
			if d.PendingDeleteAt == nil {
				pendingDeleteAt := now.Add(models.PendingDomainTTL)
				d.PendingDeleteAt = &pendingDeleteAt
			}
			until = *d.PendingDeleteAt
		} else {
			until = now.Add(models.PendingDomainTTL)
		}
		if !until.After(now) {
			d.MXAutoRetryEnabled = false
			d.MXAutoRetryNextAt = nil
			d.LastHealthStatus = "unhealthy"
			d.LastUnhealthyAt = &now
			d.LastCheckMessage = "verification window has expired; domain was retained for safety"
			if err := h.DB.Save(&d).Error; err != nil {
				fail(c, http.StatusInternalServerError, err.Error())
				return
			}
			ok(c, domainWithCount(h.DB, d))
			return
		}
		next := now.Add(10 * time.Minute)
		if next.After(until) {
			next = until
		}
		d.MXAutoRetryEnabled = true
		d.MXAutoRetryStartedAt = &now
		d.MXAutoRetryUntil = &until
		d.MXAutoRetryNextAt = &next
		d.MXAutoRetryLastAt = nil
		d.MXAutoRetryCount = 0
		d.LastCheckMessage = "已开启后台等待验证，系统会每 10 分钟自动检测一次，最多等待 2 小时"
	} else {
		d.MXAutoRetryEnabled = false
		d.MXAutoRetryNextAt = nil
		d.LastCheckMessage = "已停止后台等待验证"
	}
	if err := h.DB.Save(&d).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, domainWithCount(h.DB, d))
}

func (h *Handler) deleteDomain(c *gin.Context) {
	if _, loggedIn := h.requireLogin(c); !loggedIn {
		return
	}
	var d models.Domain
	if err := h.DB.First(&d, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "domain not found")
		return
	}
	if !h.canManageDomain(c, d) || !d.IsWaitingVerification() {
		fail(c, http.StatusForbidden, "domain access denied")
		return
	}
	if err := h.deleteDomainWithDependents(d); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("domain.delete", actor(c), d.Domain, "")
	ok(c, gin.H{"deleted": true})
}

func (h *Handler) deleteDomainWithDependents(d models.Domain) error {
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		var mailboxIDs []uint
		if err := tx.Model(&models.Mailbox{}).Where("domain_id = ?", d.ID).Pluck("id", &mailboxIDs).Error; err != nil {
			return err
		}
		if err := mailstore.DeleteMailboxShares(tx, mailboxIDs); err != nil {
			return err
		}
		messageQuery := tx.Model(&models.Message{}).Where("domain_id = ? OR (domain_id IS NULL AND root_domain = ?)", d.ID, d.Domain)
		if _, err := mailstore.DeleteMessages(tx, messageQuery, time.Now().UTC(), webhook.RedactionReasonDomainDeleted); err != nil {
			return err
		}
		endpoints := tx.Model(&models.WebhookEndpoint{}).Where("domain_id = ?", d.ID)
		if err := tx.Where("endpoint_id IN (?)", endpoints.Select("id")).Delete(&models.WebhookDelivery{}).Error; err != nil {
			return err
		}
		if err := tx.Where("domain_id = ?", d.ID).Delete(&models.WebhookEndpoint{}).Error; err != nil {
			return err
		}
		if err := tx.Where("domain_id = ?", d.ID).Delete(&models.Notification{}).Error; err != nil {
			return err
		}
		if err := tx.Where("domain_id = ?", d.ID).Delete(&models.Mailbox{}).Error; err != nil {
			return err
		}
		return tx.Delete(&d).Error
	}); err != nil {
		return err
	}
	return nil
}
