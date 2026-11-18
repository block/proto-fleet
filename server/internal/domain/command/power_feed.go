package command

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	powerFeedUnsupportedMessage = "power feed inspection is not supported by this controller"
	powerFeedUpgradeMessage     = "power feed inspection requires Fleet Node command protocol version 13; upgrade Fleet Node before retrying"
)

// modulePowerFeedAuthorization authorizes the read at the container's current site.
type modulePowerFeedAuthorization func(context.Context, *int64) error

// ModulePowerFeed is one live read of the feed that supplies a module.
type ModulePowerFeed struct {
	ModuleID         int64
	DeviceIdentifier string
	ContainerName    string
	Tank, Module     int32
	Inspection       sdk.ModulePowerFeedInspection
	// Nil when the caller may not control feeds at the module's site.
	Control *PowerFeedControl
}

// ReadModulePowerFeed asks the module's controller for its mapped feed. The
// controller derives the mapping from its own map; Fleet passes only the
// module's placement and the identity it confirmed. canControl decides, at
// the same fresh site, whether the response offers feed controls.
func (s *Service) ReadModulePowerFeed(ctx context.Context, moduleID int64, authorize modulePowerFeedAuthorization, canControl modulePowerFeedPermission) (ModulePowerFeed, error) {
	ctx, cancel := context.WithTimeout(ctx, runtimepolicy.ContainerPowerFeedReadTimeout)
	defer cancel()
	var out ModulePowerFeed
	if moduleID <= 0 {
		return out, fleeterror.NewInvalidArgumentError("module ID must be positive")
	}
	if authorize == nil {
		return out, fleeterror.NewForbiddenError("module authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return out, err
	}
	q := sqlc.New(s.conn)
	rows, err := q.ListModuleCommandTargets(ctx, sqlc.ListModuleCommandTargetsParams{ModuleIds: []int64{moduleID}, OrgID: info.OrganizationID})
	if err != nil {
		return out, err
	}
	if len(rows) != 1 {
		return out, fleeterror.NewNotFoundError("module not found")
	}
	row := rows[0]
	var site *int64
	if row.SiteID.Valid {
		site = &row.SiteID.Int64
	}
	if err := authorize(ctx, site); err != nil {
		return out, err
	}
	req := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedInspect, Module: sdk.ContainerPowerFeedModule{Tank: row.TankPosition, Module: row.Position}}
	if row.IdentityConfirmed {
		req.Module.ReportedID = row.ReportedID
	}
	if err := req.Validate(); err != nil {
		return out, fleeterror.NewFailedPreconditionError("module placement unavailable")
	}
	if s.pluginCaps == nil || !s.pluginCaps.GetRawCapabilitiesForDevice(ctx, row.DriverName, row.Manufacturer.String, row.Model.String)[sdk.CapabilityContainerPowerFeed] {
		return out, fleeterror.NewFailedPreconditionError(powerFeedUnsupportedMessage)
	}
	upgrade, err := s.fleetNodeUpgradeRequired(ctx, info.OrganizationID, row.DeviceID, row.DeviceIdentifier, remotenode.PowerFeedInspectCommandProtocolVersion)
	if err != nil {
		return out, err
	}
	if upgrade {
		return out, fleeterror.NewFailedPreconditionError(powerFeedUpgradeMessage)
	}
	params := sqlc.GetContainerMiningTargetParams{DeviceIdentifier: row.DeviceIdentifier, OrgID: info.OrganizationID}
	target, err := q.GetContainerMiningTarget(ctx, params)
	if errors.Is(err, sql.ErrNoRows) {
		return out, fleeterror.NewNotFoundError("container not found")
	}
	if err != nil {
		return out, err
	}
	if target.ID != row.DeviceID {
		return out, fleeterror.NewFailedPreconditionError("container ownership changed")
	}
	var (
		result     sdk.ContainerPowerFeedResult
		mayControl bool
	)
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
			if err := authorize(ctx, site); err != nil {
				return err
			}
			mayControl = canControl != nil && canControl(ctx, site)
			return nil
		}, func(ctx context.Context) error {
			miner, err := s.executionService.minerService.GetMinerForCommand(ctx, target.ID)
			if err != nil {
				return err
			}
			controller, ok := miner.(sdk.DeviceContainerPowerFeed)
			if !ok {
				return fleeterror.NewFailedPreconditionError(powerFeedUnsupportedMessage)
			}
			result, err = controller.ContainerPowerFeed(ctx, req)
			if err == nil {
				err = result.Validate(req)
			}
			return err
		})
	})
	if err != nil {
		return out, err
	}
	out = ModulePowerFeed{ModuleID: row.ModuleID, DeviceIdentifier: row.DeviceIdentifier, ContainerName: row.ContainerName, Tank: row.TankPosition, Module: row.Position, Inspection: *result.Inspection}
	if mayControl {
		out.Control, err = s.powerFeedControl(ctx, q, info.OrganizationID, row, out.Inspection)
		if err != nil {
			// The read itself succeeded; only the controls are withheld.
			slog.WarnContext(ctx, "power feed control status unavailable", "device_id", row.DeviceID, "error", err)
			out.Control = &PowerFeedControl{UnavailableReason: powerFeedControlCheckFailedReason}
		}
	}
	return out, nil
}

// fleetNodeUpgradeRequired reports whether the controller sits behind a Fleet
// Node below the command protocol version an operation needs.
func (s *Service) fleetNodeUpgradeRequired(ctx context.Context, orgID, deviceID int64, identifier string, version gatewaypb.CommandProtocolVersion) (bool, error) {
	if s.protocolChecker == nil {
		return false, nil
	}
	routes, err := s.resolveDeviceCommandRoutes(ctx, orgID, []resolvedDevice{{id: deviceID, identifier: identifier}})
	if err != nil {
		return false, err
	}
	for _, route := range routes {
		if route.FleetNodeID.Valid && s.protocolChecker.CommandProtocolUpgradeRequiredForVersion(route.FleetNodeID.Int64, version) {
			return true, nil
		}
	}
	return false, nil
}
