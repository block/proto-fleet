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

// MaxDurationElapsed reports elapsed time against ev's finite safety cap.
// The clock starts at creation (or an earlier started_at) and never resets on
// activation or re-curtailment. Pending work therefore consumes the same cap
// as active work, and a restore reversal cannot revive an expired event.
func MaxDurationElapsed(ev *models.Event, now time.Time) (time.Duration, bool) {
	if ev == nil || ev.AllowUnbounded || ev.MaxDurationSeconds == nil || *ev.MaxDurationSeconds <= 0 {
		return 0, false
	}
	start := ev.CreatedAt
	if ev.StartedAt != nil && (start.IsZero() || ev.StartedAt.Before(start)) {
		start = *ev.StartedAt
	}
	elapsed := now.Sub(start)
	return elapsed, elapsed >= time.Duration(*ev.MaxDurationSeconds)*time.Second
}
