package models

import "time"

// APIRateLimitRule 显式区分停用和零速率，停用时仍保留可恢复的合法参数。
type APIRateLimitRule struct {
	Enabled           bool    `gorm:"not null" json:"enabled"`
	RequestsPerSecond float64 `gorm:"not null" json:"requests_per_second"`
	Burst             int     `gorm:"not null" json:"burst"`
}

type APIRateLimitPreAuth struct {
	PerIP       APIRateLimitRule `gorm:"embedded;embeddedPrefix:per_ip_" json:"per_ip"`
	PerInstance APIRateLimitRule `gorm:"embedded;embeddedPrefix:per_instance_" json:"per_instance"`
}

type APIRateLimitBusiness struct {
	Mail             APIRateLimitRule `gorm:"embedded;embeddedPrefix:mail_" json:"mail"`
	GenerateEmail    APIRateLimitRule `gorm:"embedded;embeddedPrefix:generate_email_" json:"generate_email"`
	AvailableDomains APIRateLimitRule `gorm:"embedded;embeddedPrefix:available_domains_" json:"available_domains"`
	Stats            APIRateLimitRule `gorm:"embedded;embeddedPrefix:stats_" json:"stats"`
	YYDS             APIRateLimitRule `gorm:"embedded;embeddedPrefix:yyds_" json:"yyds"`
}

type APIRateLimitConfig struct {
	PreAuth  APIRateLimitPreAuth  `gorm:"embedded;embeddedPrefix:pre_auth_" json:"pre_auth"`
	Business APIRateLimitBusiness `gorm:"embedded;embeddedPrefix:business_" json:"business"`
}

type APIRateLimitSettings struct {
	ID        uint               `gorm:"primaryKey" json:"-"`
	Revision  int64              `gorm:"not null" json:"revision"`
	Config    APIRateLimitConfig `gorm:"embedded;embeddedPrefix:config_" json:"config"`
	UpdatedBy string             `gorm:"not null" json:"updated_by"`
	CreatedAt time.Time          `json:"-"`
	UpdatedAt time.Time          `json:"updated_at"`
}
