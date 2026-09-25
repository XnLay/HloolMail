package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"gptmail/internal/apiratelimit"
	"gptmail/internal/models"
	"gptmail/internal/observability"
	"gptmail/internal/ratelimit"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

func (h *Handler) allowAPIKeyIngress(c *gin.Context) bool {
	settings := h.APIRateLimits.Snapshot()
	route := apiRateLimitRoute(c)
	instanceKey := apiRateLimitBucketKey("pre_auth", "per_instance", "instance", route)
	instancePolicy := apiRateLimitPolicy(settings.Revision, settings.Config.PreAuth.PerInstance)
	// 饱和时先拒绝且不创建来源桶；真正扣除实例额度仍在 IP 检查通过之后。
	if !enforceAPIRateLimit(c, "pre_auth", "per_instance", route, h.APIKeyRateLimiter.Peek(instanceKey, instancePolicy)) {
		return false
	}
	if !h.checkAPIRateLimit(c, "pre_auth", "per_ip", c.ClientIP(), route, settings.Revision, settings.Config.PreAuth.PerIP) {
		return false
	}
	return enforceAPIRateLimit(c, "pre_auth", "per_instance", route, h.APIKeyRateLimiter.Check(instanceKey, instancePolicy))
}

func (h *Handler) allowAPIKeyBusiness(c *gin.Context, key *models.APIKey) bool {
	profile, registered := h.apiRateLimitProfiles[c.Request.Method+":"+c.FullPath()]
	if !registered {
		return true
	}
	settings := h.APIRateLimits.Snapshot()
	rule, ok := apiratelimit.BusinessRule(settings.Config, profile)
	if !ok {
		fail(c, http.StatusInternalServerError, "unregistered API rate limit policy")
		return false
	}
	return h.checkAPIRateLimit(c, "business", string(profile), strconv.FormatUint(uint64(key.ID), 10), apiRateLimitRoute(c), settings.Revision, rule)
}

func (h *Handler) checkAPIRateLimit(c *gin.Context, layer, profile, subject, route string, revision int64, rule models.APIRateLimitRule) bool {
	decision := h.APIKeyRateLimiter.Check(apiRateLimitBucketKey(layer, profile, subject, route), apiRateLimitPolicy(revision, rule))
	return enforceAPIRateLimit(c, layer, profile, route, decision)
}

func apiRateLimitBucketKey(layer, profile, subject, route string) string {
	return "api-key:" + layer + ":" + profile + ":" + subject + ":" + route
}

func apiRateLimitPolicy(revision int64, rule models.APIRateLimitRule) ratelimit.Policy {
	return ratelimit.Policy{
		Revision: revision, Enabled: rule.Enabled, Rate: rate.Limit(rule.RequestsPerSecond), Burst: rule.Burst,
	}
}

func enforceAPIRateLimit(c *gin.Context, layer, profile, route string, decision ratelimit.Decision) bool {
	if decision.Allowed {
		return true
	}
	seconds := max(1, int(math.Ceil(float64(decision.RetryAfter)/float64(time.Second))))
	c.Header("Retry-After", strconv.Itoa(seconds))
	observability.ObserveAPIRateLimitRejection(layer, profile, route)
	fail(c, http.StatusTooManyRequests, "rate limit exceeded")
	return false
}

func apiRateLimitRoute(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return c.Request.Method + ":" + route
	}
	// 未匹配路由不能用任意 URL 创建指标标签和令牌桶。
	return "unmatched"
}
