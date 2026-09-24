package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"gptmail/internal/models"

	"github.com/gin-gonic/gin"
)

func (h *Handler) listAPIKeys(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	var keys []models.APIKey
	query := h.DB.Where("owner_id = ?", user.ID).Order("created_at desc")
	if err := query.Find(&keys).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, keys)
}

func (h *Handler) createAPIKey(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	var input struct {
		Name       string `json:"name"`
		DailyLimit *int64 `json:"daily_limit"`
		TotalLimit *int64 `json:"total_limit"`
		ExpiresAt  string `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid json")
		return
	}
	dailyLimit := h.Config.APIKeyDefaultDailyCap
	if input.DailyLimit != nil {
		dailyLimit = *input.DailyLimit
	}
	totalLimit := int64(0)
	if input.TotalLimit != nil {
		totalLimit = *input.TotalLimit
	}
	if dailyLimit < 0 || totalLimit < 0 {
		fail(c, http.StatusBadRequest, "quota limits must be zero or greater")
		return
	}
	expiresAt, err := parseAPIKeyExpiresAt(input.ExpiresAt)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ownerID := &user.ID
	key, plain, err := h.APIKeys.CreateFor(ownerID, input.Name, dailyLimit, totalLimit, expiresAt)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("api_key.create", actor(c), key.KeyPrefix, "")
	created(c, gin.H{"api_key": key, "plain_key": plain})
}

func (h *Handler) patchAPIKey(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	var key models.APIKey
	if err := h.DB.First(&key, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "api key not found")
		return
	}
	if key.OwnerID == nil || *key.OwnerID != user.ID {
		fail(c, http.StatusForbidden, "api key access denied")
		return
	}
	var input struct {
		Enabled    *bool  `json:"enabled"`
		Name       string `json:"name"`
		DailyLimit *int64 `json:"daily_limit"`
		TotalLimit *int64 `json:"total_limit"`
		ExpiresAt  string `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid json")
		return
	}
	if input.Enabled != nil {
		key.Enabled = *input.Enabled
	}
	if strings.TrimSpace(input.Name) != "" {
		key.Name = strings.TrimSpace(input.Name)
	}
	if input.DailyLimit != nil {
		if *input.DailyLimit < 0 {
			fail(c, http.StatusBadRequest, "daily_limit must be zero or greater")
			return
		}
		key.DailyLimit = *input.DailyLimit
	}
	if input.TotalLimit != nil {
		if *input.TotalLimit < 0 {
			fail(c, http.StatusBadRequest, "total_limit must be zero or greater")
			return
		}
		key.TotalLimit = *input.TotalLimit
	}
	if strings.TrimSpace(input.ExpiresAt) != "" {
		expiresAt, err := parseAPIKeyExpiresAt(input.ExpiresAt)
		if err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		key.ExpiresAt = expiresAt
	}
	if err := h.DB.Save(&key).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("api_key.patch", actor(c), key.KeyPrefix, "")
	ok(c, key)
}

func (h *Handler) deleteAPIKey(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	var key models.APIKey
	if err := h.DB.First(&key, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "api key not found")
		return
	}
	if key.OwnerID == nil || *key.OwnerID != user.ID {
		fail(c, http.StatusForbidden, "api key access denied")
		return
	}
	if err := h.DB.Delete(&key).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("api_key.delete", actor(c), key.KeyPrefix, "")
	ok(c, gin.H{"deleted": true})
}

func (h *Handler) revealAPIKey(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	if currentAPIKey(c) != nil {
		fail(c, http.StatusForbidden, "api key auth not allowed for this endpoint")
		return
	}
	var key models.APIKey
	if err := h.DB.First(&key, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "api key not found")
		return
	}
	if key.OwnerID == nil || *key.OwnerID != user.ID {
		fail(c, http.StatusForbidden, "api key access denied")
		return
	}
	h.audit("api_key.reveal", actor(c), key.KeyPrefix, "")
	ok(c, gin.H{"plain_key": key.KeyValue})
}

func parseAPIKeyExpiresAt(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("expires_at must be an RFC3339 time")
	}
	if !parsed.After(time.Now()) {
		return nil, fmt.Errorf("expires_at must be in the future")
	}
	return &parsed, nil
}
