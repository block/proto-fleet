package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const tankCommandMaxAge = 2 * time.Minute
const tankCommandClockSkew = 5 * time.Second

type tankMiningPayload struct {
	sdk.TankMiningRequest
	TankIDs []int64 `json:"tank_ids"`
	OrgID   int64   `json:"org_id"`
	SiteID  int64   `json:"site_id"`
}

type tankAuthorization func(context.Context, *int64, bool) error

func tankSite(row sqlc.ListTankCommandTargetsRow) *int64 {
	if row.SiteID.Valid {
		return &row.SiteID.Int64
	}
	return nil
}
func currentTankTarget(row sqlc.ListTankCommandTargetsRow, now time.Time) bool {
	for _, observed := range []time.Time{row.LastSeenAt, row.ObservedAt} {
		if observed.IsZero() || now.Sub(observed) > tankCommandMaxAge || observed.After(now.Add(tankCommandClockSkew)) {
			return false
		}
	}
	return true
}
func (s *Service) tankSupported(ctx context.Context, row sqlc.ListTankCommandTargetsRow) bool {
	return s.pluginCaps != nil && s.pluginCaps.GetRawCapabilitiesForDevice(ctx, row.DriverName, row.Manufacturer.String, row.Model.String)[sdk.CapabilityTankMining]
}
func tankIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, fleeterror.NewInvalidArgumentError("select 1–100 tanks")
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if ids[0] <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("tank IDs must be positive")
	}
	return ids, nil
}

func resolveTankCommandTargets(ctx context.Context, q sqlc.Querier, ids []int64, orgID int64) ([]sqlc.ListTankCommandTargetsRow, error) {
	rows, err := q.ListTankCommandTargets(ctx, sqlc.ListTankCommandTargetsParams{TankIds: ids, OrgID: orgID})
	if err != nil {
		return nil, err
	}
	if len(rows) != len(ids) {
		return nil, fleeterror.NewNotFoundError("selected tank unavailable")
	}
	return rows, nil
}

