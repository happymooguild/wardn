package ai

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"wardn/metrics"
	"wardn/store"
)

func testInput(logsAfter []metrics.LogRecord, tracesAfter []metrics.SpanRecord) Input {
	prev := "v1.0.0"
	return Input{
		App:    store.App{Name: "checkout-service", WindowSeconds: 120},
		Deploy: store.DeployEvent{Version: "v1.0.1", PreviousVersion: &prev, Environment: "production", Source: "ci", Status: "regressed"},
		Snapshots: []store.MetricSnapshot{{
			MetricKey: "latency_p99",
			Degraded:  true,
		}},
		LogsAfter:   logsAfter,
		TracesAfter: tracesAfter,
	}
}

func TestGroupLogsCollapsesVaryingIDs(t *testing.T) {
	logs := []metrics.LogRecord{
		{Severity: "ERROR", Body: "timeout calling user 4821"},
		{Severity: "ERROR", Body: "timeout calling user 9134"},
		{Severity: "ERROR", Body: "timeout calling user 77"},
		{Severity: "ERROR", Body: "connection refused to payments"},
	}
	groups := groupLogs(logs, 2000)

	if len(groups) != 2 {
		t.Fatalf("want 2 groups after dedup, got %d", len(groups))
	}
	if groups[0].Count != 3 {
		t.Errorf("want the timeout group collapsed to count 3, got %d", groups[0].Count)
	}
	if !strings.Contains(groups[0].Sample.Body, "timeout calling user") {
		t.Errorf("most frequent group should be the timeout, got %q", groups[0].Sample.Body)
	}
}

func TestGroupLogsSeverityBreaksTies(t *testing.T) {
	groups := groupLogs([]metrics.LogRecord{
		{Severity: "WARN", Body: "cache miss"},
		{Severity: "FATAL", Body: "out of memory"},
	}, 2000)

	if groups[0].Sample.Severity != "FATAL" {
		t.Errorf("FATAL should outrank WARN at equal counts, got %q first", groups[0].Sample.Severity)
	}
}

func TestGroupLogsTruncatesLongBodies(t *testing.T) {
	groups := groupLogs([]metrics.LogRecord{
		{Severity: "ERROR", Body: strings.Repeat("x", 5000)},
	}, 100)

	// truncate() appends an ellipsis, so allow for it.
	if len([]rune(groups[0].Sample.Body)) > 101 {
		t.Errorf("body should be truncated to the cap, got %d runes", len([]rune(groups[0].Sample.Body)))
	}
}

func TestBuildRespectsCaps(t *testing.T) {
	var logs []metrics.LogRecord
	for i := range 200 {
		logs = append(logs, metrics.LogRecord{Severity: "ERROR", Body: fmt.Sprintf("distinct failure mode %d", i)})
	}
	var spans []metrics.SpanRecord
	for i := range 100 {
		spans = append(spans, metrics.SpanRecord{Name: fmt.Sprintf("GET /route/%d", i), DurationMs: float64(i)})
	}

	_, stats := Build(testInput(logs, spans), Bounds{})

	if stats.LogsSentAfter > DefaultBounds().ErrorLogsAfter {
		t.Errorf("sent %d logs, cap is %d", stats.LogsSentAfter, DefaultBounds().ErrorLogsAfter)
	}
	if stats.TracesSentAfter > DefaultBounds().SlowTracesAfter {
		t.Errorf("sent %d traces, cap is %d", stats.TracesSentAfter, DefaultBounds().SlowTracesAfter)
	}
	if stats.LogsAvailableAfter != 200 {
		t.Errorf("stats should record all 200 available logs, got %d", stats.LogsAvailableAfter)
	}
}

func TestBuildEnforcesCharCeiling(t *testing.T) {
	// Each line must be distinct *after* normalization, so vary by letters —
	// numeric suffixes would (correctly) collapse into one group and never
	// reach the ceiling.
	var logs []metrics.LogRecord
	for i := range 50 {
		logs = append(logs, metrics.LogRecord{
			Severity: "ERROR",
			Body:     fmt.Sprintf("failure %s: %s", strings.Repeat("a", i+1), strings.Repeat("detail ", 500)),
		})
	}

	bounds := Bounds{MaxTotalChars: 8000}
	req, stats := Build(testInput(logs, nil), bounds)

	if len(req.Prompt) > 8000 {
		t.Errorf("prompt is %d chars, ceiling is 8000", len(req.Prompt))
	}
	if !stats.CeilingHit {
		t.Error("stats should record that the ceiling was hit")
	}
}

