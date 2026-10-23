package containers

import (
	"testing"
	"time"
)

func TestObservationStale(t *testing.T) {
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		at    time.Time
		stale bool
	}{
		{name: "zero", stale: true},
		{name: "current", at: now},
		{name: "oldest fresh", at: now.Add(-2 * time.Minute)},
		{name: "too old", at: now.Add(-2*time.Minute - time.Nanosecond), stale: true},
		{name: "latest fresh", at: now.Add(5 * time.Second)},
		{name: "too far ahead", at: now.Add(5*time.Second + time.Nanosecond), stale: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := observationStale(tc.at, now); got != tc.stale {
				t.Errorf("observationStale(%v, %v) = %t, want %t", tc.at, now, got, tc.stale)
			}
		})
	}
}
