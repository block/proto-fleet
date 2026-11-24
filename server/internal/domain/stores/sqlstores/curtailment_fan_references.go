package sqlstores

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func countResponseProfilesByInfrastructureDevices(ctx context.Context, q sqlc.Querier, orgID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	count, err := q.CountResponseProfilesByInfrastructureDevices(ctx, sqlc.CountResponseProfilesByInfrastructureDevicesParams{
		OrgID:                   orgID,
		InfrastructureDeviceIds: ids,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count response profiles by infrastructure devices: %v", err)
	}
	return count, nil
}

func countCurtailmentEventsByInfrastructureDevices(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	ids []int64,
	includeTerminalErrors bool,
) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	count, err := q.CountCurtailmentFanClaims(ctx, sqlc.CountCurtailmentFanClaimsParams{
		IncludeTerminalErrors: includeTerminalErrors,
		OrgID:                 orgID,
		FacilityFanDeviceIds:  ids,
	})
	if err != nil {
		kind := "non-terminal"
		if includeTerminalErrors {
			kind = "active"
		}
		return 0, fleeterror.NewInternalErrorf("failed to count %s curtailment events by infrastructure devices: %v", kind, err)
	}
	return count, nil
}
