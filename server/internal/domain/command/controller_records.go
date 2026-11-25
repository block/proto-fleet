package command

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// ControllerRecordsAuthorization checks the caller's access at the
// container's current site.
type ControllerRecordsAuthorization func(ctx context.Context, siteID *int64) error

const controllerRecordsUpgradeMessage = "Upgrade this container's Fleet Node to read controller logs and audit events."

// ReadControllerRecords reads a CT1 controller's own logs or audit events on
// demand, through the container's assigned Fleet Node when it has one. Fleet
// neither stores nor records the result.
//
// The read runs inside the host and binding observations, so it holds a
// database transaction and a share lock on the container's binding until the
// controller answers, and writes to that container wait behind it. Call it
// only for an operator's request; never poll through it.
func (s *Service) ReadControllerRecords(ctx context.Context, identifier string, req sdk.ControllerRecordsRequest, authorize ControllerRecordsAuthorization) (sdk.ControllerRecordsResult, error) {
	// One server-owned budget covers target lookup, binding observation and
	// the remote read; shorter caller limits still apply.
	ctx, cancel := context.WithTimeout(ctx, runtimepolicy.ControllerRecordsReadTimeout)
	defer cancel()
	var result sdk.ControllerRecordsResult
	if err := req.Validate(); err != nil {
		return result, fleeterror.NewInvalidArgumentError(err.Error())
	}
	if authorize == nil {
		return result, fleeterror.NewForbiddenError("container authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return result, err
	}
	params := sqlc.GetContainerMiningTargetParams{DeviceIdentifier: identifier, OrgID: info.OrganizationID}
	target, err := sqlc.New(s.conn).GetContainerMiningTarget(ctx, params)
	if errors.Is(err, sql.ErrNoRows) {
		return result, fleeterror.NewNotFoundError("container not found")
	}
	if err != nil {
		return result, err
	}
	if s.controllerRecordsTargetLookedUp != nil {
		s.controllerRecordsTargetLookedUp()
	}
	writes := devicewrite.NewService(s.conn)
	err = writes.WithHostObservation(ctx, info.OrganizationID, target.ID, target.WriteEpoch, func(ctx context.Context) error {
		return writes.WithBindingObservation(ctx, target.ID, func(q sqlc.Querier) error {
			fresh, err := q.GetContainerMiningTarget(ctx, params)
			if err != nil {
				return err
			}
			if fresh.ID != target.ID || fresh.WriteEpoch != target.WriteEpoch {
				return fleeterror.NewFailedPreconditionError("container ownership changed")
			}
			var site *int64
			if fresh.SiteID.Valid {
				site = &fresh.SiteID.Int64
			}
			return authorize(ctx, site)
		}, func(ctx context.Context) error {
			miner, err := s.executionService.minerService.GetMinerForCommand(ctx, target.ID)
			if err != nil {
				return err
			}
			reader, ok := miner.(sdk.DeviceControllerRecords)
			if !ok {
				return fleeterror.NewUnimplementedError("controller records unsupported")
			}
			result, err = reader.ControllerRecords(ctx, req)
			if errors.Is(err, control.ErrCommandProtocolUpgradeRequired) {
				return fleeterror.NewFailedPreconditionError(controllerRecordsUpgradeMessage)
			}
			if err == nil {
				err = result.Validate(req)
			}
			return err
		})
	})
	return result, err
}
