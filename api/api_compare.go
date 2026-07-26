package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"wardn/ai"
	"wardn/store"
)

// listAppVersions returns the versions available to compare for an app.
func (a *API) listAppVersions(c *gin.Context) {
	app, ok := a.appFromParam(c)
	if !ok {
		return
	}
	versions, err := a.st.AppVersions(c, app.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list versions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"app": app.Name, "versions": versions})
}

type compareReq struct {
	VersionA string `json:"version_a"`
	VersionB string `json:"version_b"`
}

// metricCompare is one metric's A-vs-B comparison.
type metricCompare struct {
	Metric      string   `json:"metric"`
	Key         string   `json:"key"`
	Unit        string   `json:"unit"`
	A           *float64 `json:"a"`
	B           *float64 `json:"b"`
	DeltaPct    *float64 `json:"delta_pct"`
	HigherWorse bool     `json:"higher_worse"`
	Degraded    bool     `json:"degraded"`
}

// compareVersions produces an AI summary of the difference between two versions,
// grounded in metrics + logs + traces, and a deterministic regression flag.
func (a *API) compareVersions(c *gin.Context) {
	app, ok := a.appFromParam(c)
	if !ok {
		return
	}
	var req compareReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.VersionA) == "" || strings.TrimSpace(req.VersionB) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version_a and version_b are required"})
		return
	}
	if a.ai == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no AI provider configured - set one in AI Settings"})
		return
	}
	if _, _, err := a.ai.Resolve(c); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no AI provider configured - set one in AI Settings"})
		return
	}

	metrics, degraded := a.gatherVersionMetrics(c, app, req.VersionA, req.VersionB)
	logsA, tracesA := a.versionTelemetry(c, app, req.VersionA)
	logsB, tracesB := a.versionTelemetry(c, app, req.VersionB)

	system := "You are a senior SRE comparing two deployed versions of a service. Be concise and specific. " +
		"Ground every claim in the supplied metrics, logs, and traces; never invent data. " +
		"If version B is worse on latency, errors, or resource use, say so plainly."
	prompt := buildComparePrompt(app.Name, req.VersionA, req.VersionB, metrics, logsA, logsB, tracesA, tracesB)

	verdict, provider, model, err := a.runAI(c, system, prompt)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"app":              app.Name,
		"version_a":        req.VersionA,
		"version_b":        req.VersionB,
		"summary":          verdict.Summary,
		"detail":           verdict.LikelyCause,
		"confidence":       verdict.Confidence,
		"evidence":         verdict.Evidence,
		"metrics":          metrics,
		"is_regression":    len(degraded) > 0,
		"degraded_metrics": degraded,
		"provider":         provider,
		"model":            model,
	})
}

// rootCauseVersions asks the model to pinpoint why B regressed against A, leaning
// hard on logs and traces.
func (a *API) rootCauseVersions(c *gin.Context) {
	app, ok := a.appFromParam(c)
	if !ok {
		return
	}
	var req compareReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.VersionA) == "" || strings.TrimSpace(req.VersionB) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version_a and version_b are required"})
		return
	}
	if a.ai == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no AI provider configured - set one in AI Settings"})
		return
	}
	if _, _, err := a.ai.Resolve(c); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no AI provider configured - set one in AI Settings"})
		return
	}

	metrics, _ := a.gatherVersionMetrics(c, app, req.VersionA, req.VersionB)
	logsA, tracesA := a.versionTelemetry(c, app, req.VersionA)
	logsB, tracesB := a.versionTelemetry(c, app, req.VersionB)

	system := "You are a senior SRE performing root-cause analysis on a regressed deploy. " +
		"Error logs and slow/failed traces are the primary evidence; the metrics corroborate. " +
		"If logs and traces are unavailable, reason from the metric changes and state plainly in likely_cause that log/trace evidence was missing. " +
		"Always give a concrete, non-empty likely_cause and quote any supporting log lines or span names in evidence."
	prompt := "Find the root cause of the regression in version " + req.VersionB + " (compared to " + req.VersionA + ").\n\n" +
		buildComparePrompt(app.Name, req.VersionA, req.VersionB, metrics, logsA, logsB, tracesA, tracesB)

	verdict, provider, model, err := a.runAI(c, system, prompt)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"app":        app.Name,
		"cause":      verdict.LikelyCause,
		"summary":    verdict.Summary,
		"confidence": verdict.Confidence,
		"evidence":   verdict.Evidence,
		"suggested":  verdict.Suggested,
		"provider":   provider,
		"model":      model,
	})
}

// appFromParam resolves :id to an app, writing the error response on failure.
func (a *API) appFromParam(c *gin.Context) (store.App, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return store.App{}, false
	}
	app, err := a.st.AppByID(c, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
		return store.App{}, false
	}
	return app, true
}

// runAI resolves the configured provider and runs one analysis call.
func (a *API) runAI(ctx context.Context, system, prompt string) (ai.Verdict, string, string, error) {
	provider, _, err := a.ai.Resolve(ctx)
	if err != nil {
		return ai.Verdict{}, "", "", err
	}
	callCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	res, err := provider.Analyze(callCtx, ai.Request{System: system, Prompt: prompt, MaxTokens: 4096})
	if err != nil {
		return ai.Verdict{}, "", "", err
	}
	return res.Verdict, provider.Name(), res.Model, nil
}

