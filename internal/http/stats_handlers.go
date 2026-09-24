package httpapi

import (
	"net/http"
	"time"

	"gptmail/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *Handler) stats(c *gin.Context) {
	actor, allowed := h.requireActor(c)
	if !allowed {
		return
	}
	scope := h.scopeMessages(actor)
	domainScope := h.scopeDomains(actor)
	var messages, domains, apiKeys, mailboxes, publicDomains, apiUsageToday int64
	scope.Count(&messages)
	domainScope.Count(&domains)
	publicMailboxCapableDomainQuery(domainScope.Session(&gorm.Session{})).Count(&publicDomains)
	h.scopeAPIKeys(actor).Count(&apiKeys)
	scope.Session(&gorm.Session{}).Distinct("recipient").Count(&mailboxes)
	h.scopeAPIUsage(actor).Where("created_at >= ?", startOfDay(time.Now())).Count(&apiUsageToday)
	data := gin.H{
		"messages":        messages,
		"domains":         domains,
		"api_keys":        apiKeys,
		"mailboxes":       mailboxes,
		"public_domains":  publicDomains,
		"api_calls_today": apiUsageToday,
	}
	if currentAPIKey(c) == nil {
		var visibleDomains []models.Domain
		if err := domainScope.Session(&gorm.Session{}).Order("domain asc").Find(&visibleDomains).Error; err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		data["domain_list"] = availableDomainDTOsWithCountsOrEmpty(h.DB, visibleDomains)
		webOK(c, data)
		return
	}
	publicOK(c, data)
}

func (h *Handler) statsTimeseries(c *gin.Context) {
	user, loggedIn := h.requireLogin(c)
	if !loggedIn {
		return
	}
	actor := &requestActor{User: user}
	days := parseLimit(c.Query("days"), 30, 90)
	if days < 2 {
		days = 7
	}
	ok(c, h.statsTimeseriesData(
		days,
		h.scopeMessages(actor),
		h.scopeAPIUsage(actor),
		h.scopeDomains(actor),
	))
}

func (h *Handler) statsTimeseriesData(days int, messageScope, apiScope, domainScope *gorm.DB) gin.H {
	now := time.Now()
	today := startOfDay(now)
	labels := make([]string, days)
	messages := make([]int64, days)
	apiCalls := make([]int64, days)
	domains := make([]int64, days)
	earliest := today.AddDate(0, 0, -(days - 1))

	var totalDomainsBeforeWindow int64
	domainScope.Session(&gorm.Session{}).Where("created_at < ?", earliest).Count(&totalDomainsBeforeWindow)
	runningDomains := totalDomainsBeforeWindow

	for i := 0; i < days; i++ {
		dayStart := earliest.AddDate(0, 0, i)
		dayEnd := dayStart.AddDate(0, 0, 1)
		labels[i] = dayStart.Format("2006-01-02")
		var msgCount, apiCount, domainAdded int64
		messageScope.Session(&gorm.Session{}).Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Count(&msgCount)
		apiScope.Session(&gorm.Session{}).Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Count(&apiCount)
		domainScope.Session(&gorm.Session{}).Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Count(&domainAdded)
		runningDomains += domainAdded
		messages[i] = msgCount
		apiCalls[i] = apiCount
		domains[i] = runningDomains
	}
	return gin.H{
		"days":      labels,
		"messages":  messages,
		"domains":   domains,
		"api_calls": apiCalls,
	}
}

