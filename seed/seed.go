// Package seed lays down synthetic multi-version latency history on first boot,
// so the dashboard has a populated version-comparison chart to show and click
// into immediately (rather than waiting for the live emitter to accumulate one).
package seed

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"wardn/store"
)

// Run inserts `versions` sequential versions, each a 30-minute block of samples
// ending just before now. Most are healthy; a couple are deliberately regressed
// so the per-version chart has visible spikes to drill into.
//
// Idempotent: it no-ops if versioned data already exists for the app+metric.
// Returns the number of samples inserted.
func Run(ctx context.Context, st *store.Store, appID int64, appName, metric string) (int, error) {
	const (
		versions      = 10
		samplesPerVer = 60
		block         = 30 * time.Minute
		sampleGap     = block / samplesPerVer
	)

	existing, err := st.CountVersioned(ctx, appName, metric)
	if err != nil {
		return 0, err
	}
	if existing > 0 {
		return 0, nil // already seeded
	}

	rng := rand.New(rand.NewSource(1337))
	regressed := map[int]bool{3: true, 7: true}

	start := time.Now().UTC().Add(-time.Duration(versions) * block)
	samples := make([]store.Sample, 0, versions*samplesPerVer)

	for v := 0; v < versions; v++ {
		version := fmt.Sprintf("v1.0.%d", v)
		// gentle upward drift across versions, big bump on regressed ones
		mean := 55 + float64(v)*1.5
		if regressed[v] {
			mean += 90
		}
		std := mean * 0.12
		vStart := start.Add(time.Duration(v) * block)

		for i := 0; i < samplesPerVer; i++ {
			val := mean + rng.NormFloat64()*std
			if val < 5 {
				val = 5
			}
			samples = append(samples, store.Sample{
				Version: version,
				Value:   math.Round(val*10) / 10,
				TS:      vStart.Add(time.Duration(i) * sampleGap),
			})
		}
	}

	if err := st.InsertSamples(ctx, appID, metric, samples); err != nil {
		return 0, err
	}
	return len(samples), nil
}