// gatherVersionMetrics builds the A-vs-B comparison across all dashboard metrics
// and returns the keys that regressed (worse than the app's threshold).
func (a *API) gatherVersionMetrics(ctx context.Context, app store.App, vA, vB string) ([]metricCompare, []string) {
	dashboards, err := a.st.ListDashboards(ctx)
	if err != nil {
		return nil, nil
	}
	since := time.Now().Add(-365 * 24 * time.Hour)
	thr := app.LatencyThresholdPct
	if thr <= 0 {
		thr = 25
	}
	out := make([]metricCompare, 0, len(dashboards))
	var degraded []string
	for _, d := range dashboards {
		stats, err := a.st.VersionsWithStats(ctx, app.Name, d.MetricKey, since)
		if err != nil {
			continue
		}
		pick := func(v string) *float64 {
			for _, s := range stats {
				if s.Version == v {
					val := s.P50
					if d.Kind == "percentiles" {
						val = s.P99
					}
					return &val
				}
			}
			return nil
		}
		mc := metricCompare{Metric: d.Name, Key: d.MetricKey, Unit: d.Unit, A: pick(vA), B: pick(vB)}
		mc.HigherWorse = !isLowerWorse(d.MetricKey)
		if mc.A != nil && mc.B != nil && *mc.A != 0 {
			dp := ((*mc.B - *mc.A) / *mc.A) * 100
			mc.DeltaPct = &dp
			if mc.HigherWorse {
				mc.Degraded = dp >= thr
			} else {
				mc.Degraded = dp <= -thr
			}
			if mc.Degraded {
				degraded = append(degraded, d.MetricKey)
			}
		}
		out = append(out, mc)
	}
	return out, degraded
}

func isLowerWorse(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "throughput") || strings.Contains(k, "rps") || strings.Contains(k, "qps")
}

// versionTelemetry loads a version's stored error logs and slow traces.
func (a *API) versionTelemetry(ctx context.Context, app store.App, version string) ([]telemetryLog, []telemetrySpan) {
	logsJSON, tracesJSON, found, err := a.st.TelemetryForVersion(ctx, app.ID, version)
	if err != nil || !found {
		return nil, nil
	}
	var logs []telemetryLog
	var spans []telemetrySpan
	_ = json.Unmarshal(logsJSON, &logs)
	_ = json.Unmarshal(tracesJSON, &spans)
	return logs, spans
}

type telemetryLog struct {
	Severity string `json:"severity"`
	Body     string `json:"body"`
}
type telemetrySpan struct {
	Name       string  `json:"name"`
	DurationMs float64 `json:"duration_ms"`
	StatusCode string  `json:"status_code"`
}

// buildComparePrompt renders the metrics table plus a bounded sample of each
// version's error logs and slowest traces.
func buildComparePrompt(app, vA, vB string, metrics []metricCompare, logsA, logsB []telemetryLog, tracesA, tracesB []telemetrySpan) string {
	var s strings.Builder
	fmt.Fprintf(&s, "Service: %s\nComparing version A=%s (baseline) vs version B=%s (newer).\n\n", app, vA, vB)

	s.WriteString("# Metrics (A → B)\n")
	for _, m := range metrics {
		av, bv := "n/a", "n/a"
		if m.A != nil {
			av = fmt.Sprintf("%.2f%s", *m.A, m.Unit)
		}
		if m.B != nil {
			bv = fmt.Sprintf("%.2f%s", *m.B, m.Unit)
		}
		delta := ""
		if m.DeltaPct != nil {
			delta = fmt.Sprintf(" (%+.1f%%)", *m.DeltaPct)
		}
		flag := ""
		if m.Degraded {
			flag = "  [WORSE]"
		}
		fmt.Fprintf(&s, "- %s: %s → %s%s%s\n", m.Metric, av, bv, delta, flag)
	}

	s.WriteString("\n# Error logs\n")
	fmt.Fprintf(&s, "Version A (%s):\n%s\n", vA, renderCompareLogs(logsA))
	fmt.Fprintf(&s, "Version B (%s):\n%s\n", vB, renderCompareLogs(logsB))

	s.WriteString("\n# Traces (slowest)\n")
	fmt.Fprintf(&s, "Version A (%s):\n%s\n", vA, renderCompareSpans(tracesA))
	fmt.Fprintf(&s, "Version B (%s):\n%s\n", vB, renderCompareSpans(tracesB))

	if len(logsA)+len(logsB)+len(tracesA)+len(tracesB) == 0 {
		s.WriteString("\nNOTE: no logs or traces were captured for either version - reason from the metric changes above and say so.\n")
	}

	s.WriteString("\nSummarize what changed from A to B and whether B is a regression. " +
		"Put the one-line takeaway in 'summary' and the detailed comparison in 'likely_cause'.")
	return s.String()
}

func renderCompareLogs(logs []telemetryLog) string {
	if len(logs) == 0 {
		return "  (none)"
	}
	seen := map[string]int{}
	for _, l := range logs {
		seen[l.Severity+": "+l.Body]++
	}
	var b strings.Builder
	n := 0
	for line, count := range seen {
		if n >= 12 {
			break
		}
		if count > 1 {
			fmt.Fprintf(&b, "  - %s (×%d)\n", line, count)
		} else {
			fmt.Fprintf(&b, "  - %s\n", line)
		}
		n++
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderCompareSpans(spans []telemetrySpan) string {
	if len(spans) == 0 {
		return "  (none)"
	}
	var b strings.Builder
	for i, sp := range spans {
		if i >= 8 {
			break
		}
		status := sp.StatusCode
		if status == "" {
			status = "-"
		}
		fmt.Fprintf(&b, "  - %s  %.0fms  status=%s\n", sp.Name, sp.DurationMs, status)
	}
	return strings.TrimRight(b.String(), "\n")
}
