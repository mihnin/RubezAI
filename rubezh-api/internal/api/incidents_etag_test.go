package api

import (
	"testing"
	"time"
)

func TestIncidentETagIsRFC3339Nano(t *testing.T) {
	ts := time.Date(2026, 5, 20, 12, 0, 0, 123456789, time.UTC)
	got := incidentETag(ts)
	want := "2026-05-20T12:00:00.123456789Z"
	if got != want {
		t.Errorf("ETag = %q, ожидалось %q", got, want)
	}
}

func TestIncidentETagRoundtrip(t *testing.T) {
	// ETag должен парситься тем же RFC3339Nano-парсером, что
	// parsePatchIfMatch использует для If-Match. Это инвариант F1.
	original := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	etag := incidentETag(original)
	parsed, err := time.Parse(time.RFC3339Nano, etag)
	if err != nil {
		t.Fatalf("If-Match не парсит ETag: %v", err)
	}
	if !parsed.Equal(original) {
		t.Errorf("round-trip потерял timestamp: %v != %v", parsed, original)
	}
}
