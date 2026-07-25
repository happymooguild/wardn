package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// CreateDeployResult is returned by CreateDeploy.
type CreateDeployResult struct {
	Event     DeployEvent
	Created   bool
	Scheduled *time.Time // analysis_scheduled_at when a job was enqueued
}

// CreateDeploy inserts a deploy event (idempotent) and optionally an analysis job.
func (s *Store) CreateDeploy(ctx context.Context, app App, version, environment, source string, deployedAt time.Time, idempotencyKey string) (CreateDeployResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CreateDeployResult{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var existing DeployEvent
	err = tx.QueryRowContext(ctx,
		`SELECT id, app_id, version, previous_version, environment, deployed_at, source, status,
		        idempotency_key, failure_reason, created_at, updated_at
		   FROM deploy_events WHERE idempotency_key = $1`, idempotencyKey).
		Scan(&existing.ID, &existing.AppID, &existing.Version, &existing.PreviousVersion,
			&existing.Environment, &existing.DeployedAt, &existing.Source, &existing.Status,
			&existing.IdempotencyKey, &existing.FailureReason, &existing.CreatedAt, &existing.UpdatedAt)
	if err == nil {
		existing.AppName = app.Name
		if err := tx.Commit(); err != nil {
			return CreateDeployResult{}, err
		}
		return CreateDeployResult{Event: existing, Created: false}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreateDeployResult{}, err
	}

	var prev sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT version FROM deploy_events
		  WHERE app_id = $1
		  ORDER BY deployed_at DESC, id DESC
		  LIMIT 1`, app.ID).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CreateDeployResult{}, err
	}

	// Always schedule analysis, even for the first recorded deploy: the analyzer
	// feeds the per-version Latency dashboard (after-window pull) and its
	// before-window is time-based, so it works without a recorded predecessor.
	// With no prior data the verdict simply resolves to inconclusive/healthy.
	status := "pending_analysis"
	var previousVersion *string
	if prev.Valid {
		previousVersion = &prev.String
	}

	var ev DeployEvent
	err = tx.QueryRowContext(ctx,
		`INSERT INTO deploy_events
		   (app_id, version, previous_version, environment, deployed_at, source, status, idempotency_key)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, app_id, version, previous_version, environment, deployed_at, source, status,
		           idempotency_key, failure_reason, created_at, updated_at`,
		app.ID, version, previousVersion, environment, deployedAt, source, status, idempotencyKey,
	).Scan(&ev.ID, &ev.AppID, &ev.Version, &ev.PreviousVersion, &ev.Environment, &ev.DeployedAt,
		&ev.Source, &ev.Status, &ev.IdempotencyKey, &ev.FailureReason, &ev.CreatedAt, &ev.UpdatedAt)
	if err != nil {
		return CreateDeployResult{}, err
	}
	ev.AppName = app.Name

	var scheduled *time.Time
	if status == "pending_analysis" {
		runAfter := time.Now().UTC().Add(time.Duration(app.AnalysisDelaySeconds) * time.Second)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO analysis_jobs (deploy_event_id, kind, run_after) VALUES ($1, $2, $3)`,
			ev.ID, JobKindMetrics, runAfter)
		if err != nil {
			return CreateDeployResult{}, err
		}
		scheduled = &runAfter
	}

	if err := tx.Commit(); err != nil {
		return CreateDeployResult{}, err
	}
	return CreateDeployResult{Event: ev, Created: true, Scheduled: scheduled}, nil
}

func (s *Store) ListDeploys(ctx context.Context, appName string, limit int) ([]DeployEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT d.id, d.app_id, a.name, d.version, d.previous_version, d.environment,
		        d.deployed_at, d.source, d.status, d.failure_reason, d.created_at, d.updated_at
		   FROM deploy_events d
		   JOIN apps a ON a.id = d.app_id
		  WHERE a.name = $1
		  ORDER BY d.deployed_at DESC, d.id DESC
		  LIMIT $2`, appName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DeployEvent, 0)
	for rows.Next() {
		var d DeployEvent
		if err := rows.Scan(&d.ID, &d.AppID, &d.AppName, &d.Version, &d.PreviousVersion,
			&d.Environment, &d.DeployedAt, &d.Source, &d.Status, &d.FailureReason,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDeploy(ctx context.Context, id int64) (DeployEvent, error) {
	var d DeployEvent
	err := s.db.QueryRowContext(ctx,
		`SELECT d.id, d.app_id, a.name, d.version, d.previous_version, d.environment,
		        d.deployed_at, d.source, d.status, d.idempotency_key, d.failure_reason,
		        d.created_at, d.updated_at
		   FROM deploy_events d
		   JOIN apps a ON a.id = d.app_id
		  WHERE d.id = $1`, id).
		Scan(&d.ID, &d.AppID, &d.AppName, &d.Version, &d.PreviousVersion, &d.Environment,
			&d.DeployedAt, &d.Source, &d.Status, &d.IdempotencyKey, &d.FailureReason,
			&d.CreatedAt, &d.UpdatedAt)
	return d, err
}

func (s *Store) UpdateDeployStatus(ctx context.Context, id int64, status string, failureReason *string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deploy_events SET status = $2, failure_reason = $3, updated_at = now() WHERE id = $1`,
		id, status, failureReason)
	return err
}

