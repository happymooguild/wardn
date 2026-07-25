package ai

import (
	"regexp"
	"sort"
	"strings"

	"wardn/metrics"
	"wardn/store"
)

// Bounds caps what goes into the prompt. Logs and traces for a window can be
// enormous; without caps both token cost and latency explode (docs/todo.md).
type Bounds struct {
	ErrorLogsAfter   int
	ErrorLogsBefore  int
	SlowTracesAfter  int
	SlowTracesBefore int
	MaxLogBodyChars  int
	SeriesPoints     int
	MaxTotalChars    int
}

func DefaultBounds() Bounds {
	return Bounds{
		ErrorLogsAfter:   20, // the regression's fingerprint
		ErrorLogsBefore:  5,  // baseline: is this error actually new?
		SlowTracesAfter:  10,
		SlowTracesBefore: 5,
		MaxLogBodyChars:  2000, // one stack trace must not eat the budget
		SeriesPoints:     20,   // snapshots hold 120; the model needs far fewer
		MaxTotalChars:    60000,
	}
}

func (b Bounds) withDefaults() Bounds {
	d := DefaultBounds()
	if b.ErrorLogsAfter <= 0 {
		b.ErrorLogsAfter = d.ErrorLogsAfter
	}
	if b.ErrorLogsBefore <= 0 {
		b.ErrorLogsBefore = d.ErrorLogsBefore
	}
	if b.SlowTracesAfter <= 0 {
		b.SlowTracesAfter = d.SlowTracesAfter
	}
	if b.SlowTracesBefore <= 0 {
		b.SlowTracesBefore = d.SlowTracesBefore
	}
	if b.MaxLogBodyChars <= 0 {
		b.MaxLogBodyChars = d.MaxLogBodyChars
	}
	if b.SeriesPoints <= 0 {
		b.SeriesPoints = d.SeriesPoints
	}
	if b.MaxTotalChars <= 0 {
		b.MaxTotalChars = d.MaxTotalChars
	}
	return b
}

// Input is everything available about a deploy before bounding.
type Input struct {
	App          store.App
	Deploy       store.DeployEvent
	Snapshots    []store.MetricSnapshot
	LogsBefore   []metrics.LogRecord
	LogsAfter    []metrics.LogRecord
	TracesBefore []metrics.SpanRecord
	TracesAfter  []metrics.SpanRecord
	// TelemetryError records why logs/traces are missing, so the model is told
	// it is reasoning on metrics alone rather than silently assuming the
	// service produced no errors.
	TelemetryError string
}

// Stats records what the bounder kept and what it dropped. Persisted alongside
// the verdict: without it, a thin answer is indistinguishable from a thin
// signal, and nobody can tell which one they're looking at.
type Stats struct {
	LogsAvailableBefore  int  `json:"logs_available_before"`
	LogsAvailableAfter   int  `json:"logs_available_after"`
	LogsSentBefore       int  `json:"logs_sent_before"`
	LogsSentAfter        int  `json:"logs_sent_after"`
	LogGroupsAfter       int  `json:"log_groups_after"`
	TracesAvailableAfter int  `json:"traces_available_after"`
	TracesSentBefore     int  `json:"traces_sent_before"`
	TracesSentAfter      int  `json:"traces_sent_after"`
	PromptChars          int  `json:"prompt_chars"`
	CeilingHit           bool `json:"ceiling_hit"`
	TelemetryMissing     bool `json:"telemetry_missing"`
}

// logGroup is a deduplicated log line plus how many times it occurred.
// Collapsing N identical stack traces into one entry with a count is the
// highest-signal-per-token transformation available, and it's plain Go.
type logGroup struct {
	Sample metrics.LogRecord
	Count  int
}

