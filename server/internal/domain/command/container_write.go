package command

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

type containerWriteScope struct {
	deviceID, orgID, siteID int64
	noun, upgradeMessage    string
}

// executeContainerWrite keeps controller placement and error classification
// consistent for native container, tank, and module writes. Scope-specific
// topology validation runs under the same non-retrying binding transaction.
func (es *ExecutionService) executeContainerWrite(ctx context.Context, scope containerWriteScope, validate func(context.Context, sqlc.Querier) error, write func(context.Context) error) error {
	// Hold placement stable through gate admission and the physical write, which
	// must never be replayed by a transaction retry.
	dispatchStarted := false
	err := devicewrite.NewService(es.conn).WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		device, err := q.GetDeviceByID(ctx, sqlc.GetDeviceByIDParams{ID: scope.deviceID, OrgID: scope.orgID})
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewFailedPreconditionError("selected " + scope.noun + " controller unavailable")
		}
		if err != nil {
			return err
		}
		target, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{DeviceIdentifier: device.DeviceIdentifier, OrgID: scope.orgID})
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewFailedPreconditionError("selected " + scope.noun + " controller unavailable")
		}
		if err != nil {
			return err
		}
		if target.ID != scope.deviceID || target.SiteID.Int64 != scope.siteID {
			return fleeterror.NewFailedPreconditionError(scope.noun + " command ownership or site changed")
		}
		if validate != nil {
			if err := validate(ctx, q); err != nil {
				return err
			}
		}
		dispatchStarted = true
		if err := devicewrite.Dispatch(ctx, write); err != nil {
			if errors.Is(err, control.ErrCommandProtocolUpgradeRequired) {
				return devicewrite.NotDispatched(fleeterror.NewFailedPreconditionError(scope.upgradeMessage))
			}
			if devicewrite.IsNotDispatched(err) {
				return err
			}
			slog.ErrorContext(ctx, scope.noun+" mining outcome unconfirmed", "device_id", scope.deviceID, "error", err)
			return fleeterror.NewFailedPreconditionError(scope.noun + " mining outcome unconfirmed")
		}
		return nil
	})
	if err != nil {
		if fleeterror.IsFailedPreconditionError(err) {
			return err
		}
		if dispatchStarted && !devicewrite.IsNotDispatched(err) {
			slog.ErrorContext(ctx, scope.noun+" mining outcome unconfirmed", "device_id", scope.deviceID, "error", err)
			return fleeterror.NewFailedPreconditionError(scope.noun + " mining outcome unconfirmed")
		}
		slog.ErrorContext(ctx, scope.noun+" mining preparation failed", "device_id", scope.deviceID, "error", err)
		preparedErr := fleeterror.NewInternalError(scope.noun + " mining preparation failed; retry shortly")
		if devicewrite.IsNotDispatched(err) {
			return devicewrite.NotDispatched(preparedErr)
		}
		return preparedErr
	}
	return nil
}
