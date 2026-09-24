package httpapi

import (
	"fmt"
	"net/http"

	domaindb "gptmail/internal/domain"
	"gptmail/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type requestActor struct {
	User   *models.User
	APIKey *models.APIKey
	Global bool
}

func (h *Handler) currentActor(c *gin.Context) *requestActor {
	if key := currentAPIKey(c); key != nil {
		return &requestActor{
			User:   currentAPIKeyUser(c),
			APIKey: key,
			Global: key.OwnerID == nil,
		}
	}
	if user := currentUser(c); user != nil {
		return &requestActor{User: user}
	}
	return nil
}

func (h *Handler) requireActor(c *gin.Context) (*requestActor, bool) {
	actor := h.currentActor(c)
	if actor == nil {
		fail(c, http.StatusUnauthorized, "login or api key required")
		return nil, false
	}
	return actor, true
}

func (a *requestActor) isAdmin() bool {
	return a != nil && a.APIKey == nil && a.User != nil && a.User.Role == models.UserRoleAdmin
}

func (a *requestActor) ownerID() (uint, bool) {
	if a == nil || a.User == nil {
		return 0, false
	}
	return a.User.ID, true
}

func (a *requestActor) name() string {
	if a == nil {
		return "system"
	}
	if a.User != nil {
		return a.User.Email
	}
	if a.APIKey != nil {
		return a.APIKey.KeyPrefix
	}
	return "system"
}

func (h *Handler) scopeMessages(actor *requestActor) *gorm.DB {
	query := h.DB.Model(&models.Message{})
	if actor == nil {
		return query.Where("1 = 0")
	}
	ownerID, ok := actor.ownerID()
	if !ok {
		return query.Where("1 = 0")
	}
	return h.scopeOwnedMessages(query, ownerID)
}

func (h *Handler) scopeDomains(actor *requestActor) *gorm.DB {
	query := h.DB.Model(&models.Domain{})
	if actor == nil {
		return query.Where("1 = 0")
	}
	ownerID, ok := actor.ownerID()
	if !ok {
		return query.Where("1 = 0")
	}
	return query.Where("owner_id = ?", ownerID)
}

func (h *Handler) scopeAPIKeys(actor *requestActor) *gorm.DB {
	query := h.DB.Model(&models.APIKey{})
	if actor == nil {
		return query.Where("1 = 0")
	}
	ownerID, ok := actor.ownerID()
	if !ok {
		return query.Where("1 = 0")
	}
	return query.Where("owner_id = ?", ownerID)
}

func (h *Handler) scopeAPIUsage(actor *requestActor) *gorm.DB {
	query := h.DB.Model(&models.APIUsageLog{})
	if actor == nil {
		return query.Where("1 = 0")
	}
	if actor.APIKey != nil {
		return query.Where("api_key_id = ?", actor.APIKey.ID)
	}
	ownerID, ok := actor.ownerID()
	if !ok {
		return query.Where("1 = 0")
	}
	return query.Where("user_id = ?", ownerID)
}

func (h *Handler) authorizeInbox(c *gin.Context, email string) (domaindb.RecipientParts, *models.Domain, bool) {
	parts, err := domaindb.NormalizeRecipient(email)
	if err != nil {
		fail(c, http.StatusBadRequest, "valid email required")
		return parts, nil, false
	}
	actor, allowed := h.requireActor(c)
	if !allowed {
		return parts, nil, false
	}
	d, err := h.Resolver.ResolveDomain(parts.Recipient)
	if err != nil {
		fail(c, http.StatusNotFound, "domain not found or not verified")
		return parts, nil, false
	}
	allowedOwner, err := h.actorOwnsMessageRecipient(actor, parts.Recipient, d)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return parts, d, false
	}
	if allowedOwner {
		return parts, d, true
	}
	fail(c, http.StatusForbidden, "邮箱访问被拒：需要是邮箱所有者、域名所有者或使用有权限的 API key")
	return parts, d, false
}

func (h *Handler) authorizeInboxForUser(c *gin.Context, email string, user *models.User) (domaindb.RecipientParts, *models.Domain, bool) {
	parts, err := domaindb.NormalizeRecipient(email)
	if err != nil {
		fail(c, http.StatusBadRequest, "valid email required")
		return parts, nil, false
	}
	if user == nil {
		fail(c, http.StatusUnauthorized, "login required")
		return parts, nil, false
	}
	d, err := h.Resolver.ResolveDomain(parts.Recipient)
	if err != nil {
		fail(c, http.StatusNotFound, "domain not found or not verified")
		return parts, nil, false
	}
	allowedOwner, err := h.userOwnsMessageRecipient(user, parts.Recipient, d)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return parts, d, false
	}
	if allowedOwner {
		return parts, d, true
	}
	fail(c, http.StatusForbidden, "邮箱访问被拒：需要是邮箱所有者或域名所有者")
	return parts, d, false
}

func (h *Handler) mailboxOwnerID(actor *requestActor) (uint, error) {
	if ownerID, ok := actor.ownerID(); ok {
		return ownerID, nil
	}
	if actor != nil && actor.Global {
		var admin models.User
		if err := h.DB.Where("role = ? AND enabled = ?", models.UserRoleAdmin, true).Order("id asc").First(&admin).Error; err == nil {
			return admin.ID, nil
		}
	}
	return 0, fmt.Errorf("api key must be bound to an active user to create mailboxes")
}
