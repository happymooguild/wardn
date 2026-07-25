package metrics

import (
	"testing"
	"time"
)

// The v5 raw-query response shape is not yet verified against a live SigNoz, so
// the parser is deliberately tolerant. These tests pin that tolerance: several
// plausible envelopes must all yield usable records.

func TestParseLogRowsNestedEnvelope(t *testing.T) {
	body := []byte(`{
	  "status": "success",
	  "data": {
	    "results": [{
	      "rows": [
	        {"timestamp": "2026-07-25T10:00:00Z", "data": {"body": "connection refused", "severity_text": "ERROR", "service.name": "checkout"}},
	        {"timestamp": "2026-07-25T10:00:01Z", "data": {"body": "retry exhausted", "severity_text": "FATAL"}}
	      ]
	    }]
	  }
	}`)

	logs := parseLogRows(body)
	if len(logs) != 2 {
		t.Fatalf("want 2 logs, got %d", len(logs))
	}
	if logs[0].Body != "connection refused" {
		t.Errorf("body = %q", logs[0].Body)
	}
	if logs[0].Severity != "ERROR" {
		t.Errorf("severity = %q", logs[0].Severity)
	}
	if logs[0].Attributes["service.name"] != "checkout" {
		t.Errorf("attributes not lifted from nested data: %v", logs[0].Attributes)
	}
	if logs[0].Timestamp.IsZero() {
		t.Error("timestamp should parse from RFC3339")
	}
}

func TestParseLogRowsFlatEnvelope(t *testing.T) {
	// A flatter shape with no "data" wrapper and epoch-nanosecond timestamps.
	body := []byte(`{"data": [
	  {"timestamp": 1785016800000000000, "body": "boom", "severity": "error"}
	]}`)

	logs := parseLogRows(body)
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
	if logs[0].Severity != "ERROR" {
		t.Errorf("severity should be upper-cased, got %q", logs[0].Severity)
	}
	if logs[0].Timestamp.Year() != 2026 {
		t.Errorf("epoch-nanos timestamp misparsed: %v", logs[0].Timestamp)
	}
}

func TestParseLogRowsSkipsBodilessRows(t *testing.T) {
	body := []byte(`{"data": {"rows": [{"severity_text": "ERROR"}, {"body": "real", "severity_text": "ERROR"}]}}`)

	logs := parseLogRows(body)
	if len(logs) != 1 {
		t.Fatalf("a row with no body carries no signal and should be skipped; got %d", len(logs))
	}
}

func TestParseSpanRowsNormalizesDuration(t *testing.T) {
	body := []byte(`{"data": {"rows": [
	  {"name": "GET /checkout", "duration_nano": 1500000000, "trace_id": "abc", "service.name": "checkout"},
	  {"span_name": "SELECT items", "duration_ms": 42.5, "status_code": "ERROR"}
	]}}`)

	spans := parseSpanRows(body)
	if len(spans) != 2 {
		t.Fatalf("want 2 spans, got %d", len(spans))
	}
	if spans[0].DurationMs != 1500 {
		t.Errorf("1.5e9 nanos should be 1500ms, got %v", spans[0].DurationMs)
	}
	if spans[1].DurationMs != 42.5 {
		t.Errorf("duration_ms should pass through, got %v", spans[1].DurationMs)
	}
	if spans[1].Name != "SELECT items" {
		t.Errorf("span_name alias not honored, got %q", spans[1].Name)
	}
}

func TestParseRowsOnGarbageReturnsEmpty(t *testing.T) {
	// A schema change must degrade to "no telemetry", never a panic.
	for _, body := range []string{`not json`, `{}`, `[]`, `{"data": null}`, `{"data": {"results": []}}`} {
		if got := parseLogRows([]byte(body)); len(got) != 0 {
			t.Errorf("body %q: want no logs, got %d", body, len(got))
		}
		if got := parseSpanRows([]byte(body)); len(got) != 0 {
			t.Errorf("body %q: want no spans, got %d", body, len(got))
		}
	}
}

func TestEpochToTimeUnits(t *testing.T) {
	want := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	cases := map[string]float64{
		"seconds": float64(want.Unix()),
		"millis":  float64(want.UnixMilli()),
		"micros":  float64(want.UnixMicro()),
		"nanos":   float64(want.UnixNano()),
	}
	for unit, v := range cases {
		if got := epochToTime(v); !got.Equal(want) {
			t.Errorf("%s: got %v, want %v", unit, got, want)
		}
	}
}

func TestFilterEscapesQuotes(t *testing.T) {
	got := logFilter(TelemetryQuery{Service: "it's-a-service", ErrorsOnly: true})
	if want := "service.name = 'it''s-a-service'"; !contains(got, want) {
		t.Errorf("quote not escaped in %q", got)
	}
	if !contains(got, "severity_text IN") {
		t.Errorf("ErrorsOnly should add a severity clause, got %q", got)
	}
}

func TestFilterOmitsSeverityWhenNotErrorsOnly(t *testing.T) {
	if got := logFilter(TelemetryQuery{Service: "svc"}); contains(got, "severity_text") {
		t.Errorf("non-error query should not filter severity, got %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
