package curtailment

import (
	"context"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
)

// AuditLogger is the narrow surface curtailment uses to emit activity
// rows. activity.Service satisfies it; tests inject a fake or leave the
// default NoOpAuditLogger when audit isn't under test.
//
// The interface deliberately matches activity.Service.Log's signature so
// callers can pass *activity.Service directly without an adapter.
type AuditLogger interface {
	Log(ctx context.Context, event activitymodels.Event)
}

// NoOpAuditLogger is the default AuditLogger until cmd/fleetd wires the
// real activity.Service. Calls return without persisting.
type NoOpAuditLogger struct{}

func (NoOpAuditLogger) Log(context.Context, activitymodels.Event) {}

// Curtailment activity event types. The constants live here so the
// audit recorder and any analytics consumers share one vocabulary.
const (
	// ActivityTypeStarted is emitted on every successful Service.Start.
	ActivityTypeStarted = "curtailment_started"
	// ActivityTypeStartedUnbounded is emitted in addition to
	// ActivityTypeStarted when allow_unbounded=true. Two rows, not a flag,
	// so a feed of unbounded starts is a simple type filter.
	ActivityTypeStartedUnbounded = "curtailment_unbounded_start"
	// ActivityTypeStartedForceMaintenance is emitted in addition to
	// ActivityTypeStarted when force_include_maintenance=true.
	ActivityTypeStartedForceMaintenance = "curtailment_force_include_maintenance"
	// ActivityTypeAdminTerminated is emitted on every successful
	// Service.AdminTerminate so the privileged force-terminate path
	// captures actor + reason in the audit feed.
	ActivityTypeAdminTerminated = "curtailment_admin_terminated"
)
