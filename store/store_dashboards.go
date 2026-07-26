package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ErrDashboardExists is returned when a custom dashboard's derived key collides.
var ErrDashboardExists = errors.New("dashboard already exists")

// Dashboard is a per-version view backed by a SigNoz metric.
type Dashboard struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	MetricKey    string    `json:"metric_key"`
	SignozMetric string    `json:"signoz_metric"`
	Kind         string    `json:"kind"` // 'single' | 'percentiles'
	Unit         string    `json:"unit"`
	Decimals     int       `json:"decimals"`
	Builtin      bool      `json:"builtin"`
	CreatedAt    time.Time `json:"created_at"`
}

const dashboardCols = `id, name, metric_key, signoz_metric, kind, unit, decimals, builtin, created_at`

func scanDashboard(sc interface{ Scan(...any) error }) (Dashboard, error) {
	var d Dashboard
	err := sc.Scan(&d.ID, &d.Name, &d.MetricKey, &d.SignozMetric, &d.Kind, &d.Unit, &d.Decimals, &d.Builtin, &d.CreatedAt)
	return d, err
}

// ListDashboards returns all dashboards, built-ins first, then by creation.
func (s *Store) ListDashboards(ctx context.Context) ([]Dashboard, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+dashboardCols+` FROM dashboards ORDER BY builtin DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Dashboard, 0, 8)
	for rows.Next() {
		d, err := scanDashboard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugKey turns a display name into a stable storage key (custom_<slug>).
func slugKey(name string) string {
	s := slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "metric"
	}
	return "custom_" + s
}

// CreateDashboard inserts a custom dashboard, deriving a unique metric_key from
// the name. Returns ErrDashboardExists if that key is already taken.
func (s *Store) CreateDashboard(ctx context.Context, name, signozMetric, kind, unit string, decimals int) (Dashboard, error) {
	key := slugKey(name)
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO dashboards (name, metric_key, signoz_metric, kind, unit, decimals, builtin)
		 VALUES ($1,$2,$3,$4,$5,$6,false)
		 RETURNING `+dashboardCols,
		name, key, signozMetric, kind, unit, decimals)
	d, err := scanDashboard(row)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return Dashboard{}, ErrDashboardExists
		}
		return Dashboard{}, err
	}
	return d, nil
}

// DeleteDashboard removes a custom dashboard (built-ins are protected).
func (s *Store) DeleteDashboard(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dashboards WHERE id = $1 AND builtin = false`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("dashboard not found or is built-in")
	}
	return nil
}

// DashboardMetric is the (storage key, SigNoz metric) pair the analyzer pulls.
type DashboardMetric struct {
	MetricKey    string
	SignozMetric string
}

// DashboardMetrics returns the distinct metrics to snapshot per version - the
// union across all dashboards. Drives the analyzer's per-version pull.
func (s *Store) DashboardMetrics(ctx context.Context) ([]DashboardMetric, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT metric_key, signoz_metric FROM dashboards`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DashboardMetric, 0, 8)
	for rows.Next() {
		var m DashboardMetric
		if err := rows.Scan(&m.MetricKey, &m.SignozMetric); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// BackfillDeploy is a recent deploy plus the app context needed to re-pull a
// metric for its after-window when a new dashboard is created.
type BackfillDeploy struct {
	AppID             int64
	SignozServiceName string
	Version           string
	DeployedAt        time.Time
	WindowSeconds     int
}

// RecentDeploysForBackfill lists recent analyzed deploys (newest first) so a
// freshly created dashboard can be populated from SigNoz for existing versions.
func (s *Store) RecentDeploysForBackfill(ctx context.Context, limit int) ([]BackfillDeploy, error) {
	if limit <= 0 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, a.signoz_service_name, d.version, d.deployed_at, a.window_seconds
		   FROM deploy_events d JOIN apps a ON a.id = d.app_id
		  WHERE d.version <> ''
		  ORDER BY d.deployed_at DESC, d.id DESC
		  LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BackfillDeploy, 0, limit)
	for rows.Next() {
		var b BackfillDeploy
		if err := rows.Scan(&b.AppID, &b.SignozServiceName, &b.Version, &b.DeployedAt, &b.WindowSeconds); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
