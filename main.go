// Command wardn-backend is the skeleton API server: it ingests metric samples
// from registered apps and serves them back to the dashboard.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wardn/api"
	"wardn/config"
	"wardn/seed"
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

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.New(st),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM (so k8s rollouts drain cleanly).
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// mustConnect retries because Postgres in the same compose/Helm release may not
// be accepting connections yet when the backend boots.
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
