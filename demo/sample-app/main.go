// Command sample-app stands in for a real instrumented service.
//
// It always POSTs synthetic latency samples to wardn (existing dashboard).
// When OTEL_EXPORTER_OTLP_ENDPOINT is set, it also exports OTLP/HTTP gauges
// to SigNoz so the analyzer has something to query.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type sample struct {
	App       string  `json:"app"`
	Metric    string  `json:"metric"`
	Version   string  `json:"version"`
	Value     float64 `json:"value"`
	Timestamp string  `json:"timestamp"`
}

func main() {
	var (
		base       = os.Getenv("WARDN_URL")
		apiKey     = os.Getenv("WARDN_API_KEY")
		appName    = envOr("APP_NAME", "checkout-service")
		appVer     = envOr("APP_VERSION", "v1.0.10")
		metric     = envOr("METRIC", "latency_ms")
		interval   = envDuration("INTERVAL", 5*time.Second)
		baseMS     = envFloat("BASE_LATENCY_MS", 60)
		jitterMS   = envFloat("JITTER_MS", 12)
		baseRPS    = envFloat("BASE_RPS", 120)
		regressed  = envBool("REGRESSED", false)
		regressAdd = envFloat("REGRESSION_ADD_MS", 140)
		otlp       = strings.TrimRight(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), "/")
		// wardn now sources metrics from SigNoz around deploy markers, not from a
		// continuous push. The legacy /api/v1/metrics push is off by default and
		// only kept behind this flag for the older non-OTLP demo path.
		pushWardn = envBool("WARDN_PUSH_METRICS", false)
	)
	if base == "" {
		base = "http://localhost:8080"
	}
	if pushWardn && apiKey == "" {
		log.Fatal("WARDN_API_KEY is required when WARDN_PUSH_METRICS=true")
	}

	mode := "healthy"
	if regressed {
		mode = "regressed"
	}
	log.Printf("sample-app: %q %s [%s] every %s (push=%v otlp=%q)",
		appName, appVer, mode, interval, pushWardn, otlp)

	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for ; ; <-ticker.C {
		value := baseMS + (rand.Float64()*2-1)*jitterMS
		if regressed {
			value += regressAdd
		}
		if value < 1 {
			value = 1
		}
		errorRate := 0.5 + rand.Float64()
		if regressed {
			errorRate = 8 + rand.Float64()*4
		}
		// Throughput (req/s), with a little jitter. A regressed build sheds load,
		// so it also dips — giving the throughput dashboard a visible signal.
		rps := baseRPS * (0.95 + rand.Float64()*0.1)
		if regressed {
			rps *= 0.7
		}

		if pushWardn {
			postWardn(client, base, apiKey, sample{
				App:       appName,
				Metric:    metric,
				Version:   appVer,
				Value:     round1(value),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
		}
		if otlp != "" {
			postOTLP(client, otlp, appName, appVer, value, errorRate, rps)
		}
	}
}

func postWardn(client *http.Client, base, apiKey string, s sample) {
	body, _ := json.Marshal(s)
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/metrics", bytes.NewReader(body))
	if err != nil {
		log.Printf("build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("post metric: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("post metric: unexpected status %d", resp.StatusCode)
		return
	}
	log.Printf("posted wardn %s=%.1f", s.Metric, s.Value)
}

// postOTLP sends OTLP/HTTP JSON gauges that SigNoz can scrape via PromQL.
func postOTLP(client *http.Client, endpoint, service, version string, latencyMS, errorRate, rps float64) {
	nowNano := time.Now().UnixNano()
	payload := map[string]any{
		"resourceMetrics": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						kv("service.name", service),
						kv("service.version", version),
					},
				},
				"scopeMetrics": []any{
					map[string]any{
						"scope": map[string]any{"name": "wardn-sample-app"},
						"metrics": []any{
							gaugeMetric("wardn_demo_latency_ms", "ms", latencyMS, nowNano, service, version),
							gaugeMetric("wardn_demo_error_rate", "1", errorRate, nowNano, service, version),
							gaugeMetric("wardn_demo_rps", "1", rps, nowNano, service, version),
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	url := endpoint
	if !strings.HasSuffix(url, "/v1/metrics") {
		url = endpoint + "/v1/metrics"
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("otlp export: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("otlp export: status %d", resp.StatusCode)
	}
}

func gaugeMetric(name, unit string, value float64, ts int64, service, version string) map[string]any {
	return map[string]any{
		"name": name,
		"unit": unit,
		"gauge": map[string]any{
			"dataPoints": []any{
				map[string]any{
					"asDouble":     value,
					"timeUnixNano": fmt.Sprintf("%d", ts),
					// version as a datapoint attribute makes it a PromQL-filterable
					// label, so wardn can pull exactly one deploy's samples and never
					// mix in a prior version's stale (carried-forward) series.
					"attributes": []any{
						kv("service_name", service),
						kv("version", version),
					},
				},
			},
		},
	}
}

func kv(key, val string) map[string]any {
	return map[string]any{
		"key":   key,
		"value": map[string]any{"stringValue": val},
	}
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
