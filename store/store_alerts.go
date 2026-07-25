package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

const alertSelectCols = `id, app_id, metric_key, channel_type, channel_config, on_verdict, threshold_pct, enabled, created_at`

func (s *Store) ListAlertConfigs(ctx context.Context, appID int64) ([]AlertConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+alertSelectCols+` FROM alert_configs WHERE app_id = $1 ORDER BY id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AlertConfig, 0)
	for rows.Next() {
		c, err := scanAlertConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetAlertConfig(ctx context.Context, id int64) (AlertConfig, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+alertSelectCols+` FROM alert_configs WHERE id = $1`, id)
	return scanAlertConfig(row)
}

func (s *Store) CreateAlertConfig(ctx context.Context, appID int64, metricKey *string, channelType string, channelConfig json.RawMessage, onVerdict string, thresholdPct *float64, enabled bool) (AlertConfig, error) {
	if onVerdict == "" {
		onVerdict = "regressed"
	}
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO alert_configs (app_id, metric_key, channel_type, channel_config, on_verdict, threshold_pct, enabled)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING `+alertSelectCols,
		appID, metricKey, channelType, channelConfig, onVerdict, thresholdPct, enabled)
	return scanAlertConfig(row)
}

func (s *Store) UpdateAlertConfig(ctx context.Context, id int64, metricKey *string, channelType string, channelConfig json.RawMessage, onVerdict string, thresholdPct *float64, enabled bool) (AlertConfig, error) {
	row := s.db.QueryRowContext(ctx,
		`UPDATE alert_configs SET
		   metric_key = $2, channel_type = $3, channel_config = $4, on_verdict = $5,
		   threshold_pct = $6, enabled = $7
		 WHERE id = $1
		 RETURNING `+alertSelectCols,
		id, metricKey, channelType, channelConfig, onVerdict, thresholdPct, enabled)
	return scanAlertConfig(row)
}

func (s *Store) DeleteAlertConfig(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alert_configs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListEnabledAlertsForApp(ctx context.Context, appID int64) ([]AlertConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+alertSelectCols+` FROM alert_configs WHERE app_id = $1 AND enabled = true`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AlertConfig, 0)
	for rows.Next() {
		c, err := scanAlertConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ListMetricDefinitions(ctx context.Context) ([]MetricDefinition, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, name, description, promql_template, unit, higher_is_worse
		   FROM metric_definitions ORDER BY key`)
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

func (s *Store) DeliveryExists(ctx context.Context, alertConfigID, deployEventID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM alert_deliveries WHERE alert_config_id = $1 AND deploy_event_id = $2`,
		alertConfigID, deployEventID).Scan(&n)
	return n > 0, err
}

func (s *Store) InsertDelivery(ctx context.Context, alertConfigID, deployEventID int64, status string, responseCode *int, errMsg *string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_deliveries (alert_config_id, deploy_event_id, status, response_code, error_message)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (alert_config_id, deploy_event_id) DO UPDATE SET
		   status = EXCLUDED.status,
		   response_code = EXCLUDED.response_code,
		   error_message = EXCLUDED.error_message,
		   created_at = now()`,
		alertConfigID, deployEventID, status, responseCode, errMsg)
	return err
}

func (s *Store) ListDeliveries(ctx context.Context, appID int64, limit int) ([]AlertDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT d.id, d.alert_config_id, d.deploy_event_id, d.status, d.response_code, d.error_message, d.created_at
		   FROM alert_deliveries d
		   JOIN alert_configs c ON c.id = d.alert_config_id
		  WHERE c.app_id = $1
		  ORDER BY d.created_at DESC
		  LIMIT $2`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AlertDelivery, 0)
	for rows.Next() {
		var d AlertDelivery
		if err := rows.Scan(&d.ID, &d.AlertConfigID, &d.DeployEventID, &d.Status, &d.ResponseCode, &d.ErrorMessage, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanAlertConfig(scanner interface {
	Scan(dest ...any) error
}) (AlertConfig, error) {
	var c AlertConfig
	var metricKey sql.NullString
	var threshold sql.NullFloat64
	var cfg []byte
	err := scanner.Scan(&c.ID, &c.AppID, &metricKey, &c.ChannelType, &cfg, &c.OnVerdict, &threshold, &c.Enabled, &c.CreatedAt)
	if err != nil {
		return c, err
	}
	if metricKey.Valid {
		c.MetricKey = &metricKey.String
	}
	if threshold.Valid {
		v := threshold.Float64
		c.ThresholdPct = &v
	}
	c.ChannelConfig = json.RawMessage(cfg)
	return c, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