func (s *Store) ListSnapshots(ctx context.Context, deployEventID int64) ([]MetricSnapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, deploy_event_id, metric_key, window_before_start, window_before_end,
		        window_after_start, window_after_end, before_value, after_value,
		        before_request_count, after_request_count, delta_pct, delta_abs, degraded,
		        series_before, series_after, COALESCE(raw_query,''), created_at
		   FROM metric_snapshots WHERE deploy_event_id = $1 ORDER BY metric_key`, deployEventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MetricSnapshot, 0)
	for rows.Next() {
		var sn MetricSnapshot
		var beforeJSON, afterJSON []byte
		if err := rows.Scan(&sn.ID, &sn.DeployEventID, &sn.MetricKey, &sn.WindowBeforeStart, &sn.WindowBeforeEnd,
			&sn.WindowAfterStart, &sn.WindowAfterEnd, &sn.BeforeValue, &sn.AfterValue,
			&sn.BeforeRequestCount, &sn.AfterRequestCount, &sn.DeltaPct, &sn.DeltaAbs, &sn.Degraded,
			&beforeJSON, &afterJSON, &sn.RawQuery, &sn.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(beforeJSON, &sn.SeriesBefore)
		_ = json.Unmarshal(afterJSON, &sn.SeriesAfter)
		if sn.SeriesBefore == nil {
			sn.SeriesBefore = []SeriesPoint{}
		}
		if sn.SeriesAfter == nil {
			sn.SeriesAfter = []SeriesPoint{}
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *Store) UpsertSnapshot(ctx context.Context, sn MetricSnapshot) error {
	beforeJSON, err := json.Marshal(sn.SeriesBefore)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(sn.SeriesAfter)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO metric_snapshots (
		   deploy_event_id, metric_key, window_before_start, window_before_end,
		   window_after_start, window_after_end, before_value, after_value,
		   before_request_count, after_request_count, delta_pct, delta_abs, degraded,
		   series_before, series_after, raw_query
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		 ON CONFLICT (deploy_event_id, metric_key) DO UPDATE SET
		   window_before_start = EXCLUDED.window_before_start,
		   window_before_end = EXCLUDED.window_before_end,
		   window_after_start = EXCLUDED.window_after_start,
		   window_after_end = EXCLUDED.window_after_end,
		   before_value = EXCLUDED.before_value,
		   after_value = EXCLUDED.after_value,
		   before_request_count = EXCLUDED.before_request_count,
		   after_request_count = EXCLUDED.after_request_count,
		   delta_pct = EXCLUDED.delta_pct,
		   delta_abs = EXCLUDED.delta_abs,
		   degraded = EXCLUDED.degraded,
		   series_before = EXCLUDED.series_before,
		   series_after = EXCLUDED.series_after,
		   raw_query = EXCLUDED.raw_query`,
		sn.DeployEventID, sn.MetricKey, sn.WindowBeforeStart, sn.WindowBeforeEnd,
		sn.WindowAfterStart, sn.WindowAfterEnd, sn.BeforeValue, sn.AfterValue,
		sn.BeforeRequestCount, sn.AfterRequestCount, sn.DeltaPct, sn.DeltaAbs, sn.Degraded,
		beforeJSON, afterJSON, sn.RawQuery)
	return err
}

