package httpserver

import (
	"testing"
	"time"

	"github.com/pflege-de/teamster/internal/models"
)

func TestHashFingerprintDeterministic(t *testing.T) {
	baseTime := time.Date(2025, 12, 7, 20, 7, 0, 0, time.UTC)

	alertA := models.Alert{
		Source:    "universal",
		Generator: "custom",
		StartsAt:  baseTime,
		Labels: map[string]string{
			"severity":  "critical",
			"alertname": "HighCPU",
		},
	}
	alertB := models.Alert{
		Source:    "universal",
		Generator: "custom",
		StartsAt:  baseTime,
		Labels: map[string]string{
			"alertname": "HighCPU",
			"severity":  "critical",
		},
	}

	hashA := hashFingerprint(alertA)
	hashB := hashFingerprint(alertB)

	if hashA != hashB {
		t.Fatalf("expected deterministic hash, got %s and %s", hashA, hashB)
	}
}
