package poolconfigurations

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/google/uuid"
)

type lifecycleContextKey struct{}

// WithLifecycleTransition is an internal worker capability. No RPC accepts this
// value. Authority still checks the durable event, owner generation and target.
func WithLifecycleTransition(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, lifecycleContextKey{}, id)
}

func LifecycleTransitionID(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(lifecycleContextKey{}).(uuid.UUID)
	return id
}

func AuthorizeLifecycleTransition(ctx context.Context, q sqlc.Querier, scope Scope, deviceID int64) (sqlc.GetPoolLifecycleTransitionRow, error) {
	id := LifecycleTransitionID(ctx)
	if id == uuid.Nil {
		return sqlc.GetPoolLifecycleTransitionRow{}, fleeterror.NewForbiddenError("pool lifecycle authority is unavailable")
	}
	row, err := q.GetPoolLifecycleTransition(ctx, id)
	if err != nil {
		return row, err
	}
	if row.OrganizationID != scope.OrganizationID || row.OwnerAccountID != scope.AccountID || row.DeviceID != deviceID || row.State == "superseded" || row.AssetKind == 0 {
		return row, fleeterror.NewForbiddenError("pool lifecycle ownership changed")
	}
	newer, err := q.PoolLifecycleHasNewerAssignment(ctx, id)
	if err != nil {
		return row, err
	}
	if newer {
		return row, fleeterror.NewForbiddenError("a newer pool assignment replaced the lifecycle policy")
	}
	current, err := q.GetPoolLifecycleAllocation(ctx, sqlc.GetPoolLifecycleAllocationParams{OrganizationID: row.OrganizationID, Kind: row.AssetKind, AssetID: row.AssetID, DeviceID: row.DeviceID})
	if err != nil {
		return row, err
	}
	if current.OwnerAccountID != row.OwnerAccountID || current.Revision != row.AllocationRevision {
		return row, fleeterror.NewForbiddenError("pool lifecycle ownership changed")
	}
	return row, nil
}

// RecordLifecycleAssignmentAudit records the policy operation as system work.
func RecordLifecycleAssignmentAudit(ctx context.Context, q sqlc.Querier, p Principal, configurationID, operationID string) error {
	return recordConfigurationAudit(ctx, q, p, "apply_default_pool_configuration", map[string]string{"configuration_id": configurationID, "operation_id": operationID, "transition_id": LifecycleTransitionID(ctx).String()})
}
