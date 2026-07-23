package minerchannel

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
	activityTypeCreated  = "miner_channel_created"
	activityTypeDeleted  = "miner_channel_deleted"
	activityTypeReleased = "miner_channel_released"
	activityTypeExpired  = "miner_channel_expired"
	activityTypeUpdated  = "miner_channel_updated"
)
