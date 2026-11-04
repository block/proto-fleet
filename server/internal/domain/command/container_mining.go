package command

import (
	"context"
	"database/sql"
	"errors"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type containerMiningAuthorization func(context.Context, *int64, sdk.ContainerMiningRequest) error

// Authorization scope stays in Fleet's queue; only Request reaches the device.
// A zero SiteID represents an explicitly authorized unassigned controller.
type containerMiningPayload struct {
	Request sdk.ContainerMiningRequest `json:"request"`
	OrgID   int64                      `json:"org_id"`
	SiteID  int64                      `json:"site_id"`
}

func (s *Service) SetContainerMining(ctx context.Context, identifier string, req sdk.ContainerMiningRequest, authorize containerMiningAuthorization) (*CommandResult, error) {
	if err := req.Validate(); err != nil {
		return nil, fleeterror.NewInvalidArgumentError(err.Error())
	}
	if req.ReadOnly() {
		return nil, fleeterror.NewInvalidArgumentError("write operation required")
	}
	if authorize == nil {
		return nil, fleeterror.NewForbiddenError("container authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return withContainerCommandTransaction(ctx, s, func(txCtx context.Context, q sqlc.Querier) (*CommandResult, error) {
		target, err := q.LockContainerCommandTarget(txCtx, sqlc.LockContainerCommandTargetParams{DeviceIdentifier: identifier, OrgID: info.OrganizationID})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("container not found")
		}
		if err != nil {
			return nil, err
		}
		// Require an active Proto controller, also for a bookmarked action dialog.
		if !target.IsActive || target.DriverName != "proto" {
			return nil, fleeterror.NewNotFoundError("container not found")
		}
		var site *int64
		if target.SiteID.Valid {
			site = &target.SiteID.Int64
		}
		if err := authorize(txCtx, site, req); err != nil {
			return nil, err
		}
		routes, err := s.resolveDeviceCommandRoutes(txCtx, info.OrganizationID, []resolvedDevice{{id: target.ID, identifier: identifier}})
		if err != nil {
			return nil, err
		}
		for _, route := range routes {
			if route.FleetNodeID.Valid && s.protocolChecker != nil && s.protocolChecker.CommandProtocolUpgradeRequiredForVersion(route.FleetNodeID.Int64, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8) {
				return nil, fleeterror.NewFailedPreconditionError("container commands require an upgraded Fleet Node")
			}
		}
		pending, err := q.HasPendingContainerCommand(txCtx, sqlc.HasPendingContainerCommandParams{DeviceID: target.ID, CommandTypes: []string{"ContainerMining"}})
		if err != nil {
			return nil, err
		}
		if pending {
			return nil, fleeterror.NewFailedPreconditionError("a mining command is already pending for this container")
		}
		payload := containerMiningPayload{Request: req, OrgID: info.OrganizationID, SiteID: target.SiteID.Int64}
		return s.queueContainerCommand(txCtx, identifier, &Command{commandType: commandtype.ContainerMining, payload: payload}, "container_mining", "Container mining settings requested: "+req.Action)
	})
}

func (es *ExecutionService) executeContainerMining(ctx context.Context, deviceID, orgID int64, payload containerMiningPayload, controller sdk.DeviceContainerMining) error {
	if payload.OrgID <= 0 || payload.OrgID != orgID || payload.SiteID < 0 || payload.Request.Validate() != nil || payload.Request.ReadOnly() {
		return fleeterror.NewFailedPreconditionError("invalid container mining write or authorization scope")
	}
	return es.executeContainerWrite(ctx, containerWriteScope{
		deviceID: deviceID, orgID: orgID, siteID: payload.SiteID,
		noun: "container", upgradeMessage: "container commands require an upgraded Fleet Node",
	}, nil, func(ctx context.Context) error {
		result, err := controller.ContainerMining(ctx, payload.Request)
		if err == nil {
			err = result.Validate(payload.Request)
		}
		return err
	})
}

func (s *Service) ReadContainerMining(ctx context.Context, identifier string, req sdk.ContainerMiningRequest, authorize containerMiningAuthorization) (sdk.ContainerMiningResult, error) {
	// Include target lookup, observation admission, binding locks and the remote
	// acknowledgment in one server-owned budget; preserve shorter caller limits.
	ctx, cancel := context.WithTimeout(ctx, runtimepolicy.ContainerMiningReadTimeout)
	defer cancel()
	var result sdk.ContainerMiningResult
	if err := req.Validate(); err != nil {
		return result, fleeterror.NewInvalidArgumentError(err.Error())
	}
	if !req.ReadOnly() {
		return result, fleeterror.NewInvalidArgumentError("read operation required")
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
			return authorize(ctx, site, req)
		}, func(ctx context.Context) error {
			miner, err := s.executionService.minerService.GetMinerForCommand(ctx, target.ID)
			if err != nil {
				return err
			}
			controller, ok := miner.(sdk.DeviceContainerMining)
			if !ok {
				return fleeterror.NewUnimplementedError("container mining unsupported")
			}
			result, err = controller.ContainerMining(ctx, req)
			if err == nil {
				err = result.Validate(req)
			}
			return err
		})
	})
	return result, err
}
