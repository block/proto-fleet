package command

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
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
	if row.Module != nil && row.Module.Validate() != nil {
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

func (s *Service) miningProtocolUpgradeRequired(ctx context.Context, q sqlc.Querier, rows []miningCommandTarget, orgID int64, scope miningScope) (bool, error) {
	if s.protocolChecker == nil {
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
		if route.FleetNodeID.Valid && s.protocolChecker.CommandProtocolUpgradeRequiredForVersion(route.FleetNodeID.Int64, scope.protocol()) {
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
				return nil, fleeterror.NewForbiddenError(scope.message("tank actions denied"))
			}
			if row.DeviceID != previous {
				previous = row.DeviceID
				supported = s.miningSupported(ctx, row, scope)
				pending, err = q.HasPendingScopedMining(ctx, row.DeviceID)
				if err != nil {
					return nil, err
				}
			}
			if !currentMiningTarget(row, time.Now()) || !supported {
				options.CanStart = false
				options.CanStop = false
				options.UnavailableReason = scope.message("Tank controls unsupported or topology stale; refresh before retrying.")
			}
			if pending {
				options.CanStart = false
				options.CanStop = false
				options.UnavailableReason = "A tank or module mining command is already pending for this container."
			}
		}
		for _, modules := range modulesByController {
			if (sdk.ModuleMiningRequest{Modules: modules, Mining: new(false)}).Validate() != nil {
				options.CanStart = false
				options.CanStop = false
				options.UnavailableReason = "Module identities or positions are invalid or ambiguous; refresh before retrying."
				break
			}
		}
		upgradeRequired, err := s.miningProtocolUpgradeRequired(ctx, q, rows, info.OrganizationID, scope)
		if err != nil {
			return nil, err
		}
		if upgradeRequired {
			options.CanStart = false
			options.CanStop = false
			options.UnavailableReason = scope.upgradeMessage()
		}
		if !options.CanStart && !options.CanStop && options.UnavailableReason == "" {
			options.UnavailableReason = scope.message("No mining action is authorized for every selected tank.")
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
		return nil, fleeterror.NewForbiddenError(scope.message("tank write authorization required"))
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	var commit func()
	result, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) ([]miningCommand, error) {
		txCtx, after := db.WithCommitHooks(db.WithTxQueries(ctx, q))
		commit = after
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
				return nil, fleeterror.NewNotFoundError(scope.message("selected tank controller unavailable"))
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
		previous = 0
		for _, row := range rows {
			if err := authorize(txCtx, miningSite(row), *mining); err != nil {
				return nil, err
			}
			if !currentMiningTarget(row, time.Now()) {
				return nil, fleeterror.NewFailedPreconditionError(scope.message("tank controls unsupported or topology stale; refresh before retrying"))
			}
			if row.DeviceID == previous {
				continue
			}
			previous = row.DeviceID
			if !s.miningSupported(txCtx, row, scope) {
				return nil, fleeterror.NewFailedPreconditionError(scope.message("tank controls unsupported or topology stale; refresh before retrying"))
			}
			pending, err := q.HasPendingScopedMining(txCtx, row.DeviceID)
			if err != nil {
				return nil, err
			}
			if pending {
				return nil, fleeterror.NewFailedPreconditionError("a tank or module mining command is already pending for this container")
			}
		}
		upgradeRequired, err := s.miningProtocolUpgradeRequired(txCtx, q, rows, info.OrganizationID, scope)
		if err != nil {
			return nil, err
		}
		if upgradeRequired {
			return nil, fleeterror.NewFailedPreconditionError(scope.upgradeMessage())
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
			batch, err := s.processCommand(txCtx, &Command{
				commandType: scope.commandType(),
				payload:     payload,
				deviceSelector: &pb.DeviceSelector{
					SelectionType: &pb.DeviceSelector_IncludeDevices{
						IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{first.DeviceIdentifier}},
					},
				},
			})
			if err != nil {
				return nil, err
			}
			s.finalizeDispatch(txCtx, batch, scope.eventType(), payload.activityDescription(first.ContainerName))
			response = append(response, miningCommand{
				DeviceIdentifier: first.DeviceIdentifier,
				ContainerName:    first.ContainerName,
				TargetIDs:        payload.ids(),
				BatchIdentifier:  batch.BatchIdentifier,
			})
		}
		return response, nil
	})
	if err == nil && commit != nil {
		commit()
	}
	return result, err
}

