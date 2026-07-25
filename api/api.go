// Package api is the HTTP surface of the backend.
//
// Endpoints (skeleton):
//
//	GET  /healthz                          liveness
//	POST /api/v1/metrics                   ingest one sample      (API-key auth, scoped to app)
//	GET  /api/v1/versions?app=&metric=     per-version percentiles (open; the version chart)
//	GET  /api/v1/metrics?app=&version=     raw samples for a version (open; the drill-down)
//	GET  /api/v1/apps                      list registered apps    (open)
//
// Read endpoints are open on purpose for now — the dashboard is an unauthenticated
// browser app until the auth/RBAC stage lands. Ingest is always key-gated.
package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"wardn/store"
)

type API struct{ st *store.Store }

// New returns the fully-wired HTTP handler (routes + CORS).
func New(st *store.Store) http.Handler {
	a := &API{st: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/v1/metrics", a.ingest)
	mux.HandleFunc("GET /api/v1/versions", a.versions)
	mux.HandleFunc("GET /api/v1/metrics", a.series)
	mux.HandleFunc("GET /api/v1/apps", a.apps)
	return cors(mux)
}

// HashKey is the one-way transform applied to API keys before they touch the DB.
// SHA-256 is fine here: API keys are long and random, so we don't need a slow
// password hash.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type ingestReq struct {
	App       string  `json:"app"`
	Metric    string  `json:"metric"`
	Version   string  `json:"version"`
	Value     float64 `json:"value"`
	Timestamp string  `json:"timestamp"` // optional RFC3339; defaults to now
}

func (a *API) ingest(w http.ResponseWriter, r *http.Request) {
	key := bearer(r)
	if key == "" {
		writeErr(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	app, err := a.st.AppByAPIKeyHash(r.Context(), HashKey(key))
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	if err != nil {
		log.Printf("ingest: lookup app: %v", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req ingestReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}

	// Key scoping: a key may only post for the app it belongs to. If the caller
	// names an app, it must match; if it omits it, we use the key's app.
	if req.App != "" && req.App != app.Name {
		writeErr(w, http.StatusForbidden, "api key not valid for app "+req.App)
		return
	}

	metric := req.Metric
	if metric == "" {
		metric = "latency_ms"
	}
	version := req.Version
	if version == "" {
		version = "untagged"
	}

	ts := time.Now().UTC()
	if req.Timestamp != "" {
		parsed, err := time.Parse(time.RFC3339, req.Timestamp)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "timestamp must be RFC3339")
			return
		}
		ts = parsed
	}

	if err := a.st.InsertMetric(r.Context(), app.ID, metric, version, req.Value, ts); err != nil {
		log.Printf("ingest: insert metric: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not store metric")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// versions returns the per-version percentile profile that the version chart plots.
func (a *API) versions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	app := q.Get("app")
	if app == "" {
		writeErr(w, http.StatusBadRequest, "app query param is required")
		return
	}
	metric := q.Get("metric")
	if metric == "" {
		metric = "latency_ms"
	}

	stats, err := a.st.VersionsWithStats(r.Context(), app, metric)
	if err != nil {
		log.Printf("versions: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not read versions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"app":      app,
		"metric":   metric,
		"versions": stats,
	})
}

// series returns the raw samples for one version — the drill-down time-series.
func (a *API) series(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	app := q.Get("app")
	version := q.Get("version")
	if app == "" || version == "" {
		writeErr(w, http.StatusBadRequest, "app and version query params are required")
		return
	}
	metric := q.Get("metric")
	if metric == "" {
		metric = "latency_ms"
	}

	points, err := a.st.VersionSeries(r.Context(), app, metric, version)
	if err != nil {
		log.Printf("series: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not read series")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"app":     app,
		"metric":  metric,
		"version": version,
		"points":  points,
	})
}

func (a *API) apps(w http.ResponseWriter, r *http.Request) {
	apps, err := a.st.ListApps(r.Context())
	if err != nil {
		log.Printf("apps: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not list apps")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps})
}

// ---- helpers ----

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// cors allows the Vite dev server (localhost:5173) to call the API directly.
// In the Helm deployment the frontend proxies /api same-origin, so this is a
// dev convenience, not a production posture.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
