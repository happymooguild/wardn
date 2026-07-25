// Package config loads runtime configuration from the environment.
// Everything has a sensible local-dev default so `go run .` works with no setup
// beyond a running Postgres.
package config

import (
	"os"
	"strings"
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

	SessionSecret string // signs the login session cookie
	SeedAdminUser string // seeded dashboard admin username
	SeedAdminPass string // seeded dashboard admin password (stored bcrypt-hashed)
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