func (h *Handler) adminStats(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	now := time.Now()
	today := startOfDay(now)
	var totalMessages, mailboxes, publicMailboxes, privateMailboxes, activeDomains, failedDomains, pendingDomains, totalDomains, usageToday int64
	var publicDomains, privateDomains, inactiveDomains, verifiedDomains, expiringDomains, expiredDomains int64
	var users, adminUsers, regularUsers, enabledUsers, disabledUsers, apiKeys, activeAPIKeys, disabledAPIKeys, staleDomains int64
	h.DB.Model(&models.Message{}).Count(&totalMessages)
	h.DB.Model(&models.Mailbox{}).Count(&mailboxes)
	h.DB.Model(&models.Mailbox{}).
		Joins("JOIN domains ON domains.id = mailboxes.domain_id").
		Where("domains.mode = ?", models.DomainModePublic).
		Count(&publicMailboxes)
	h.DB.Model(&models.Mailbox{}).
		Joins("JOIN domains ON domains.id = mailboxes.domain_id").
		Where("domains.mode = ?", models.DomainModePrivate).
		Count(&privateMailboxes)
	h.DB.Model(&models.Domain{}).Count(&totalDomains)
	h.DB.Model(&models.Domain{}).Where("mode = ?", models.DomainModePublic).Count(&publicDomains)
	h.DB.Model(&models.Domain{}).Where("mode = ?", models.DomainModePrivate).Count(&privateDomains)
	h.DB.Model(&models.Domain{}).Where("active = ?", true).Count(&activeDomains)
	h.DB.Model(&models.Domain{}).Where("active = ?", false).Count(&inactiveDomains)
	h.DB.Model(&models.Domain{}).Where("mx_verified = ?", true).Count(&verifiedDomains)
	h.DB.Model(&models.Domain{}).Where("active = ? AND mx_verified = ?", true, false).Count(&failedDomains)
	h.DB.Model(&models.Domain{}).Where("active = ? AND first_verified_at IS NULL AND pending_delete_at IS NOT NULL", true).Count(&pendingDomains)
	h.DB.Model(&models.Domain{}).Where("last_mx_check_at IS NULL OR last_mx_check_at < ?", now.Add(-24*time.Hour)).Count(&staleDomains)
	h.DB.Model(&models.Domain{}).Where("domain_expires_at IS NOT NULL AND domain_expires_at >= ? AND domain_expires_at < ?", now, now.AddDate(0, 0, 30)).Count(&expiringDomains)
	h.DB.Model(&models.Domain{}).Where("domain_expires_at IS NOT NULL AND domain_expires_at < ?", now).Count(&expiredDomains)
	h.DB.Model(&models.User{}).Count(&users)
	h.DB.Model(&models.User{}).Where("role = ?", models.UserRoleAdmin).Count(&adminUsers)
	h.DB.Model(&models.User{}).Where("role = ?", models.UserRoleUser).Count(&regularUsers)
	h.DB.Model(&models.User{}).Where("enabled = ?", true).Count(&enabledUsers)
	h.DB.Model(&models.User{}).Where("enabled = ?", false).Count(&disabledUsers)
	h.DB.Model(&models.APIKey{}).Count(&apiKeys)
	h.DB.Model(&models.APIKey{}).Where("enabled = ?", true).Count(&activeAPIKeys)
	h.DB.Model(&models.APIKey{}).Where("enabled = ?", false).Count(&disabledAPIKeys)
	h.DB.Model(&models.APIUsageLog{}).Where("created_at >= ?", today).Count(&usageToday)
	ok(c, gin.H{
		"messages":          totalMessages,
		"mailboxes":         mailboxes,
		"public_mailboxes":  publicMailboxes,
		"private_mailboxes": privateMailboxes,
		"total_domains":     totalDomains,
		"public_domains":    publicDomains,
		"private_domains":   privateDomains,
		"active_domains":    activeDomains,
		"inactive_domains":  inactiveDomains,
		"verified_domains":  verifiedDomains,
		"failed_domains":    failedDomains,
		"pending_domains":   pendingDomains,
		"stale_domains":     staleDomains,
		"expiring_domains":  expiringDomains,
		"expired_domains":   expiredDomains,
		"users":             users,
		"admin_users":       adminUsers,
		"regular_users":     regularUsers,
		"enabled_users":     enabledUsers,
		"disabled_users":    disabledUsers,
		"api_keys":          apiKeys,
		"active_api_keys":   activeAPIKeys,
		"disabled_api_keys": disabledAPIKeys,
		"api_usage_today":   usageToday,
		"growth": gin.H{
			"today":        h.adminCreatedCountsSince(today),
			"last_7_days":  h.adminCreatedCountsSince(today.AddDate(0, 0, -6)),
			"last_30_days": h.adminCreatedCountsSince(today.AddDate(0, 0, -29)),
			"last_90_days": h.adminCreatedCountsSince(today.AddDate(0, 0, -89)),
		},
		"dev_mode":                h.Config.DevMode,
		"admin_token_enabled":     h.Config.AdminToken != "",
		"expected_mx":             h.Config.ExpectedMX,
		"message_retention_hours": int64(h.Config.MessageRetention / time.Hour),
	})
}

func (h *Handler) adminStatsTimeseries(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	days := parseLimit(c.Query("days"), 7, 90)
	if days < 2 {
		days = 7
	}
	ok(c, h.adminStatsTimeseriesData(days))
}

func (h *Handler) adminCreatedCountsSince(since time.Time) gin.H {
	var mailboxes, domains, users, apiCalls int64
	h.DB.Model(&models.Mailbox{}).Where("created_at >= ?", since).Count(&mailboxes)
	h.DB.Model(&models.Domain{}).Where("created_at >= ?", since).Count(&domains)
	h.DB.Model(&models.User{}).Where("created_at >= ?", since).Count(&users)
	h.DB.Model(&models.APIUsageLog{}).Where("created_at >= ?", since).Count(&apiCalls)
	return gin.H{
		"messages":  h.adminMessageCountSince(since),
		"mailboxes": mailboxes,
		"domains":   domains,
		"users":     users,
		"api_calls": apiCalls,
	}
}