func (es *ExecutionService) executeTankMining(ctx context.Context, deviceID, orgID int64, payload scopedMiningPayload, controller sdk.DeviceTankMiner) error {
	return es.executeScopedMining(ctx, deviceID, orgID, payload, tankScope, func(ctx context.Context) error {
		if controller == nil {
			return fleeterror.NewFailedPreconditionError("tank mining unsupported")
		}
		return controller.SetTankMining(ctx, payload.TankMiningRequest)
	})
}
func (es *ExecutionService) executeScopedMining(ctx context.Context, deviceID, orgID int64, payload scopedMiningPayload, scope miningScope, write func(context.Context) error) error {
	if err := payload.Validate(); err != nil {
		return fleeterror.NewFailedPreconditionError(err.Error())
	}
	if payload.OrgID != orgID || (scope == moduleScope) != (len(payload.ModuleIDs) > 0) {
		return fleeterror.NewFailedPreconditionError(scope.message("tank command ownership or site changed"))
	}
	// Hold placement stable through gate admission and the physical write, which
	// must never be replayed by a transaction retry.
	dispatchStarted := false
	err := devicewrite.NewService(es.conn).WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		device, err := q.GetDeviceByID(ctx, sqlc.GetDeviceByIDParams{ID: deviceID, OrgID: orgID})
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewFailedPreconditionError(scope.message("selected tank controller unavailable"))
		}
		if err != nil {
			return err
		}
		target, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{DeviceIdentifier: device.DeviceIdentifier, OrgID: orgID})
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewFailedPreconditionError(scope.message("selected tank controller unavailable"))
		}
		if err != nil {
			return err
		}
		if target.ID != deviceID || target.SiteID.Int64 != payload.SiteID {
			return fleeterror.NewFailedPreconditionError(scope.message("tank command ownership or site changed"))
		}
		rows, err := scope.resolve(ctx, q, payload.ids(), orgID)
		if fleeterror.IsNotFoundError(err) {
			return fleeterror.NewFailedPreconditionError(scope.message("selected tank no longer exists"))
		}
		if err != nil {
			return err
		}
		if len(rows) != len(payload.ids()) {
			return fleeterror.NewFailedPreconditionError(scope.message("selected tank no longer exists"))
		}
		for i, row := range rows {
			if row.DeviceID != deviceID || row.SiteID.Int64 != payload.SiteID || row.ID != payload.ids()[i] || !payload.matches(row, i) || !currentMiningTarget(row, time.Now()) {
				return fleeterror.NewFailedPreconditionError(scope.message("tank topology or placement changed; refresh before retrying"))
			}
		}
		dispatchStarted = true
		if err := devicewrite.Dispatch(ctx, func(ctx context.Context) error {
			return write(ctx)
		}); err != nil {
			if errors.Is(err, control.ErrCommandProtocolUpgradeRequired) {
				return devicewrite.NotDispatched(fleeterror.NewFailedPreconditionError(scope.upgradeMessage()))
			}
			if devicewrite.IsNotDispatched(err) {
				return err
			}
			slog.ErrorContext(ctx, scope.message("tank mining outcome unconfirmed"), "device_id", deviceID, "error", err)
			return fleeterror.NewFailedPreconditionError(scope.message("tank mining outcome unconfirmed"))
		}
		return nil
	})
	if err != nil {
		if fleeterror.IsFailedPreconditionError(err) {
			return err
		}
		if dispatchStarted {
			slog.ErrorContext(ctx, scope.message("tank mining outcome unconfirmed"), "device_id", deviceID, "error", err)
			return fleeterror.NewFailedPreconditionError(scope.message("tank mining outcome unconfirmed"))
		}
		slog.ErrorContext(ctx, scope.message("tank mining preparation failed"), "device_id", deviceID, "error", err)
		return fleeterror.NewInternalError(scope.message("tank mining preparation failed; retry shortly"))
	}
	return nil
}
