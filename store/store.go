// Package store is the Postgres data layer for the skeleton.
//
// Scope for now is intentionally small: registered apps (each with a hashed API
// key) and a metrics table where every sample carries the app version it was
// observed on. That version dimension is what powers the version-by-version
// comparison on the dashboard. This is the seam the fuller data model from the
// design doc (deploy_events, snapshots, analyses, RBAC, ...) grows into later.
package store

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/lib/pq"
)

type Store struct{ db *sql.DB }

type App struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// User is a dashboard login. Role is 'admin' for now (RBAC comes in a later stage).
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
}

// Point is one sample of a metric at a moment in time.
type Point struct {
	TS    time.Time `json:"ts"`
	Value float64   `json:"value"`
}

// Sample is one metric observation to insert (used for seeding).
type Sample struct {
	Version string
	Value   float64
	TS      time.Time
}

// VersionStat is the aggregated latency profile of a single app version:
// the percentiles the dashboard plots + drills into.
type VersionStat struct {
	Version string    `json:"version"`
	P50     float64   `json:"p50"`
	P90     float64   `json:"p90"`
	P95     float64   `json:"p95"`
	P99     float64   `json:"p99"`
	Count   int       `json:"count"`
	FirstTS time.Time `json:"first_ts"`
	LastTS  time.Time `json:"last_ts"`
}

// schema is applied idempotently on every boot.
const schema = `
CREATE TABLE IF NOT EXISTS apps (
    id           BIGSERIAL PRIMARY KEY,
    name         TEXT UNIQUE NOT NULL,
    api_key_hash TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'admin',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS metrics (
    id      BIGSERIAL PRIMARY KEY,
    app_id  BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name    TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    value   DOUBLE PRECISION NOT NULL,
    ts      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- for databases created before the version column existed
ALTER TABLE metrics ADD COLUMN IF NOT EXISTS version TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_metrics_app_name_ts      ON metrics (app_id, name, ts DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_app_name_version ON metrics (app_id, name, version);
`

func Open(url string) (*Store, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// SeedApp upserts an app + its hashed key and returns the app id.
func (s *Store) SeedApp(ctx context.Context, name, apiKeyHash string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO apps (name, api_key_hash) VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET api_key_hash = EXCLUDED.api_key_hash
		 RETURNING id`,
		name, apiKeyHash).Scan(&id)
	return id, err
}

// SeedUser inserts a user if one with that username doesn't already exist
// (DO NOTHING keeps a later password change from being reset on every boot).
func (s *Store) SeedUser(ctx context.Context, username, passwordHash, role string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)
		 ON CONFLICT (username) DO NOTHING`,
		username, passwordHash, role)
	return err
}

// UserByUsername looks up a login. sql.ErrNoRows means no such user.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role FROM users WHERE username = $1`,
		username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	return u, err
}

// AppByAPIKeyHash resolves the app an API key belongs to. sql.ErrNoRows means
// the key is unknown -> the caller should return 401.
func (s *Store) AppByAPIKeyHash(ctx context.Context, hash string) (App, error) {
	var a App
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name FROM apps WHERE api_key_hash = $1`, hash).Scan(&a.ID, &a.Name)
	return a, err
}

func (s *Store) InsertMetric(ctx context.Context, appID int64, name, version string, value float64, ts time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO metrics (app_id, name, version, value, ts) VALUES ($1, $2, $3, $4, $5)`,
		appID, name, version, value, ts)
	return err
}

// InsertSamples bulk-inserts many samples in one transaction (used by the seeder).
func (s *Store) InsertSamples(ctx context.Context, appID int64, name string, samples []Sample) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO metrics (app_id, name, version, value, ts) VALUES ($1, $2, $3, $4, $5)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, smp := range samples {
		if _, err := stmt.ExecContext(ctx, appID, name, smp.Version, smp.Value, smp.TS); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CountVersioned returns how many versioned samples exist for an app+metric.
// Used to make seeding idempotent (only seed an empty dataset).
func (s *Store) CountVersioned(ctx context.Context, appName, metric string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM metrics m
		   JOIN apps a ON a.id = m.app_id
		  WHERE a.name = $1 AND m.name = $2 AND m.version <> ''`,
		appName, metric).Scan(&n)
	return n, err
}

// VersionsWithStats returns one row per version with its latency percentiles,
// ordered chronologically (by when the version first appeared) so the chart's
// x-axis reads left-to-right as deploy history.
// since bounds the window: only samples at or after it are counted, and versions
// with no samples in the window drop out entirely.
func (s *Store) VersionsWithStats(ctx context.Context, appName, metric string, since time.Time) ([]VersionStat, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.version,
		        percentile_cont(0.5)  WITHIN GROUP (ORDER BY m.value) AS p50,
		        percentile_cont(0.9)  WITHIN GROUP (ORDER BY m.value) AS p90,
		        percentile_cont(0.95) WITHIN GROUP (ORDER BY m.value) AS p95,
		        percentile_cont(0.99) WITHIN GROUP (ORDER BY m.value) AS p99,
		        count(*)  AS n,
		        min(m.ts) AS first_ts,
		        max(m.ts) AS last_ts
		   FROM metrics m
		   JOIN apps a ON a.id = m.app_id
		  WHERE a.name = $1 AND m.name = $2 AND m.version <> '' AND m.ts >= $3
		  GROUP BY m.version
		  ORDER BY min(m.ts) ASC`,
		appName, metric, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]VersionStat, 0, 16)
	for rows.Next() {
		var v VersionStat
		if err := rows.Scan(&v.Version, &v.P50, &v.P90, &v.P95, &v.P99, &v.Count, &v.FirstTS, &v.LastTS); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VersionSeries returns the raw samples for one version, oldest first — the
// detail time-series shown when a version is selected.
func (s *Store) VersionSeries(ctx context.Context, appName, metric, version string, since time.Time) ([]Point, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.ts, m.value
		   FROM metrics m
		   JOIN apps a ON a.id = m.app_id
		  WHERE a.name = $1 AND m.name = $2 AND m.version = $3 AND m.ts >= $4
		  ORDER BY m.ts ASC`,
		appName, metric, version, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]Point, 0, 256)
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.TS, &p.Value); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func (s *Store) ListApps(ctx context.Context) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM apps ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	apps := make([]App, 0, 8)
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}
