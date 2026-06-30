package cohort

import (
	"context"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
)

// AuditLogger emits activity rows. activity.Service satisfies it.
type AuditLogger interface {
	Log(ctx context.Context, event activitymodels.Event)
}

// NoOpAuditLogger is the default until cmd/fleetd wires activity.Service.
type NoOpAuditLogger struct{}

func (NoOpAuditLogger) Log(context.Context, activitymodels.Event) {}

const (
	activityTypeCreated  = "cohort_created"
	activityTypeDeleted  = "cohort_deleted"
	activityTypeReleased = "cohort_released"
	activityTypeExpired  = "cohort_expired"
	activityTypeUpdated  = "cohort_updated"
)
