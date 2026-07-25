// Package analyzer compares before/after SigNoz metrics around a deploy marker.
package analyzer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"wardn/alert"
	"wardn/metrics"
	"wardn/store"
)

const maxAttempts = 5

type Worker struct {
	Store    *store.Store
	Metrics  metrics.MetricsProvider
	Alerts   *alert.Engine
	Poll     time.Duration
	WorkerID string
}

func New(st *store.Store, mp metrics.MetricsProvider, alerts *alert.Engine, poll time.Duration) *Worker {
	host, _ := os.Hostname()
	if host == "" {
		host = "wardn"
	}
	return &Worker{
		Store:    st,
		Metrics:  mp,
		Alerts:   alerts,
		Poll:     poll,
		WorkerID: fmt.Sprintf("%s-%d", host, os.Getpid()),
	}
}

func (w *Worker) Run(ctx context.Context) {
	if w.Poll <= 0 {
		w.Poll = 5 * time.Second
	}
	log.Printf("analyzer: worker %s started (poll %s)", w.WorkerID, w.Poll)
	t := time.NewTicker(w.Poll)
	defer t.Stop()
	for {
		w.tick(ctx)
		select {
		case <-ctx.Done():
			log.Print("analyzer: stopping")
			return
		case <-t.C:
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	job, deploy, app, err := w.Store.ClaimJob(ctx, w.WorkerID, 5*time.Minute)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("analyzer: claim: %v", err)
		}
		return
	}
	if err := w.process(ctx, job, deploy, app); err != nil {
		log.Printf("analyzer: job %d deploy %d: %v", job.ID, deploy.ID, err)
		_ = w.Store.FailJob(ctx, job.ID, err.Error())
		if job.Attempts >= maxAttempts {
			reason := err.Error()
			_ = w.Store.UpdateDeployStatus(ctx, deploy.ID, "failed", &reason)
			_ = w.Store.CompleteJob(ctx, job.ID)
		}
	}
}

