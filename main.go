// Command wardn-backend is the API server: metric ingest, deploy markers,
// SigNoz-backed analysis, and alerting.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wardn/ai"
	"wardn/alert"
	"wardn/analyzer"
	"wardn/api"
	"wardn/config"
	"wardn/demo/seed"
	"wardn/metrics"
	"wardn/secret"
	"wardn/store"
)

func main() {
	cfg := config.Load()
	log.Printf("wardn backend starting (port %s)", cfg.Port)

	st := mustConnect(cfg.DatabaseURL)
	defer st.Close()

	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Print("migrated")

	// Seed each service + (on first boot) its synthetic version history. The
	// index becomes the demo "variant" so different services look distinct.
	for i, as := range cfg.SeedApps {
		appID, err := st.SeedApp(ctx, as.Name, api.HashKey(as.Key))
		if err != nil {
			log.Fatalf("seed app %q: %v", as.Name, err)
		}
		log.Printf("seeded app %q (id %d)", as.Name, appID)

		if cfg.SeedDemo {
			n, err := seed.Run(ctx, st, appID, as.Name, cfg.SeedMetric, i)
			if err != nil {
				log.Fatalf("seed demo data for %q: %v", as.Name, err)
			}
			if n > 0 {
				log.Printf("seeded %d synthetic samples for %q", n, as.Name)
			}
		}
	}

	adminHash, err := api.HashPassword(cfg.SeedAdminPass)
	if err != nil {
		log.Fatalf("hash admin password: %v", err)
	}
	if err := st.SeedUser(ctx, cfg.SeedAdminUser, adminHash, "admin"); err != nil {
		log.Fatalf("seed admin user: %v", err)
	}
	log.Printf("seeded admin user %q", cfg.SeedAdminUser)

	alertEngine := alert.New(st, cfg.PublicBaseURL, cfg.AllowLocalWebhooks)

	var provider metrics.MetricsProvider
	var telemetry metrics.TelemetryProvider
	if cfg.SignozURL != "" && cfg.SignozAPIKey != "" {
		signoz := metrics.NewSignoz(cfg.SignozURL, cfg.SignozAPIKey)
		provider, telemetry = signoz, signoz
		log.Printf("signoz metrics provider configured (%s)", cfg.SignozURL)
	} else {
		log.Print("SIGNOZ_URL / SIGNOZ_API_KEY unset — analyzer will fail jobs until configured")
	}

	// Credentials can be encrypted at rest only if an encryption key exists.
	// Without one the env fallback still works; the UI just can't store keys.
	var box *secret.Box
	if b, err := secret.NewBox(cfg.SecretKey); err == nil {
		box = b
	} else {
		log.Print("WARDN_SECRET_KEY unset — AI keys cannot be saved from the UI; " +
			"set ANTHROPIC_API_KEY / OPENAI_API_KEY instead")
	}

	aiResolver := &ai.Resolver{
		Store:   st,
		Box:     box,
		Timeout: cfg.AITimeout,
		Env: ai.Credential{
			Kind:    cfg.AIProvider,
			APIKey:  cfg.AIAPIKey,
			Model:   cfg.AIModel,
			BaseURL: cfg.AIBaseURL,
		},
	}
	if cfg.AIAPIKey != "" {
		log.Printf("ai provider %q configured from environment", cfg.AIProvider)
	}

	bounds := ai.DefaultBounds()
	bounds.MaxTotalChars = cfg.AIMaxContextChars

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	worker := analyzer.New(st, provider, alertEngine, cfg.AnalyzerPoll)
	worker.Telemetry = telemetry
	worker.AI = aiResolver
	worker.Bounds = bounds
	worker.AITimeout = cfg.AITimeout
	go worker.Run(workerCtx)

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.New(st, api.Options{
			SessionSecret: cfg.SessionSecret,
			ClockSkewMax:  cfg.ClockSkewMax,
			Alerts:        alertEngine,
			AI:            aiResolver,
			SecretBox:     box,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("shutting down")
	workerCancel()

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func mustConnect(url string) *store.Store {
	var (
		st  *store.Store
		err error
	)
	for i := 1; i <= 30; i++ {
		if st, err = store.Open(url); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err = st.Ping(ctx)
			cancel()
			if err == nil {
				return st
			}
		}
		log.Printf("waiting for postgres (attempt %d/30): %v", i, err)
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("could not connect to postgres: %v", err)
	return nil
}
