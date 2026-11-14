package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	boardIdentifyMaxBoards  = 30
	boardIdentifyMaxModules = 10

	boardIdentityUnavailableReason   = "Board identity is unavailable or stale. Refresh the module and check its controller connection before retrying."
	boardIdentifyUnsupportedReason   = "Board identification is not supported by this controller."
	boardIdentifyPendingReason       = "A board identify command is already pending for this container."
	boardIdentifyUpgradeMessage      = "board identification requires Fleet Node command protocol version 10; upgrade Fleet Node before retrying"
	boardIdentifyChangedSinceConfirm = "A selected board moved or was replaced after you confirmed it. Review the boards and try again."
)

// boardIdentifyAuthorization authorizes identify for one board at its current site.
type boardIdentifyAuthorization func(context.Context, *int64) error

// BoardIdentifyTarget is a board the operator confirmed, pinned to the
// installation they saw. A move or replacement changes the installation.
type BoardIdentifyTarget struct {
	HashboardID, InstallationID int64
}

type boardIdentifyPayload struct {
	sdk.BoardIdentifyRequest
	OrgID          int64 `json:"org_id"`
	SiteID         int64 `json:"site_id"`
	HashboardID    int64 `json:"hashboard_id"`
	InstallationID int64 `json:"installation_id"`
}

func boardIdentifySite(row sqlc.ListBoardIdentifyTargetsRow) *int64 {
	if row.SiteID.Valid {
		return &row.SiteID.Int64
	}
	return nil
}

func boardIdentifyRequest(row sqlc.ListBoardIdentifyTargetsRow, mode sdk.BoardIdentifyMode) sdk.BoardIdentifyRequest {
	mac := ""
	if len(row.MacAddresses) == 1 {
		mac = row.MacAddresses[0]
	}
	return sdk.BoardIdentifyRequest{Mode: mode, Board: sdk.BoardIdentifyTarget{
		Tank: row.TankPosition, Module: row.ModulePosition, Slot: row.Slot,
		ModuleReportedID: strings.TrimSpace(row.ReportedID), MACAddress: mac,
	}}
}

// boardIdentifyReady requires a confirmed module identity, exactly one board
// MAC, a slot observation that still names this board, and fresh telemetry.
func boardIdentifyReady(row sqlc.ListBoardIdentifyTargetsRow, now time.Time) bool {
	if !row.IdentityConfirmed || !row.SlotMatches || row.Occupancy.String != "occupied" || row.UnavailableReason.String != "" {
		return false
	}
	if boardIdentifyRequest(row, sdk.BoardIdentifyOff).Validate() != nil {
		return false
	}
	for _, observed := range []time.Time{row.ModuleSeenAt, row.ObservedAt, row.SlotObservedAt.Time} {
		if observed.IsZero() || now.Sub(observed) > tankCommandMaxAge || observed.After(now.Add(tankCommandClockSkew)) {
			return false
		}
	}
	return true
}

func positiveUniqueIDs(ids []int64, limit int, noun string) ([]int64, error) {
	if len(ids) > limit {
		return nil, fleeterror.NewInvalidArgumentError(fmt.Sprintf("select at most %d %ss", limit, noun))
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > 0 && ids[0] <= 0 {
		return nil, fleeterror.NewInvalidArgumentError(noun + " IDs must be positive")
	}
	return ids, nil
}

// resolveBoardIdentifyTargets expands the selection into current boards and
// fails unless every selected board is installed and every module has one.
func resolveBoardIdentifyTargets(ctx context.Context, q sqlc.Querier, orgID int64, moduleIDs, hashboardIDs []int64) ([]sqlc.ListBoardIdentifyTargetsRow, error) {
	rows, err := q.ListBoardIdentifyTargets(ctx, sqlc.ListBoardIdentifyTargetsParams{HashboardIds: append([]int64{}, hashboardIDs...), ModuleIds: append([]int64{}, moduleIDs...), OrgID: orgID})
	if err != nil {
		return nil, err
	}
	boards := map[int64]bool{}
	modules := map[int64]bool{}
	for _, row := range rows {
		boards[row.HashboardID] = true
		modules[row.ModuleID] = true
	}
	for _, id := range hashboardIDs {
		if !boards[id] {
			return nil, fleeterror.NewNotFoundError("selected board is not currently installed")
		}
	}
	for _, id := range moduleIDs {
		if !modules[id] {
			return nil, fleeterror.NewNotFoundError("selected module has no installed boards")
		}
	}
	if len(rows) > boardIdentifyMaxBoards {
		return nil, fleeterror.NewInvalidArgumentError(fmt.Sprintf("selection expands to %d boards; select at most %d", len(rows), boardIdentifyMaxBoards))
	}
	return rows, nil
}

func (s *Service) boardIdentifySupported(ctx context.Context, row sqlc.ListBoardIdentifyTargetsRow) bool {
	return s.pluginCaps != nil && s.pluginCaps.GetRawCapabilitiesForDevice(ctx, row.DriverName, row.Manufacturer.String, row.Model.String)[sdk.CapabilityBoardIdentify]
}

func (s *Service) boardIdentifyUpgradeRequired(ctx context.Context, q sqlc.Querier, rows []sqlc.ListBoardIdentifyTargetsRow, orgID int64) (bool, error) {
	if s.protocolChecker == nil {
		return false, nil
	}
	devices := []resolvedDevice{}
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
		if route.FleetNodeID.Valid && s.protocolChecker.CommandProtocolUpgradeRequiredForVersion(route.FleetNodeID.Int64, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V10) {
			return true, nil
		}
	}
	return false, nil
}

