package command

import (
	"context"
	"database/sql"
	"errors"
	"time"

	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const tankCommandMaxAge = 2 * time.Minute
const tankCommandClockSkew = 5 * time.Second

type scopedMiningPayload struct {
	sdk.TankMiningRequest
	TankIDs   []int64                  `json:"tank_ids"`
	OrgID     int64                    `json:"org_id"`
	SiteID    int64                    `json:"site_id"`
	ModuleIDs []int64                  `json:"module_ids,omitempty"`
	Modules   []sdk.ModuleMiningTarget `json:"modules,omitempty"`
}

type miningAuthorization func(context.Context, *int64, bool) error

func miningSite(row miningCommandTarget) *int64 {
	if row.SiteID.Valid {
		return &row.SiteID.Int64
	}
	return nil
}
func currentMiningTarget(row miningCommandTarget, now time.Time) bool {
	if !row.identityConfirmed() {
		return false
	}
	for _, observed := range []time.Time{row.LastSeenAt, row.ObservedAt} {
		if observed.IsZero() || now.Sub(observed) > tankCommandMaxAge || observed.After(now.Add(tankCommandClockSkew)) {
			return false
		}
	}
	return true
}
func (s *Service) miningSupported(ctx context.Context, row miningCommandTarget, scope miningScope) bool {
	return s.pluginCaps != nil && s.pluginCaps.GetRawCapabilitiesForDevice(ctx, row.DriverName, row.Manufacturer.String, row.Model.String)[scope.capability()]
}
func resolveTankCommandTargets(ctx context.Context, q sqlc.Querier, ids []int64, orgID int64) ([]miningCommandTarget, error) {
	rows, err := q.ListTankCommandTargets(ctx, sqlc.ListTankCommandTargetsParams{TankIds: ids, OrgID: orgID})
	if err != nil {
		return nil, err
	}
	if len(rows) != len(ids) {
		return nil, fleeterror.NewNotFoundError("selected tank unavailable")
	}
	targets := make([]miningCommandTarget, len(rows))
	for i, row := range rows {
		targets[i] = miningCommandTarget{ID: row.TankID, DeviceID: row.DeviceID, Position: row.Position, LastSeenAt: row.LastSeenAt, ObservedAt: row.ObservedAt, DeviceIdentifier: row.DeviceIdentifier, ContainerName: row.ContainerName, DriverName: row.DriverName, SiteID: row.SiteID, Manufacturer: row.Manufacturer, Model: row.Model}
	}
	return targets, nil
}

func (s *Service) miningProtocolUpgradeRequired(ctx context.Context, q sqlc.Querier, rows []miningCommandTarget, orgID int64, scope miningScope, forDispatch bool) (bool, error) {
	upgradeRequired := s.protocolUpgradeCheck(forDispatch)
	if upgradeRequired == nil {
		return false, nil
	}
	devices := make([]resolvedDevice, 0, len(rows))
	for _, row := range rows {
		if len(devices) == 0 || devices[len(devices)-1].id != row.DeviceID {
			devices = append(devices, resolvedDevice{id: row.DeviceID, identifier: row.DeviceIdentifier})
		}
	}
	routes, err := s.resolveDeviceCommandRoutes(db.WithTxQueries(ctx, q), orgID, devices)
	if err != nil {
		return false, err
	}
	for _, route := range routes {
		if route.FleetNodeID.Valid && upgradeRequired(route.FleetNodeID.Int64, scope.protocol()) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) GetTankMiningOptions(ctx context.Context, ids []int64, authorize miningAuthorization) (*containerpb.GetTankMiningOptionsResponse, error) {
	r, err := s.getMiningOptions(ctx, ids, authorize, tankScope)
	if err != nil {
		return nil, err
	}
	return &containerpb.GetTankMiningOptionsResponse{CanStart: r.CanStart, CanStop: r.CanStop, UnavailableReason: r.UnavailableReason}, nil
}
func (s *Service) getMiningOptions(ctx context.Context, ids []int64, authorize miningAuthorization, scope miningScope) (*miningOptions, error) {
	ids, err := scope.ids(ids)
	if err != nil {
		return nil, err
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (*miningOptions, error) {
		rows, err := scope.resolve(ctx, q, ids, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		options := &miningOptions{CanStart: true, CanStop: true}
		modulesByController := make(map[int64][]sdk.ModuleMiningTarget)
		var previous int64
		var supported, pending bool
		var unsupported, anyPending bool
		targetReason := ""
		for _, row := range rows {
			if row.Module != nil {
				modulesByController[row.DeviceID] = append(modulesByController[row.DeviceID], *row.Module)
			}
			// A caller with neither action permission receives no command capabilities.
			start := authorize != nil && authorize(ctx, miningSite(row), true) == nil
			stop := authorize != nil && authorize(ctx, miningSite(row), false) == nil
			options.CanStart = options.CanStart && start
			options.CanStop = options.CanStop && stop
			if !start && !stop {
				return nil, fleeterror.NewForbiddenError(scope.noun() + " actions denied")
			}
			if row.DeviceID != previous {
				previous = row.DeviceID
				supported = s.miningSupported(ctx, row, scope)
				pending, err = q.HasPendingContainerCommand(ctx, sqlc.HasPendingContainerCommandParams{DeviceID: row.DeviceID, CommandTypes: []string{"SetTankMining", "SetModuleMining"}})
				if err != nil {
					return nil, err
				}
			}
			unsupported = unsupported || !supported
			anyPending = anyPending || pending
			if targetReason == "" && !currentMiningTarget(row, time.Now()) {
				targetReason = scope.targetUnavailableReason(row)
			}
		}
		for _, modules := range modulesByController {
			if (sdk.ModuleMiningRequest{Modules: modules, Mining: new(false)}).Validate() != nil {
				targetReason = moduleIdentityUnavailableReason
				break
			}
		}
		upgradeRequired, err := s.miningProtocolUpgradeRequired(ctx, q, rows, info.OrganizationID, scope, false)
		if err != nil {
			return nil, err
		}
		switch {
		case upgradeRequired:
			options.UnavailableReason = scope.upgradeMessage()
		case anyPending:
			options.UnavailableReason = "A tank or module mining command is already pending for this container."
		case unsupported:
			options.UnavailableReason = scope.controlsUnsupportedReason()
		default:
			options.UnavailableReason = targetReason
		}
		if options.UnavailableReason != "" {
			options.CanStart = false
			options.CanStop = false
		}
		if !options.CanStart && !options.CanStop && options.UnavailableReason == "" {
			options.UnavailableReason = "No mining action is authorized for every selected " + scope.noun() + "."
		}
		return options, nil
	})
}

// SetTankMining atomically admits the exact selection. A controller gets one
// queue message, irrespective of its selected tanks' module count.
func (s *Service) SetTankMining(ctx context.Context, ids []int64, mining *bool, authorize miningAuthorization) (*containerpb.SetTankMiningResponse, error) {
	commands, err := s.setMining(ctx, ids, mining, authorize, tankScope)
	if err != nil {
		return nil, err
	}
	result := &containerpb.SetTankMiningResponse{}
	for _, c := range commands {
		result.Commands = append(result.Commands, &containerpb.TankMiningCommand{DeviceIdentifier: c.DeviceIdentifier, ContainerName: c.ContainerName, TankIds: c.TargetIDs, BatchIdentifier: c.BatchIdentifier})
	}
	return result, nil
}
func (s *Service) setMining(ctx context.Context, ids []int64, mining *bool, authorize miningAuthorization, scope miningScope) ([]miningCommand, error) {
	ids, err := scope.ids(ids)
	if err != nil {
		return nil, err
	}
	if mining == nil {
		return nil, fleeterror.NewInvalidArgumentError("mining state required")
	}
	if authorize == nil {
		return nil, fleeterror.NewForbiddenError(scope.noun() + " write authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return withContainerCommandTransaction(ctx, s, func(txCtx context.Context, q sqlc.Querier) ([]miningCommand, error) {
		rows, err := scope.resolve(txCtx, q, ids, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		previous := int64(0)
		for _, row := range rows {
			if row.DeviceID == previous {
				continue
			}
			previous = row.DeviceID
			_, err := q.LockContainerCommandTarget(txCtx, sqlc.LockContainerCommandTargetParams{DeviceIdentifier: row.DeviceIdentifier, OrgID: info.OrganizationID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fleeterror.NewNotFoundError("selected " + scope.noun() + " controller unavailable")
			}
			if err != nil {
				return nil, err
			}
		}
		// Re-resolve under the current controller ownership locks, including site.
		rows, err = scope.resolve(txCtx, q, ids, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err := authorize(txCtx, miningSite(row), *mining); err != nil {
				return nil, err
			}
		}
		upgradeRequired, err := s.miningProtocolUpgradeRequired(txCtx, q, rows, info.OrganizationID, scope, true)
		if err != nil {
			return nil, err
		}
		if upgradeRequired {
			return nil, fleeterror.NewFailedPreconditionError(scope.upgradeMessage())
		}
		previous = 0
		unsupported := false
		for _, row := range rows {
			if row.DeviceID == previous {
				continue
			}
			previous = row.DeviceID
			pending, err := q.HasPendingContainerCommand(txCtx, sqlc.HasPendingContainerCommandParams{DeviceID: row.DeviceID, CommandTypes: []string{"SetTankMining", "SetModuleMining"}})
			if err != nil {
				return nil, err
			}
			if pending {
				return nil, fleeterror.NewFailedPreconditionError("a tank or module mining command is already pending for this container")
			}
			unsupported = unsupported || !s.miningSupported(txCtx, row, scope)
		}
		if unsupported {
			return nil, fleeterror.NewFailedPreconditionError(scope.controlsUnsupportedReason())
		}
		for _, row := range rows {
			if !currentMiningTarget(row, time.Now()) {
				return nil, fleeterror.NewFailedPreconditionError(scope.targetUnavailableReason(row))
			}
		}
		response := []miningCommand{}
		for i := 0; i < len(rows); {
			first := rows[i]
			payload := scopedMiningPayload{
				TankMiningRequest: sdk.TankMiningRequest{Mining: mining},
				OrgID:             info.OrganizationID,
				SiteID:            first.SiteID.Int64,
			}
			for i < len(rows) && rows[i].DeviceID == first.DeviceID {
				if scope == moduleScope {
					payload.ModuleIDs = append(payload.ModuleIDs, rows[i].ID)
					payload.Modules = append(payload.Modules, *rows[i].Module)
				} else {
					payload.TankIDs = append(payload.TankIDs, rows[i].ID)
					payload.Positions = append(payload.Positions, rows[i].Position)
				}
				i++
			}
			if err := payload.Validate(); err != nil {
				return nil, fleeterror.NewFailedPreconditionError(err.Error())
			}
			batch, err := s.queueContainerCommand(txCtx, first.DeviceIdentifier, &Command{
				commandType: scope.commandType(), payload: payload,
			}, scope.eventType(), payload.activityDescription(first.ContainerName))
			if err != nil {
				return nil, err
			}
			response = append(response, miningCommand{
				DeviceIdentifier: first.DeviceIdentifier,
				ContainerName:    first.ContainerName,
				TargetIDs:        payload.ids(),
				BatchIdentifier:  batch.BatchIdentifier,
			})
		}
		return response, nil
	})
}

func (es *ExecutionService) executeTankMining(ctx context.Context, deviceID, orgID int64, payload scopedMiningPayload, controller sdk.DeviceTankMiner) error {
	return es.executeScopedMining(ctx, deviceID, orgID, payload, tankScope, func(ctx context.Context) error {
		return controller.SetTankMining(ctx, payload.TankMiningRequest)
	})
}
func (es *ExecutionService) executeScopedMining(ctx context.Context, deviceID, orgID int64, payload scopedMiningPayload, scope miningScope, write func(context.Context) error) error {
	if err := payload.Validate(); err != nil {
		return fleeterror.NewFailedPreconditionError(err.Error())
	}
	if payload.OrgID != orgID || (scope == moduleScope) != (len(payload.ModuleIDs) > 0) {
		return fleeterror.NewFailedPreconditionError(scope.noun() + " command ownership or site changed")
	}
	return es.executeContainerWrite(ctx, containerWriteScope{
		deviceID: deviceID, orgID: orgID, siteID: payload.SiteID,
		noun: scope.noun(), upgradeMessage: scope.upgradeMessage(),
	}, func(ctx context.Context, q sqlc.Querier) error {
		rows, err := scope.resolve(ctx, q, payload.ids(), orgID)
		if fleeterror.IsNotFoundError(err) {
			return fleeterror.NewFailedPreconditionError("selected " + scope.noun() + " no longer exists")
		}
		if err != nil {
			return err
		}
		if len(rows) != len(payload.ids()) {
			return fleeterror.NewFailedPreconditionError("selected " + scope.noun() + " no longer exists")
		}
		for i, row := range rows {
			if !currentMiningTarget(row, time.Now()) {
				return fleeterror.NewFailedPreconditionError(scope.targetUnavailableReason(row))
			}
			if row.DeviceID != deviceID || row.SiteID.Int64 != payload.SiteID || row.ID != payload.ids()[i] || !payload.matches(row, i) {
				return fleeterror.NewFailedPreconditionError(scope.noun() + " topology or placement changed; refresh before retrying")
			}
		}
		return nil
	}, write)
}
