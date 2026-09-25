package httpapi

import (
	"gptmail/internal/apiratelimit"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type automationRoute struct {
	method  string
	path    string
	profile apiratelimit.Profile
	handler gin.HandlerFunc
}

// 路由注册和策略归属在同一目录声明，避免新增自动化入口时漏挂限流。
func (h *Handler) registerAutomationRoutes(group *gin.RouterGroup, routes []automationRoute) {
	defaults := apiratelimit.DefaultConfig()
	for _, route := range routes {
		rule, ok := apiratelimit.BusinessRule(defaults, route.profile)
		if !ok {
			panic("unknown automation rate limit profile: " + string(route.profile))
		}
		h.apiRateLimitProfiles[route.method+":"+group.BasePath()+route.path] = route.profile
		// 共享 URL 的 Web 会话仍使用固定规则；API Key 已在扣配额前限流。
		group.Handle(route.method, route.path, h.perAPIRateLimit(rate.Limit(rule.RequestsPerSecond), rule.Burst), route.handler)
	}
}

func (h *Handler) nativeAutomationRoutes() []automationRoute {
	return []automationRoute{
		{"GET", "/stats", apiratelimit.Stats, h.stats},
		{"GET", "/emails", apiratelimit.Mail, h.listEmails},
		{"GET", "/emails/next", apiratelimit.Mail, h.nextEmail},
		{"GET", "/email/:id", apiratelimit.Mail, h.getEmail},
		{"PATCH", "/email/:id/read", apiratelimit.Mail, h.markEmailRead},
		{"DELETE", "/email/:id", apiratelimit.Mail, h.deleteEmail},
		{"DELETE", "/emails/clear", apiratelimit.Mail, h.clearEmails},
		{"GET", "/mailboxes", apiratelimit.Mail, h.listMailboxes},
		{"GET", "/mailboxes/stats", apiratelimit.Mail, h.mailboxStats},
		{"DELETE", "/mailboxes/:id", apiratelimit.Mail, h.deleteMailbox},
		{"POST", "/generate-email", apiratelimit.GenerateEmail, h.generateEmail},
		{"GET", "/domains/available", apiratelimit.AvailableDomains, h.availableDomains},
	}
}

func (h *Handler) yydsAutomationRoutes() []automationRoute {
	return []automationRoute{
		{"GET", "/domains", apiratelimit.YYDS, h.yydsListDomains},
		{"POST", "/accounts", apiratelimit.YYDS, h.yydsCreateAccount},
		{"POST", "/accounts/wildcard", apiratelimit.YYDS, h.yydsCreateWildcardAccount},
		{"POST", "/token", apiratelimit.YYDS, h.yydsUnsupportedTempToken},
		{"GET", "/accounts/me", apiratelimit.YYDS, h.yydsUnsupportedTempToken},
		{"GET", "/accounts/:id", apiratelimit.YYDS, h.yydsGetAccount},
		{"DELETE", "/accounts/:id", apiratelimit.YYDS, h.yydsDeleteAccount},
		{"GET", "/messages", apiratelimit.YYDS, h.yydsListMessages},
		{"POST", "/messages/mark-read", apiratelimit.YYDS, h.yydsMarkMailboxRead},
		{"GET", "/messages/:id", apiratelimit.YYDS, h.yydsGetMessage},
		{"PATCH", "/messages/:id", apiratelimit.YYDS, h.yydsPatchMessage},
		{"DELETE", "/messages/:id", apiratelimit.YYDS, h.yydsDeleteMessage},
		{"GET", "/sources/:id", apiratelimit.YYDS, h.yydsGetMessageSource},
	}
}
