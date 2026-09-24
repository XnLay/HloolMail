package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	domaindb "gptmail/internal/domain"
	"gptmail/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type generateEmailRequest struct {
	Prefix      string          `json:"prefix"`
	Domain      string          `json:"domain"`
	AddressType string          `json:"address_type"`
	Subdomain   string          `json:"subdomain"`
	Share       json.RawMessage `json:"share"`
}

type generateEmailShareOptions struct {
	Enabled   bool
	ExpiresAt *time.Time
}

type generateEmailShareDTO struct {
	ID           uint       `json:"id"`
	ResourceType string     `json:"resource_type"`
	Token        string     `json:"token,omitempty"`
	Key          string     `json:"key,omitempty"`
	TokenPrefix  string     `json:"token_prefix"`
	URL          string     `json:"url"`
	AccessURL    string     `json:"access_url"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

const (
	generateEmailAddressTypeRoot      = "root"
	generateEmailAddressTypeSubdomain = "subdomain"
)

type generateEmailTarget struct {
	Domain          *models.Domain
	Host            string
	Subdomain       string
	AddressType     string
	RequireWildcard bool
}

func (h *Handler) generateEmail(c *gin.Context) {
	actor, allowed := h.requireActor(c)
	if !allowed {
		return
	}
	if currentAPIKey(c) == nil && !h.consumeUserQuota(c) {
		return
	}
	var input generateEmailRequest
	if err := c.ShouldBindJSON(&input); err != nil && !errors.Is(err, io.EOF) {
		fail(c, http.StatusBadRequest, "invalid json")
		return
	}
	shareOptions, err := parseGenerateEmailShareOptions(input.Share)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	target, err := h.generateEmailTarget(input, actor)
	if err != nil {
		var httpErr httpStatusError
		if errors.As(err, &httpErr) {
			fail(c, httpErr.Status, httpErr.Message)
			return
		}
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	d := target.Domain
	ownerID, err := h.mailboxOwnerID(actor)
	if err != nil {
		fail(c, http.StatusForbidden, err.Error())
		return
	}
	local := sanitizeLocal(input.Prefix)
	maxRetries := 10
	if local == "" {
		maxRetries = 10
	}
	attempt := 0
	for {
		if local == "" {
			local = randomLocal()
		}
		email := local + "@" + target.Host
		host := target.Host
		var existing models.Mailbox
		err := h.DB.Where("email = ?", email).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			shareDraft, err := newPendingMailboxShare(shareOptions)
			if err != nil {
				fail(c, http.StatusInternalServerError, err.Error())
				return
			}
			mailbox, link, err := h.createMailboxWithAccounting(ownerID, d, email, local, host, target.RequireWildcard, actor, shareDraft)
			if err != nil {
				if isUniqueConstraintError(err) {
					attempt++
					if attempt >= maxRetries {
						fail(c, http.StatusConflict, "email address already in use; change the prefix or generate a random address")
						return
					}
					local = ""
					continue
				}
				var httpErr httpStatusError
				if errors.As(err, &httpErr) {
					fail(c, httpErr.Status, httpErr.Message)
					return
				}
				fail(c, http.StatusInternalServerError, err.Error())
				return
			}
			h.audit("mailbox.create", actor.name(), email, "")
			created(c, h.generateEmailResponse(c, mailbox, d, false, link, shareDraft))
			return
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) && err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		if existing.OwnerID == ownerID && existing.DomainID == d.ID {
			var link *models.ShareLink
			var token string
			var accessKey string
			if shareOptions.Enabled {
				createdLink, createdToken, createdAccessKey, err := h.createMailboxShare(existing, shareOptions.ExpiresAt)
				if err != nil {
					fail(c, http.StatusInternalServerError, err.Error())
					return
				}
				link = createdLink
				token = createdToken
				accessKey = createdAccessKey
			}
			h.audit("mailbox.reuse", actor.name(), email, "")
			ok(c, h.generateEmailResponseWithShare(c, existing, d, true, link, token, accessKey))
			return
		}
		attempt++
		if attempt >= maxRetries {
			fail(c, http.StatusConflict, "邮箱地址已被占用，请更换前缀或先随机生成")
			return
		}
		local = ""
	}
}

func (h *Handler) generateEmailTarget(input generateEmailRequest, actor *requestActor) (generateEmailTarget, error) {
	addressType := strings.ToLower(strings.TrimSpace(input.AddressType))
	hasSubdomain := strings.TrimSpace(input.Subdomain) != ""
	wantsSubdomain := domainWantsWildcard(input.Domain)
	switch addressType {
	case "":
		wantsSubdomain = wantsSubdomain || hasSubdomain
	case generateEmailAddressTypeRoot:
		if wantsSubdomain {
			return generateEmailTarget{}, fmt.Errorf("wildcard domain input requires address_type=subdomain")
		}
		if hasSubdomain {
			return generateEmailTarget{}, fmt.Errorf("subdomain requires address_type=subdomain")
		}
	case generateEmailAddressTypeSubdomain, "wildcard":
		wantsSubdomain = true
	default:
		return generateEmailTarget{}, fmt.Errorf("address_type must be root or subdomain")
	}

	if !wantsSubdomain {
		d, err := h.selectDomainForActor(input.Domain, actor)
		if err != nil {
			return generateEmailTarget{}, err
		}
		return generateEmailTarget{
			Domain:      d,
			Host:        d.Domain,
			AddressType: generateEmailAddressTypeRoot,
		}, nil
	}

	d, err := h.selectWildcardDomainForActor(input.Domain, actor)
	if err != nil {
		return generateEmailTarget{}, err
	}
	subdomain := sanitizeSubdomainLabel(input.Subdomain)
	if strings.TrimSpace(input.Subdomain) != "" && subdomain == "" {
		return generateEmailTarget{}, fmt.Errorf("valid subdomain label required")
	}
	if subdomain == "" {
		subdomain = randomLocal()
	}
	host := subdomain + "." + d.Domain
	if err := h.ensureSubdomainHostAvailable(host, d); err != nil {
		return generateEmailTarget{}, err
	}
	return generateEmailTarget{
		Domain:          d,
		Host:            host,
		Subdomain:       subdomain,
		AddressType:     generateEmailAddressTypeSubdomain,
		RequireWildcard: true,
	}, nil
}

func sanitizeSubdomainLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, ".-")
	if value == "" || strings.Contains(value, ".") {
		return ""
	}
	var builder strings.Builder
	for _, r := range strings.Trim(value, "-") {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-':
			builder.WriteRune(r)
		}
	}
	label := strings.Trim(builder.String(), "-")
	if len(label) > 63 {
		return ""
	}
	return label
}

func (h *Handler) ensureSubdomainHostAvailable(host string, parent *models.Domain) error {
	return ensureSubdomainHostAvailable(h.DB, host, parent)
}

func ensureSubdomainHostAvailable(db *gorm.DB, host string, parent *models.Domain) error {
	host = domaindb.NormalizeDomain(host)
	if host == "" || parent == nil || strings.EqualFold(host, parent.Domain) {
		return nil
	}
	for _, candidate := range registeredSubdomainBoundaryCandidates(host, parent.Domain) {
		var existing models.Domain
		err := db.Where("domain = ?", candidate).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if existing.ID != parent.ID {
			return httpStatusError{Status: http.StatusConflict, Message: "subdomain is already registered as a domain; choose another subdomain"}
		}
	}
	return nil
}

func registeredSubdomainBoundaryCandidates(host, parent string) []string {
	host = domaindb.NormalizeDomain(host)
	parent = domaindb.NormalizeDomain(parent)
	if host == "" || parent == "" || strings.EqualFold(host, parent) || !strings.HasSuffix(host, "."+parent) {
		return nil
	}
	out := []string{host}
	current := host
	for strings.HasSuffix(current, "."+parent) {
		dot := strings.Index(current, ".")
		if dot < 0 {
			break
		}
		current = current[dot+1:]
		if current == parent {
			break
		}
		out = append(out, current)
	}
	return out
}

func parseGenerateEmailShareOptions(raw json.RawMessage) (generateEmailShareOptions, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return generateEmailShareOptions{}, nil
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		return generateEmailShareOptions{Enabled: enabled}, nil
	}
	var input struct {
		Enabled   *bool  `json:"enabled"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return generateEmailShareOptions{}, fmt.Errorf("share must be a boolean or object")
	}
	enabled = true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	expiresAt, err := parseShareExpiresAt(input.ExpiresAt, true)
	if err != nil {
		return generateEmailShareOptions{}, err
	}
	return generateEmailShareOptions{Enabled: enabled, ExpiresAt: expiresAt}, nil
}

