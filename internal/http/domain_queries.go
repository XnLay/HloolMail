package httpapi

import (
	"errors"
	"fmt"
	"strings"
	"time"

	domaindb "gptmail/internal/domain"
	"gptmail/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const rootReadyDomainSQL = "mode = ? AND active = ? AND mx_verified = ?"

const wildcardReadyDomainSQL = "mode = ? AND active = ? AND wildcard_enabled = ?"

const mailboxCapableDomainSQL = "mode = ? AND active = ? AND (mx_verified = ? OR wildcard_enabled = ?)"

func rootReadyDomainArgs(mode string) []interface{} {
	return []interface{}{mode, true, true}
}

func publicReadyDomainArgs() []interface{} {
	return rootReadyDomainArgs(models.DomainModePublic)
}

func publicReadyDomainQuery(query *gorm.DB) *gorm.DB {
	return query.Where(rootReadyDomainSQL, publicReadyDomainArgs()...)
}

func privateReadyDomainQuery(query *gorm.DB) *gorm.DB {
	return query.Where(rootReadyDomainSQL, rootReadyDomainArgs(models.DomainModePrivate)...)
}

func wildcardReadyDomainArgs(mode string) []interface{} {
	return []interface{}{mode, true, true}
}

func publicWildcardReadyDomainQuery(query *gorm.DB) *gorm.DB {
	return query.Where(wildcardReadyDomainSQL, wildcardReadyDomainArgs(models.DomainModePublic)...)
}

func privateWildcardReadyDomainQuery(query *gorm.DB) *gorm.DB {
	return query.Where(wildcardReadyDomainSQL, wildcardReadyDomainArgs(models.DomainModePrivate)...)
}

func mailboxCapableDomainArgs(mode string) []interface{} {
	return []interface{}{mode, true, true, true}
}

func publicMailboxCapableDomainQuery(query *gorm.DB) *gorm.DB {
	return query.Where(mailboxCapableDomainSQL, mailboxCapableDomainArgs(models.DomainModePublic)...)
}

func ownerRootReadyPublicDomainQuery(query *gorm.DB, ownerID uint) *gorm.DB {
	args := append([]interface{}{ownerID}, mailboxCapableDomainArgs(models.DomainModePublic)...)
	return query.Where("owner_id = ? AND "+mailboxCapableDomainSQL, args...)
}

func privateMailboxCapableDomainQuery(query *gorm.DB) *gorm.DB {
	return query.Where(mailboxCapableDomainSQL, mailboxCapableDomainArgs(models.DomainModePrivate)...)
}

func visibleDomainsForOwner(query *gorm.DB, ownerID uint) *gorm.DB {
	args := append([]interface{}{ownerID}, mailboxCapableDomainArgs(models.DomainModePublic)...)
	return query.Where("owner_id = ? OR ("+mailboxCapableDomainSQL+")", args...)
}

func (h *Handler) canManageDomain(c *gin.Context, d models.Domain) bool {
	user := currentUser(c)
	if user == nil {
		return false
	}
	return d.OwnerID != nil && *d.OwnerID == user.ID
}

func (h *Handler) canViewDomain(actor *requestActor, d models.Domain) bool {
	if actor == nil {
		return false
	}
	ownerID, ok := actor.ownerID()
	return ok && d.OwnerID != nil && *d.OwnerID == ownerID
}

func (h *Handler) selectDomainForActor(input string, actor *requestActor) (*models.Domain, error) {
	domainName := domaindb.NormalizeDomain(input)
	if domainName != "" {
		var d models.Domain
		err := h.DB.Where("domain = ?", domainName).First(&d).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("域名不存在或当前 API key 无权使用该域名")
			}
			return nil, err
		}
		if !d.Active {
			return nil, fmt.Errorf("域名已停用")
		}
		if !d.MXVerified {
			return nil, fmt.Errorf("域名 MX 未验证")
		}
		if d.Mode == models.DomainModePrivate {
			if ownerID, ok := actor.ownerID(); ok && d.OwnerID != nil && *d.OwnerID == ownerID {
				return &d, nil
			}
			return nil, fmt.Errorf("该私有域名仅域名所有者可使用")
		}
		return &d, nil
	}
	var domains []models.Domain
	if err := publicReadyDomainQuery(h.DB.Order("domain asc")).Find(&domains).Error; err != nil {
		return nil, err
	}
	domains, _, err := h.filterAvailablePublicDomains(domains, actor)
	if err != nil {
		return nil, err
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("暂无可随机选择的公有域名；如需使用私有域名，请传入 domain")
	}
	index := randomIndex(len(domains))
	return &domains[index], nil
}

