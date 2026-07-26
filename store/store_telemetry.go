package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// SaveTelemetry upserts the before/after logs and traces captured for a deploy.
// Values are opaque JSON arrays ([]metrics.LogRecord / []metrics.SpanRecord)
// marshalled by the caller, keeping this layer decoupled from the metrics types.
func (s *Store) SaveTelemetry(ctx context.Context, deployID int64, logsBefore, logsAfter, tracesBefore, tracesAfter json.RawMessage) error {
	norm := func(r json.RawMessage) json.RawMessage {
		if len(r) == 0 {
			return json.RawMessage("[]")
		}
		return r
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deploy_telemetry (deploy_event_id, logs_before, logs_after, traces_before, traces_after)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (deploy_event_id) DO UPDATE SET
		   logs_before = EXCLUDED.logs_before,
		   logs_after = EXCLUDED.logs_after,
		   traces_before = EXCLUDED.traces_before,
		   traces_after = EXCLUDED.traces_after,
		   captured_at = now()`,
		deployID, norm(logsBefore), norm(logsAfter), norm(tracesBefore), norm(tracesAfter))
	return err
}

// TelemetryForVersion returns the after-window logs and traces of the most
// recent deploy of a given version - used by the version-compare AI flow.
func (s *Store) TelemetryForVersion(ctx context.Context, appID int64, version string) (logsAfter, tracesAfter json.RawMessage, found bool, err error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT dt.logs_after, dt.traces_after
		   FROM deploy_telemetry dt JOIN deploy_events d ON d.id = dt.deploy_event_id
		  WHERE d.app_id = $1 AND d.version = $2
		  ORDER BY d.deployed_at DESC, d.id DESC
		  LIMIT 1`, appID, version)
	err = row.Scan(&logsAfter, &tracesAfter)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	return logsAfter, tracesAfter, true, nil
}

// LoadTelemetry returns the stored before/after logs and traces for a deploy.
// found is false when nothing was captured (caller may fall back to a live pull).
func (s *Store) LoadTelemetry(ctx context.Context, deployID int64) (logsBefore, logsAfter, tracesBefore, tracesAfter json.RawMessage, found bool, err error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT logs_before, logs_after, traces_before, traces_after
		   FROM deploy_telemetry WHERE deploy_event_id = $1`, deployID)
	err = row.Scan(&logsBefore, &logsAfter, &tracesBefore, &tracesAfter)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, nil, nil, false, err
	}
	return logsBefore, logsAfter, tracesBefore, tracesAfter, true, nil
}