func (h *Handler) adminStatsTimeseriesData(days int) gin.H {
	now := time.Now()
	today := startOfDay(now)
	earliest := today.AddDate(0, 0, -(days - 1))
	labels := make([]string, days)
	messages := make([]int64, days)
	messageTotals := make([]int64, days)
	apiCalls := make([]int64, days)
	domains := make([]int64, days)
	newDomains := make([]int64, days)
	mailboxes := make([]int64, days)
	newMailboxes := make([]int64, days)
	users := make([]int64, days)
	newUsers := make([]int64, days)
	messageAdds := h.adminMessageCountsByDay(earliest, days)

	var runningMessages, runningDomains, runningMailboxes, runningUsers int64
	runningMessages = h.adminMessageCountBefore(earliest)
	h.DB.Model(&models.Domain{}).Where("created_at < ?", earliest).Count(&runningDomains)
	h.DB.Model(&models.Mailbox{}).Where("created_at < ?", earliest).Count(&runningMailboxes)
	h.DB.Model(&models.User{}).Where("created_at < ?", earliest).Count(&runningUsers)

	for i := 0; i < days; i++ {
		dayStart := earliest.AddDate(0, 0, i)
		dayEnd := dayStart.AddDate(0, 0, 1)
		labels[i] = dayStart.Format("2006-01-02")

		messageAdded := messageAdds[i]
		var apiCount, domainAdded, mailboxAdded, userAdded int64
		h.DB.Model(&models.APIUsageLog{}).Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Count(&apiCount)
		h.DB.Model(&models.Domain{}).Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Count(&domainAdded)
		h.DB.Model(&models.Mailbox{}).Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Count(&mailboxAdded)
		h.DB.Model(&models.User{}).Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Count(&userAdded)

		runningMessages += messageAdded
		runningDomains += domainAdded
		runningMailboxes += mailboxAdded
		runningUsers += userAdded
		messages[i] = messageAdded
		messageTotals[i] = runningMessages
		apiCalls[i] = apiCount
		newDomains[i] = domainAdded
		domains[i] = runningDomains
		newMailboxes[i] = mailboxAdded
		mailboxes[i] = runningMailboxes
		newUsers[i] = userAdded
		users[i] = runningUsers
	}

	return gin.H{
		"days":           labels,
		"messages":       messages,
		"message_totals": messageTotals,
		"new_messages":   messages,
		"domains":        domains,
		"new_domains":    newDomains,
		"mailboxes":      mailboxes,
		"new_mailboxes":  newMailboxes,
		"users":          users,
		"new_users":      newUsers,
		"api_calls":      apiCalls,
	}
}

func (h *Handler) adminMessageCountSince(since time.Time) int64 {
	start := startOfDay(since)
	today := startOfDay(time.Now())
	days := 1
	for day := start; day.Before(today); day = day.AddDate(0, 0, 1) {
		days++
	}
	counts := h.adminMessageCountsByDay(start, days)
	var total int64
	for _, count := range counts {
		total += count
	}
	return total
}

func (h *Handler) adminMessageCountBefore(cutoff time.Time) int64 {
	dayKey := startOfDay(cutoff).Format("2006-01-02")
	var statTotal int64
	if h.DB.Migrator().HasTable(&models.MessageDailyStat{}) {
		h.DB.Model(&models.MessageDailyStat{}).
			Select("COALESCE(SUM(message_count), 0)").
			Where("day < ?", dayKey).
			Scan(&statTotal)
	}
	var liveTotal int64
	h.DB.Unscoped().Model(&models.Message{}).Where("created_at < ?", cutoff).Count(&liveTotal)
	if liveTotal > statTotal {
		return liveTotal
	}
	return statTotal
}

func (h *Handler) adminMessageCountsByDay(earliest time.Time, days int) []int64 {
	counts := make([]int64, days)
	if days <= 0 {
		return counts
	}
	dayIndex := make(map[string]int, days)
	for i := 0; i < days; i++ {
		dayIndex[earliest.AddDate(0, 0, i).Format("2006-01-02")] = i
	}

	if h.DB.Migrator().HasTable(&models.MessageDailyStat{}) {
		latest := earliest.AddDate(0, 0, days-1).Format("2006-01-02")
		var rows []models.MessageDailyStat
		if err := h.DB.Model(&models.MessageDailyStat{}).
			Where("day >= ? AND day <= ?", earliest.Format("2006-01-02"), latest).
			Find(&rows).Error; err == nil {
			for _, row := range rows {
				if index, ok := dayIndex[row.Day]; ok && row.MessageCount > counts[index] {
					counts[index] = row.MessageCount
				}
			}
		}
	}

	for i := 0; i < days; i++ {
		dayStart := earliest.AddDate(0, 0, i)
		dayEnd := dayStart.AddDate(0, 0, 1)
		var liveCount int64
		h.DB.Unscoped().Model(&models.Message{}).Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Count(&liveCount)
		if liveCount > counts[i] {
			counts[i] = liveCount
		}
	}
	return counts
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
