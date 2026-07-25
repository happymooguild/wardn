// Package config loads runtime configuration from the environment.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port        string
	DatabaseURL string
	SeedApp     string
	SeedAPIKey  string
	SeedDemo    bool
	SeedMetric  string

	SessionSecret string
	SeedAdminUser string
	SeedAdminPass string

	SignozURL     string
	SignozAPIKey  string
	SignozUIURL   string
	ClockSkewMax  time.Duration
	AnalyzerPoll  time.Duration
	PublicBaseURL string
	AllowLocalWebhooks bool
}

func Load() Config {
	return Config{
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://wardn:wardn@localhost:5432/wardn?sslmode=disable"),
		SeedApp:     env("SEED_APP", "checkout-service"),
		SeedAPIKey:  env("SEED_API_KEY", "wardn_dev_key_checkout"),
		SeedDemo:    envBool("SEED_DEMO", true),
		SeedMetric:  env("SEED_METRIC", "latency_ms"),

		SessionSecret: env("SESSION_SECRET", "dev-insecure-session-secret-change-me"),
		SeedAdminUser: env("SEED_ADMIN_USER", "admin"),
		SeedAdminPass: env("SEED_ADMIN_PASS", "admin@12345"),

		SignozURL:          env("SIGNOZ_URL", ""),
		SignozAPIKey:       env("SIGNOZ_API_KEY", ""),
		SignozUIURL:        env("SIGNOZ_UI_URL", ""),
		ClockSkewMax:       envDuration("CLOCK_SKEW_MAX", 24*time.Hour),
		AnalyzerPoll:       envDuration("ANALYZER_POLL_INTERVAL", 5*time.Second),
		PublicBaseURL:      env("PUBLIC_BASE_URL", "http://localhost:8088"),
		AllowLocalWebhooks: envBool("ALLOW_LOCAL_WEBHOOKS", true),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch os.Getenv(key) {
	case "1", "true", "TRUE", "yes":
		return true
	case "0", "false", "FALSE", "no":
		return false
	default:
		return def
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	return def
}
