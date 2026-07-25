// Package config loads runtime configuration from the environment.
// Everything has a sensible local-dev default so `go run .` works with no setup
// beyond a running Postgres.
package config

import "os"

type Config struct {
	Port        string // HTTP listen port
	DatabaseURL string // Postgres connection string
	SeedApp     string // name of the app seeded on startup (the sample-app posts as this)
	SeedAPIKey  string // plaintext API key seeded for SeedApp; stored hashed
	SeedDemo    bool   // seed synthetic multi-version history on first boot
	SeedMetric  string // metric name to seed history for
}

func Load() Config {
	return Config{
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://wardn:wardn@localhost:5432/wardn?sslmode=disable"),
		SeedApp:     env("SEED_APP", "checkout-service"),
		SeedAPIKey:  env("SEED_API_KEY", "wardn_dev_key_checkout"),
		SeedDemo:    envBool("SEED_DEMO", true),
		SeedMetric:  env("SEED_METRIC", "latency_ms"),
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
