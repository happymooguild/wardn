package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SignozProvider queries SigNoz via POST /api/v5/query_range (PromQL).
type SignozProvider struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewSignoz(baseURL, apiKey string) *SignozProvider {
	return &SignozProvider{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type v5Request struct {
	SchemaVersion  string         `json:"schemaVersion"`
	Start          int64          `json:"start"`
	End            int64          `json:"end"`
	RequestType    string         `json:"requestType"`
	CompositeQuery compositeQuery `json:"compositeQuery"`
}

type compositeQuery struct {
	Queries []v5Query `json:"queries"`
}

type v5Query struct {
	Type string    `json:"type"`
	Spec promqSpec `json:"spec"`
}

type promqSpec struct {
	Name  string `json:"name"`
	Query string `json:"query"`
	Step  int    `json:"step"`
}

func (p *SignozProvider) Query(ctx context.Context, promql string, start, end time.Time) (Series, error) {
	if !end.After(start) {
		return Series{}, fmt.Errorf("signoz: end must be after start")
	}
	windowSec := end.Sub(start).Seconds()
	step := int(math.Max(15, math.Ceil(windowSec/60)))
	return p.QuerySeries(ctx, promql, start, end, step)
}

func (p *SignozProvider) QuerySeries(ctx context.Context, promql string, start, end time.Time, stepSec int) (Series, error) {
	if p.BaseURL == "" || p.APIKey == "" {
		return Series{}, fmt.Errorf("signoz: URL or API key not configured")
	}
	if !end.After(start) {
		return Series{}, fmt.Errorf("signoz: end must be after start")
	}
	if stepSec < 1 {
		stepSec = 1
	}

	body := v5Request{
		SchemaVersion: "v1",
		Start:         start.UTC().UnixMilli(),
		End:           end.UTC().UnixMilli(),
		RequestType:   "time_series",
		CompositeQuery: compositeQuery{
			Queries: []v5Query{{
				Type: "promql",
				Spec: promqSpec{Name: "A", Query: promql, Step: stepSec},
			}},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Series{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/api/v5/query_range", bytes.NewReader(raw))
	if err != nil {
		return Series{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("SIGNOZ-API-KEY", p.APIKey)

	res, err := p.HTTPClient.Do(req)
	if err != nil {
		return Series{}, err
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return Series{}, fmt.Errorf("signoz: HTTP %d: %s", res.StatusCode, truncate(string(respBody), 300))
	}

	points, err := parseV5Series(respBody)
	if err != nil {
		return Series{}, err
	}
	return Series{Points: points, Scalar: AverageScalar(points)}, nil
}

// ListMetrics enumerates available metrics via GET /api/v2/metrics (last 24h),
// optionally filtered by a search string.
func (p *SignozProvider) ListMetrics(ctx context.Context, search string) ([]MetricInfo, error) {
	if p.BaseURL == "" || p.APIKey == "" {
		return nil, fmt.Errorf("signoz: URL or API key not configured")
	}
	now := time.Now().UTC()
	q := url.Values{}
	q.Set("start", fmt.Sprintf("%d", now.Add(-24*time.Hour).UnixMilli()))
	q.Set("end", fmt.Sprintf("%d", now.UnixMilli()))
	q.Set("limit", "500")
	if search != "" {
		q.Set("searchText", search)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/api/v2/metrics?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("SIGNOZ-API-KEY", p.APIKey)

	res, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("signoz: HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
	}
	var env struct {
		Data struct {
			Metrics []struct {
				MetricName string `json:"metricName"`
				Type       string `json:"type"`
				Unit       string `json:"unit"`
			} `json:"metrics"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("signoz: decode metrics list: %w", err)
	}
	out := make([]MetricInfo, 0, len(env.Data.Metrics))
	for _, m := range env.Data.Metrics {
		out = append(out, MetricInfo{Name: m.MetricName, Type: m.Type, Unit: m.Unit})
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// parseV5Series extracts points from the SigNoz v5 envelope.
// Tolerates several nested shapes used across versions.
func parseV5Series(body []byte) ([]Point, error) {
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("signoz: decode: %w", err)
	}

	// Prefer envelope.data, else whole object.
	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		data = envelope
	}

	var points []Point
	collectPoints(data, &points)
	return points, nil
}

func collectPoints(node any, out *[]Point) {
	switch n := node.(type) {
	case map[string]any:
		// Series value shape: { "timestamp": ms, "value": number }
		if ts, ok := asFloat(n["timestamp"]); ok {
			if v, ok := asFloat(n["value"]); ok {
				*out = append(*out, Point{T: time.UnixMilli(int64(ts)), V: v})
				return
			}
		}
		// Prometheus-like: [ts_seconds, "value"]
		if vals, ok := n["values"].([]any); ok {
			for _, item := range vals {
				if pair, ok := item.([]any); ok && len(pair) >= 2 {
					ts, tok := asFloat(pair[0])
					v, vok := asFloat(pair[1])
					if tok && vok {
						// Heuristic: ms if huge
						t := time.Unix(int64(ts), 0)
						if ts > 1e12 {
							t = time.UnixMilli(int64(ts))
						}
						*out = append(*out, Point{T: t, V: v})
					}
				} else {
					collectPoints(item, out)
				}
			}
			return
		}
		for _, v := range n {
			collectPoints(v, out)
		}
	case []any:
		for _, v := range n {
			collectPoints(v, out)
		}
	}
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		var f float64
		_, err := fmt.Sscanf(x, "%f", &f)
		return f, err == nil
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}
