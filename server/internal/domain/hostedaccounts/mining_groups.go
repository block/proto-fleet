package hostedaccounts

import (
	"context"
	"database/sql"
	"errors"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// requireAllocationMiningGroups runs after organization, binding and inventory
// locks, before changing ownership. Recall remains available in every mode.
func requireAllocationMiningGroups(ctx context.Context, q sqlc.Querier, orgID int64, deviceIDs []int64) error {
	if err := q.EnsureDeviceWriteFences(ctx, deviceIDs); err != nil {
		return err
	}
	if _, err := q.LockDeviceWriteFences(ctx, deviceIDs); err != nil {
		return err
	}
	for _, deviceID := range deviceIDs {
		_, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		ready, err := q.CT1HashboardAllocationReady(ctx, sqlc.CT1HashboardAllocationReadyParams{OrganizationID: orgID, DeviceID: deviceID})
		if err != nil {
			return err
		}
		if !ready {
			reason, err := q.GetCT1HashboardAllocationBlocker(ctx, sqlc.GetCT1HashboardAllocationBlockerParams{OrganizationID: orgID, DeviceID: deviceID})
			if err != nil {
				return err
			}
			return fleeterror.NewErrorWithEndpointCode(reason, connect.CodeFailedPrecondition, int32(pb.AssetAssignmentErrorCode_ASSET_ASSIGNMENT_ERROR_CODE_MINING_GROUPS_NOT_READY))
		}
	}
	return nil
}
