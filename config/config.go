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

	SignozURL          string
	SignozAPIKey       string
	SignozUIURL        string
	ClockSkewMax       time.Duration
	AnalyzerPoll       time.Duration
	PublicBaseURL      string
	AllowLocalWebhooks bool

	// AI reasoning layer. The API key here is the *fallback* used when no
	// provider has been configured through the UI, so Compose and Helm work
	// out of the box the same way SIGNOZ_API_KEY does.
	AIProvider        string
	AIModel           string
	AIAPIKey          string
	AIBaseURL         string
	AITimeout         time.Duration
	AIMaxContextChars int
	// SecretKey encrypts provider credentials at rest. Unset means credentials
	// cannot be stored via the UI - the env fallback still works.
	SecretKey string
}

func Load() Config {
	aiKind, aiKey := resolveAI()
	return Config{
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://wardn:wardn@localhost:5432/wardn?sslmode=disable"),
		// "name:key,name:key" - one entry per service.
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

		AIProvider:        aiKind,
		AIModel:           env("AI_MODEL", ""),
		AIAPIKey:          aiKey,
		AIBaseURL:         env("AI_BASE_URL", ""),
		AITimeout:         envDuration("AI_TIMEOUT", 120*time.Second),
		AIMaxContextChars: envInt("AI_MAX_CONTEXT_CHARS", 60000),
		SecretKey:         env("WARDN_SECRET_KEY", ""),
	}
}

// resolveAI picks the fallback provider from the environment. Setting just
// ANTHROPIC_API_KEY or just OPENAI_API_KEY is enough - AI_PROVIDER only needs
// to be set to disambiguate when both are present. AI_API_KEY is the
// provider-agnostic form, which is what the Helm chart injects from its Secret.
func resolveAI() (kind, apiKey string) {
	generic := os.Getenv("AI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	openaiKey := os.Getenv("OPENAI_API_KEY")

	switch os.Getenv("AI_PROVIDER") {
	case "anthropic":
		return "anthropic", firstNonEmpty(anthropicKey, generic)
	case "openai":
		return "openai", firstNonEmpty(openaiKey, generic)
	}
	switch {
	case anthropicKey != "":
		return "anthropic", anthropicKey
	case openaiKey != "":
		return "openai", openaiKey
	case generic != "":
		// Provider unstated - default to Claude, matching AI_MODEL's default.
		return "anthropic", generic
	}
	return "", ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
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
