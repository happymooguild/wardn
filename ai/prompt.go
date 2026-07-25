package ai

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"wardn/metrics"
	"wardn/store"
)

const systemPrompt = `You are a site-reliability engineer investigating whether a specific deploy caused a regression.

You are given, for one service: the metric values measured in a window immediately before the deploy and an equal window immediately after, plus a bounded sample of error logs and slow traces from the same two windows.

Ground every claim in the supplied data:
- Quote log lines and span names verbatim in your evidence. Never invent an identifier, file name, or stack frame that does not appear in the input.
- The samples are bounded (top-N by frequency and duration), not exhaustive. Absence of an error in the sample is weak evidence that it did not occur.
- The before-window is your baseline. An error present in both windows is background noise; one that appears only after the deploy is the signal.
- Correlation with the deploy is not proof of causation. If the evidence is thin or points at an external dependency, say so plainly and set confidence to "low". A candid "insufficient evidence, here is what to check" is more useful than a confident guess.

Answer as JSON matching the required schema. Keep likely_cause to a few sentences of plain prose an on-call engineer can act on at 3am.`

func renderPrompt(in Input, b Bounds, afterLogs, beforeLogs []logGroup, afterSpans, beforeSpans []metrics.SpanRecord) string {
	var s strings.Builder

	window := time.Duration(in.App.WindowSeconds) * time.Second
	fmt.Fprintf(&s, "# Deploy under investigation\n\n")
	fmt.Fprintf(&s, "- Service: %s\n", in.App.Name)
	fmt.Fprintf(&s, "- Environment: %s\n", in.Deploy.Environment)
	fmt.Fprintf(&s, "- Version deployed: %s\n", in.Deploy.Version)
	if in.Deploy.PreviousVersion != nil && *in.Deploy.PreviousVersion != "" {
		fmt.Fprintf(&s, "- Previous version: %s\n", *in.Deploy.PreviousVersion)
	}
	fmt.Fprintf(&s, "- Deployed at: %s (source: %s)\n", in.Deploy.DeployedAt.UTC().Format(time.RFC3339), in.Deploy.Source)
	fmt.Fprintf(&s, "- Verdict from statistical comparison: %s\n", in.Deploy.Status)
	fmt.Fprintf(&s, "- Comparison window: %s before vs %s after the deploy\n\n", window, window)

	s.WriteString("# Metrics: before vs after\n\n")
	if len(in.Snapshots) == 0 {
		s.WriteString("No metric snapshots were recorded for this deploy.\n\n")
	}
	for _, sn := range in.Snapshots {
		fmt.Fprintf(&s, "## %s%s\n", sn.MetricKey, degradedTag(sn.Degraded))
		fmt.Fprintf(&s, "- before: %s\n", formatValue(sn.BeforeValue))
		fmt.Fprintf(&s, "- after:  %s\n", formatValue(sn.AfterValue))
		if sn.DeltaPct != nil {
			fmt.Fprintf(&s, "- change: %+.1f%%\n", *sn.DeltaPct)
		}
		if sn.RawQuery != "" {
			fmt.Fprintf(&s, "- query: %s\n", sn.RawQuery)
		}
		if pts := downsample(sn.SeriesBefore, b.SeriesPoints); len(pts) > 0 {
			fmt.Fprintf(&s, "- series before: %s\n", formatSeries(pts))
		}
		if pts := downsample(sn.SeriesAfter, b.SeriesPoints); len(pts) > 0 {
			fmt.Fprintf(&s, "- series after:  %s\n", formatSeries(pts))
		}
		s.WriteString("\n")
	}

	if in.TelemetryError != "" {
		fmt.Fprintf(&s, "# Logs and traces\n\nUnavailable — could not be fetched from the observability backend (%s). "+
			"Reason from the metrics alone, and say in your answer that log and trace evidence was missing.\n\n", in.TelemetryError)
		return s.String()
	}

	s.WriteString("# Error logs after the deploy\n\n")
	s.WriteString(renderLogs(afterLogs, "No error logs were returned for the after-window."))

	s.WriteString("# Error logs before the deploy (baseline)\n\n")
	s.WriteString(renderLogs(beforeLogs, "No error logs were returned for the before-window — errors above are likely new."))

	s.WriteString("# Slowest traces after the deploy\n\n")
	s.WriteString(renderSpans(afterSpans, "No traces were returned for the after-window."))

	s.WriteString("# Slowest traces before the deploy (baseline)\n\n")
	s.WriteString(renderSpans(beforeSpans, "No traces were returned for the before-window."))

	s.WriteString("# Your task\n\n")
	s.WriteString("Explain why this deploy regressed, using only the evidence above.\n")

	return s.String()
}

func renderLogs(groups []logGroup, empty string) string {
	if len(groups) == 0 {
		return empty + "\n\n"
	}
	var s strings.Builder
	s.WriteString("Identical lines are collapsed; `x N` is the number of occurrences in the sample.\n\n")
	for _, g := range groups {
		sev := g.Sample.Severity
		if sev == "" {
			sev = "LOG"
		}
		fmt.Fprintf(&s, "- [%s] x %d", sev, g.Count)
		if !g.Sample.Timestamp.IsZero() {
			fmt.Fprintf(&s, " (first seen %s)", g.Sample.Timestamp.UTC().Format(time.RFC3339))
		}
		fmt.Fprintf(&s, "\n  %s\n", indentBody(g.Sample.Body))
	}
	s.WriteString("\n")
	return s.String()
}

func renderSpans(spans []metrics.SpanRecord, empty string) string {
	if len(spans) == 0 {
		return empty + "\n\n"
	}
	var s strings.Builder
	for _, sp := range spans {
		fmt.Fprintf(&s, "- %.1fms  %s", sp.DurationMs, sp.Name)
		if sp.Service != "" {
			fmt.Fprintf(&s, "  (service %s)", sp.Service)
		}
		if sp.StatusCode != "" {
			fmt.Fprintf(&s, "  status=%s", sp.StatusCode)
		}
		if sp.TraceID != "" {
			fmt.Fprintf(&s, "  trace=%s", sp.TraceID)
		}
		s.WriteString("\n")
	}
	s.WriteString("\n")
	return s.String()
}

// indentBody keeps multi-line stack traces readable inside a bullet list.
func indentBody(body string) string {
	return strings.ReplaceAll(strings.TrimSpace(body), "\n", "\n  ")
}

func degradedTag(degraded bool) string {
	if degraded {
		return " (DEGRADED — crossed the configured threshold)"
	}
	return ""
}

func formatValue(v *float64) string {
	if v == nil {
		return "no data"
	}
	return fmt.Sprintf("%.3f", *v)
}

func formatSeries(points []store.SeriesPoint) string {
	parts := make([]string, 0, len(points))
	for _, p := range points {
		parts = append(parts, fmt.Sprintf("%.2f", p.V))
	}
	return strings.Join(parts, ", ")
}

// clipUTF8 truncates to at most n bytes without splitting a rune.
func clipUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