// boardIdentifyUnavailable returns the first reason the boards cannot be
// identified now, or "" when every board is ready.
func (s *Service) boardIdentifyUnavailable(ctx context.Context, q sqlc.Querier, rows []sqlc.ListBoardIdentifyTargetsRow, orgID int64) (string, error) {
	upgrade, err := s.boardIdentifyUpgradeRequired(ctx, q, rows, orgID)
	if err != nil {
		return "", err
	}
	if upgrade {
		return boardIdentifyUpgradeMessage, nil
	}
	previous := int64(0)
	for _, row := range rows {
		if row.DeviceID == previous {
			continue
		}
		previous = row.DeviceID
		pending, err := q.HasPendingContainerCommand(ctx, sqlc.HasPendingContainerCommandParams{DeviceID: row.DeviceID, CommandTypes: []string{"SetBoardIdentify"}})
		if err != nil {
			return "", err
		}
		if pending {
			return boardIdentifyPendingReason, nil
		}
		if !s.boardIdentifySupported(ctx, row) {
			return boardIdentifyUnsupportedReason, nil
		}
	}
	now := time.Now()
	for _, row := range rows {
		if !boardIdentifyReady(row, now) {
			return boardIdentityUnavailableReason, nil
		}
	}
	return "", nil
}

// GetBoardIdentifyOptions expands the selection into the boards an identify
// command would address, so the operator can confirm them before dispatch.
func (s *Service) GetBoardIdentifyOptions(ctx context.Context, moduleIDs, hashboardIDs []int64, authorize boardIdentifyAuthorization) (*containerpb.GetBoardIdentifyOptionsResponse, error) {
	moduleIDs, err := positiveUniqueIDs(moduleIDs, boardIdentifyMaxModules, "module")
	if err != nil {
		return nil, err
	}
	hashboardIDs, err = positiveUniqueIDs(hashboardIDs, boardIdentifyMaxBoards, "board")
	if err != nil {
		return nil, err
	}
	if len(moduleIDs)+len(hashboardIDs) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("select at least one module or board")
	}
	if authorize == nil {
		return nil, fleeterror.NewForbiddenError("board identify authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (*containerpb.GetBoardIdentifyOptionsResponse, error) {
		rows, err := resolveBoardIdentifyTargets(ctx, q, info.OrganizationID, moduleIDs, hashboardIDs)
		if err != nil {
			return nil, err
		}
		out := &containerpb.GetBoardIdentifyOptionsResponse{}
		for _, row := range rows {
			if err := authorize(ctx, boardIdentifySite(row)); err != nil {
				return nil, err
			}
			out.Boards = append(out.Boards, &containerpb.IdentifyBoard{
				HashboardId: row.HashboardID, InstallationId: row.InstallationID, ModuleId: row.ModuleID,
				DeviceIdentifier: row.DeviceIdentifier, ContainerName: row.ContainerName,
				Tank: row.TankPosition, Module: row.ModulePosition, Slot: row.Slot, SerialNumber: row.SerialNumber,
			})
		}
		out.UnavailableReason, err = s.boardIdentifyUnavailable(ctx, q, rows, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		out.CanIdentify = out.UnavailableReason == ""
		return out, nil
	})
}