func (s *Store) EnabledMetrics(ctx context.Context, appID int64) ([]MetricDefinition, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT md.key, md.name, md.description, md.promql_template, md.unit, md.higher_is_worse
		   FROM app_metrics am
		   JOIN metric_definitions md ON md.key = am.metric_key
		  WHERE am.app_id = $1 AND am.enabled = true
		  ORDER BY md.key`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MetricDefinition, 0)
	for rows.Next() {
		var m MetricDefinition
		if err := rows.Scan(&m.Key, &m.Name, &m.Description, &m.PromQLTemplate, &m.Unit, &m.HigherIsWorse); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ClaimJob locks the next due analysis job. Returns sql.ErrNoRows if none available.
func (s *Store) ClaimJob(ctx context.Context, workerID string, staleAfter time.Duration) (AnalysisJob, DeployEvent, App, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AnalysisJob{}, DeployEvent{}, App{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	stale := time.Now().UTC().Add(-staleAfter)
	var job AnalysisJob
	err = tx.QueryRowContext(ctx,
		`SELECT id, deploy_event_id, kind, run_after, attempts, locked_by, locked_at, done_at, last_error
		   FROM analysis_jobs
		  WHERE done_at IS NULL
		    AND run_after <= now()
		    AND (locked_at IS NULL OR locked_at < $1)
		  ORDER BY run_after
		  FOR UPDATE SKIP LOCKED
		  LIMIT 1`, stale).
		Scan(&job.ID, &job.DeployEventID, &job.Kind, &job.RunAfter, &job.Attempts,
			&job.LockedBy, &job.LockedAt, &job.DoneAt, &job.LastError)
	if err != nil {
		return AnalysisJob{}, DeployEvent{}, App{}, err
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE analysis_jobs
		    SET locked_by = $2, locked_at = now(), attempts = attempts + 1
		  WHERE id = $1`, job.ID, workerID)
	if err != nil {
		return AnalysisJob{}, DeployEvent{}, App{}, err
	}
	job.Attempts++

	var dep DeployEvent
	err = tx.QueryRowContext(ctx,
		`SELECT id, app_id, version, previous_version, environment, deployed_at, source, status,
		        idempotency_key, failure_reason, created_at, updated_at
		   FROM deploy_events WHERE id = $1`, job.DeployEventID).
		Scan(&dep.ID, &dep.AppID, &dep.Version, &dep.PreviousVersion, &dep.Environment,
			&dep.DeployedAt, &dep.Source, &dep.Status, &dep.IdempotencyKey, &dep.FailureReason,
			&dep.CreatedAt, &dep.UpdatedAt)
	if err != nil {
		return AnalysisJob{}, DeployEvent{}, App{}, err
	}

	row := tx.QueryRowContext(ctx, `SELECT `+appSelectCols+` FROM apps WHERE id = $1`, dep.AppID)
	app, err := scanApp(row)
	if err != nil {
		return AnalysisJob{}, DeployEvent{}, App{}, err
	}
	dep.AppName = app.Name

	// Only the metrics pass owns the deploy's verdict. An AI job runs *after*
	// a verdict exists, so it must never walk the status back to 'analyzing'.
	if job.Kind == JobKindMetrics {
		_, err = tx.ExecContext(ctx,
			`UPDATE deploy_events SET status = 'analyzing', updated_at = now() WHERE id = $1 AND status = 'pending_analysis'`,
			dep.ID)
		if err != nil {
			return AnalysisJob{}, DeployEvent{}, App{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return AnalysisJob{}, DeployEvent{}, App{}, err
	}
	return job, dep, app, nil
}

func (s *Store) RescheduleJob(ctx context.Context, jobID int64, runAfter time.Time, unlock bool) error {
	if unlock {
		_, err := s.db.ExecContext(ctx,
			`UPDATE analysis_jobs SET run_after = $2, locked_by = NULL, locked_at = NULL WHERE id = $1`,
			jobID, runAfter)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE analysis_jobs SET run_after = $2 WHERE id = $1`, jobID, runAfter)
	return err
}

func (s *Store) CompleteJob(ctx context.Context, jobID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE analysis_jobs SET done_at = now(), locked_by = NULL, locked_at = NULL WHERE id = $1`,
		jobID)
	return err
}

func (s *Store) FailJob(ctx context.Context, jobID int64, lastError string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE analysis_jobs SET last_error = $2, locked_by = NULL, locked_at = NULL WHERE id = $1`,
		jobID, lastError)
	return err
}

func (s *Store) JobAttempts(ctx context.Context, jobID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT attempts FROM analysis_jobs WHERE id = $1`, jobID).Scan(&n)
	return n, err
}

func FormatIdempotencyKey(appID int64, version string, deployedAt time.Time) string {
	return fmt.Sprintf("%d|%s|%s", appID, version, deployedAt.UTC().Format(time.RFC3339Nano))
}
