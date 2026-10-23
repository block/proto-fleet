package containers

import "time"

func observationStale(at, now time.Time) bool {
	return at.IsZero() || now.Sub(at) > 2*time.Minute || at.After(now.Add(5*time.Second))
}
