package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Logs and traces go through the same POST /api/v5/query_range endpoint the
// PromQL client uses, with requestType "raw" and a builder query on the logs /
// traces signal.
//
// SPIKE PENDING: the exact v5 builder-query shape for the logs and traces
// signals is reconstructed from the documented API, not yet verified against a
// live SigNoz - the same caveat design-doc §7 raised for PromQL, and the same
// class of assumption that made the annotation feature turn out not to exist.
// Verify against your instance before relying on it. Two things make that
// cheap: the response parsing below is shape-tolerant (it walks the envelope
// for recognizable fields rather than binding to one nesting), and a telemetry
// failure degrades the analysis to metrics-only instead of failing the job.

type v5RawRequest struct {
	SchemaVersion  string            `json:"schemaVersion"`
	Start          int64             `json:"start"`
	End            int64             `json:"end"`
	RequestType    string            `json:"requestType"`
	CompositeQuery rawCompositeQuery `json:"compositeQuery"`
}

type rawCompositeQuery struct {
	Queries []rawQuery `json:"queries"`
}

type rawQuery struct {
	Type string       `json:"type"`
	Spec rawQuerySpec `json:"spec"`
}

type rawQuerySpec struct {
	Name     string     `json:"name"`
	Signal   string     `json:"signal"`
	Disabled bool       `json:"disabled"`
	Limit    int        `json:"limit"`
	Offset   int        `json:"offset"`
	Order    []orderBy  `json:"order,omitempty"`
	Filter   *rawFilter `json:"filter,omitempty"`
}

type orderBy struct {
	Key       orderKey `json:"key"`
	Direction string   `json:"direction"`
}

type orderKey struct {
	Name string `json:"name"`
}

type rawFilter struct {
	Expression string `json:"expression"`
}

const defaultTelemetryLimit = 50

// Logs returns log records for the window, newest first.
func (p *SignozProvider) Logs(ctx context.Context, q TelemetryQuery) ([]LogRecord, error) {
	body, err := p.rawQuery(ctx, "logs", q, logFilter(q), []orderBy{
		{Key: orderKey{Name: "timestamp"}, Direction: "desc"},
	})
	if err != nil {
		return nil, err
	}
	return parseLogRows(body), nil
}

// Traces returns spans for the window, slowest first.
func (p *SignozProvider) Traces(ctx context.Context, q TelemetryQuery) ([]SpanRecord, error) {
	body, err := p.rawQuery(ctx, "traces", q, traceFilter(q), []orderBy{
		{Key: orderKey{Name: "duration_nano"}, Direction: "desc"},
	})
	if err != nil {
		return nil, err
	}
	return parseSpanRows(body), nil
}

func logFilter(q TelemetryQuery) string {
	clauses := serviceClauses(q)
	if q.ErrorsOnly {
		clauses = append(clauses, "severity_text IN ('ERROR', 'FATAL', 'CRITICAL')")
	}
	return strings.Join(clauses, " AND ")
}

func traceFilter(q TelemetryQuery) string {
	clauses := serviceClauses(q)
	if q.ErrorsOnly {
		clauses = append(clauses, "has_error = true")
	}
	return strings.Join(clauses, " AND ")
}

func serviceClauses(q TelemetryQuery) []string {
	var clauses []string
	if q.Service != "" {
		clauses = append(clauses, fmt.Sprintf("service.name = '%s'", escapeLiteral(q.Service)))
	}
	// NB: no deployment.environment clause - SigNoz rejects the whole search
	// expression when the attribute isn't present on the signal, and service.name
	// already scopes it. Re-add per-signal only once the app emits that attribute.
	return clauses
}

// escapeLiteral guards the filter expression against a service name containing
// a quote. Service names come from app config rather than end users, but the
// expression is still a query language and deserves the escaping.
func escapeLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (p *SignozProvider) rawQuery(ctx context.Context, signal string, q TelemetryQuery, filter string, order []orderBy) ([]byte, error) {
	if p.BaseURL == "" || p.APIKey == "" {
		return nil, fmt.Errorf("signoz: URL or API key not configured")
	}
	if !q.End.After(q.Start) {
		return nil, fmt.Errorf("signoz: end must be after start")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultTelemetryLimit
	}

	spec := rawQuerySpec{
		Name:   "A",
		Signal: signal,
		Limit:  limit,
		Order:  order,
	}
	if filter != "" {
		spec.Filter = &rawFilter{Expression: filter}
	}

	payload := v5RawRequest{
		SchemaVersion: "v1",
		Start:         q.Start.UTC().UnixMilli(),
		End:           q.End.UTC().UnixMilli(),
		RequestType:   "raw",
		CompositeQuery: rawCompositeQuery{
			Queries: []rawQuery{{Type: "builder_query", Spec: spec}},
		},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/api/v5/query_range", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("SIGNOZ-API-KEY", p.APIKey)

	res, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("signoz %s: HTTP %d: %s", signal, res.StatusCode, truncate(string(respBody), 300))
	}
	return respBody, nil
}