func (w *Worker) process(ctx context.Context, job store.AnalysisJob, deploy store.DeployEvent, app store.App) error {
	if w.Metrics == nil {
		return fmt.Errorf("metrics provider not configured (set SIGNOZ_URL and SIGNOZ_API_KEY)")
	}

	window := time.Duration(app.WindowSeconds) * time.Second
	if window <= 0 {
		window = 2 * time.Minute
	}
	T := deploy.DeployedAt.UTC()
	afterEnd := T.Add(window)
	now := time.Now().UTC()
	if now.Before(afterEnd) {
		log.Printf("analyzer: deploy %d waiting until after-window ends (%s)", deploy.ID, afterEnd.Format(time.RFC3339))
		return w.Store.RescheduleJob(ctx, job.ID, afterEnd, true)
	}

	defs, err := w.Store.EnabledMetrics(ctx, app.ID)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		status := "inconclusive"
		reason := "no metrics enabled"
		_ = w.Store.UpdateDeployStatus(ctx, deploy.ID, status, &reason)
		return w.Store.CompleteJob(ctx, job.ID)
	}

	beforeStart := T.Add(-window)
	beforeEnd := T
	afterStart := T

	anyDegraded := false
	anyData := false
	snapshots := make([]store.MetricSnapshot, 0, len(defs))

	for _, def := range defs {
		promql := renderTemplate(def.PromQLTemplate, app.SignozServiceName, app.Environment)
		beforeSeries, errB := w.Metrics.Query(ctx, promql, beforeStart, beforeEnd)
		afterSeries, errA := w.Metrics.Query(ctx, promql, afterStart, afterEnd)
		if errB != nil {
			return fmt.Errorf("query before %s: %w", def.Key, errB)
		}
		if errA != nil {
			return fmt.Errorf("query after %s: %w", def.Key, errA)
		}

		sn := store.MetricSnapshot{
			DeployEventID:     deploy.ID,
			MetricKey:         def.Key,
			WindowBeforeStart: beforeStart,
			WindowBeforeEnd:   beforeEnd,
			WindowAfterStart:  afterStart,
			WindowAfterEnd:    afterEnd,
			RawQuery:          promql,
			SeriesBefore:      toSeriesPoints(metrics.Downsample(beforeSeries.Points, 120)),
			SeriesAfter:       toSeriesPoints(metrics.Downsample(afterSeries.Points, 120)),
		}

		if len(beforeSeries.Points) > 0 || len(afterSeries.Points) > 0 {
			anyData = true
		}

		var beforeVal, afterVal *float64
		if len(beforeSeries.Points) > 0 {
			v := beforeSeries.Scalar
			beforeVal = &v
			sn.BeforeValue = beforeVal
		}
		if len(afterSeries.Points) > 0 {
			v := afterSeries.Scalar
			afterVal = &v
			sn.AfterValue = afterVal
		}

		bc := int64(len(beforeSeries.Points))
		ac := int64(len(afterSeries.Points))
		sn.BeforeRequestCount = &bc
		sn.AfterRequestCount = &ac

		if beforeVal != nil && afterVal != nil {
			deltaAbs := *afterVal - *beforeVal
			eps := math.Max(0.01*math.Abs(*beforeVal), epsilonFloor(def.Key))
			denom := math.Max(math.Abs(*beforeVal), eps)
			deltaPct := (deltaAbs / denom) * 100
			sn.DeltaAbs = &deltaAbs
			sn.DeltaPct = &deltaPct

			degraded := false
			if def.HigherIsWorse {
				if def.Key == "error_rate" {
					degraded = (*afterVal-*beforeVal) >= app.ErrorRateThresholdPP ||
						(*beforeVal > 0 && deltaPct >= app.LatencyThresholdPct)
				} else {
					degraded = deltaPct >= app.LatencyThresholdPct
				}
			}
			sn.Degraded = degraded
			if degraded {
				anyDegraded = true
			}
		}

		if err := w.Store.UpsertSnapshot(ctx, sn); err != nil {
			return err
		}
		snapshots = append(snapshots, sn)
	}

	status := "healthy"
	var reason *string
	volumeOK := false
	for _, sn := range snapshots {
		if sn.AfterRequestCount != nil && *sn.AfterRequestCount >= int64(app.MinRequests) {
			volumeOK = true
			break
		}
		// If we got scalar values, treat as enough signal for demo windows.
		if sn.AfterValue != nil {
			volumeOK = true
			break
		}
	}
	if !anyData {
		status = "inconclusive"
		r := "no metric data returned from SigNoz for before/after windows"
		reason = &r
	} else if !volumeOK {
		status = "inconclusive"
		r := "insufficient after-window samples"
		reason = &r
	} else if anyDegraded {
		status = "regressed"
	}

	if err := w.Store.UpdateDeployStatus(ctx, deploy.ID, status, reason); err != nil {
		return err
	}
	deploy.Status = status
	if err := w.Store.CompleteJob(ctx, job.ID); err != nil {
		return err
	}

	log.Printf("analyzer: deploy %d (%s@%s) → %s", deploy.ID, app.Name, deploy.Version, status)
	if status == "regressed" && w.Alerts != nil {
		w.Alerts.NotifyRegression(ctx, app, deploy, snapshots)
	}
	return nil
}

func renderTemplate(tmpl, service, environment string) string {
	out := strings.ReplaceAll(tmpl, "{{service}}", service)
	out = strings.ReplaceAll(out, "{{environment}}", environment)
	return out
}

func epsilonFloor(metricKey string) float64 {
	if metricKey == "error_rate" {
		return 0.01
	}
	return 0.1 // ms
}

func toSeriesPoints(pts []metrics.Point) []store.SeriesPoint {
	out := make([]store.SeriesPoint, len(pts))
	for i, p := range pts {
		out[i] = store.SeriesPoint{T: p.T.Unix(), V: p.V}
	}
	return out
}

// Compare is exported for unit tests of the delta math.
func Compare(before, after, thresholdPct float64, higherIsWorse bool) (deltaPct float64, degraded bool) {
	eps := math.Max(0.01*math.Abs(before), 0.1)
	denom := math.Max(math.Abs(before), eps)
	deltaPct = ((after - before) / denom) * 100
	if higherIsWorse {
		degraded = deltaPct >= thresholdPct
	} else {
		degraded = deltaPct <= -thresholdPct
	}
	return deltaPct, degraded
}