type pendingMailboxShare struct {
	Token         string
	AccessKey     string
	TokenHash     string
	TokenPrefix   string
	AccessKeyHash string
	ExpiresAt     *time.Time
}

func newPendingMailboxShare(options generateEmailShareOptions) (*pendingMailboxShare, error) {
	if !options.Enabled {
		return nil, nil
	}
	token, prefix, tokenHash, err := newShareToken()
	if err != nil {
		return nil, err
	}
	accessKey, accessKeyHash, err := newShareAccessKey()
	if err != nil {
		return nil, err
	}
	return &pendingMailboxShare{
		Token:         token,
		AccessKey:     accessKey,
		TokenHash:     tokenHash,
		TokenPrefix:   prefix,
		AccessKeyHash: accessKeyHash,
		ExpiresAt:     options.ExpiresAt,
	}, nil
}

func (h *Handler) generateEmailResponse(c *gin.Context, mailbox models.Mailbox, domain *models.Domain, reuse bool, link *models.ShareLink, share *pendingMailboxShare) gin.H {
	token := ""
	accessKey := ""
	if share != nil {
		token = share.Token
		accessKey = share.AccessKey
	}
	return h.generateEmailResponseWithShare(c, mailbox, domain, reuse, link, token, accessKey)
}

func (h *Handler) generateEmailResponseWithShare(c *gin.Context, mailbox models.Mailbox, domain *models.Domain, reuse bool, link *models.ShareLink, token, accessKey string) gin.H {
	subdomain := yydsSubdomainForHost(mailbox.Host, domain.Domain)
	addressType := generateEmailAddressTypeRoot
	if subdomain != "" {
		addressType = generateEmailAddressTypeSubdomain
	}
	out := gin.H{
		"email":        mailbox.Email,
		"domain_id":    domain.ID,
		"domain":       domain,
		"local_part":   mailbox.LocalPart,
		"host":         mailbox.Host,
		"root_domain":  domain.Domain,
		"address_type": addressType,
		"subdomain":    subdomain,
	}
	if subdomain != "" {
		out["wildcard_pattern"] = "*." + domain.Domain
	}
	if reuse {
		out["reuse"] = true
	}
	if link != nil {
		out["share"] = h.generateEmailShareDTO(c, *link, token, accessKey)
	}
	return out
}

func (h *Handler) generateEmailShareDTO(c *gin.Context, link models.ShareLink, token, accessKey string) generateEmailShareDTO {
	shareURL := h.shareWebURL(c, token)
	return generateEmailShareDTO{
		ID:           link.ID,
		ResourceType: link.ResourceType,
		Token:        token,
		Key:          accessKey,
		TokenPrefix:  link.TokenPrefix,
		URL:          shareURL,
		AccessURL:    shareAccessURL(shareURL, accessKey),
		ExpiresAt:    link.ExpiresAt,
	}
}

func sanitizeLocal(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		}
	}
	return strings.Trim(builder.String(), ".-_")
}

func randomLocal() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "mail" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "mail" + hex.EncodeToString(buf)
}

func randomIndex(length int) int {
	if length <= 1 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(length)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(length))
	}
	return int(n.Int64())
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") || strings.Contains(text, "duplicate")
}
