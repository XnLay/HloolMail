package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gptmail/internal/mailhtml"
	"gptmail/internal/mailstore"
	"gptmail/internal/models"
	"gptmail/internal/webhook"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *Handler) listEmails(c *gin.Context) {
	parts, d, allowed := h.authorizeInbox(c, c.Query("email"))
	if !allowed {
		return
	}
	if currentAPIKey(c) == nil && !h.consumeUserQuota(c) {
		return
	}
	messageQuery, err := h.scopeInboxMessages(h.DB.Model(&models.Message{}), h.currentActor(c), parts.Recipient, d)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	paged := c.Query("page") != "" || c.Query("per_page") != ""
	if paged {
		page := parsePage(c.Query("page"))
		perPage := parseLimit(c.Query("per_page"), 10, 100)
		var total int64
		if err := messageQuery.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		totalPages := pageCount(total, perPage)
		if page > totalPages {
			page = totalPages
		}
		var messages []models.Message
		if err := messageQuery.Session(&gorm.Session{}).
			Order("created_at desc").
			Limit(perPage).
			Offset((page - 1) * perPage).
			Find(&messages).Error; err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		summaries, err := h.messageSummariesWithAttachmentCounts(messages)
		if err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		ok(c, paginatedResponse[messageSummary]{
			Items:      summaries,
			Page:       page,
			PerPage:    perPage,
			Total:      total,
			TotalPages: totalPages,
		})
		return
	}
	limit := parseLimit(c.Query("limit"), 50, 200)
	var messages []models.Message
	if err := messageQuery.
		Order("created_at desc").
		Limit(limit).
		Find(&messages).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	summaries, err := h.messageSummariesWithAttachmentCounts(messages)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, summaries)
}

func (h *Handler) nextEmail(c *gin.Context) {
	parts, d, allowed := h.authorizeInbox(c, c.Query("email"))
	if !allowed {
		return
	}
	if currentAPIKey(c) == nil && !h.consumeUserQuota(c) {
		return
	}
	messageQuery, err := h.scopeInboxMessages(h.DB.Model(&models.Message{}), h.currentActor(c), parts.Recipient, d)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	for attempt := 0; attempt < 5; attempt++ {
		var msg models.Message
		err := messageQuery.Session(&gorm.Session{}).Where("seen = ?", false).
			Order("created_at desc").
			First(&msg).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ok(c, gin.H{"has_email": false, "message": nil})
			return
		}
		if err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		result := h.DB.Model(&models.Message{}).
			Where("id = ? AND seen = ?", msg.ID, false).
			Update("seen", true)
		if result.Error != nil {
			fail(c, http.StatusInternalServerError, result.Error.Error())
			return
		}
		if result.RowsAffected == 0 {
			continue
		}
		msg.Seen = true
		msg.HTMLContent = mailhtml.Sanitize(msg.HTMLContent)
		attachments, err := h.attachmentMetadataForMessage(msg.ID)
		if err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		ok(c, gin.H{"has_email": true, "message": nextEmailMessageDTO{
			Message:         msg,
			AttachmentCount: int64(len(attachments)),
			Attachments:     attachments,
		}})
		return
	}
	ok(c, gin.H{"has_email": false, "message": nil})
}

func (h *Handler) getEmail(c *gin.Context) {
	var msg models.Message
	if err := h.DB.First(&msg, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "message not found")
		return
	}
	if _, _, allowed := h.authorizeInbox(c, msg.Recipient); !allowed {
		return
	}
	canAccess, err := h.actorCanAccessMessage(h.currentActor(c), msg)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !canAccess {
		fail(c, http.StatusNotFound, "message not found")
		return
	}
	msg.HTMLContent = mailhtml.Sanitize(msg.HTMLContent)
	attachments, err := h.attachmentMetadataForMessage(msg.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if currentAPIKey(c) != nil {
		publicOK(c, publicMessageDetail(msg, attachments))
		return
	}
	webOK(c, webMessageDetail(msg, attachments))
}

func (h *Handler) markEmailRead(c *gin.Context) {
	var msg models.Message
	if err := h.DB.First(&msg, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "message not found")
		return
	}
	if _, _, allowed := h.authorizeInbox(c, msg.Recipient); !allowed {
		return
	}
	canAccess, err := h.actorCanAccessMessage(h.currentActor(c), msg)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !canAccess {
		fail(c, http.StatusNotFound, "message not found")
		return
	}
	if err := h.DB.Model(&msg).Update("seen", true).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": msg.ID, "seen": true})
}

func (h *Handler) deleteEmail(c *gin.Context) {
	var msg models.Message
	if err := h.DB.First(&msg, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "message not found")
		return
	}
	if _, _, allowed := h.authorizeInbox(c, msg.Recipient); !allowed {
		return
	}
	canAccess, err := h.actorCanAccessMessage(h.currentActor(c), msg)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !canAccess {
		fail(c, http.StatusNotFound, "message not found")
		return
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		messageQuery := tx.Model(&models.Message{}).Where("id = ?", msg.ID)
		_, err := mailstore.DeleteMessages(tx, messageQuery, time.Now().UTC(), webhook.RedactionReasonMessageDeleted)
		return err
	}); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (h *Handler) clearEmails(c *gin.Context) {
	parts, d, allowed := h.authorizeInbox(c, c.Query("email"))
	if !allowed {
		return
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		messageQuery, err := h.scopeInboxMessages(tx.Model(&models.Message{}), h.currentActor(c), parts.Recipient, d)
		if err != nil {
			return err
		}
		_, err = mailstore.DeleteMessages(tx, messageQuery, time.Now().UTC(), webhook.RedactionReasonMessageDeleted)
		return err
	}); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"cleared": true})
}

func (h *Handler) inboxStream(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	if !h.requireSameOriginSessionRead(c) {
		return
	}
	parts, _, allowed := h.authorizeInboxForUser(c, c.Query("email"), user)
	if !allowed {
		return
	}
	if h.Hub == nil {
		fail(c, http.StatusServiceUnavailable, "inbox stream unavailable")
		return
	}
	ch, cancel := h.Hub.Subscribe(parts.Recipient)
	defer cancel()
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		fail(c, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	fmt.Fprint(c.Writer, ": connected\n\n")
	flusher.Flush()
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(c.Writer, ": heartbeat\n\n")
			flusher.Flush()
		case event := <-ch:
			c.SSEvent("message", event)
			flusher.Flush()
		}
	}
}

func stripTags(value string) string {
	var builder strings.Builder
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				builder.WriteRune(r)
			}
		}
	}
	return builder.String()
}
