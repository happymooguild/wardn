// Command sample-app stands in for a real instrumented service.
//
// On a fixed interval it POSTs a synthetic latency sample to the wardn backend,
// authenticating with an API key it reads from the environment (mounted from a
// Kubernetes Secret in the Helm deployment). Flip REGRESSED=true to simulate a
// bad "v2" deploy — latency jumps by REGRESSION_ADD_MS — which is what makes the
// dashboard line climb.
//
// No third-party dependencies: standard library only, so it builds offline.
package main

import (
	"bytes"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
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
		base     = os.Getenv("WARDN_URL")        // e.g. http://wardn-backend:8080
		apiKey   = os.Getenv("WARDN_API_KEY")    // from the mounted Secret
		appName  = envOr("APP_NAME", "checkout-service")
		appVer   = envOr("APP_VERSION", "v1.0.10") // newest version, after the seeded history
		metric   = envOr("METRIC", "latency_ms")
		interval = envDuration("INTERVAL", 5*time.Second)
		baseMS   = envFloat("BASE_LATENCY_MS", 60)   // healthy baseline
		jitterMS = envFloat("JITTER_MS", 12)         // +/- noise
		regressed = envBool("REGRESSED", false)      // simulate a bad deploy
		regressAdd = envFloat("REGRESSION_ADD_MS", 140)
	)
	if base == "" {
		base = "http://localhost:8080"
	}
	if apiKey == "" {
		log.Fatal("WARDN_API_KEY is required")
	}

	version := "v1 (healthy)"
	if regressed {
		version = "v2 (regressed)"
	}
	log.Printf("sample-app: posting %s for %q %s to %s every %s [%s]",
		metric, appName, appVer, base, interval, version)

	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Emit one immediately, then on every tick.
	for ; ; <-ticker.C {
		value := baseMS + (rand.Float64()*2-1)*jitterMS
		if regressed {
			value += regressAdd
		}
		if value < 1 {
			value = 1
		}
		post(client, base, apiKey, sample{
			App:       appName,
			Metric:    metric,
			Version:   appVer,
			Value:     round1(value),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}
}

func post(client *http.Client, base, apiKey string, s sample) {
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
	log.Printf("posted %s=%.1f", s.Metric, s.Value)
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