// SetBoardIdentify queues one command per confirmed board, so each board
// reports its own controller outcome. The confirmed set must still match the
// current installations exactly; nothing is queued otherwise.
func (s *Service) SetBoardIdentify(ctx context.Context, targets []BoardIdentifyTarget, mode sdk.BoardIdentifyMode, authorize boardIdentifyAuthorization) (*containerpb.SetBoardIdentifyResponse, error) {
	if mode != sdk.BoardIdentifyBlink && mode != sdk.BoardIdentifyOff {
		return nil, fleeterror.NewInvalidArgumentError("identify mode must be blink or off")
	}
	if len(targets) == 0 || len(targets) > boardIdentifyMaxBoards {
		return nil, fleeterror.NewInvalidArgumentError(fmt.Sprintf("confirm 1–%d boards", boardIdentifyMaxBoards))
	}
	expected := map[int64]int64{}
	ids := make([]int64, 0, len(targets))
	for _, target := range targets {
		if target.HashboardID <= 0 || target.InstallationID <= 0 || expected[target.HashboardID] != 0 {
			return nil, fleeterror.NewInvalidArgumentError("confirmed boards must be unique with known installations")
		}
		expected[target.HashboardID] = target.InstallationID
		ids = append(ids, target.HashboardID)
	}
	if authorize == nil {
		return nil, fleeterror.NewForbiddenError("board identify authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return withContainerCommandTransaction(ctx, s, func(txCtx context.Context, q sqlc.Querier) (*containerpb.SetBoardIdentifyResponse, error) {
		rows, err := resolveBoardIdentifyTargets(txCtx, q, info.OrganizationID, nil, ids)
		if fleeterror.IsNotFoundError(err) {
			return nil, fleeterror.NewFailedPreconditionError(boardIdentifyChangedSinceConfirm)
		}
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
				return nil, fleeterror.NewNotFoundError("selected board controller unavailable")
			}
			if err != nil {
				return nil, err
			}
		}
		// Re-resolve under the controller ownership locks, including site.
		rows, err = resolveBoardIdentifyTargets(txCtx, q, info.OrganizationID, nil, ids)
		if fleeterror.IsNotFoundError(err) {
			return nil, fleeterror.NewFailedPreconditionError(boardIdentifyChangedSinceConfirm)
		}
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.InstallationID != expected[row.HashboardID] {
				return nil, fleeterror.NewFailedPreconditionError(boardIdentifyChangedSinceConfirm)
			}
			if err := authorize(txCtx, boardIdentifySite(row)); err != nil {
				return nil, err
			}
		}
		reason, err := s.boardIdentifyUnavailable(txCtx, q, rows, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			return nil, fleeterror.NewFailedPreconditionError(reason)
		}
		// One queue message per board keeps each board's outcome exact, since
		// MDK reports only an aggregate update count. The queue runs one message
		// per controller at a time, so a start/stop queued after a large identify
		// waits behind every board's message on that controller (accepted; the
		// board cap bounds it).
		response := &containerpb.SetBoardIdentifyResponse{}
		for _, row := range rows {
			payload := boardIdentifyPayload{
				BoardIdentifyRequest: boardIdentifyRequest(row, mode),
				OrgID:                info.OrganizationID, SiteID: row.SiteID.Int64,
				HashboardID: row.HashboardID, InstallationID: row.InstallationID,
			}
			description := fmt.Sprintf("%s tank %d/module %d/slot %d: identify=%s", row.ContainerName, row.TankPosition, row.ModulePosition, row.Slot, mode)
			batch, err := s.queueContainerCommand(txCtx, row.DeviceIdentifier, &Command{commandType: commandtype.SetBoardIdentify, payload: payload}, "set_board_identify", description)
			if err != nil {
				return nil, err
			}
			response.Commands = append(response.Commands, &containerpb.BoardIdentifyCommand{HashboardId: row.HashboardID, DeviceIdentifier: row.DeviceIdentifier, BatchIdentifier: batch.BatchIdentifier})
		}
		return response, nil
	})
}

func (es *ExecutionService) executeBoardIdentify(ctx context.Context, deviceID, orgID int64, payload boardIdentifyPayload, controller sdk.DeviceBoardIdentifier) error {
	if err := payload.Validate(); err != nil || payload.HashboardID <= 0 || payload.InstallationID <= 0 {
		return fleeterror.NewFailedPreconditionError("invalid board identify payload")
	}
	if payload.OrgID != orgID {
		return fleeterror.NewFailedPreconditionError("board command ownership or site changed")
	}
	return es.executeContainerWrite(ctx, containerWriteScope{
		deviceID: deviceID, orgID: orgID, siteID: payload.SiteID,
		noun: "board", action: "identify", upgradeMessage: boardIdentifyUpgradeMessage,
	}, func(ctx context.Context, q sqlc.Querier) error {
		rows, err := resolveBoardIdentifyTargets(ctx, q, orgID, nil, []int64{payload.HashboardID})
		if fleeterror.IsNotFoundError(err) {
			return fleeterror.NewFailedPreconditionError("selected board is no longer installed")
		}
		if err != nil {
			return err
		}
		row := rows[0]
		if row.InstallationID != payload.InstallationID || row.DeviceID != deviceID || row.SiteID.Int64 != payload.SiteID ||
			boardIdentifyRequest(row, payload.Mode) != payload.BoardIdentifyRequest {
			return fleeterror.NewFailedPreconditionError("board moved or was replaced; refresh before retrying")
		}
		if !boardIdentifyReady(row, time.Now()) {
			return fleeterror.NewFailedPreconditionError(boardIdentityUnavailableReason)
		}
		return nil
	}, func(ctx context.Context) error {
		return controller.SetBoardIdentify(ctx, payload.BoardIdentifyRequest)
	})
}
