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

	"wardn/alert"
	"wardn/analyzer"
	"wardn/api"
	"wardn/config"
	"wardn/demo/seed"
	"wardn/metrics"
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
	appID, err := st.SeedApp(ctx, cfg.SeedApp, api.HashKey(cfg.SeedAPIKey))
	if err != nil {
		log.Fatalf("seed app: %v", err)
	}
	log.Printf("migrated + seeded app %q (id %d)", cfg.SeedApp, appID)

	if cfg.SeedDemo {
		n, err := seed.Run(ctx, st, appID, cfg.SeedApp, cfg.SeedMetric)
		if err != nil {
			log.Fatalf("seed demo data: %v", err)
		}
		if n > 0 {
			log.Printf("seeded %d synthetic samples across demo versions", n)
		} else {
			log.Print("demo data already present, skipping seed")
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
	if cfg.SignozURL != "" && cfg.SignozAPIKey != "" {
		provider = metrics.NewSignoz(cfg.SignozURL, cfg.SignozAPIKey)
		log.Printf("signoz metrics provider configured (%s)", cfg.SignozURL)
	} else {
		log.Print("SIGNOZ_URL / SIGNOZ_API_KEY unset — analyzer will fail jobs until configured")
	}

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	worker := analyzer.New(st, provider, alertEngine, cfg.AnalyzerPoll)
	go worker.Run(workerCtx)

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.New(st, api.Options{
			SessionSecret: cfg.SessionSecret,
			ClockSkewMax:  cfg.ClockSkewMax,
			Alerts:        alertEngine,
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
