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

	"wardn/ai"
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

	// AI reasoning (design-doc §8). Nil-safe: without a resolver or telemetry
	// provider the metrics pass is unaffected and AI jobs fail with an
	// actionable message rather than panicking.
	Telemetry metrics.TelemetryProvider
	AI        *ai.Resolver
	Bounds    ai.Bounds
	AITimeout time.Duration
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
		Bounds:   ai.DefaultBounds(),
	}
}

func (w *Worker) Run(ctx context.Context) {
	if w.Poll <= 0 {
		w.Poll = 5 * time.Second
	}
	log.Printf("analyzer: worker %s started (poll %s)", w.WorkerID, w.Poll)
	t := time.NewTicker(w.Poll)
	defer t.Stop()
	sweep := time.NewTicker(5 * time.Minute)
	defer sweep.Stop()
	for {
		w.tick(ctx)
		select {
		case <-ctx.Done():
			log.Print("analyzer: stopping")
			return
		case <-sweep.C:
			w.sweepStaleAnalyses(ctx)
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

	var perr error
	switch job.Kind {
	case store.JobKindAI:
		perr = w.processAI(ctx, job, deploy, app)
	default:
		perr = w.process(ctx, job, deploy, app)
	}
	if perr == nil {
		return
	}

	log.Printf("analyzer: %s job %d deploy %d: %v", job.Kind, job.ID, deploy.ID, perr)
	_ = w.Store.FailJob(ctx, job.ID, perr.Error())
	if job.Attempts < maxAttempts {
		return
	}

	// Out of retries. Which record gets marked failed depends on the job kind:
	// an exhausted AI job must not mark the deploy itself failed — the deploy's
	// verdict came from the metrics pass and is still valid.
	reason := perr.Error()
	if job.Kind == store.JobKindAI {
		if analysis, err := w.Store.PendingAnalysis(ctx, deploy.ID); err == nil {
			_ = w.Store.FailAnalysis(ctx, analysis.ID, "failed", reason, map[string]any{})
		}
	} else {
		_ = w.Store.UpdateDeployStatus(ctx, deploy.ID, "failed", &reason)
	}
	_ = w.Store.CompleteJob(ctx, job.ID)
}

// afterSettle is skipped at the start of the after-window so a rolling deploy's
// terminating old pod can't pollute the new version's samples.
const afterSettle = 15 * time.Second

// ingestVersionMetrics pulls raw samples for every dashboard's metric (built-in
// and custom, read from the DB) over the service's after-window from SigNoz and
// stores them keyed by this deploy's version, powering the per-version
// dashboards. Best-effort: per-metric failures are logged and skipped, never
// fatal, so they don't undo an already-written verdict.
func (w *Worker) ingestVersionMetrics(ctx context.Context, app store.App, deploy store.DeployEvent, start, end time.Time) {
	metrics, err := w.Store.DashboardMetrics(ctx)
	if err != nil {
		log.Printf("analyzer: deploy %d dashboard metrics: %v", deploy.ID, err)
		return
	}
	for _, m := range metrics {
		// Filter by the deploy's exact version so we pull only this version's
		// samples — never a prior version's stale series carried forward by the
		// query engine into our window. (Marker version == emitted service version.)
		promql := fmt.Sprintf(`%s{service_name=%q,version=%q}`, m.SignozMetric, app.SignozServiceName, deploy.Version)
		series, err := w.Metrics.QuerySeries(ctx, promql, start, end, 5) // ~native scrape resolution
		if err != nil {
			log.Printf("analyzer: deploy %d %s pull: %v", deploy.ID, m.MetricKey, err)
			continue
		}
		if len(series.Points) == 0 {
			continue
		}
		samples := make([]store.Sample, 0, len(series.Points))
		for _, p := range series.Points {
			samples = append(samples, store.Sample{Version: deploy.Version, Value: p.V, TS: p.T})
		}
		if err := w.Store.InsertSamples(ctx, app.ID, m.MetricKey, samples); err != nil {
			log.Printf("analyzer: deploy %d %s store: %v", deploy.ID, m.MetricKey, err)
			continue
		}
		log.Printf("analyzer: deploy %d stored %d %s samples for version %s", deploy.ID, len(samples), m.MetricKey, deploy.Version)
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
	// Skip the first `afterSettle` seconds after the marker: during a rolling
	// deploy the terminating old pod keeps emitting briefly, and those samples
	// would pollute the new version's window. Start measuring once it's settled.
	afterStart := T.Add(afterSettle)
	afterEnd := afterStart.Add(window)
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

	// Version-filter the comparison: the after-window is this deploy's version,
	// the before-window the previous version. Without this a prior version's
	// stale (carried-forward) series bleeds into the after-window and skews the
	// verdict. Marker version == emitted service version, so this lines up.
	prevVersion := ""
	if deploy.PreviousVersion != nil {
		prevVersion = *deploy.PreviousVersion
	}

	anyDegraded := false
	anyData := false
	snapshots := make([]store.MetricSnapshot, 0, len(defs))

	for _, def := range defs {
		afterPromql := renderTemplate(def.PromQLTemplate, app.SignozServiceName, app.Environment, deploy.Version)
		beforePromql := renderTemplate(def.PromQLTemplate, app.SignozServiceName, app.Environment, prevVersion)
		beforeSeries, errB := w.Metrics.Query(ctx, beforePromql, beforeStart, beforeEnd)
		afterSeries, errA := w.Metrics.Query(ctx, afterPromql, afterStart, afterEnd)
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
			RawQuery:          afterPromql,
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
					// Absolute percentage-point rise only. A relative % change on a
					// sub-1% baseline is noise (0.9%→1.2% is +40% but meaningless),
					// so error_rate must clear the pp threshold, not the latency %.
					degraded = (*afterVal - *beforeVal) >= app.ErrorRateThresholdPP
				} else {
					// latency, cpu, memory: a sufficient relative rise is a regression.
					degraded = deltaPct >= app.LatencyThresholdPct
				}
			} else {
				// lower-is-worse (e.g. throughput): a sufficient relative drop is a
				// regression.
				degraded = deltaPct <= -app.LatencyThresholdPct
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

	// Feed the per-version dashboards (latency, error rate, throughput) straight
	// from SigNoz: pull each metric's raw samples for the after-window and store
	// them keyed by the deploy's version. Postgres then computes the per-version
	// stats. This replaces the old continuous sample-app push — wardn only reads
	// SigNoz, and only around markers.
	w.ingestVersionMetrics(ctx, app, deploy, afterStart, afterEnd)

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
	if status == "regressed" {
		if w.Alerts != nil {
			w.Alerts.NotifyRegression(ctx, app, deploy, snapshots)
		}
		// Per design-doc §4: automatic root-cause runs on a regression only
		// when the app opted in. Enqueue failures are logged, not returned —
		// the metrics verdict is already written and must not be undone by a
		// retry of this job.
		if app.AIEnabled && w.AI != nil {
			if _, err := w.Store.CreateAnalysis(ctx, deploy.ID, "auto"); err != nil {
				log.Printf("analyzer: enqueue auto analysis for deploy %d: %v", deploy.ID, err)
			}
		}
	}
	return nil
}

func renderTemplate(tmpl, service, environment, version string) string {
	out := strings.ReplaceAll(tmpl, "{{service}}", service)
	out = strings.ReplaceAll(out, "{{environment}}", environment)
	out = strings.ReplaceAll(out, "{{version}}", version)
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