// collectRows walks the response envelope for arrays of objects that look like
// telemetry rows. Same tolerance strategy as parseV5Series: bind to field names,
// not to one particular nesting, so a schema shift degrades rather than breaks.
func collectRows(node any, out *[]map[string]any) {
	switch v := node.(type) {
	case []any:
		for _, item := range v {
			if obj, ok := item.(map[string]any); ok && looksLikeRow(obj) {
				*out = append(*out, flattenRow(obj))
				continue
			}
			collectRows(item, out)
		}
	case map[string]any:
		for _, val := range v {
			collectRows(val, out)
		}
	}
}

var rowFieldNames = []string{
	"body", "message", "severity_text", "severity", "log_level",
	"name", "span_name", "duration_nano", "durationNano", "duration_ms",
	"trace_id", "traceID", "traceId",
}

func looksLikeRow(obj map[string]any) bool {
	for _, key := range rowFieldNames {
		if _, ok := obj[key]; ok {
			return true
		}
	}
	// Rows are sometimes wrapped as {"timestamp": ..., "data": {...}}.
	if _, ok := obj["data"].(map[string]any); ok {
		if _, hasTS := obj["timestamp"]; hasTS {
			return true
		}
	}
	return false
}

// flattenRow lifts a nested "data" object up to the top level so field lookup
// is uniform regardless of whether the row was wrapped.
func flattenRow(obj map[string]any) map[string]any {
	inner, ok := obj["data"].(map[string]any)
	if !ok {
		return obj
	}
	merged := make(map[string]any, len(obj)+len(inner))
	for k, v := range obj {
		if k == "data" {
			continue
		}
		merged[k] = v
	}
	for k, v := range inner {
		merged[k] = v
	}
	return merged
}

func parseLogRows(body []byte) []LogRecord {
	rows := rowsFrom(body)
	out := make([]LogRecord, 0, len(rows))
	for _, row := range rows {
		body := firstString(row, "body", "message", "log", "content")
		if body == "" {
			continue
		}
		out = append(out, LogRecord{
			Timestamp:  firstTime(row, "timestamp", "time", "ts"),
			Severity:   strings.ToUpper(firstString(row, "severity_text", "severity", "log_level", "level")),
			Body:       body,
			Attributes: stringAttrs(row),
		})
	}
	return out
}

func parseSpanRows(body []byte) []SpanRecord {
	rows := rowsFrom(body)
	out := make([]SpanRecord, 0, len(rows))
	for _, row := range rows {
		name := firstString(row, "name", "span_name", "operation")
		if name == "" {
			continue
		}
		out = append(out, SpanRecord{
			TraceID:    firstString(row, "trace_id", "traceID", "traceId"),
			SpanID:     firstString(row, "span_id", "spanID", "spanId"),
			Name:       name,
			Service:    firstString(row, "service.name", "service_name", "serviceName"),
			DurationMs: durationMs(row),
			StatusCode: firstString(row, "status_code", "statusCode", "status"),
			Timestamp:  firstTime(row, "timestamp", "time", "ts"),
		})
	}
	return out
}

func rowsFrom(body []byte) []map[string]any {
	var envelope any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	var rows []map[string]any
	collectRows(envelope, &rows)
	return rows
}

// durationMs normalizes whichever duration field the backend supplied. Nanos
// are the SigNoz native unit; the others show up across versions.
func durationMs(row map[string]any) float64 {
	if v, ok := firstFloat(row, "duration_nano", "durationNano", "durationNanos"); ok {
		return v / 1e6
	}
	if v, ok := firstFloat(row, "duration_ms", "durationMs"); ok {
		return v
	}
	if v, ok := firstFloat(row, "duration"); ok {
		return v / 1e6
	}
	return 0
}

func firstString(row map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := row[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func firstFloat(row map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := row[k]; ok {
			if f, ok := asFloat(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

// firstTime accepts the several timestamp encodings seen in the wild: RFC3339
// strings, and numeric epochs in seconds, millis, or nanos.
func firstTime(row map[string]any, keys ...string) time.Time {
	for _, k := range keys {
		v, ok := row[k]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case string:
			if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
				return parsed.UTC()
			}
			if n, err := strconv.ParseFloat(t, 64); err == nil {
				return epochToTime(n)
			}
		default:
			if f, ok := asFloat(v); ok {
				return epochToTime(f)
			}
		}
	}
	return time.Time{}
}

func epochToTime(n float64) time.Time {
	switch {
	case n > 1e17: // nanoseconds
		return time.Unix(0, int64(n)).UTC()
	case n > 1e14: // microseconds
		return time.UnixMicro(int64(n)).UTC()
	case n > 1e11: // milliseconds
		return time.UnixMilli(int64(n)).UTC()
	default: // seconds
		return time.Unix(int64(n), 0).UTC()
	}
}

// stringAttrs keeps the handful of attributes worth spending tokens on. A log
// row can carry dozens of fields; the prompt only needs identity and origin.
func stringAttrs(row map[string]any) map[string]string {
	wanted := []string{"service.name", "service_name", "deployment.environment", "k8s.pod.name", "trace_id", "http.status_code"}
	var attrs map[string]string
	for _, k := range wanted {
		if s, ok := row[k].(string); ok && s != "" {
			if attrs == nil {
				attrs = make(map[string]string, len(wanted))
			}
			attrs[k] = s
		}
	}
	return attrs
}
