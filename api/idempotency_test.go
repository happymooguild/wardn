package api

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"wardn/store"
)

func TestIdempotencyKeyStable(t *testing.T) {
	ts := time.Date(2026, 7, 19, 10, 15, 0, 0, time.UTC)
	a := store.FormatIdempotencyKey(1, "abc", ts)
	b := store.FormatIdempotencyKey(1, "abc", ts)
	if a != b {
		t.Fatalf("unstable key")
	}
	sum := sha256.Sum256([]byte(a))
	_ = hex.EncodeToString(sum[:])
	c := store.FormatIdempotencyKey(1, "abc", ts.Add(time.Second))
	if a == c {
		t.Fatalf("expected different key for different timestamp")
	}
}
