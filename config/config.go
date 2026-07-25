// Package config loads runtime configuration from the environment.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// AppSeed is a service to register on boot: a name + the API key its emitter uses.
type AppSeed struct {
	Name string
	Key  string
}

type Config struct {
	Port        string    // HTTP listen port
	DatabaseURL string    // Postgres connection string
	SeedApps    []AppSeed // services seeded on startup (the sample-apps post as these)
	SeedDemo    bool      // seed synthetic multi-version history on first boot
	SeedMetric  string    // metric name to seed history for

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
		// "name:key,name:key" — one entry per service.
		SeedApps:   parseAppSeeds(env("SEED_APPS", "checkout-service:wardn_dev_key_checkout,payments-service:wardn_dev_key_payments")),
		SeedDemo:   envBool("SEED_DEMO", true),
		SeedMetric: env("SEED_METRIC", "latency_ms"),

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

func parseAppSeeds(s string) []AppSeed {
	var out []AppSeed
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, key, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		out = append(out, AppSeed{Name: strings.TrimSpace(name), Key: strings.TrimSpace(key)})
	}
	return out
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
