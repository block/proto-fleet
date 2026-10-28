package curtailment

import (
	"math"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// ToStringSet builds a membership set, collapsing duplicate values.
func ToStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// IsFinite reports whether a telemetry or control value is neither NaN nor Inf.
func IsFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// MaxDurationElapsed reports how long ev has run against its finite safety cap
// and whether the cap has expired. Pending and restoring events that never
// became active measure from creation because dispatch timestamps advance on
// retries, so an event that expired while pending stays expired after it
// starts restoring.
func MaxDurationElapsed(ev *models.Event, now time.Time) (time.Duration, bool) {
	if ev == nil || ev.AllowUnbounded || ev.MaxDurationSeconds == nil || *ev.MaxDurationSeconds <= 0 {
		return 0, false
	}
	start := ev.CreatedAt
	switch {
	case ev.StartedAt != nil:
		start = *ev.StartedAt
	case ev.State != models.EventStatePending && ev.State != models.EventStateRestoring:
		return 0, false
	}
	elapsed := now.Sub(start)
	return elapsed, elapsed >= time.Duration(*ev.MaxDurationSeconds)*time.Second
}
