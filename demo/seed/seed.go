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
	"fmt"
	"math"
	"math/rand"
	"time"

	"wardn/store"
)

// versionSchedule fixes how long ago each version was deployed. Deploy times are
// shared across services (so the time-range selector behaves the same); only the
// latency profile differs per service (see profileFor).
var versionSchedule = []struct {
	name    string
	daysAgo float64
}{
	{"v1.0.0", 200}, // only visible at "last 1 year"+
	{"v1.0.1", 75},  // last 3 months
	{"v1.0.2", 45},  // last 2 months
	{"v1.0.3", 22},  // last 1 month
	{"v1.0.4", 10},  // last 2 weeks
	{"v1.0.5", 6},   // last 1 week
	{"v1.0.6", 4},   // last 5 days
	{"v1.0.7", 2.5}, // last 3 days
	{"v1.0.8", 1.5}, // last 2 days
	{"v1.0.9", 0.5}, // last 1 day
}

type profile struct {
	base      float64      // baseline latency of the first version
	step      float64      // per-version drift
	regressed map[int]bool // which version indexes are regressed
	regMean   float64      // latency of a regressed version
}

// profileFor gives each service a distinct latency signature so switching apps
// on the dashboard shows visibly different data.
func profileFor(variant int) profile {
	switch variant % 2 {
	case 1:
		// e.g. payments-service: slower baseline, later regressions, bigger spike
		return profile{base: 92, step: 2.6, regressed: map[int]bool{5: true, 8: true}, regMean: 260}
	default:
		// e.g. checkout-service: the original profile
		return profile{base: 52, step: 1.8, regressed: map[int]bool{3: true, 7: true}, regMean: 155}
	}
}

// Run inserts each version's samples for one service. Idempotent: it no-ops if
// versioned data already exists for the app+metric. `variant` selects the latency
// profile. Returns the number of samples inserted.
func Run(ctx context.Context, st *store.Store, appID int64, appName, metric string, variant int) (int, error) {
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

	prof := profileFor(variant)
	rng := rand.New(rand.NewSource(int64(1337 + variant*101)))
	now := time.Now().UTC()
	gap := time.Duration(windowMinutes) * time.Minute / samplesPerVer

	samples := make([]store.Sample, 0, len(versionSchedule)*samplesPerVer)
	for i, spec := range versionSchedule {
		mean := prof.base + float64(i)*prof.step
		if prof.regressed[i] {
			mean = prof.regMean
		}
		std := mean * 0.12
		start := now.Add(-time.Duration(spec.daysAgo * 24 * float64(time.Hour)))

		for j := 0; j < samplesPerVer; j++ {
			val := mean + rng.NormFloat64()*std
			if val < 5 {
				val = 5
			}
			samples = append(samples, store.Sample{
				Version: spec.name,
				Value:   math.Round(val*10) / 10,
				TS:      start.Add(time.Duration(j) * gap),
			})
		}
	}

	if err := st.InsertSamples(ctx, appID, metric, samples); err != nil {
		return 0, fmt.Errorf("insert samples: %w", err)
	}
	return len(samples), nil
}
