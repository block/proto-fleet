package curtailment

import (
	"context"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
)

// AuditLogger emits activity rows. activity.Service satisfies it via the
// Log/LogStrict pair; curtailment uses LogStrict so persistence failures
// surface to Metrics.IncAuditWriteFailure instead of being swallowed.
type AuditLogger interface {
	Log(ctx context.Context, event activitymodels.Event)
	LogStrict(ctx context.Context, event activitymodels.Event) error
}

// NoOpAuditLogger is the default until cmd/fleetd wires activity.Service.
type NoOpAuditLogger struct{}

func (NoOpAuditLogger) Log(context.Context, activitymodels.Event)             {}
func (NoOpAuditLogger) LogStrict(context.Context, activitymodels.Event) error { return nil }

// Curtailment activity event types. Start override flags are metadata on
// ActivityTypeStarted so one curtailment cycle appears as one activity row.
const (
	ActivityTypeStarted         = "curtailment_started"
	ActivityTypeAdminTerminated = "curtailment_admin_terminated"
	// ActivityTypeAdminTerminatedReplay fires when AdminTerminate echoes
	// an already-terminal event in the requested state — preserves the
	// race-loser's reason + actor in the audit feed.
	ActivityTypeAdminTerminatedReplay = "curtailment_admin_terminated_replay"
	ActivityTypeUpdated               = "curtailment_updated"
)