func TestBuildKeepsSlowestTracesFirst(t *testing.T) {
	spans := []metrics.SpanRecord{
		{Name: "fast", DurationMs: 5},
		{Name: "slowest", DurationMs: 9000},
		{Name: "medium", DurationMs: 300},
	}
	req, _ := Build(testInput(nil, spans), Bounds{SlowTracesAfter: 1})

	if !strings.Contains(req.Prompt, "slowest") {
		t.Error("the single retained trace should be the slowest one")
	}
	if strings.Contains(req.Prompt, "- 5.0ms  fast") {
		t.Error("the fastest span should have been dropped")
	}
}

func TestBuildTellsModelWhenTelemetryMissing(t *testing.T) {
	in := testInput(nil, nil)
	in.TelemetryError = "signoz: HTTP 503"

	req, stats := Build(in, Bounds{})

	if !stats.TelemetryMissing {
		t.Error("stats should flag missing telemetry")
	}
	if !strings.Contains(req.Prompt, "Unavailable") {
		t.Error("prompt must tell the model logs/traces were unavailable, not imply the service was clean")
	}
}

func TestDownsampleKeepsLastPoint(t *testing.T) {
	points := make([]store.SeriesPoint, 120)
	for i := range points {
		points[i] = store.SeriesPoint{T: int64(i), V: float64(i)}
	}
	out := downsample(points, 20)

	if len(out) != 20 {
		t.Fatalf("want 20 points, got %d", len(out))
	}
	if out[len(out)-1].V != 119 {
		t.Errorf("last point must survive downsampling, got %v", out[len(out)-1].V)
	}
}

func TestDownsampleShortSeriesUnchanged(t *testing.T) {
	points := []store.SeriesPoint{{T: 1, V: 1}, {T: 2, V: 2}}
	if got := downsample(points, 20); len(got) != 2 {
		t.Errorf("short series should pass through, got %d points", len(got))
	}
}

func TestParseVerdictRejectsEmptyCause(t *testing.T) {
	if _, err := parseVerdict(`{"summary":"x","likely_cause":"","confidence":"high"}`); err == nil {
		t.Error("a verdict with no likely_cause should be an error, not a silent pass")
	}
}

func TestParseVerdictNormalizesConfidence(t *testing.T) {
	v, err := parseVerdict(`{"summary":"x","likely_cause":"y","confidence":"very sure"}`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Confidence != "low" {
		t.Errorf("an out-of-enum confidence should fall back to low, got %q", v.Confidence)
	}
}

func TestBuildStatsRecordDropRate(t *testing.T) {
	logs := make([]metrics.LogRecord, 0, 60)
	for i := range 60 {
		logs = append(logs, metrics.LogRecord{Severity: "ERROR", Body: fmt.Sprintf("err %d", i)})
	}
	_, stats := Build(testInput(logs, nil), Bounds{})

	if stats.LogsAvailableAfter <= stats.LogsSentAfter {
		t.Errorf("stats must make the drop visible: %d available vs %d sent",
			stats.LogsAvailableAfter, stats.LogsSentAfter)
	}
	if stats.PromptChars == 0 {
		t.Error("stats should record the rendered prompt size")
	}
}

func TestGroupLogsSkipsBlankBodies(t *testing.T) {
	groups := groupLogs([]metrics.LogRecord{
		{Severity: "ERROR", Body: "   "},
		{Severity: "ERROR", Body: "real failure"},
	}, 2000)

	if len(groups) != 1 {
		t.Fatalf("blank bodies should be skipped, got %d groups", len(groups))
	}
}

func TestClipUTF8DoesNotSplitRunes(t *testing.T) {
	s := strings.Repeat("é", 10) // 2 bytes each
	out := clipUTF8(s, 5)

	if !utf8ValidString(out) {
		t.Errorf("clip produced invalid UTF-8: %q", out)
	}
	if len(out) > 5 {
		t.Errorf("clip exceeded the byte budget: %d", len(out))
	}
}

func utf8ValidString(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

var _ = time.Now

func TestPermanentStatusClassification(t *testing.T) {
	// Auth/shape errors are terminal; rate limits and server errors are not.
	for _, code := range []int{400, 401, 403, 404, 413, 422} {
		if !permanentStatus(code) {
			t.Errorf("HTTP %d should be permanent — retrying repeats the same rejection", code)
		}
	}
	for _, code := range []int{408, 429, 500, 502, 503, 529} {
		if permanentStatus(code) {
			t.Errorf("HTTP %d should be retryable, not permanent", code)
		}
	}
}

func TestErrPermanentUnwraps(t *testing.T) {
	inner := errors.New("invalid x-api-key")
	var target *ErrPermanent
	if !errors.As(&ErrPermanent{Err: inner}, &target) {
		t.Fatal("errors.As should match *ErrPermanent")
	}
	if !errors.Is(&ErrPermanent{Err: inner}, inner) {
		t.Error("ErrPermanent must unwrap to the underlying error")
	}
}
