// Package metrics provides a backend-agnostic PromQL query interface.
package metrics

import (
	"context"
	"time"
)

type Point struct {
	T time.Time
	V float64
}

type Series struct {
	Points []Point
	Scalar float64
}

type MetricsProvider interface {
	Query(ctx context.Context, promql string, start, end time.Time) (Series, error)
	// QuerySeries is Query with an explicit step (seconds) so callers that need
	// fine-resolution samples (e.g. per-version percentile charts) can ask for
	// them instead of the coarse auto-step Query picks.
	QuerySeries(ctx context.Context, promql string, start, end time.Time, stepSec int) (Series, error)
}

// MetricInfo describes an available metric discovered from the backend.
type MetricInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Unit string `json:"unit"`
}

// MetricLister is optionally implemented by a provider that can enumerate the
// metric names it knows about (for the custom-dashboard metric picker).
type MetricLister interface {
	ListMetrics(ctx context.Context, search string) ([]MetricInfo, error)
}

// AverageScalar returns the mean of point values, or 0 if empty.
func AverageScalar(points []Point) float64 {
	if len(points) == 0 {
		return 0
	}
	var sum float64
	for _, p := range points {
		sum += p.V
	}
	return sum / float64(len(points))
}

// Downsample keeps at most maxPoints evenly spaced samples.
func Downsample(points []Point, maxPoints int) []Point {
	if maxPoints <= 0 || len(points) <= maxPoints {
		return points
	}
	out := make([]Point, 0, maxPoints)
	step := float64(len(points)-1) / float64(maxPoints-1)
	for i := 0; i < maxPoints; i++ {
		idx := int(float64(i) * step)
		if idx >= len(points) {
			idx = len(points) - 1
		}
		out = append(out, points[idx])
	}
	return out
}