// Build selects, deduplicates, and renders the prompt within the bounds.
func Build(in Input, b Bounds) (Request, Stats) {
	b = b.withDefaults()

	stats := Stats{
		LogsAvailableBefore:  len(in.LogsBefore),
		LogsAvailableAfter:   len(in.LogsAfter),
		TracesAvailableAfter: len(in.TracesAfter),
		TelemetryMissing:     in.TelemetryError != "",
	}

	afterGroups := groupLogs(in.LogsAfter, b.MaxLogBodyChars)
	beforeGroups := groupLogs(in.LogsBefore, b.MaxLogBodyChars)
	afterSpans := slowest(in.TracesAfter)
	beforeSpans := slowest(in.TracesBefore)

	nAfterLogs := min(len(afterGroups), b.ErrorLogsAfter)
	nBeforeLogs := min(len(beforeGroups), b.ErrorLogsBefore)
	nAfterSpans := min(len(afterSpans), b.SlowTracesAfter)
	nBeforeSpans := min(len(beforeSpans), b.SlowTracesBefore)

	// Render, then shrink if we're still over the ceiling. A single
	// pathological payload shouldn't be able to blow the budget even when
	// every individual cap was respected.
	var prompt string
	for {
		prompt = renderPrompt(in, b,
			afterGroups[:nAfterLogs], beforeGroups[:nBeforeLogs],
			afterSpans[:nAfterSpans], beforeSpans[:nBeforeSpans])

		if len(prompt) <= b.MaxTotalChars {
			break
		}
		stats.CeilingHit = true

		// Shed the least-informative content first: baseline samples, then
		// traces, and only then the after-window errors that carry the signal.
		shrunk := true
		switch {
		case nBeforeLogs > 1:
			nBeforeLogs--
		case nBeforeSpans > 1:
			nBeforeSpans--
		case nAfterSpans > 1:
			nAfterSpans--
		case nAfterLogs > 1:
			nAfterLogs--
		default:
			shrunk = false
		}
		if !shrunk {
			// Down to one of each and still over: clip rather than loop.
			prompt = clipUTF8(prompt, b.MaxTotalChars)
			break
		}
	}

	stats.LogsSentAfter = nAfterLogs
	stats.LogsSentBefore = nBeforeLogs
	stats.LogGroupsAfter = len(afterGroups)
	stats.TracesSentAfter = nAfterSpans
	stats.TracesSentBefore = nBeforeSpans
	stats.PromptChars = len(prompt)

	// 8192 leaves room for adaptive thinking to share the output budget with the
	// (small) JSON verdict without truncating mid-thought.
	return Request{System: systemPrompt, Prompt: prompt, MaxTokens: 8192}, stats
}

// groupLogs collapses near-identical lines, most frequent first. Severity
// breaks ties so a single FATAL outranks a chatty ERROR.
func groupLogs(logs []metrics.LogRecord, maxBody int) []logGroup {
	index := make(map[string]int, len(logs))
	groups := make([]logGroup, 0, len(logs))

	for _, l := range logs {
		l.Body = truncate(strings.TrimSpace(l.Body), maxBody)
		if l.Body == "" {
			continue
		}
		key := normalizeLogBody(l.Body)
		if i, ok := index[key]; ok {
			groups[i].Count++
			continue
		}
		index[key] = len(groups)
		groups = append(groups, logGroup{Sample: l, Count: 1})
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return severityRank(groups[i].Sample.Severity) > severityRank(groups[j].Sample.Severity)
	})
	return groups
}

var (
	hexRun   = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	numRun   = regexp.MustCompile(`\b\d+\b`)
	quoted   = regexp.MustCompile(`"[^"]*"`)
	uuidLike = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
)

// normalizeLogBody strips the parts that vary per occurrence — ids, counters,
// quoted values — so "timeout calling user 4821" and "timeout calling user
// 9134" collapse into one group instead of two.
func normalizeLogBody(body string) string {
	s := uuidLike.ReplaceAllString(body, "<uuid>")
	s = hexRun.ReplaceAllString(s, "<hex>")
	s = numRun.ReplaceAllString(s, "<n>")
	s = quoted.ReplaceAllString(s, `"<v>"`)
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func severityRank(sev string) int {
	switch strings.ToUpper(sev) {
	case "FATAL", "CRITICAL":
		return 4
	case "ERROR":
		return 3
	case "WARN", "WARNING":
		return 2
	default:
		return 1
	}
}

// slowest returns spans ordered by duration descending. The backend is asked
// for this order too, but re-sorting locally keeps the bound honest if the
// query's ordering silently changes.
func slowest(spans []metrics.SpanRecord) []metrics.SpanRecord {
	out := make([]metrics.SpanRecord, len(spans))
	copy(out, spans)
	sort.SliceStable(out, func(i, j int) bool { return out[i].DurationMs > out[j].DurationMs })
	return out
}

// downsample thins a series to at most n points, always keeping the last one
// so the end state of the window survives.
func downsample(points []store.SeriesPoint, n int) []store.SeriesPoint {
	if n <= 0 || len(points) <= n {
		return points
	}
	out := make([]store.SeriesPoint, 0, n)
	stride := float64(len(points)-1) / float64(n-1)
	for i := 0; i < n-1; i++ {
		out = append(out, points[int(float64(i)*stride)])
	}
	return append(out, points[len(points)-1])
}
