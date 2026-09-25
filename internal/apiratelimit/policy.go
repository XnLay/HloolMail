package apiratelimit

import (
	"errors"
	"fmt"
	"math"

	"gptmail/internal/models"
)

const (
	MinRequestsPerSecond = 0.001
	MaxRequestsPerSecond = 100000.0
	MaxBurst             = 100000
)

type Profile string

const (
	Mail             Profile = "mail"
	GenerateEmail    Profile = "generate_email"
	AvailableDomains Profile = "available_domains"
	Stats            Profile = "stats"
	YYDS             Profile = "yyds"
)

var ErrInvalidConfig = errors.New("invalid API rate limit configuration")

func DefaultConfig() models.APIRateLimitConfig {
	rule := func(rps float64, burst int) models.APIRateLimitRule {
		return models.APIRateLimitRule{Enabled: true, RequestsPerSecond: rps, Burst: burst}
	}
	return models.APIRateLimitConfig{
		PreAuth: models.APIRateLimitPreAuth{PerIP: rule(5, 20), PerInstance: rule(50, 200)},
		Business: models.APIRateLimitBusiness{
			Mail: rule(2, 20), GenerateEmail: rule(1, 10), AvailableDomains: rule(0.5, 5),
			Stats: rule(2, 10), YYDS: rule(2, 20),
		},
	}
}

func BusinessRule(config models.APIRateLimitConfig, profile Profile) (models.APIRateLimitRule, bool) {
	switch profile {
	case Mail:
		return config.Business.Mail, true
	case GenerateEmail:
		return config.Business.GenerateEmail, true
	case AvailableDomains:
		return config.Business.AvailableDomains, true
	case Stats:
		return config.Business.Stats, true
	case YYDS:
		return config.Business.YYDS, true
	default:
		return models.APIRateLimitRule{}, false
	}
}

func Validate(config models.APIRateLimitConfig) error {
	for _, field := range []struct {
		name string
		rule models.APIRateLimitRule
	}{
		{"pre_auth.per_ip", config.PreAuth.PerIP},
		{"pre_auth.per_instance", config.PreAuth.PerInstance},
		{"business.mail", config.Business.Mail},
		{"business.generate_email", config.Business.GenerateEmail},
		{"business.available_domains", config.Business.AvailableDomains},
		{"business.stats", config.Business.Stats},
		{"business.yyds", config.Business.YYDS},
	} {
		rps := field.rule.RequestsPerSecond
		if math.IsNaN(rps) || math.IsInf(rps, 0) || rps < MinRequestsPerSecond || rps > MaxRequestsPerSecond {
			return fmt.Errorf("%w: %s.requests_per_second must be between %g and %g", ErrInvalidConfig, field.name, MinRequestsPerSecond, MaxRequestsPerSecond)
		}
		if field.rule.Burst < 1 || field.rule.Burst > MaxBurst {
			return fmt.Errorf("%w: %s.burst must be between 1 and %d", ErrInvalidConfig, field.name, MaxBurst)
		}
	}
	return nil
}
