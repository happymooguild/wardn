package metrics

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAverageScalar(t *testing.T) {
	if AverageScalar(nil) != 0 {
		t.Fatal("empty")
	}
	pts := []Point{{V: 10}, {V: 20}, {V: 30}}
	if AverageScalar(pts) != 20 {
		t.Fatalf("got %v", AverageScalar(pts))
	}
}

func TestParseV5SeriesPrometheusShape(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"status": "success",
		"data": map[string]any{
			"result": []any{
				map[string]any{
					"values": []any{
						[]any{float64(1700000000), "12.5"},
						[]any{float64(1700000060), "13.5"},
					},
				},
			},
		},
	})
	pts, err := parseV5Series(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("len=%d", len(pts))
	}
	if pts[0].V != 12.5 {
		t.Fatalf("v0=%v", pts[0].V)
	}
	if !pts[0].T.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("t0=%v", pts[0].T)
	}
}

func TestDownsample(t *testing.T) {
	pts := make([]Point, 100)
	out := Downsample(pts, 10)
	if len(out) != 10 {
		t.Fatalf("len=%d", len(out))
	}
}
