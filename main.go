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
	"wardn/demo/seed"
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

	// Seed the admin login so the dashboard is reachable during testing.
	adminHash, err := api.HashPassword(cfg.SeedAdminPass)
	if err != nil {
		log.Fatalf("hash admin password: %v", err)
	}
	if err := st.SeedUser(ctx, cfg.SeedAdminUser, adminHash, "admin"); err != nil {
		log.Fatalf("seed admin user: %v", err)
	}
	log.Printf("seeded admin user %q", cfg.SeedAdminUser)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.New(st, cfg.SessionSecret),
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
