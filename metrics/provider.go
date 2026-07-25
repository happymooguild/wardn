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
