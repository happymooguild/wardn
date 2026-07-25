package metrics

import (
	"context"
	"time"
)

// LogRecord is one log line pulled from the observability backend.
type LogRecord struct {
	Timestamp  time.Time         `json:"timestamp"`
	Severity   string            `json:"severity"`
	Body       string            `json:"body"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// SpanRecord is one trace span, used to spot latency regressions.
type SpanRecord struct {
	TraceID    string    `json:"trace_id"`
	SpanID     string    `json:"span_id,omitempty"`
	Name       string    `json:"name"`
	Service    string    `json:"service"`
	DurationMs float64   `json:"duration_ms"`
	StatusCode string    `json:"status_code,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// TelemetryQuery scopes a logs or traces fetch to one service and window.
type TelemetryQuery struct {
	Service     string
	Environment string
	Start       time.Time
	End         time.Time
	Limit       int
	// ErrorsOnly restricts logs to error/fatal severities and traces to spans
	// with a non-OK status. The bounder asks for errors first because they are
	// the highest-signal-per-token thing available.
	ErrorsOnly bool
}

// TelemetryProvider fetches the logs and traces the AI layer reasons over.
//
// Deliberately separate from MetricsProvider: the analyzer's contract is
// metrics-only and shouldn't grow just because the AI layer needs more signals.
type TelemetryProvider interface {
	Logs(ctx context.Context, q TelemetryQuery) ([]LogRecord, error)
	Traces(ctx context.Context, q TelemetryQuery) ([]SpanRecord, error)
}