func (s *Service) GetTankMiningOptions(ctx context.Context, ids []int64, authorize tankAuthorization) (*containerpb.GetTankMiningOptionsResponse, error) {
	ids, err := tankIDs(ids)
	if err != nil {
		return nil, err
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (*containerpb.GetTankMiningOptionsResponse, error) {
		rows, err := resolveTankCommandTargets(ctx, q, ids, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		options := &containerpb.GetTankMiningOptionsResponse{CanStart: true, CanStop: true}
		for _, row := range rows {
			// A caller with neither action permission receives no command capabilities.
			start := authorize != nil && authorize(ctx, tankSite(row), true) == nil
			stop := authorize != nil && authorize(ctx, tankSite(row), false) == nil
			options.CanStart = options.CanStart && start
			options.CanStop = options.CanStop && stop
			if !start && !stop {
				return nil, fleeterror.NewForbiddenError("tank actions denied")
			}
			if !currentTankTarget(row, time.Now()) || !s.tankSupported(ctx, row) {
				options.CanStart = false
				options.CanStop = false
				options.UnavailableReason = "Tank controls unsupported or topology stale; refresh before retrying."
			}
			pending, err := q.HasPendingTankMining(ctx, row.DeviceID)
			if err != nil {
				return nil, err
			}
			if pending {
				options.CanStart = false
				options.CanStop = false
				options.UnavailableReason = "A tank mining command is already pending for this container."
			}
		}
		if !options.CanStart && !options.CanStop && options.UnavailableReason == "" {
			options.UnavailableReason = "No mining action is authorized for every selected tank."
		}
		return options, nil
	})
}

// SetTankMining atomically admits the exact selection. A controller gets one
// queue message, irrespective of its selected tanks' module count.
func (s *Service) SetTankMining(ctx context.Context, ids []int64, mining *bool, authorize tankAuthorization) (*containerpb.SetTankMiningResponse, error) {
	ids, err := tankIDs(ids)
	if err != nil {
		return nil, err
	}
	if mining == nil {
		return nil, fleeterror.NewInvalidArgumentError("mining state required")
	}
	if authorize == nil {
		return nil, fleeterror.NewForbiddenError("tank write authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	var commit func()
	result, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (*containerpb.SetTankMiningResponse, error) {
		txCtx, after := db.WithCommitHooks(db.WithTxQueries(ctx, q))
		commit = after
		rows, err := resolveTankCommandTargets(txCtx, q, ids, info.OrganizationID)
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
				return nil, fleeterror.NewNotFoundError("selected tank controller unavailable")
			}
			if err != nil {
				return nil, err
			}
		}
		// Re-resolve under the current controller ownership locks, including site.
		rows, err = resolveTankCommandTargets(txCtx, q, ids, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err := authorize(txCtx, tankSite(row), *mining); err != nil {
				return nil, err
			}
			if !currentTankTarget(row, time.Now()) || !s.tankSupported(txCtx, row) {
				return nil, fleeterror.NewFailedPreconditionError("tank controls unsupported or topology stale; refresh before retrying")
			}
			pending, err := q.HasPendingTankMining(txCtx, row.DeviceID)
			if err != nil {
				return nil, err
			}
			if pending {
				return nil, fleeterror.NewFailedPreconditionError("a tank mining command is already pending for this container")
			}
		}
		response := &containerpb.SetTankMiningResponse{}
		for i := 0; i < len(rows); {
			first := rows[i]
			payload := tankMiningPayload{
				TankMiningRequest: sdk.TankMiningRequest{Mining: mining},
				OrgID:             info.OrganizationID,
				SiteID:            first.SiteID.Int64,
			}
			for i < len(rows) && rows[i].DeviceID == first.DeviceID {
				payload.TankIDs = append(payload.TankIDs, rows[i].TankID)
				payload.Positions = append(payload.Positions, rows[i].Position)
				i++
			}
			if err := payload.Validate(); err != nil {
				return nil, fleeterror.NewFailedPreconditionError(err.Error())
			}
			batch, err := s.processCommand(txCtx, &Command{
				commandType: commandtype.SetTankMining,
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
			s.finalizeDispatch(txCtx, batch, "set_tank_mining", fmt.Sprintf("%s tanks %v: mining=%t", first.ContainerName, payload.Positions, *mining))
			response.Commands = append(response.Commands, &containerpb.TankMiningCommand{
				DeviceIdentifier: first.DeviceIdentifier,
				ContainerName:    first.ContainerName,
				TankIds:          payload.TankIDs,
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

func (es *ExecutionService) executeTankMining(ctx context.Context, deviceID, orgID, siteID int64, payload tankMiningPayload, controller sdk.DeviceTankMiner) error {
	if err := payload.Validate(); err != nil {
		return fleeterror.NewFailedPreconditionError(err.Error())
	}
	if controller == nil || payload.OrgID != orgID || payload.SiteID != siteID || len(payload.TankIDs) != len(payload.Positions) {
		return fleeterror.NewFailedPreconditionError("tank command ownership or site changed")
	}
	// Hold placement stable through gate admission and the physical write, which
	// must never be replayed by a transaction retry.
	err := db.WithTransactionNoRetryNoResult(ctx, es.conn, func(q sqlc.Querier) error {
		device, err := q.GetDeviceByID(ctx, sqlc.GetDeviceByIDParams{ID: deviceID, OrgID: orgID})
		if err != nil {
			return err
		}
		target, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{DeviceIdentifier: device.DeviceIdentifier, OrgID: orgID})
		if err != nil {
			return err
		}
		if target.ID != deviceID || target.SiteID.Int64 != siteID {
			return fmt.Errorf("tank command ownership or site changed")
		}
		rows, err := q.ListTankCommandTargets(ctx, sqlc.ListTankCommandTargetsParams{TankIds: payload.TankIDs, OrgID: orgID})
		if err != nil {
			return err
		}
		if len(rows) != len(payload.TankIDs) {
			return fmt.Errorf("selected tank no longer exists")
		}
		for i, row := range rows {
			if row.DeviceID != deviceID || row.SiteID.Int64 != siteID || row.TankID != payload.TankIDs[i] || row.Position != payload.Positions[i] || !currentTankTarget(row, time.Now()) {
				return fmt.Errorf("tank topology or placement changed; refresh before retrying")
			}
		}
		if err := controller.SetTankMining(ctx, payload.TankMiningRequest); err != nil {
			return fmt.Errorf("tank mining outcome unconfirmed: %w", err)
		}
		return nil
	})
	if err != nil {
		return fleeterror.NewFailedPreconditionError(err.Error())
	}
	return nil
}
