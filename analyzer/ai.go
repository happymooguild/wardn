package analyzer

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"wardn/ai"
	"wardn/metrics"
	"wardn/store"
)

// processAI runs the LLM root-cause pass for one deploy.
//
// Failure policy: anything the operator must fix (no provider configured, a
// refusal, an unusable stored key) is terminal — the analysis row records why
// and the job completes. Only transient transport errors bubble up to be
// retried by the job machinery.
func (w *Worker) processAI(ctx context.Context, job store.AnalysisJob, deploy store.DeployEvent, app store.App) error {
	analysis, err := w.Store.PendingAnalysis(ctx, deploy.ID)
	if errors.Is(err, sql.ErrNoRows) {
		// The row was already resolved (cancelled, or swept as stale). Nothing
		// to do — completing the job stops it from spinning.
		return w.Store.CompleteJob(ctx, job.ID)
	}
	if err != nil {
		return err
	}

	if w.AI == nil {
		return w.terminal(ctx, job, analysis.ID, "failed", ai.ErrNotConfigured.Error(), nil)
	}

	provider, cfg, err := w.AI.Resolve(ctx)
	if err != nil {
		return w.terminal(ctx, job, analysis.ID, "failed", err.Error(), nil)
	}
	if err := w.Store.MarkAnalysisRunning(ctx, analysis.ID, cfg.Kind, cfg.Model); err != nil {
		return err
	}

	snapshots, err := w.Store.ListSnapshots(ctx, deploy.ID)
	if err != nil {
		return err
	}

	input := ai.Input{
		App:       app,
		Deploy:    deploy,
		Snapshots: snapshots,
	}
	input.LogsBefore, input.LogsAfter, input.TracesBefore, input.TracesAfter, input.TelemetryError =
		w.gatherTelemetry(ctx, app, deploy)

	req, stats := ai.Build(input, w.Bounds)
	log.Printf("analyzer: ai deploy %d — prompt %d chars, %d/%d after-logs, %d/%d after-traces",
		deploy.ID, stats.PromptChars, stats.LogsSentAfter, stats.LogsAvailableAfter,
		stats.TracesSentAfter, stats.TracesAvailableAfter)

	callCtx, cancel := context.WithTimeout(ctx, w.aiTimeout())
	defer cancel()

	result, err := provider.Analyze(callCtx, req)
	if err != nil {
		var refused *ai.ErrRefused
		if errors.As(err, &refused) {
			return w.terminal(ctx, job, analysis.ID, "refused", refused.Error(), stats)
		}
		// A bad key or an unknown model is terminal — retrying just repeats the
		// same rejection and delays the operator seeing it.
		var permanent *ai.ErrPermanent
		if errors.As(err, &permanent) {
			return w.terminal(ctx, job, analysis.ID, "failed", permanent.Error(), stats)
		}
		// Transport, timeout, or decode failure — let the job retry.
		return err
	}

	if err := w.Store.CompleteAnalysis(ctx, analysis.ID, store.AnalysisResult{
		Provider:     provider.Name(),
		Model:        result.Model,
		Summary:      result.Verdict.Summary,
		LikelyCause:  result.Verdict.LikelyCause,
		Confidence:   result.Verdict.Confidence,
		Evidence:     result.Verdict.Evidence,
		Suggested:    result.Verdict.Suggested,
		ContextStats: stats,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
	}); err != nil {
		return err
	}

	log.Printf("analyzer: ai deploy %d (%s@%s) → %s confidence, %d in / %d out tokens",
		deploy.ID, app.Name, deploy.Version, result.Verdict.Confidence,
		result.InputTokens, result.OutputTokens)

	return w.Store.CompleteJob(ctx, job.ID)
}

