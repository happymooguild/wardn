// Package seed lays down synthetic multi-version latency history on first boot,
// so the dashboard has a populated version-comparison chart to show and click
// into immediately (rather than waiting for the live emitter to accumulate one).
//
// The versions are spread across time — from ~half a day to ~200 days ago — so
// the dashboard's time-range selector has something to reveal: short windows show
// only recent versions, long windows show the whole history.
package seed

import (
	"context"
	"math"
	"math/rand"
	"time"

	"wardn/store"
)

// verSpec places one version: how long ago it was deployed and its latency mean.
type verSpec struct {
	name    string
	daysAgo float64
	mean    float64
}

// Run inserts each version's samples. Idempotent: it no-ops if versioned data
// already exists for the app+metric. Returns the number of samples inserted.
func Run(ctx context.Context, st *store.Store, appID int64, appName, metric string) (int, error) {
	const (
		samplesPerVer = 60
		windowMinutes = 30 // each version's samples span this long
	)

	existing, err := st.CountVersioned(ctx, appName, metric)
	if err != nil {
		return 0, err
	}
	if existing > 0 {
		return 0, nil // already seeded
	}

	// One version lands in each time-range bucket, so every step of the range
	// dropdown reveals one more. v1.0.3 and v1.0.7 are deliberately regressed.
	specs := []verSpec{
		{"v1.0.0", 200, 52},  // only visible at "last 1 year"+
		{"v1.0.1", 75, 55},   // last 3 months
		{"v1.0.2", 45, 57},   // last 2 months
		{"v1.0.3", 22, 150},  // last 1 month — regressed
		{"v1.0.4", 10, 60},   // last 2 weeks
		{"v1.0.5", 6, 62},    // last 1 week
		{"v1.0.6", 4, 64},    // last 5 days
		{"v1.0.7", 2.5, 165}, // last 3 days — regressed
		{"v1.0.8", 1.5, 66},  // last 2 days
		{"v1.0.9", 0.5, 68},  // last 1 day
	}

	rng := rand.New(rand.NewSource(1337))
	now := time.Now().UTC()
	gap := time.Duration(windowMinutes) * time.Minute / samplesPerVer

	samples := make([]store.Sample, 0, len(specs)*samplesPerVer)
	for _, spec := range specs {
		start := now.Add(-time.Duration(spec.daysAgo * 24 * float64(time.Hour)))
		std := spec.mean * 0.12
		for i := 0; i < samplesPerVer; i++ {
			val := spec.mean + rng.NormFloat64()*std
			if val < 5 {
				val = 5
			}
			samples = append(samples, store.Sample{
				Version: spec.name,
				Value:   math.Round(val*10) / 10,
				TS:      start.Add(time.Duration(i) * gap),
			})
		}
	}

	if err := st.InsertSamples(ctx, appID, metric, samples); err != nil {
		return 0, err
	}
	return len(samples), nil
}
