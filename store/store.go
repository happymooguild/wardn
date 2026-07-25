// Package store is the Postgres data layer.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	_ "github.com/lib/pq"
)

// ErrAppExists is returned by CreateApp when the name is already taken, so the
// API can surface a 409 instead of silently rotating an existing app's key.
var ErrAppExists = errors.New("app already exists")

type Store struct{ db *sql.DB }

type App struct {
	ID                   int64   `json:"id"`
	Name                 string  `json:"name"`
	Environment          string  `json:"environment"`
	SignozServiceName    string  `json:"signoz_service_name"`
	WindowSeconds        int     `json:"window_seconds"`
	AnalysisDelaySeconds int     `json:"analysis_delay_seconds"`
	LatencyThresholdPct  float64 `json:"latency_threshold_pct"`
	ErrorRateThresholdPP float64 `json:"error_rate_threshold_pp"`
	MinRequests          int     `json:"min_requests"`
	// AIEnabled opts this app into automatic root-cause analysis on a
	// regression (design-doc §4: "if regression found AND app opted in").
	AIEnabled bool `json:"ai_enabled"`
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

// VersionStat is the aggregated latency profile of a single app version.
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

type MetricDefinition struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	PromQLTemplate string `json:"promql_template"`
	Unit           string `json:"unit"`
	HigherIsWorse  bool   `json:"higher_is_worse"`
}

type AppMetric struct {
	AppID     int64  `json:"app_id"`
	MetricKey string `json:"metric_key"`
	Enabled   bool   `json:"enabled"`
}