// terminal records a non-retryable outcome and closes the job.
func (w *Worker) terminal(ctx context.Context, job store.AnalysisJob, analysisID int64, status, message string, stats any) error {
	if stats == nil {
		stats = map[string]any{}
	}
	if err := w.Store.FailAnalysis(ctx, analysisID, status, message, stats); err != nil {
		return err
	}
	log.Printf("analyzer: ai analysis %d → %s: %s", analysisID, status, message)
	return w.Store.CompleteJob(ctx, job.ID)
}

// gatherTelemetry fetches logs and traces for both windows. Telemetry is
// best-effort: if SigNoz is unreachable the analysis still runs on metrics
// alone, with the prompt told explicitly that log evidence was missing rather
// than being left to infer the service was clean.
func (w *Worker) gatherTelemetry(ctx context.Context, app store.App, deploy store.DeployEvent) (
	logsBefore, logsAfter []metrics.LogRecord,
	tracesBefore, tracesAfter []metrics.SpanRecord,
	telemetryErr string,
) {
	if w.Telemetry == nil {
		return nil, nil, nil, nil, "no telemetry provider configured (set SIGNOZ_URL and SIGNOZ_API_KEY)"
	}

	window := time.Duration(app.WindowSeconds) * time.Second
	if window <= 0 {
		window = 2 * time.Minute
	}
	t := deploy.DeployedAt.UTC()

	service := app.SignozServiceName
	if service == "" {
		service = app.Name
	}

	beforeQ := metrics.TelemetryQuery{
		Service: service, Environment: app.Environment,
		Start: t.Add(-window), End: t, ErrorsOnly: true,
	}
	afterQ := metrics.TelemetryQuery{
		Service: service, Environment: app.Environment,
		Start: t, End: t.Add(window), ErrorsOnly: true,
	}
	// Ask for more than the bounder will keep: dedup collapses duplicates, so
	// over-fetching is what makes the top-N actually representative.
	beforeQ.Limit = w.telemetryFetchLimit()
	afterQ.Limit = w.telemetryFetchLimit()

	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	var err error
	if logsAfter, err = w.Telemetry.Logs(ctx, afterQ); err != nil {
		record(err)
	}
	if logsBefore, err = w.Telemetry.Logs(ctx, beforeQ); err != nil {
		record(err)
	}
	if tracesAfter, err = w.Telemetry.Traces(ctx, afterQ); err != nil {
		record(err)
	}
	if tracesBefore, err = w.Telemetry.Traces(ctx, beforeQ); err != nil {
		record(err)
	}

	// Only report a telemetry failure when it actually left us empty-handed.
	// A partial fetch is still worth reasoning over.
	if firstErr != nil && len(logsAfter) == 0 && len(tracesAfter) == 0 {
		log.Printf("analyzer: ai deploy %d telemetry unavailable: %v", deploy.ID, firstErr)
		return nil, nil, nil, nil, firstErr.Error()
	}
	if firstErr != nil {
		log.Printf("analyzer: ai deploy %d partial telemetry: %v", deploy.ID, firstErr)
	}
	return logsBefore, logsAfter, tracesBefore, tracesAfter, ""
}

func (w *Worker) telemetryFetchLimit() int {
	b := w.Bounds
	if b.ErrorLogsAfter <= 0 {
		b = ai.DefaultBounds()
	}
	if n := b.ErrorLogsAfter * 10; n > 0 {
		return n
	}
	return 200
}

func (w *Worker) aiTimeout() time.Duration {
	if w.AITimeout > 0 {
		return w.AITimeout
	}
	return 120 * time.Second
}

// sweepStaleAnalyses fails analyses abandoned by a worker that died mid-call,
// so the UI stops polling a row nothing will ever advance.
func (w *Worker) sweepStaleAnalyses(ctx context.Context) {
	n, err := w.Store.StaleAnalyses(ctx, 15*time.Minute)
	if err != nil {
		log.Printf("analyzer: sweep stale analyses: %v", err)
		return
	}
	if n > 0 {
		log.Printf("analyzer: failed %d stale analyses", n)
	}
}
