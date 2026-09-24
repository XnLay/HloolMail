package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"gptmail/internal/mailstore"
	"gptmail/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *Handler) listMailboxes(c *gin.Context) {
	actor, allowed := h.requireActor(c)
	if !allowed {
		return
	}
	var mailboxes []models.Mailbox
	query := h.DB.Model(&models.Mailbox{})
	ownerID, hasOwner := actor.ownerID()
	if !hasOwner {
		fail(c, http.StatusForbidden, "api key must be bound to an active user")
		return
	}
	query = query.Where("owner_id = ?", ownerID)
	search := strings.ToLower(strings.TrimSpace(c.Query("q")))
	if search != "" {
		like := likeContainsLiteral(search)
		query = query.Where("LOWER(email) LIKE ?"+likeEscapeClause+" OR LOWER(local_part) LIKE ?"+likeEscapeClause+" OR LOWER(host) LIKE ?"+likeEscapeClause, like, like, like)
	}
	paged := c.Query("page") != "" || c.Query("per_page") != "" || search != ""
	page := parsePage(c.Query("page"))
	perPage := parseLimit(c.Query("per_page"), 10, 50)
	total := int64(0)
	totalPages := 1
	if paged {
		if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		totalPages = pageCount(total, perPage)
		if page > totalPages {
			page = totalPages
		}
		query = query.Limit(perPage).Offset((page - 1) * perPage)
	}
	if err := query.Order("created_at desc").Find(&mailboxes).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	stats, err := mailboxMessageStats(h.DB, mailboxes)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]mailboxWithCount, 0, len(mailboxes))
	for _, m := range mailboxes {
		stat := stats[m.ID]
		out = append(out, mailboxWithCount{
			Mailbox:       m,
			MessageCount:  stat.MessageCount,
			LastMessageAt: stat.LastMessageAt,
		})
	}
	if paged {
		ok(c, paginatedResponse[mailboxWithCount]{
			Items:      out,
			Page:       page,
			PerPage:    perPage,
			Total:      total,
			TotalPages: totalPages,
		})
		return
	}
	ok(c, out)
}

type mailboxWithCount struct {
	models.Mailbox
	MessageCount  int64      `json:"message_count"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
}

type mailboxMessageStat struct {
	MessageCount  int64
	LastMessageAt *time.Time
}

func mailboxMessageStats(db *gorm.DB, mailboxes []models.Mailbox) (map[uint]mailboxMessageStat, error) {
	out := make(map[uint]mailboxMessageStat, len(mailboxes))
	if len(mailboxes) == 0 {
		return out, nil
	}
	ids := make([]uint, 0, len(mailboxes))
	for _, mailbox := range mailboxes {
		ids = append(ids, mailbox.ID)
	}
	type aggregate struct {
		MailboxID    uint
		MessageCount int64
	}
	var rows []aggregate
	if err := db.Model(&models.Message{}).
		Select("mailbox_id, COUNT(*) AS message_count").
		Where("mailbox_id IN ?", ids).
		Group("mailbox_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.MailboxID] = mailboxMessageStat{
			MessageCount: row.MessageCount,
		}
	}
	type latestRow struct {
		MailboxID uint
		CreatedAt time.Time
	}
	var latestRows []latestRow
	if err := db.Raw(`
		SELECT mailbox_id, created_at
		FROM (
			SELECT mailbox_id, created_at,
				ROW_NUMBER() OVER (PARTITION BY mailbox_id ORDER BY created_at DESC, id DESC) AS row_num
			FROM messages
			WHERE mailbox_id IN ? AND deleted_at IS NULL
		) ranked
		WHERE row_num = 1
	`, ids).Scan(&latestRows).Error; err != nil {
		return nil, err
	}
	for _, row := range latestRows {
		stat := out[row.MailboxID]
		createdAt := row.CreatedAt
		stat.LastMessageAt = &createdAt
		out[row.MailboxID] = stat
	}
	return out, nil
}

func (h *Handler) deleteMailbox(c *gin.Context) {
	actor, allowed := h.requireActor(c)
	if !allowed {
		return
	}
	var mailbox models.Mailbox
	if err := h.DB.First(&mailbox, "id = ?", c.Param("id")).Error; err != nil {
		fail(c, http.StatusNotFound, "mailbox not found")
		return
	}
	ownerID, hasOwner := actor.ownerID()
	if !hasOwner || mailbox.OwnerID != ownerID {
		fail(c, http.StatusForbidden, "mailbox access denied")
		return
	}
	messagesDeleted, err := mailstore.DeleteMailbox(h.DB, mailbox.OwnerID, mailbox.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, http.StatusNotFound, "mailbox not found")
		} else {
			fail(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	h.audit("mailbox.delete", actor.name(), mailbox.Email, "")
	ok(c, gin.H{"deleted": true, "messages_deleted": messagesDeleted})
}
