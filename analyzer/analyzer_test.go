package analyzer

import "testing"

func TestCompare(t *testing.T) {
	pct, degraded := Compare(100, 130, 25, true)
	if !degraded {
		t.Fatalf("expected degraded, got pct=%.2f", pct)
	}
	pct, degraded = Compare(100, 110, 25, true)
	if degraded {
		t.Fatalf("expected healthy, got pct=%.2f degraded", pct)
	}
	pct, degraded = Compare(0, 50, 25, true)
	if !degraded {
		t.Fatalf("expected degraded from near-zero baseline, pct=%.2f", pct)
	}
}