func (h *Handler) selectWildcardDomainForActor(input string, actor *requestActor) (*models.Domain, error) {
	domainName := domaindb.NormalizeDomain(input)
	if domainName != "" {
		var d models.Domain
		err := h.DB.Where("domain = ?", domainName).First(&d).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("domain does not exist or current API key cannot use it")
			}
			return nil, err
		}
		if !d.Active {
			return nil, fmt.Errorf("domain is disabled")
		}
		if !d.WildcardEnabled {
			return nil, fmt.Errorf("domain wildcard MX is not enabled")
		}
		if d.Mode == models.DomainModePrivate {
			if ownerID, ok := actor.ownerID(); ok && d.OwnerID != nil && *d.OwnerID == ownerID {
				return &d, nil
			}
			return nil, fmt.Errorf("private domain can only be used by the domain owner")
		}
		return &d, nil
	}

	var candidates []models.Domain
	if ownerID, ok := actor.ownerID(); ok {
		var privateDomains []models.Domain
		if err := privateWildcardReadyDomainQuery(h.DB.Order("domain asc")).Where("owner_id = ?", ownerID).Find(&privateDomains).Error; err != nil {
			return nil, err
		}
		candidates = append(candidates, privateDomains...)
	}

	var publicDomains []models.Domain
	if err := publicWildcardReadyDomainQuery(h.DB.Order("domain asc")).Find(&publicDomains).Error; err != nil {
		return nil, err
	}
	filteredPublicDomains, _, err := h.filterAvailablePublicDomains(publicDomains, actor)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, filteredPublicDomains...)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no wildcard-enabled parent domain is available; pass a domain with wildcard MX enabled or add one in the web console")
	}
	index := randomIndex(len(candidates))
	return &candidates[index], nil
}

func (h *Handler) upsertDomain(rawDomain, mode string, wildcard bool, ownerID *uint, actorName string) (*models.Domain, domaindb.DNSInstructions, error) {
	if domainWantsWildcard(rawDomain) {
		wildcard = true
	}
	domainName := domaindb.NormalizeDomain(rawDomain)
	if domainName == "" || !strings.Contains(domainName, ".") {
		return nil, domaindb.DNSInstructions{}, fmt.Errorf("valid domain required")
	}
	var d models.Domain
	err := h.DB.Where("domain = ?", domainName).First(&d).Error
	now := time.Now()
	isNewDomain := false
	wasActive := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		verificationToken, err := domaindb.NewVerificationToken()
		if err != nil {
			return nil, domaindb.DNSInstructions{}, err
		}
		isNewDomain = true
		d = models.Domain{
			Domain:            domainName,
			Mode:              mode,
			Active:            true,
			MXVerified:        h.Config.DevMode && strings.HasSuffix(domainName, ".test"),
			WildcardEnabled:   wildcard && h.Config.DevMode && strings.HasSuffix(domainName, ".test"),
			WildcardRequested: wildcard,
			VerificationToken: verificationToken,
			OwnerID:           ownerID,
		}
	} else if err != nil {
		return nil, domaindb.DNSInstructions{}, err
	} else {
		wasActive = d.Active
		if ownerID != nil && d.OwnerID != nil && *d.OwnerID != *ownerID {
			return nil, domaindb.DNSInstructions{}, fmt.Errorf("domain is already owned by another user")
		}
		d.Mode = mode
		d.Active = true
		d.WildcardRequested = wildcard
		if !wildcard {
			d.WildcardEnabled = false
		}
		if ownerID != nil {
			d.OwnerID = ownerID
		}
		if d.VerificationToken == "" {
			verificationToken, err := domaindb.NewVerificationToken()
			if err != nil {
				return nil, domaindb.DNSInstructions{}, err
			}
			d.VerificationToken = verificationToken
		}
		if h.Config.DevMode && strings.HasSuffix(domainName, ".test") {
			d.MXVerified = true
			if wildcard {
				d.WildcardEnabled = true
			}
		}
	}
	applyDomainVerificationLifecycle(&d, now, isNewDomain || !wasActive)
	if d.ID == 0 {
		err = h.DB.Create(&d).Error
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, domaindb.DNSInstructions{}, fmt.Errorf("domain %s is already registered", domainName)
		}
	} else {
		err = h.DB.Save(&d).Error
	}
	if err != nil {
		return nil, domaindb.DNSInstructions{}, err
	}
	h.audit("domain.request", actorName, d.Domain, mode)
	dns := domaindb.Instructions(d.Domain, h.Config.ExpectedMX)
	return &d, dns, nil
}

