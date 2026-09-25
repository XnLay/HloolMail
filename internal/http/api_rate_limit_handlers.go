package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gptmail/internal/apiratelimit"
	"gptmail/internal/db"
	"gptmail/internal/models"

	"github.com/gin-gonic/gin"
)

type apiRateLimitSettingsResponse struct {
	models.APIRateLimitSettings
	Defaults        models.APIRateLimitConfig `json:"defaults"`
	AppliedRevision int64                     `json:"applied_revision"`
	Constraints     apiRateLimitConstraints   `json:"constraints"`
}

type apiRateLimitConstraints struct {
	MinRequestsPerSecond float64 `json:"min_requests_per_second"`
	MaxRequestsPerSecond float64 `json:"max_requests_per_second"`
	MinBurst             int     `json:"min_burst"`
	MaxBurst             int     `json:"max_burst"`
}

func (h *Handler) adminRateLimitSettings(c *gin.Context) {
	if _, ok := h.requireAdminSession(c); !ok {
		return
	}
	if err := h.APIRateLimits.Refresh(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondRateLimitSettings(c, h.APIRateLimits.Snapshot())
}

func (h *Handler) updateAdminRateLimitSettings(c *gin.Context) {
	user, ok := h.requireAdminSession(c)
	if !ok {
		return
	}
	// 此入口只接受会话写入；附带旧管理 Token 也不能绕过同源要求。
	if !h.requestHasSameOrigin(c) {
		fail(c, http.StatusForbidden, "same-origin request required")
		return
	}
	var input apiRateLimitSettingsInput
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid rate limit settings: "+err.Error())
		return
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		fail(c, http.StatusBadRequest, "request must contain exactly one JSON object")
		return
	}
	config, err := input.config()
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := h.APIRateLimits.Save(c.Request.Context(), input.Revision, config,
		newAuditLog("api_rate_limit_settings.update", user.Email, "1", ""))
	switch {
	case errors.Is(err, db.ErrAPIRateLimitConflict):
		fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, apiratelimit.ErrInvalidConfig):
		fail(c, http.StatusBadRequest, err.Error())
	case err != nil:
		fail(c, http.StatusInternalServerError, err.Error())
	default:
		h.respondRateLimitSettings(c, saved)
	}
}

func (h *Handler) respondRateLimitSettings(c *gin.Context, settings models.APIRateLimitSettings) {
	ok(c, apiRateLimitSettingsResponse{APIRateLimitSettings: settings,
		Defaults: apiratelimit.DefaultConfig(), AppliedRevision: h.APIRateLimits.Snapshot().Revision,
		Constraints: apiRateLimitConstraints{apiratelimit.MinRequestsPerSecond, apiratelimit.MaxRequestsPerSecond, 1, apiratelimit.MaxBurst}})
}

// 输入边界使用指针识别缺失字段，转换后的领域配置只包含确定值。
type apiRateLimitRuleInput struct {
	Enabled           *bool    `json:"enabled"`
	RequestsPerSecond *float64 `json:"requests_per_second"`
	Burst             *int     `json:"burst"`
}

type apiRateLimitSettingsInput struct {
	Revision int64 `json:"revision"`
	Config   struct {
		PreAuth struct {
			PerIP       apiRateLimitRuleInput `json:"per_ip"`
			PerInstance apiRateLimitRuleInput `json:"per_instance"`
		} `json:"pre_auth"`
		Business struct {
			Mail             apiRateLimitRuleInput `json:"mail"`
			GenerateEmail    apiRateLimitRuleInput `json:"generate_email"`
			AvailableDomains apiRateLimitRuleInput `json:"available_domains"`
			Stats            apiRateLimitRuleInput `json:"stats"`
			YYDS             apiRateLimitRuleInput `json:"yyds"`
		} `json:"business"`
	} `json:"config"`
}

func (input apiRateLimitSettingsInput) config() (models.APIRateLimitConfig, error) {
	var config models.APIRateLimitConfig
	if input.Revision < 1 {
		return config, errors.New("revision must be positive")
	}
	for _, field := range []struct {
		name   string
		input  apiRateLimitRuleInput
		target *models.APIRateLimitRule
	}{
		{"pre_auth.per_ip", input.Config.PreAuth.PerIP, &config.PreAuth.PerIP},
		{"pre_auth.per_instance", input.Config.PreAuth.PerInstance, &config.PreAuth.PerInstance},
		{"business.mail", input.Config.Business.Mail, &config.Business.Mail},
		{"business.generate_email", input.Config.Business.GenerateEmail, &config.Business.GenerateEmail},
		{"business.available_domains", input.Config.Business.AvailableDomains, &config.Business.AvailableDomains},
		{"business.stats", input.Config.Business.Stats, &config.Business.Stats},
		{"business.yyds", input.Config.Business.YYDS, &config.Business.YYDS},
	} {
		if field.input.Enabled == nil || field.input.RequestsPerSecond == nil || field.input.Burst == nil {
			return config, fmt.Errorf("config.%s requires enabled, requests_per_second and burst", field.name)
		}
		*field.target = models.APIRateLimitRule{Enabled: *field.input.Enabled,
			RequestsPerSecond: *field.input.RequestsPerSecond, Burst: *field.input.Burst}
	}
	return config, apiratelimit.Validate(config)
}