type DeployEvent struct {
	ID              int64     `json:"id"`
	AppID           int64     `json:"app_id"`
	AppName         string    `json:"app,omitempty"`
	Version         string    `json:"version"`
	PreviousVersion *string   `json:"previous_version"`
	Environment     string    `json:"environment"`
	DeployedAt      time.Time `json:"deployed_at"`
	Source          string    `json:"source"`
	Status          string    `json:"status"`
	IdempotencyKey  string    `json:"idempotency_key,omitempty"`
	FailureReason   *string   `json:"failure_reason,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SeriesPoint struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

type MetricSnapshot struct {
	ID                 int64         `json:"id"`
	DeployEventID      int64         `json:"deploy_event_id"`
	MetricKey          string        `json:"metric_key"`
	WindowBeforeStart  time.Time     `json:"window_before_start"`
	WindowBeforeEnd    time.Time     `json:"window_before_end"`
	WindowAfterStart   time.Time     `json:"window_after_start"`
	WindowAfterEnd     time.Time     `json:"window_after_end"`
	BeforeValue        *float64      `json:"before_value"`
	AfterValue         *float64      `json:"after_value"`
	BeforeRequestCount *int64        `json:"before_request_count"`
	AfterRequestCount  *int64        `json:"after_request_count"`
	DeltaPct           *float64      `json:"delta_pct"`
	DeltaAbs           *float64      `json:"delta_abs"`
	Degraded           bool          `json:"degraded"`
	SeriesBefore       []SeriesPoint `json:"series_before"`
	SeriesAfter        []SeriesPoint `json:"series_after"`
	RawQuery           string        `json:"raw_query,omitempty"`
	CreatedAt          time.Time     `json:"created_at"`
}

// Job kinds. "metrics" is the statistical before/after comparison; "ai" is the
// LLM root-cause pass, which runs only after a verdict exists.
const (
	JobKindMetrics = "metrics"
	JobKindAI      = "ai"
)

type AnalysisJob struct {
	ID            int64
	DeployEventID int64
	Kind          string
	RunAfter      time.Time
	Attempts      int
	LockedBy      sql.NullString
	LockedAt      sql.NullTime
	DoneAt        sql.NullTime
	LastError     sql.NullString
}

// AIProvider is a configured LLM credential. The key itself is never in this
// struct — it lives encrypted in the DB and is only decrypted at call time.
type AIProvider struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Model     string    `json:"model"`
	BaseURL   string    `json:"base_url,omitempty"`
	KeyLast4  string    `json:"key_last4"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Analysis is one AI root-cause attempt for a deploy.
type Analysis struct {
	ID             int64           `json:"id"`
	DeployEventID  int64           `json:"deploy_event_id"`
	Status         string          `json:"status"`
	Trigger        string          `json:"trigger"`
	Provider       string          `json:"provider,omitempty"`
	Model          string          `json:"model,omitempty"`
	Summary        *string         `json:"summary,omitempty"`
	LikelyCause    *string         `json:"likely_cause,omitempty"`
	Confidence     *string         `json:"confidence,omitempty"`
	Evidence       json.RawMessage `json:"evidence"`
	SuggestedSteps json.RawMessage `json:"suggested_steps"`
	ContextStats   json.RawMessage `json:"context_stats"`
	InputTokens    *int            `json:"input_tokens,omitempty"`
	OutputTokens   *int            `json:"output_tokens,omitempty"`
	Error          *string         `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}

type AlertConfig struct {
	ID            int64           `json:"id"`
	AppID         int64           `json:"app_id"`
	MetricKey     *string         `json:"metric_key"`
	ChannelType   string          `json:"channel_type"`
	ChannelConfig json.RawMessage `json:"channel_config"`
	OnVerdict     string          `json:"on_verdict"`
	ThresholdPct  *float64        `json:"threshold_pct,omitempty"`
	Enabled       bool            `json:"enabled"`
	CreatedAt     time.Time       `json:"created_at"`
}

type AlertDelivery struct {
	ID            int64     `json:"id"`
	AlertConfigID int64     `json:"alert_config_id"`
	DeployEventID int64     `json:"deploy_event_id"`
	Status        string    `json:"status"`
	ResponseCode  *int      `json:"response_code,omitempty"`
	ErrorMessage  *string   `json:"error_message,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

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

ALTER TABLE metrics ADD COLUMN IF NOT EXISTS version TEXT NOT NULL DEFAULT '';

ALTER TABLE apps ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT 'production';
ALTER TABLE apps ADD COLUMN IF NOT EXISTS signoz_service_name TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN IF NOT EXISTS window_seconds INT NOT NULL DEFAULT 120;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS analysis_delay_seconds INT NOT NULL DEFAULT 30;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS latency_threshold_pct DOUBLE PRECISION NOT NULL DEFAULT 25;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS error_rate_threshold_pp DOUBLE PRECISION NOT NULL DEFAULT 1.0;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS min_requests INT NOT NULL DEFAULT 10;

UPDATE apps SET signoz_service_name = name WHERE signoz_service_name = '' OR signoz_service_name IS NULL;

CREATE TABLE IF NOT EXISTS metric_definitions (
    key              TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    promql_template  TEXT NOT NULL,
    unit             TEXT NOT NULL DEFAULT '',
    higher_is_worse  BOOLEAN NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS app_metrics (
    app_id     BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    metric_key TEXT NOT NULL REFERENCES metric_definitions(key),
    enabled    BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (app_id, metric_key)
);

CREATE TABLE IF NOT EXISTS deploy_events (
    id               BIGSERIAL PRIMARY KEY,
    app_id           BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    version          TEXT NOT NULL,
    previous_version TEXT,
    environment      TEXT NOT NULL,
    deployed_at      TIMESTAMPTZ NOT NULL,
    source           TEXT NOT NULL CHECK (source IN ('ci','argocd','flux','manual')),
    status           TEXT NOT NULL CHECK (status IN (
                       'received','pending_analysis','analyzing',
                       'healthy','regressed','inconclusive','failed'
                     )),
    idempotency_key  TEXT NOT NULL UNIQUE,
    failure_reason   TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS deploy_events_app_time ON deploy_events (app_id, deployed_at DESC);

CREATE TABLE IF NOT EXISTS metric_snapshots (
    id                   BIGSERIAL PRIMARY KEY,
    deploy_event_id      BIGINT NOT NULL REFERENCES deploy_events(id) ON DELETE CASCADE,
    metric_key           TEXT NOT NULL,
    window_before_start  TIMESTAMPTZ NOT NULL,
    window_before_end    TIMESTAMPTZ NOT NULL,
    window_after_start   TIMESTAMPTZ NOT NULL,
    window_after_end     TIMESTAMPTZ NOT NULL,
    before_value         DOUBLE PRECISION,
    after_value          DOUBLE PRECISION,
    before_request_count BIGINT,
    after_request_count  BIGINT,
    delta_pct            DOUBLE PRECISION,
    delta_abs            DOUBLE PRECISION,
    degraded             BOOLEAN NOT NULL DEFAULT false,
    series_before        JSONB NOT NULL DEFAULT '[]',
    series_after         JSONB NOT NULL DEFAULT '[]',
    raw_query            TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (deploy_event_id, metric_key)
);

CREATE TABLE IF NOT EXISTS analysis_jobs (
    id              BIGSERIAL PRIMARY KEY,
    deploy_event_id BIGINT NOT NULL REFERENCES deploy_events(id) ON DELETE CASCADE,
    run_after       TIMESTAMPTZ NOT NULL,
    attempts        INT NOT NULL DEFAULT 0,
    locked_by       TEXT,
    locked_at       TIMESTAMPTZ,
    done_at         TIMESTAMPTZ,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS analysis_jobs_claim ON analysis_jobs (run_after)
  WHERE done_at IS NULL;

CREATE TABLE IF NOT EXISTS alert_configs (
    id             BIGSERIAL PRIMARY KEY,
    app_id         BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    metric_key     TEXT,
    channel_type   TEXT NOT NULL CHECK (channel_type IN ('slack','webhook')),
    channel_config JSONB NOT NULL,
    on_verdict     TEXT NOT NULL DEFAULT 'regressed',
    enabled        BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE alert_configs ADD COLUMN IF NOT EXISTS threshold_pct DOUBLE PRECISION;

CREATE TABLE IF NOT EXISTS alert_deliveries (
    id              BIGSERIAL PRIMARY KEY,
    alert_config_id BIGINT NOT NULL REFERENCES alert_configs(id) ON DELETE CASCADE,
    deploy_event_id BIGINT NOT NULL REFERENCES deploy_events(id) ON DELETE CASCADE,
    status          TEXT NOT NULL,
    response_code   INT,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (alert_config_id, deploy_event_id)
);

-- AI reasoning layer (design-doc §8).
-- Note: 'analysis_jobs' / 'pending_analysis' above mean the *statistical*
-- before/after comparison. Anything AI-related is named ai_* or analyses.

ALTER TABLE analysis_jobs ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'metrics';
ALTER TABLE apps ADD COLUMN IF NOT EXISTS ai_enabled BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS ai_providers (
    id           BIGSERIAL PRIMARY KEY,
    kind         TEXT NOT NULL CHECK (kind IN ('anthropic','openai')),
    model        TEXT NOT NULL,
    base_url     TEXT NOT NULL DEFAULT '',
    api_key_enc  BYTEA NOT NULL,
    key_last4    TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one active provider at a time.
CREATE UNIQUE INDEX IF NOT EXISTS ai_providers_one_enabled
  ON ai_providers ((enabled)) WHERE enabled;

CREATE TABLE IF NOT EXISTS analyses (
    id               BIGSERIAL PRIMARY KEY,
    deploy_event_id  BIGINT NOT NULL REFERENCES deploy_events(id) ON DELETE CASCADE,
    status           TEXT NOT NULL CHECK (status IN
                       ('pending','running','succeeded','failed','refused')),
    -- 'trigger' and 'error' are keywords; the suffixed names keep the DDL
    -- unambiguous and match alert_deliveries.error_message.
    trigger_source   TEXT NOT NULL CHECK (trigger_source IN ('auto','manual')),
    provider         TEXT NOT NULL DEFAULT '',
    model            TEXT NOT NULL DEFAULT '',
    summary          TEXT,
    likely_cause     TEXT,
    confidence       TEXT,
    evidence         JSONB NOT NULL DEFAULT '[]',
    suggested_steps  JSONB NOT NULL DEFAULT '[]',
    context_stats    JSONB NOT NULL DEFAULT '{}',
    input_tokens     INT,
    output_tokens    INT,
    error_message    TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS analyses_deploy ON analyses (deploy_event_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_metrics_app_name_ts      ON metrics (app_id, name, ts DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_app_name_version ON metrics (app_id, name, version);

-- Verdict metrics: compared before/after a deploy, version-filtered so the new
-- version's window isn't polluted by the previous version's stale series.
-- (Demo gauges from the sample-app; swap for real APM PromQL in production.)
INSERT INTO metric_definitions (key, name, description, promql_template, unit, higher_is_worse)
VALUES
  ('latency_p99', 'Latency p99', 'Response time (higher is worse)',
   'avg(wardn_demo_latency_ms{service_name="{{service}}",version="{{version}}"})', 'ms', true),
  ('error_rate', 'Error rate', 'Error percentage (higher is worse)',
   'avg(wardn_demo_error_rate{service_name="{{service}}",version="{{version}}"})', 'percent', true),
  ('cpu', 'CPU', 'CPU utilisation (higher is worse)',
   'avg(wardn_demo_cpu_pct{service_name="{{service}}",version="{{version}}"})', 'percent', true),
  ('memory', 'Memory', 'Memory footprint (higher is worse)',
   'avg(wardn_demo_mem_mb{service_name="{{service}}",version="{{version}}"})', 'MB', true)
ON CONFLICT (key) DO UPDATE SET
  promql_template = EXCLUDED.promql_template,
  description = EXCLUDED.description;

-- Enable every verdict metric for every existing app (new apps get them seeded).
INSERT INTO app_metrics (app_id, metric_key, enabled)
SELECT a.id, m.key, true
  FROM apps a CROSS JOIN (VALUES ('latency_p99'),('error_rate'),('cpu'),('memory')) AS m(key)
ON CONFLICT DO NOTHING;

-- Per-version dashboards. Each maps a display to a SigNoz metric wardn pulls per
-- version around every deploy marker. Built-ins are seeded; users add custom rows.
CREATE TABLE IF NOT EXISTS dashboards (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    metric_key    TEXT UNIQUE NOT NULL,   -- storage key (what the version charts query)
    signoz_metric TEXT NOT NULL,          -- the metric pulled from SigNoz
    kind          TEXT NOT NULL DEFAULT 'single',  -- 'single' | 'percentiles'
    unit          TEXT NOT NULL DEFAULT '',
    decimals      INT  NOT NULL DEFAULT 0,
    builtin       BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO dashboards (name, metric_key, signoz_metric, kind, unit, decimals, builtin) VALUES
  ('Latency',    'latency_ms',  'wardn_demo_latency_ms', 'percentiles', 'ms',      0, true),
  ('Error rate', 'error_rate',  'wardn_demo_error_rate', 'single',      '%',       2, true),
  ('Throughput', 'throughput',  'wardn_demo_rps',        'single',      ' req/s',  0, true),
  ('CPU',        'cpu_pct',     'wardn_demo_cpu_pct',    'single',      '%',       1, true),
  ('Memory',     'mem_mb',      'wardn_demo_mem_mb',     'single',      ' MB',     0, true)
ON CONFLICT (metric_key) DO NOTHING;
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

const appSelectCols = `id, name, environment, signoz_service_name, window_seconds,
	analysis_delay_seconds, latency_threshold_pct, error_rate_threshold_pp, min_requests,
	ai_enabled`

func scanApp(scanner interface {
	Scan(dest ...any) error
}) (App, error) {
	var a App
	err := scanner.Scan(
		&a.ID, &a.Name, &a.Environment, &a.SignozServiceName, &a.WindowSeconds,
		&a.AnalysisDelaySeconds, &a.LatencyThresholdPct, &a.ErrorRateThresholdPP, &a.MinRequests,
		&a.AIEnabled,
	)
	return a, err
}

// SeedApp upserts an app + its hashed key and returns the app id.
func (s *Store) SeedApp(ctx context.Context, name, apiKeyHash string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO apps (name, api_key_hash, signoz_service_name)
		 VALUES ($1, $2, $1)
		 ON CONFLICT (name) DO UPDATE SET
		   api_key_hash = EXCLUDED.api_key_hash,
		   signoz_service_name = CASE
		     WHEN apps.signoz_service_name = '' THEN EXCLUDED.signoz_service_name
		     ELSE apps.signoz_service_name
		   END
		 RETURNING id`,
		name, apiKeyHash).Scan(&id)
	if err != nil {
		return 0, err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO app_metrics (app_id, metric_key, enabled) VALUES
		 ($1, 'latency_p99', true), ($1, 'error_rate', true), ($1, 'cpu', true), ($1, 'memory', true)
		 ON CONFLICT DO NOTHING`, id)
	return id, err
}

// CreateApp inserts a brand-new app with its hashed key and default enabled
// metrics, then returns the full row. Unlike SeedApp it does not upsert: a name
// that already exists yields ErrAppExists rather than overwriting the key.
func (s *Store) CreateApp(ctx context.Context, name, apiKeyHash string) (App, error) {
	if _, err := s.AppByName(ctx, name); err == nil {
		return App{}, ErrAppExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return App{}, err
	}
	var id int64
	if err := s.db.QueryRowContext(ctx,
		`INSERT INTO apps (name, api_key_hash, signoz_service_name)
		 VALUES ($1, $2, $1) RETURNING id`,
		name, apiKeyHash).Scan(&id); err != nil {
		return App{}, err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO app_metrics (app_id, metric_key, enabled) VALUES
		 ($1, 'latency_p99', true), ($1, 'error_rate', true), ($1, 'cpu', true), ($1, 'memory', true)
		 ON CONFLICT DO NOTHING`, id); err != nil {
		return App{}, err
	}
	return s.AppByID(ctx, id)
}

func (s *Store) SeedUser(ctx context.Context, username, passwordHash, role string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)
		 ON CONFLICT (username) DO NOTHING`,
		username, passwordHash, role)
	return err
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role FROM users WHERE username = $1`,
		username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	return u, err
}

func (s *Store) AppByAPIKeyHash(ctx context.Context, hash string) (App, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+appSelectCols+` FROM apps WHERE api_key_hash = $1`, hash)
	return scanApp(row)
}

func (s *Store) AppByID(ctx context.Context, id int64) (App, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+appSelectCols+` FROM apps WHERE id = $1`, id)
	return scanApp(row)
}

func (s *Store) AppByName(ctx context.Context, name string) (App, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+appSelectCols+` FROM apps WHERE name = $1`, name)
	return scanApp(row)
}

func (s *Store) InsertMetric(ctx context.Context, appID int64, name, version string, value float64, ts time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO metrics (app_id, name, version, value, ts) VALUES ($1, $2, $3, $4, $5)`,
		appID, name, version, value, ts)
	return err
}

func (s *Store) InsertSamples(ctx context.Context, appID int64, name string, samples []Sample) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

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
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+appSelectCols+` FROM apps ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	apps := make([]App, 0, 8)
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}