func applyDomainVerificationLifecycle(d *models.Domain, now time.Time, allowPendingDelete bool) {
	if d.HasMailboxCapability() {
		if d.FirstVerifiedAt == nil {
			d.FirstVerifiedAt = &now
		}
		d.PendingDeleteAt = nil
		return
	}
	if d.FirstVerifiedAt != nil {
		d.PendingDeleteAt = nil
		return
	}
	if allowPendingDelete {
		pendingDeleteAt := now.Add(models.PendingDomainTTL)
		d.PendingDeleteAt = &pendingDeleteAt
	}
}

func domainWantsWildcard(rawDomain string) bool {
	return strings.HasPrefix(strings.TrimSpace(rawDomain), "*")
}

type availableDomainDTO struct {
	ID                uint     `json:"id"`
	Domain            string   `json:"domain"`
	Mode              string   `json:"mode"`
	MessageCount      int64    `json:"message_count"`
	RootReady         bool     `json:"root_ready"`
	WildcardReady     bool     `json:"wildcard_ready"`
	WildcardRequested bool     `json:"wildcard_requested"`
	WildcardPattern   string   `json:"wildcard_pattern,omitempty"`
	Capabilities      []string `json:"capabilities"`
}

type webDomainDTO struct {
	ID                uint       `json:"id"`
	Domain            string     `json:"domain"`
	Mode              string     `json:"mode"`
	OwnerID           *uint      `json:"owner_id,omitempty"`
	Active            bool       `json:"active"`
	MXVerified        bool       `json:"mx_verified"`
	WildcardEnabled   bool       `json:"wildcard_enabled"`
	WildcardRequested bool       `json:"wildcard_requested"`
	LastMXCheckAt     *time.Time `json:"last_mx_check_at,omitempty"`
	DomainExpiresAt   *time.Time `json:"domain_expires_at,omitempty"`
	MessageCount      int64      `json:"message_count"`
	FirstVerifiedAt   *time.Time `json:"first_verified_at,omitempty"`
	PendingDeleteAt   *time.Time `json:"pending_delete_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type domainDTO struct {
	models.Domain
	MessageCount int64 `json:"message_count"`
}

func domainNames(domains []models.Domain) []string {
	if len(domains) == 0 {
		return []string{}
	}
	names := make([]string, 0, len(domains))
	for _, d := range domains {
		names = append(names, d.Domain)
	}
	return names
}

func rootReadyDomains(domains []models.Domain) []models.Domain {
	if len(domains) == 0 {
		return []models.Domain{}
	}
	out := make([]models.Domain, 0, len(domains))
	for _, d := range domains {
		if d.IsRootMailboxReady() {
			out = append(out, d)
		}
	}
	return out
}

func messageCountsByDomain(db *gorm.DB, domains []models.Domain) map[string]int64 {
	if len(domains) == 0 {
		return map[string]int64{}
	}
	type countResult struct {
		RootDomain string
		Count      int64
	}
	var counts []countResult
	db.Model(&models.Message{}).
		Select("root_domain, COUNT(*) as count").
		Where("root_domain IN ?", domainNames(domains)).
		Group("root_domain").
		Scan(&counts)
	countMap := make(map[string]int64, len(counts))
	for _, c := range counts {
		countMap[c.RootDomain] = c.Count
	}
	return countMap
}

func mailboxCountsByDomainID(db *gorm.DB, domains []models.Domain) map[uint]int64 {
	if len(domains) == 0 {
		return map[uint]int64{}
	}
	ids := make([]uint, 0, len(domains))
	for _, d := range domains {
		ids = append(ids, d.ID)
	}
	type countResult struct {
		DomainID uint
		Count    int64
	}
	var counts []countResult
	db.Model(&models.Mailbox{}).
		Select("domain_id, COUNT(*) as count").
		Where("domain_id IN ?", ids).
		Group("domain_id").
		Scan(&counts)
	countMap := make(map[uint]int64, len(counts))
	for _, c := range counts {
		countMap[c.DomainID] = c.Count
	}
	return countMap
}

func availableDomainDTOsWithCountsOrEmpty(db *gorm.DB, domains []models.Domain) []availableDomainDTO {
	if len(domains) == 0 {
		return []availableDomainDTO{}
	}
	countMap := messageCountsByDomain(db, domains)
	out := make([]availableDomainDTO, 0, len(domains))
	for _, d := range domains {
		capabilities := make([]string, 0, 2)
		if d.IsRootMailboxReady() {
			capabilities = append(capabilities, "root_mailbox")
		}
		if d.IsWildcardReady() {
			capabilities = append(capabilities, "subdomain_mailbox")
		}
		wildcardPattern := ""
		if d.WildcardRequested || d.WildcardEnabled {
			wildcardPattern = "*." + d.Domain
		}
		out = append(out, availableDomainDTO{
			ID:                d.ID,
			Domain:            d.Domain,
			Mode:              d.Mode,
			MessageCount:      countMap[d.Domain],
			RootReady:         d.IsRootMailboxReady(),
			WildcardReady:     d.IsWildcardReady(),
			WildcardRequested: d.WildcardRequested,
			WildcardPattern:   wildcardPattern,
			Capabilities:      capabilities,
		})
	}
	return out
}

func webDomainsWithCounts(db *gorm.DB, domains []models.Domain) []webDomainDTO {
	if len(domains) == 0 {
		return []webDomainDTO{}
	}
	countMap := messageCountsByDomain(db, domains)
	out := make([]webDomainDTO, 0, len(domains))
	for _, d := range domains {
		dto := webDomainDTO{
			ID:                d.ID,
			Domain:            d.Domain,
			Mode:              d.Mode,
			OwnerID:           d.OwnerID,
			Active:            d.Active,
			MXVerified:        d.MXVerified,
			WildcardEnabled:   d.WildcardEnabled,
			WildcardRequested: d.WildcardRequested,
			LastMXCheckAt:     d.LastMXCheckAt,
			DomainExpiresAt:   d.DomainExpiresAt,
			MessageCount:      countMap[d.Domain],
			FirstVerifiedAt:   d.FirstVerifiedAt,
			PendingDeleteAt:   d.PendingDeleteAt,
			CreatedAt:         d.CreatedAt,
			UpdatedAt:         d.UpdatedAt,
		}
		out = append(out, dto)
	}
	return out
}

func domainsWithCounts(db *gorm.DB, domains []models.Domain) []domainDTO {
	if len(domains) == 0 {
		return nil
	}
	countMap := messageCountsByDomain(db, domains)
	out := make([]domainDTO, 0, len(domains))
	for _, d := range domains {
		dto := domainDTO{Domain: d, MessageCount: countMap[d.Domain]}
		out = append(out, dto)
	}
	return out
}

func domainsWithCountsOrEmpty(db *gorm.DB, domains []models.Domain) []domainDTO {
	if len(domains) == 0 {
		return []domainDTO{}
	}
	return domainsWithCounts(db, domains)
}

func domainWithCount(db *gorm.DB, d models.Domain) domainDTO {
	var count int64
	db.Model(&models.Message{}).Where("root_domain = ?", d.Domain).Count(&count)
	dto := domainDTO{Domain: d, MessageCount: count}
	return dto
}
