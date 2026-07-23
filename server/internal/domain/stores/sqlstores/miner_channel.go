package sqlstores

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sqlc-dev/pqtype"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const (
	minerChannelMembershipUniqueConstraint = "uq_miner_channel_membership_one_per_device"
	minerChannelActiveLabelUniqueIndex     = "uq_miner_channel_active_label_per_org"
	minerChannelIdempotencyUniqueIndex     = "uq_miner_channel_idempotency"
	maxDefaultMinerChannelDeviceListLimit  = int32(2147483647)
	defaultMinerChannelPageSize            = int32(50)
	maxMinerChannelPageSize                = int32(500)
)

var _ interfaces.MinerChannelStore = &SQLMinerChannelStore{}
var _ interfaces.MinerChannelFirmwareEnforcementStore = &SQLMinerChannelStore{}

type SQLMinerChannelStore struct {
	SQLConnectionManager
}

type minerChannelPageCursor struct {
	IsDefault bool      `json:"is_default,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	ID        int64     `json:"id"`
}

type minerChannelDevicePageCursor struct {
	DisplayName      string `json:"display_name"`
	DeviceIdentifier string `json:"device_identifier"`
}

func NewSQLMinerChannelStore(conn *sql.DB) *SQLMinerChannelStore {
	return &SQLMinerChannelStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLMinerChannelStore) CreateMinerChannel(ctx context.Context, params models.CreateMinerChannelParams) (*models.MinerChannel, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.MinerChannel, error) {
		row, err := q.CreateMinerChannel(ctx, sqlc.CreateMinerChannelParams{
			OrgID:              params.OrgID,
			Label:              params.Label,
			OwnerUserID:        ptrToNullInt64(params.OwnerUserID),
			OwnerUsername:      ptrToNullString(params.OwnerUsername),
			ExpiresAt:          ptrToNullTime(params.ExpiresAt),
			DesiredConfigJsonb: rawMessageToNull(params.DesiredConfigJSON),
			Purpose:            params.Purpose,
			SourceActorType:    string(params.SourceActorType),
			SourceActorID:      ptrToNullString(params.SourceActorID),
			IdempotencyKey:     ptrToNullString(params.IdempotencyKey),
		})
		if err != nil {
			return nil, mapMinerChannelInsertError(err)
		}
		switch {
		case params.DeviceSelector != nil:
			payload, selectedCount, err := s.buildSelectedMinerChannelMemberPayload(ctx, q, params)
			if err != nil {
				return nil, err
			}
			inserted, err := q.BulkInsertMinerChannelMemberships(ctx, sqlc.BulkInsertMinerChannelMembershipsParams{
				MinerChannelID: row.ID,
				OrgID:          row.OrgID,
				MembersJsonb:   payload,
			})
			if err != nil {
				return nil, mapMinerChannelMembershipError(err)
			}
			if inserted != selectedCount {
				return nil, fleeterror.NewInternalErrorf("bulk insert wrote %d miner channel members, expected %d", inserted, selectedCount)
			}
		case len(params.DeviceIdentifiers) > 0:
			payload, err := s.buildMinerChannelMemberPayloadForIdentifiers(ctx, q, row.OrgID, params.DeviceIdentifiers)
			if err != nil {
				return nil, err
			}
			inserted, err := q.BulkInsertMinerChannelMemberships(ctx, sqlc.BulkInsertMinerChannelMembershipsParams{
				MinerChannelID: row.ID,
				OrgID:          row.OrgID,
				MembersJsonb:   payload,
			})
			if err != nil {
				return nil, mapMinerChannelMembershipError(err)
			}
			if inserted != int64(len(params.DeviceIdentifiers)) {
				return nil, fleeterror.NewInternalErrorf("bulk insert wrote %d miner channel members, expected %d", inserted, len(params.DeviceIdentifiers))
			}
		}
		minerChannel, err := s.getMinerChannelWithQueries(ctx, q, row.OrgID, row.ID)
		if err != nil {
			return nil, err
		}
		if err := validateMinerChannelSingleMinerType(minerChannel); err != nil {
			return nil, err
		}
		return minerChannel, nil
	})
}

func validateMinerChannelSingleMinerType(minerChannel *models.MinerChannel) error {
	_, _, err := minerChannelSingleMinerType(minerChannel)
	return err
}

func minerChannelSingleMinerType(minerChannel *models.MinerChannel) (string, string, error) {
	if minerChannel == nil || len(minerChannel.Members) == 0 {
		return "", "", nil
	}
	var manufacturer string
	var model string
	for _, member := range minerChannel.Members {
		nextManufacturer := strings.TrimSpace(member.Display.Manufacturer)
		nextModel := strings.TrimSpace(member.Display.Model)
		if nextManufacturer == "" || nextModel == "" {
			return "", "", fleeterror.NewInvalidArgumentErrorf("MinerChannel member %q is missing manufacturer or model information.", member.DeviceIdentifier)
		}
		if manufacturer == "" && model == "" {
			manufacturer = nextManufacturer
			model = nextModel
			continue
		}
		if !sameMinerType(nextManufacturer, manufacturer) || !sameMinerType(nextModel, model) {
			return "", "", fleeterror.NewInvalidArgumentError("MinerChannel members must have a single manufacturer and model.")
		}
	}
	return manufacturer, model, nil
}

func sameMinerType(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func (s *SQLMinerChannelStore) GetMinerChannel(ctx context.Context, orgID, minerChannelID int64) (*models.MinerChannel, error) {
	return s.getMinerChannelWithQueries(ctx, s.GetQueries(ctx), orgID, minerChannelID)
}

func (s *SQLMinerChannelStore) ListMinerChannels(ctx context.Context, params models.ListMinerChannelsParams) (models.PagedMinerChannels, error) {
	pageSize := normalizeMinerChannelPageSize(params.PageSize)
	cursor, err := decodeMinerChannelPageCursor(params.PageToken)
	if err != nil {
		return models.PagedMinerChannels{}, err
	}
	search := strings.TrimSpace(params.Search)
	q := s.GetQueries(ctx)
	rows, err := q.ListMinerChannels(ctx, sqlc.ListMinerChannelsParams{
		OrgID:           params.OrgID,
		IncludeReleased: params.IncludeReleased,
		SearchFilterSet: search != "",
		Search:          search,
		CursorSet:       cursor != nil,
		CursorIsDefault: cursor != nil && cursor.IsDefault,
		CursorUpdatedAt: nullTimeFromCursor(cursor),
		CursorID:        nullInt64FromCursor(cursor),
		LimitCount:      pageSize + 1,
	})
	if err != nil {
		return models.PagedMinerChannels{}, fleeterror.NewInternalErrorf("failed to list miner channels: %v", err)
	}
	total, err := q.CountMinerChannels(ctx, sqlc.CountMinerChannelsParams{
		OrgID:           params.OrgID,
		IncludeReleased: params.IncludeReleased,
		SearchFilterSet: search != "",
		Search:          search,
	})
	if err != nil {
		return models.PagedMinerChannels{}, fleeterror.NewInternalErrorf("failed to count miner channels: %v", err)
	}
	var nextPageToken string
	if len(rows) > int(pageSize) {
		last := rows[pageSize-1]
		nextPageToken, err = encodeMinerChannelPageCursor(minerChannelPageCursor{
			IsDefault: last.IsDefault,
			UpdatedAt: last.UpdatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return models.PagedMinerChannels{}, err
		}
		rows = rows[:pageSize]
	}
	out := make([]*models.MinerChannel, 0, len(rows))
	for _, row := range rows {
		minerChannel := minerChannelFromListRow(row)
		out = append(out, &minerChannel)
	}
	if err := s.loadFirmwareTargetsForMinerChannels(ctx, q, params.OrgID, out); err != nil {
		return models.PagedMinerChannels{}, err
	}
	if err := s.loadFirmwareStatusesForMinerChannels(ctx, q, params.OrgID, out); err != nil {
		return models.PagedMinerChannels{}, err
	}
	if err := s.loadConfigStatusesForMinerChannels(ctx, q, params.OrgID, out); err != nil {
		return models.PagedMinerChannels{}, err
	}
	return models.PagedMinerChannels{
		MinerChannels: out,
		NextPageToken: nextPageToken,
		TotalCount:    int32Count(total),
	}, nil
}

func (s *SQLMinerChannelStore) ListMinerChannelsByOwner(ctx context.Context, params models.ListMinerChannelsByOwnerParams) (models.PagedMinerChannels, error) {
	pageSize := normalizeMinerChannelPageSize(params.PageSize)
	cursor, err := decodeMinerChannelPageCursor(params.PageToken)
	if err != nil {
		return models.PagedMinerChannels{}, err
	}
	search := strings.TrimSpace(params.Search)
	q := s.GetQueries(ctx)
	rows, err := q.ListMinerChannelsByOwner(ctx, sqlc.ListMinerChannelsByOwnerParams{
		OrgID:           params.OrgID,
		OwnerUserID:     sql.NullInt64{Int64: params.OwnerUserID, Valid: true},
		IncludeReleased: params.IncludeReleased,
		SearchFilterSet: search != "",
		Search:          search,
		CursorSet:       cursor != nil,
		CursorUpdatedAt: nullTimeFromCursor(cursor),
		CursorID:        nullInt64FromCursor(cursor),
		LimitCount:      pageSize + 1,
	})
	if err != nil {
		return models.PagedMinerChannels{}, fleeterror.NewInternalErrorf("failed to list owned miner channels: %v", err)
	}
	total, err := q.CountMinerChannelsByOwner(ctx, sqlc.CountMinerChannelsByOwnerParams{
		OrgID:           params.OrgID,
		OwnerUserID:     sql.NullInt64{Int64: params.OwnerUserID, Valid: true},
		IncludeReleased: params.IncludeReleased,
		SearchFilterSet: search != "",
		Search:          search,
	})
	if err != nil {
		return models.PagedMinerChannels{}, fleeterror.NewInternalErrorf("failed to count owned miner channels: %v", err)
	}
	var nextPageToken string
	if len(rows) > int(pageSize) {
		last := rows[pageSize-1]
		nextPageToken, err = encodeMinerChannelPageCursor(minerChannelPageCursor{
			UpdatedAt: last.UpdatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return models.PagedMinerChannels{}, err
		}
		rows = rows[:pageSize]
	}
	out := make([]*models.MinerChannel, 0, len(rows))
	for _, row := range rows {
		minerChannel := minerChannelFromOwnerRow(row)
		out = append(out, &minerChannel)
	}
	if err := s.loadFirmwareTargetsForMinerChannels(ctx, q, params.OrgID, out); err != nil {
		return models.PagedMinerChannels{}, err
	}
	if err := s.loadFirmwareStatusesForMinerChannels(ctx, q, params.OrgID, out); err != nil {
		return models.PagedMinerChannels{}, err
	}
	if err := s.loadConfigStatusesForMinerChannels(ctx, q, params.OrgID, out); err != nil {
		return models.PagedMinerChannels{}, err
	}
	return models.PagedMinerChannels{
		MinerChannels: out,
		NextPageToken: nextPageToken,
		TotalCount:    int32Count(total),
	}, nil
}

func (s *SQLMinerChannelStore) loadFirmwareTargetsForMinerChannels(ctx context.Context, q sqlc.Querier, orgID int64, minerChannels []*models.MinerChannel) error {
	for _, minerChannel := range minerChannels {
		rows, err := q.ListMinerChannelFirmwareTargets(ctx, sqlc.ListMinerChannelFirmwareTargetsParams{
			MinerChannelID: minerChannel.ID,
			OrgID:          orgID,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to list miner channel firmware targets: %v", err)
		}
		minerChannel.FirmwareTargets = make([]models.MinerChannelFirmwareTarget, 0, len(rows))
		for _, row := range rows {
			minerChannel.FirmwareTargets = append(minerChannel.FirmwareTargets, firmwareTargetFromRow(row))
		}
	}
	return nil
}

func (s *SQLMinerChannelStore) loadFirmwareStatusesForMinerChannels(ctx context.Context, q sqlc.Querier, orgID int64, minerChannels []*models.MinerChannel) error {
	if len(minerChannels) == 0 {
		return nil
	}
	minerChannelIDs := make([]int64, 0, len(minerChannels))
	minerChannelByID := make(map[int64]*models.MinerChannel, len(minerChannels))
	for _, minerChannel := range minerChannels {
		minerChannelIDs = append(minerChannelIDs, minerChannel.ID)
		minerChannelByID[minerChannel.ID] = minerChannel
	}
	rows, err := q.ListMinerChannelFirmwareStatuses(ctx, sqlc.ListMinerChannelFirmwareStatusesParams{
		OrgID:           orgID,
		MinerChannelIds: minerChannelIDs,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to list miner channel firmware statuses: %v", err)
	}
	statusByDevice := make(map[int64]map[string]models.MinerChannelFirmwareStatus, len(minerChannels))
	for _, row := range rows {
		status := firmwareStatusFromMinerChannelRow(row)
		minerChannel := minerChannelByID[row.MinerChannelID]
		if minerChannel == nil {
			continue
		}
		minerChannel.FirmwareStatuses = append(minerChannel.FirmwareStatuses, status)
		if statusByDevice[row.MinerChannelID] == nil {
			statusByDevice[row.MinerChannelID] = make(map[string]models.MinerChannelFirmwareStatus)
		}
		statusByDevice[row.MinerChannelID][row.DeviceIdentifier] = status
	}
	for _, minerChannel := range minerChannels {
		byDevice := statusByDevice[minerChannel.ID]
		for i := range minerChannel.Members {
			if status, ok := byDevice[minerChannel.Members[i].DeviceIdentifier]; ok {
				next := status
				minerChannel.Members[i].FirmwareStatus = &next
			}
		}
	}
	return nil
}

func (s *SQLMinerChannelStore) loadConfigStatusesForMinerChannels(ctx context.Context, q sqlc.Querier, orgID int64, minerChannels []*models.MinerChannel) error {
	if len(minerChannels) == 0 {
		return nil
	}
	minerChannelIDs := make([]int64, 0, len(minerChannels))
	minerChannelByID := make(map[int64]*models.MinerChannel, len(minerChannels))
	for _, minerChannel := range minerChannels {
		minerChannelIDs = append(minerChannelIDs, minerChannel.ID)
		minerChannelByID[minerChannel.ID] = minerChannel
	}
	rows, err := q.ListMinerChannelConfigStatuses(ctx, sqlc.ListMinerChannelConfigStatusesParams{OrgID: orgID, MinerChannelIds: minerChannelIDs})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to list miner channel config progress: %v", err)
	}
	statusesByMinerChannel := make(map[int64]map[string][]models.MinerChannelConfigStatus, len(minerChannels))
	progressByMinerChannel := make(map[int64]map[models.MinerChannelConfigDimension]*models.MinerChannelConfigProgress, len(minerChannels))
	for _, row := range rows {
		status := configStatusFromColumns(row.Dimension, row.Supported, row.EnforcementState, row.RetryCount, row.LastError, row.LastDispatchedAt, row.ConfirmedAt, row.ObservedAt)
		if statusesByMinerChannel[row.MinerChannelID] == nil {
			statusesByMinerChannel[row.MinerChannelID] = make(map[string][]models.MinerChannelConfigStatus)
		}
		statusesByMinerChannel[row.MinerChannelID][row.DeviceIdentifier] = append(statusesByMinerChannel[row.MinerChannelID][row.DeviceIdentifier], status)
		if progressByMinerChannel[row.MinerChannelID] == nil {
			progressByMinerChannel[row.MinerChannelID] = make(map[models.MinerChannelConfigDimension]*models.MinerChannelConfigProgress)
		}
		progress := progressByMinerChannel[row.MinerChannelID][status.Dimension]
		if progress == nil {
			progress = &models.MinerChannelConfigProgress{Dimension: status.Dimension}
			progressByMinerChannel[row.MinerChannelID][status.Dimension] = progress
		}
		incrementConfigProgress(progress, status.State)
	}
	for minerChannelID, minerChannel := range minerChannelByID {
		for i := range minerChannel.Members {
			minerChannel.Members[i].ConfigStatuses = statusesByMinerChannel[minerChannelID][minerChannel.Members[i].DeviceIdentifier]
		}
		for _, progress := range progressByMinerChannel[minerChannelID] {
			minerChannel.ConfigProgress = append(minerChannel.ConfigProgress, *progress)
		}
	}
	return nil
}

func incrementConfigProgress(progress *models.MinerChannelConfigProgress, state models.MinerChannelConfigLifecycleState) {
	progress.TargetedCount++
	switch state {
	case models.MinerChannelConfigStateUnsupported:
		progress.UnsupportedCount++
	case models.MinerChannelConfigStateWaitingForObservation:
		progress.WaitingCount++
	case models.MinerChannelConfigStateApplying:
		progress.ApplyingCount++
	case models.MinerChannelConfigStateVerifying:
		progress.VerifyingCount++
	case models.MinerChannelConfigStateConverged:
		progress.ConvergedCount++
	case models.MinerChannelConfigStateHeld:
		progress.HeldCount++
	case models.MinerChannelConfigStateFailed:
		progress.FailedCount++
	}
}

func (s *SQLMinerChannelStore) UpdateMinerChannel(ctx context.Context, params models.UpdateMinerChannelParams) (*models.MinerChannel, error) {
	row, err := s.GetQueries(ctx).UpdateMinerChannel(ctx, sqlc.UpdateMinerChannelParams{
		ID:                    params.MinerChannelID,
		OrgID:                 params.OrgID,
		Label:                 ptrToNullString(params.Label),
		Purpose:               ptrToNullString(params.Purpose),
		ExpiresAt:             ptrToNullTime(params.ExpiresAt),
		ClearExpiresAt:        params.ClearExpiresAt,
		DesiredConfigJsonb:    rawMessageToNull(params.DesiredConfigJSON),
		DesiredConfigJsonbSet: params.DesiredConfigJSONSet,
		ClearDesiredConfig:    params.ClearDesiredConfig,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("Active miner channel %d not found.", params.MinerChannelID)
		}
		return nil, mapMinerChannelUpdateError(err)
	}
	return s.getMinerChannelWithQueries(ctx, s.GetQueries(ctx), row.OrgID, row.ID)
}

func (s *SQLMinerChannelStore) UpdateDefaultMinerChannelConfig(ctx context.Context, params models.UpdateMinerChannelParams) (*models.MinerChannel, error) {
	row, err := s.GetQueries(ctx).UpdateDefaultMinerChannelConfig(ctx, sqlc.UpdateDefaultMinerChannelConfigParams{
		ID: params.MinerChannelID, OrgID: params.OrgID,
		DesiredConfigJsonb: rawMessageToNull(params.DesiredConfigJSON), ClearDesiredConfig: params.ClearDesiredConfig,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("Active default miner channel %d not found.", params.MinerChannelID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to update default miner channel config: %v", err)
	}
	return s.getMinerChannelWithQueries(ctx, s.GetQueries(ctx), row.OrgID, row.ID)
}

func (s *SQLMinerChannelStore) SetMinerChannelFirmwareTarget(ctx context.Context, params models.SetMinerChannelFirmwareTargetParams) (*models.MinerChannel, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.MinerChannel, error) {
		target, err := q.GetMinerChannel(ctx, sqlc.GetMinerChannelParams{ID: params.MinerChannelID, OrgID: params.OrgID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fleeterror.NewNotFoundErrorf("MinerChannel %d not found.", params.MinerChannelID)
			}
			return nil, fleeterror.NewInternalErrorf("failed to get miner channel: %v", err)
		}
		if models.MinerChannelState(target.State) != models.MinerChannelStateActive {
			return nil, fleeterror.NewInvalidArgumentErrorf("MinerChannel %d is not active.", params.MinerChannelID)
		}

		if params.FirmwareFileID == nil {
			if _, err := q.DeleteMinerChannelFirmwareTarget(ctx, sqlc.DeleteMinerChannelFirmwareTargetParams{
				MinerChannelID: params.MinerChannelID,
				OrgID:          params.OrgID,
				Manufacturer:   *params.Manufacturer,
				Model:          *params.Model,
			}); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to clear miner channel firmware target: %v", err)
			}
		} else if _, err := q.UpsertMinerChannelFirmwareTarget(ctx, sqlc.UpsertMinerChannelFirmwareTargetParams{
			MinerChannelID: params.MinerChannelID,
			OrgID:          params.OrgID,
			Manufacturer:   *params.Manufacturer,
			Model:          *params.Model,
			FirmwareFileID: ptrToNullString(params.FirmwareFileID),
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to set miner channel firmware target: %v", err)
		}
		if _, err := q.ResetFirmwareEnforcementForMinerChannelTarget(ctx, sqlc.ResetFirmwareEnforcementForMinerChannelTargetParams{
			OrgID:          params.OrgID,
			MinerChannelID: params.MinerChannelID,
			Manufacturer:   *params.Manufacturer,
			Model:          *params.Model,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to reset miner channel firmware enforcement: %v", err)
		}

		return s.getMinerChannelWithQueries(ctx, q, params.OrgID, params.MinerChannelID)
	})
}

func (s *SQLMinerChannelStore) ClearMissingFirmwareTarget(ctx context.Context, orgID int64, firmwareFileID string) (int64, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (int64, error) {
		if _, err := q.ResetFirmwareEnforcementForFirmwareFile(ctx, sqlc.ResetFirmwareEnforcementForFirmwareFileParams{
			OrgID:          orgID,
			FirmwareFileID: ptrToNullString(&firmwareFileID),
		}); err != nil {
			return 0, fleeterror.NewInternalErrorf("failed to reset missing firmware enforcement: %v", err)
		}

		clearedTargets, err := q.ClearMinerChannelFirmwareTargetFileReferences(ctx, sqlc.ClearMinerChannelFirmwareTargetFileReferencesParams{
			OrgID:          orgID,
			FirmwareFileID: ptrToNullString(&firmwareFileID),
		})
		if err != nil {
			return 0, fleeterror.NewInternalErrorf("failed to clear miner channel firmware targets: %v", err)
		}

		return clearedTargets, nil
	})
}

func (s *SQLMinerChannelStore) MoveDevicesToMinerChannel(ctx context.Context, params models.MembershipMutationParams) (*models.MinerChannel, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.MinerChannel, error) {
		target, err := q.GetMinerChannel(ctx, sqlc.GetMinerChannelParams{ID: params.MinerChannelID, OrgID: params.OrgID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fleeterror.NewNotFoundErrorf("MinerChannel %d not found.", params.MinerChannelID)
			}
			return nil, fleeterror.NewInternalErrorf("failed to get target miner channel: %v", err)
		}
		if target.IsDefault || models.MinerChannelState(target.State) != models.MinerChannelStateActive {
			return nil, fleeterror.NewInvalidArgumentErrorf("MinerChannel %d is not an active reservation miner channel.", params.MinerChannelID)
		}

		if _, err := q.DeleteMinerChannelMembershipsByDevice(ctx, sqlc.DeleteMinerChannelMembershipsByDeviceParams{
			OrgID:             params.OrgID,
			DeviceIdentifiers: params.DeviceIdentifiers,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to clear existing miner channel memberships: %v", err)
		}
		payload, err := s.buildMinerChannelMemberPayloadForIdentifiers(ctx, q, params.OrgID, params.DeviceIdentifiers)
		if err != nil {
			return nil, err
		}
		inserted, err := q.BulkInsertMinerChannelMemberships(ctx, sqlc.BulkInsertMinerChannelMembershipsParams{
			MinerChannelID: params.MinerChannelID,
			OrgID:          params.OrgID,
			MembersJsonb:   payload,
		})
		if err != nil {
			return nil, mapMinerChannelMembershipError(err)
		}
		if inserted != int64(len(params.DeviceIdentifiers)) {
			return nil, fleeterror.NewInternalErrorf("bulk insert wrote %d miner channel members, expected %d", inserted, len(params.DeviceIdentifiers))
		}
		minerChannel, err := s.getMinerChannelWithQueries(ctx, q, params.OrgID, params.MinerChannelID)
		if err != nil {
			return nil, err
		}
		if err := validateMinerChannelSingleMinerType(minerChannel); err != nil {
			return nil, err
		}
		if _, err := q.ResetFirmwareEnforcementForDevices(ctx, sqlc.ResetFirmwareEnforcementForDevicesParams{
			OrgID:             params.OrgID,
			DeviceIdentifiers: params.DeviceIdentifiers,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to reset moved device firmware enforcement: %v", err)
		}
		return minerChannel, nil
	})
}

func (s *SQLMinerChannelStore) RemoveDevicesAndGetMinerChannel(ctx context.Context, params models.MembershipMutationParams) (*models.MinerChannel, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.MinerChannel, error) {
		if _, err := q.GetMinerChannel(ctx, sqlc.GetMinerChannelParams{ID: params.MinerChannelID, OrgID: params.OrgID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fleeterror.NewNotFoundErrorf("MinerChannel %d not found.", params.MinerChannelID)
			}
			return nil, fleeterror.NewInternalErrorf("failed to get miner channel: %v", err)
		}
		matched, err := q.CountMinerChannelMemberships(ctx, sqlc.CountMinerChannelMembershipsParams{
			MinerChannelID:    params.MinerChannelID,
			OrgID:             params.OrgID,
			DeviceIdentifiers: params.DeviceIdentifiers,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to validate miner channel memberships: %v", err)
		}
		if matched != int64(len(params.DeviceIdentifiers)) {
			return nil, fleeterror.NewNotFoundErrorf("Found %d of %d selected miner channel members.", matched, len(params.DeviceIdentifiers))
		}
		if _, err := q.ResetFirmwareEnforcementForDevices(ctx, sqlc.ResetFirmwareEnforcementForDevicesParams{
			OrgID:             params.OrgID,
			DeviceIdentifiers: params.DeviceIdentifiers,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to reset removed device firmware enforcement: %v", err)
		}
		deleted, err := q.DeleteMinerChannelMemberships(ctx, sqlc.DeleteMinerChannelMembershipsParams{
			MinerChannelID:    params.MinerChannelID,
			OrgID:             params.OrgID,
			DeviceIdentifiers: params.DeviceIdentifiers,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to delete miner channel memberships: %v", err)
		}
		if deleted != int64(len(params.DeviceIdentifiers)) {
			return nil, fleeterror.NewInternalErrorf("deleted %d miner channel members, expected %d", deleted, len(params.DeviceIdentifiers))
		}
		return s.getMinerChannelWithQueries(ctx, q, params.OrgID, params.MinerChannelID)
	})
}

func (s *SQLMinerChannelStore) ReleaseMinerChannel(ctx context.Context, orgID, minerChannelID int64) (*models.MinerChannel, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.MinerChannel, error) {
		row, err := q.ReleaseMinerChannel(ctx, sqlc.ReleaseMinerChannelParams{ID: minerChannelID, OrgID: orgID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fleeterror.NewNotFoundErrorf("MinerChannel %d not found.", minerChannelID)
			}
			return nil, fleeterror.NewInternalErrorf("failed to release miner channel: %v", err)
		}
		if _, err := q.ResetFirmwareEnforcementForMinerChannelMembers(ctx, sqlc.ResetFirmwareEnforcementForMinerChannelMembersParams{
			MinerChannelID: minerChannelID,
			OrgID:          orgID,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to reset released miner channel firmware enforcement: %v", err)
		}
		if _, err := q.DeleteMinerChannelMembershipsByMinerChannel(ctx, sqlc.DeleteMinerChannelMembershipsByMinerChannelParams{
			MinerChannelID: minerChannelID,
			OrgID:          orgID,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to clear miner channel memberships: %v", err)
		}
		return s.getMinerChannelWithQueries(ctx, q, row.OrgID, row.ID)
	})
}

func (s *SQLMinerChannelStore) SweepExpiredMinerChannels(ctx context.Context) ([]*models.MinerChannel, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) ([]*models.MinerChannel, error) {
		expired, err := q.ListExpiredActiveMinerChannels(ctx)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to list expired miner channels: %v", err)
		}
		out := make([]*models.MinerChannel, 0, len(expired))
		for _, row := range expired {
			released, err := q.ReleaseMinerChannel(ctx, sqlc.ReleaseMinerChannelParams{ID: row.ID, OrgID: row.OrgID})
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to release expired miner channel %d: %v", row.ID, err)
			}
			if _, err := q.ResetFirmwareEnforcementForMinerChannelMembers(ctx, sqlc.ResetFirmwareEnforcementForMinerChannelMembersParams{
				MinerChannelID: row.ID,
				OrgID:          row.OrgID,
			}); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to reset expired miner channel %d firmware enforcement: %v", row.ID, err)
			}
			if _, err := q.DeleteMinerChannelMembershipsByMinerChannel(ctx, sqlc.DeleteMinerChannelMembershipsByMinerChannelParams{
				MinerChannelID: row.ID,
				OrgID:          row.OrgID,
			}); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to clear expired miner channel %d memberships: %v", row.ID, err)
			}
			minerChannel := minerChannelFromRow(released, 0)
			out = append(out, &minerChannel)
		}
		return out, nil
	})
}

func (s *SQLMinerChannelStore) InsertMinerChannelMember(ctx context.Context, params models.InsertMinerChannelMemberParams) error {
	err := s.GetQueries(ctx).InsertMinerChannelMembership(ctx, sqlc.InsertMinerChannelMembershipParams{
		MinerChannelID:   params.MinerChannelID,
		OrgID:            params.OrgID,
		DeviceIdentifier: params.DeviceIdentifier,
	})
	if err != nil {
		return mapMinerChannelMembershipError(err)
	}
	return nil
}

func (s *SQLMinerChannelStore) DeleteMinerChannelMemberships(ctx context.Context, orgID, minerChannelID int64, deviceIdentifiers []string) (int64, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (int64, error) {
		if _, err := q.ResetFirmwareEnforcementForDevices(ctx, sqlc.ResetFirmwareEnforcementForDevicesParams{
			OrgID:             orgID,
			DeviceIdentifiers: deviceIdentifiers,
		}); err != nil {
			return 0, fleeterror.NewInternalErrorf("failed to reset removed device firmware enforcement: %v", err)
		}
		count, err := q.DeleteMinerChannelMemberships(ctx, sqlc.DeleteMinerChannelMembershipsParams{
			MinerChannelID:    minerChannelID,
			OrgID:             orgID,
			DeviceIdentifiers: deviceIdentifiers,
		})
		if err != nil {
			return 0, fleeterror.NewInternalErrorf("failed to delete miner channel memberships: %v", err)
		}
		return count, nil
	})
}

func (s *SQLMinerChannelStore) ListMinerChannelMembers(ctx context.Context, orgID, minerChannelID int64) ([]models.MinerChannelMember, error) {
	rows, err := s.GetQueries(ctx).ListMinerChannelMembers(ctx, sqlc.ListMinerChannelMembersParams{
		MinerChannelID: minerChannelID,
		OrgID:          orgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list miner channel members: %v", err)
	}
	out := make([]models.MinerChannelMember, 0, len(rows))
	for _, row := range rows {
		out = append(out, memberFromRow(row))
	}
	return out, nil
}

func (s *SQLMinerChannelStore) ResolveEffectiveMinerChannelForDevice(ctx context.Context, orgID int64, deviceIdentifier string) (*models.MinerChannel, error) {
	row, err := s.GetQueries(ctx).ResolveEffectiveMinerChannelForDevice(ctx, sqlc.ResolveEffectiveMinerChannelForDeviceParams{
		OrgID:            orgID,
		DeviceIdentifier: deviceIdentifier,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("Device %q not found.", deviceIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("failed to resolve effective miner channel: %v", err)
	}
	minerChannel := minerChannelFromResolvedRow(row)
	return &minerChannel, nil
}

func (s *SQLMinerChannelStore) ListDefaultMinerChannelDevices(ctx context.Context, orgID int64) ([]models.DefaultMinerChannelDevice, error) {
	rows, err := s.GetQueries(ctx).ListDefaultMinerChannelDevices(ctx, sqlc.ListDefaultMinerChannelDevicesParams{
		OrgID:      orgID,
		LimitCount: maxDefaultMinerChannelDeviceListLimit,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list default miner channel devices: %v", err)
	}
	out := make([]models.DefaultMinerChannelDevice, 0, len(rows))
	for _, deviceIdentifier := range rows {
		out = append(out, models.DefaultMinerChannelDevice{
			DeviceIdentifier: deviceIdentifier,
		})
	}
	return out, nil
}

func (s *SQLMinerChannelStore) ListMinerChannelDeviceOwnership(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.MinerChannelDeviceOwnership, error) {
	rows, err := s.GetQueries(ctx).ListMinerChannelDeviceOwnership(ctx, sqlc.ListMinerChannelDeviceOwnershipParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list miner channel device ownership: %v", err)
	}
	out := make([]models.MinerChannelDeviceOwnership, 0, len(rows))
	for _, row := range rows {
		out = append(out, ownershipFromRow(row.DeviceIdentifier, row.MinerChannelID, row.OwnerUserID, row.OwnerUsername))
	}
	return out, nil
}

func (s *SQLMinerChannelStore) ListActiveOwnedMinerChannelMemberships(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.MinerChannelDeviceOwnership, error) {
	rows, err := s.GetQueries(ctx).ListActiveOwnedMinerChannelMemberships(ctx, sqlc.ListActiveOwnedMinerChannelMembershipsParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list active owned miner channel memberships: %v", err)
	}
	out := make([]models.MinerChannelDeviceOwnership, 0, len(rows))
	for _, row := range rows {
		out = append(out, ownershipFromRow(row.DeviceIdentifier, row.MinerChannelID, row.OwnerUserID, row.OwnerUsername))
	}
	return out, nil
}

func (s *SQLMinerChannelStore) ListDevices(ctx context.Context, params models.ListDevicesParams) (models.PagedMinerChannelDevices, error) {
	pageSize := normalizeMinerChannelPageSize(params.PageSize)
	cursor, err := decodeMinerChannelDevicePageCursor(params.PageToken)
	if err != nil {
		return models.PagedMinerChannelDevices{}, err
	}
	search := strings.TrimSpace(params.Filter.Search)
	queryParams := sqlc.ListMinerChannelDevicesParams{
		Assignments:            minerChannelAssignmentStrings(params.Filter.Assignments),
		MinerChannelIds:        int64Slice(params.Filter.MinerChannelIDs),
		OwnerUserIds:           int64Slice(params.Filter.OwnerUserIDs),
		IncludeUnowned:         params.Filter.IncludeUnowned,
		Manufacturers:          trimStrings(params.Filter.Manufacturers),
		Models:                 trimStrings(params.Filter.Models),
		SearchFilterSet:        search != "",
		Search:                 search,
		CursorSet:              cursor != nil,
		CursorDisplayName:      cursorDisplayName(cursor),
		CursorDeviceIdentifier: cursorDeviceIdentifier(cursor),
		LimitCount:             pageSize + 1,
		OrgID:                  params.OrgID,
	}
	q := s.GetQueries(ctx)
	rows, err := q.ListMinerChannelDevices(ctx, queryParams)
	if err != nil {
		return models.PagedMinerChannelDevices{}, fleeterror.NewInternalErrorf("failed to list miner channel devices: %v", err)
	}
	counts, err := q.CountMinerChannelDevices(ctx, sqlc.CountMinerChannelDevicesParams{
		Assignments:     queryParams.Assignments,
		MinerChannelIds: queryParams.MinerChannelIds,
		OwnerUserIds:    queryParams.OwnerUserIds,
		IncludeUnowned:  queryParams.IncludeUnowned,
		Manufacturers:   queryParams.Manufacturers,
		Models:          queryParams.Models,
		SearchFilterSet: queryParams.SearchFilterSet,
		Search:          queryParams.Search,
		OrgID:           queryParams.OrgID,
	})
	if err != nil {
		return models.PagedMinerChannelDevices{}, fleeterror.NewInternalErrorf("failed to count miner channel devices: %v", err)
	}
	var nextPageToken string
	if len(rows) > int(pageSize) {
		last := rows[pageSize-1]
		nextPageToken, err = encodeMinerChannelDevicePageCursor(minerChannelDevicePageCursor{
			DisplayName:      last.DisplayName,
			DeviceIdentifier: last.DeviceIdentifier,
		})
		if err != nil {
			return models.PagedMinerChannelDevices{}, err
		}
		rows = rows[:pageSize]
	}
	out := make([]models.MinerChannelDevice, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.MinerChannelDevice{
			DeviceIdentifier:      row.DeviceIdentifier,
			EffectiveMinerChannel: minerChannelFromDeviceRow(row),
			Display:               displayFromColumns(row.DisplayName, row.WorkerName, row.Manufacturer, row.Model, row.IpAddress, row.SerialNumber, row.FirmwareVersion),
		})
	}
	if err := s.loadFirmwareStatusesForDevices(ctx, q, params.OrgID, out); err != nil {
		return models.PagedMinerChannelDevices{}, err
	}
	if err := s.loadConfigStatusesForDevices(ctx, q, params.OrgID, out); err != nil {
		return models.PagedMinerChannelDevices{}, err
	}
	return models.PagedMinerChannelDevices{
		Devices:        out,
		NextPageToken:  nextPageToken,
		TotalCount:     int32Count(counts.TotalCount),
		AvailableCount: int32Count(counts.AvailableCount),
		ReservedCount:  int32Count(counts.ReservedCount),
	}, nil
}

func (s *SQLMinerChannelStore) loadFirmwareStatusesForDevices(ctx context.Context, q sqlc.Querier, orgID int64, devices []models.MinerChannelDevice) error {
	if len(devices) == 0 {
		return nil
	}
	deviceIdentifiers := make([]string, 0, len(devices))
	for _, device := range devices {
		deviceIdentifiers = append(deviceIdentifiers, device.DeviceIdentifier)
	}
	rows, err := q.ListMinerChannelFirmwareStatusesForDevices(ctx, sqlc.ListMinerChannelFirmwareStatusesForDevicesParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to list miner channel device firmware statuses: %v", err)
	}
	statusByDevice := make(map[string]models.MinerChannelFirmwareStatus, len(rows))
	for _, row := range rows {
		statusByDevice[row.DeviceIdentifier] = firmwareStatusFromDeviceRow(row)
	}
	for i := range devices {
		if status, ok := statusByDevice[devices[i].DeviceIdentifier]; ok {
			next := status
			devices[i].FirmwareStatus = &next
		}
	}
	return nil
}

func (s *SQLMinerChannelStore) loadConfigStatusesForDevices(ctx context.Context, q sqlc.Querier, orgID int64, devices []models.MinerChannelDevice) error {
	identifiers := make([]string, 0, len(devices))
	for _, device := range devices {
		identifiers = append(identifiers, device.DeviceIdentifier)
	}
	statuses, err := s.listConfigStatuses(ctx, q, orgID, identifiers)
	if err != nil {
		return err
	}
	for i := range devices {
		devices[i].ConfigStatuses = statuses[devices[i].DeviceIdentifier]
	}
	return nil
}

func (s *SQLMinerChannelStore) listConfigStatuses(ctx context.Context, q sqlc.Querier, orgID int64, identifiers []string) (map[string][]models.MinerChannelConfigStatus, error) {
	out := make(map[string][]models.MinerChannelConfigStatus)
	if len(identifiers) == 0 {
		return out, nil
	}
	rows, err := q.ListMinerChannelConfigStatusesForDevices(ctx, sqlc.ListMinerChannelConfigStatusesForDevicesParams{OrgID: orgID, DeviceIdentifiers: identifiers})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list miner channel config statuses: %v", err)
	}
	for _, row := range rows {
		out[row.DeviceIdentifier] = append(out[row.DeviceIdentifier], configStatusFromColumns(
			row.Dimension, row.Supported, row.EnforcementState, row.RetryCount, row.LastError,
			row.LastDispatchedAt, row.ConfirmedAt, row.ObservedAt,
		))
	}
	return out, nil
}

func configStatusFromColumns(
	dimension string,
	supported bool,
	enforcementState sql.NullString,
	retryCount sql.NullInt32,
	lastError sql.NullString,
	lastDispatchedAt sql.NullTime,
	confirmedAt sql.NullTime,
	observedAt sql.NullTime,
) models.MinerChannelConfigStatus {
	state := models.MinerChannelConfigStateWaitingForObservation
	if !supported {
		state = models.MinerChannelConfigStateUnsupported
	} else if !observedAt.Valid || time.Since(observedAt.Time) > 15*time.Minute {
		state = models.MinerChannelConfigStateWaitingForObservation
	} else {
		switch models.EnforcementState(enforcementState.String) {
		case models.EnforcementStateDispatching, models.EnforcementStatePending, models.EnforcementStateDrifted:
			state = models.MinerChannelConfigStateApplying
		case models.EnforcementStateDispatched:
			state = models.MinerChannelConfigStateVerifying
		case models.EnforcementStateConfirmed:
			state = models.MinerChannelConfigStateConverged
		case models.EnforcementStateHeld:
			state = models.MinerChannelConfigStateHeld
		case models.EnforcementStateFailed:
			state = models.MinerChannelConfigStateFailed
		}
	}
	return models.MinerChannelConfigStatus{
		Dimension: models.MinerChannelConfigDimension(dimension), Supported: supported, State: state,
		RetryCount: retryCount.Int32, LastError: ptrFromNullString(lastError),
		LastDispatchedAt: nullTimeToPtr(lastDispatchedAt), ConfirmedAt: nullTimeToPtr(confirmedAt), ObservedAt: nullTimeToPtr(observedAt),
	}
}

func (s *SQLMinerChannelStore) ListOrgsWithFirmwareTargets(ctx context.Context) ([]int64, error) {
	orgIDs, err := s.GetQueries(ctx).ListOrgsWithFirmwareTargets(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list orgs with firmware targets: %v", err)
	}
	return orgIDs, nil
}

func (s *SQLMinerChannelStore) ListOrgsWithDesiredConfig(ctx context.Context) ([]int64, error) {
	orgIDs, err := s.GetQueries(ctx).ListOrgsWithDesiredConfig(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list orgs with desired config: %v", err)
	}
	return orgIDs, nil
}

func (s *SQLMinerChannelStore) ListConfigEnforcementCandidates(ctx context.Context, orgID int64, dimension models.MinerChannelConfigDimension) ([]models.ConfigEnforcementCandidate, error) {
	rows, err := s.GetQueries(ctx).ListConfigEnforcementCandidates(ctx, sqlc.ListConfigEnforcementCandidatesParams{
		OrgID: orgID, Dimension: string(dimension),
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list config enforcement candidates: %v", err)
	}
	out := make([]models.ConfigEnforcementCandidate, 0, len(rows))
	for _, row := range rows {
		desired, parseErr := models.ParseMinerChannelDesiredConfig(rawMessageFromNull(row.DesiredConfigJsonb))
		if parseErr != nil {
			return nil, fleeterror.NewInternalErrorf("failed to decode miner channel desired config: %v", parseErr)
		}
		state := ptrFromNullString(row.EnforcementState)
		var enforcementState *models.EnforcementState
		if state != nil {
			converted := models.EnforcementState(*state)
			enforcementState = &converted
		}
		out = append(out, models.ConfigEnforcementCandidate{
			OrgID: row.OrgID, DeviceIdentifier: row.DeviceIdentifier, DriverName: row.DriverName,
			Manufacturer: row.Manufacturer, Model: row.Model, WorkerName: row.WorkerName,
			MinerChannelID: row.MinerChannelID, ActorUserID: row.ActorUserID,
			ActorExternalUserID: row.ActorExternalUserID, ActorUsername: row.ActorUsername,
			DesiredConfig: desired, Dimension: dimension,
			ObservedStateJSON: rawMessageFromNull(row.ObservedStateJsonb),
			ObservedStateHash: ptrFromNullString(row.ObservedStateHash),
			ConfigObservedAt:  nullTimeToPtr(row.ConfigObservedAt),
			DesiredStateHash:  ptrFromNullString(row.DesiredStateHash), Supported: nullBoolPtr(row.Supported), State: enforcementState,
			RetryCount: row.RetryCount.Int32, LastBatchUUID: ptrFromNullString(row.LastBatchUuid),
			LastDispatchedAt: nullTimeToPtr(row.LastDispatchedAt), ConfirmedAt: nullTimeToPtr(row.ConfirmedAt),
			LastError: ptrFromNullString(row.LastError),
		})
	}
	return out, nil
}

func (s *SQLMinerChannelStore) UpsertDeviceConfigState(ctx context.Context, params models.UpsertDeviceConfigStateParams) error {
	if err := s.GetQueries(ctx).UpsertDeviceConfigState(ctx, sqlc.UpsertDeviceConfigStateParams{
		OrgID: params.OrgID, DeviceIdentifier: params.DeviceIdentifier, Dimension: string(params.Dimension),
		ObservedStateJsonb: params.ObservedStateJSON, ObservedStateHash: params.ObservedStateHash, ObservedAt: params.ObservedAt,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert device config state: %v", err)
	}
	return nil
}

func (s *SQLMinerChannelStore) UpsertConfigSupport(ctx context.Context, params models.ConfigEnforcementMutationParams) error {
	if err := s.GetQueries(ctx).UpsertConfigSupport(ctx, sqlc.UpsertConfigSupportParams{
		OrgID: params.OrgID, DeviceIdentifier: params.DeviceIdentifier, Dimension: string(params.Dimension),
		DesiredStateHash: nullableString(params.DesiredStateHash), Supported: sql.NullBool{Bool: params.Supported, Valid: true},
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert config support: %v", err)
	}
	return nil
}

func (s *SQLMinerChannelStore) ClaimConfigDispatch(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error) {
	rows, err := s.GetQueries(ctx).ClaimConfigDispatch(ctx, sqlc.ClaimConfigDispatchParams{
		OrgID: params.OrgID, DeviceIdentifier: params.DeviceIdentifier, Dimension: string(params.Dimension),
		DesiredStateHash: nullableString(params.DesiredStateHash), DispatchingBefore: params.DispatchingBefore,
	})
	return rows > 0, configMutationError("claim config dispatch", err)
}

func (s *SQLMinerChannelStore) MarkConfigDispatched(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkConfigDispatched(ctx, sqlc.MarkConfigDispatchedParams{
		OrgID: params.OrgID, DeviceIdentifier: params.DeviceIdentifier, Dimension: string(params.Dimension),
		DesiredStateHash: nullableString(params.DesiredStateHash), LastBatchUuid: nullableString(params.LastBatchUUID),
		LastDispatchedAt: nullableTime(params.LastDispatchedAt),
	})
	return rows > 0, configMutationError("mark config dispatched", err)
}

func (s *SQLMinerChannelStore) MarkConfigConfirmed(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkConfigConfirmed(ctx, sqlc.MarkConfigConfirmedParams{
		OrgID: params.OrgID, DeviceIdentifier: params.DeviceIdentifier, Dimension: string(params.Dimension),
		DesiredStateHash: nullableString(params.DesiredStateHash), ConfirmedAt: nullableTime(params.ConfirmedAt), ObservedAt: nullableTime(params.ObservedAt),
	})
	return rows > 0, configMutationError("mark config confirmed", err)
}

func (s *SQLMinerChannelStore) MarkConfigDrifted(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkConfigDrifted(ctx, sqlc.MarkConfigDriftedParams{
		OrgID: params.OrgID, DeviceIdentifier: params.DeviceIdentifier, Dimension: string(params.Dimension), ObservedAt: nullableTime(params.ObservedAt),
	})
	return rows > 0, configMutationError("mark config drifted", err)
}

func (s *SQLMinerChannelStore) MarkConfigDispatchFailure(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkConfigDispatchFailure(ctx, sqlc.MarkConfigDispatchFailureParams{
		OrgID: params.OrgID, DeviceIdentifier: params.DeviceIdentifier, Dimension: string(params.Dimension),
		DesiredStateHash: nullableString(params.DesiredStateHash), RetryState: string(params.State), LastError: nullableString(params.LastError), MaxRetries: params.MaxRetries,
	})
	return rows > 0, configMutationError("mark config dispatch failure", err)
}

func (s *SQLMinerChannelStore) MarkConfigDispatchHeld(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkConfigDispatchHeld(ctx, sqlc.MarkConfigDispatchHeldParams{
		OrgID: params.OrgID, DeviceIdentifier: params.DeviceIdentifier, Dimension: string(params.Dimension),
		DesiredStateHash: nullableString(params.DesiredStateHash), LastError: nullableString(params.LastError), LastDispatchedAt: nullableTime(params.LastDispatchedAt),
	})
	return rows > 0, configMutationError("mark config dispatch held", err)
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}
func nullBoolPtr(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	return &value.Bool
}
func nullableTime(value time.Time) sql.NullTime {
	return sql.NullTime{Time: value, Valid: !value.IsZero()}
}
func configMutationError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fleeterror.NewInternalErrorf("failed to %s: %v", action, err)
}

func (s *SQLMinerChannelStore) ListFirmwareEnforcementCandidates(ctx context.Context, orgID int64) ([]models.FirmwareEnforcementCandidate, error) {
	rows, err := s.GetQueries(ctx).ListFirmwareEnforcementCandidates(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list firmware enforcement candidates: %v", err)
	}
	out := make([]models.FirmwareEnforcementCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, firmwareEnforcementCandidateFromRow(row))
	}
	return out, nil
}

func (s *SQLMinerChannelStore) ClaimFirmwareDispatch(ctx context.Context, params models.ClaimFirmwareDispatchParams) (bool, error) {
	rows, err := s.GetQueries(ctx).ClaimFirmwareDispatch(ctx, sqlc.ClaimFirmwareDispatchParams{
		OrgID:                  params.OrgID,
		DeviceIdentifier:       params.DeviceIdentifier,
		DesiredFirmwareFileID:  sql.NullString{String: params.DesiredFirmwareFileID, Valid: params.DesiredFirmwareFileID != ""},
		DesiredFirmwareVersion: sql.NullString{String: params.DesiredFirmwareVersion, Valid: params.DesiredFirmwareVersion != ""},
		DispatchingBefore:      params.DispatchingBefore,
	})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to claim firmware dispatch: %v", err)
	}
	return rows > 0, nil
}

func (s *SQLMinerChannelStore) MarkFirmwareDispatched(ctx context.Context, params models.MarkFirmwareDispatchedParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkFirmwareDispatched(ctx, sqlc.MarkFirmwareDispatchedParams{
		OrgID:                  params.OrgID,
		DeviceIdentifier:       params.DeviceIdentifier,
		DesiredFirmwareFileID:  sql.NullString{String: params.DesiredFirmwareFileID, Valid: params.DesiredFirmwareFileID != ""},
		DesiredFirmwareVersion: sql.NullString{String: params.DesiredFirmwareVersion, Valid: params.DesiredFirmwareVersion != ""},
		LastBatchUuid:          sql.NullString{String: params.LastBatchUUID, Valid: params.LastBatchUUID != ""},
		LastDispatchedAt:       sql.NullTime{Time: params.LastDispatchedAt, Valid: !params.LastDispatchedAt.IsZero()},
	})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to mark firmware dispatched: %v", err)
	}
	return rows > 0, nil
}

func (s *SQLMinerChannelStore) MarkFirmwareConfirmed(ctx context.Context, params models.MarkFirmwareConfirmedParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkFirmwareConfirmed(ctx, sqlc.MarkFirmwareConfirmedParams{
		OrgID:                  params.OrgID,
		DeviceIdentifier:       params.DeviceIdentifier,
		DesiredFirmwareFileID:  sql.NullString{String: params.DesiredFirmwareFileID, Valid: params.DesiredFirmwareFileID != ""},
		DesiredFirmwareVersion: sql.NullString{String: params.DesiredFirmwareVersion, Valid: params.DesiredFirmwareVersion != ""},
		ConfirmedAt:            sql.NullTime{Time: params.ConfirmedAt, Valid: !params.ConfirmedAt.IsZero()},
		ObservedAt:             sql.NullTime{Time: params.ObservedAt, Valid: !params.ObservedAt.IsZero()},
	})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to mark firmware confirmed: %v", err)
	}
	return rows > 0, nil
}

func (s *SQLMinerChannelStore) MarkFirmwareDrifted(ctx context.Context, params models.MarkFirmwareDriftedParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkFirmwareDrifted(ctx, sqlc.MarkFirmwareDriftedParams{
		OrgID:            params.OrgID,
		DeviceIdentifier: params.DeviceIdentifier,
		ObservedAt:       sql.NullTime{Time: params.ObservedAt, Valid: !params.ObservedAt.IsZero()},
	})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to mark firmware drifted: %v", err)
	}
	return rows > 0, nil
}

func (s *SQLMinerChannelStore) MarkFirmwareDispatchFailure(ctx context.Context, params models.MarkFirmwareDispatchFailureParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkFirmwareDispatchFailure(ctx, sqlc.MarkFirmwareDispatchFailureParams{
		OrgID:                  params.OrgID,
		DeviceIdentifier:       params.DeviceIdentifier,
		DesiredFirmwareFileID:  sql.NullString{String: params.DesiredFirmwareFileID, Valid: params.DesiredFirmwareFileID != ""},
		DesiredFirmwareVersion: sql.NullString{String: params.DesiredFirmwareVersion, Valid: params.DesiredFirmwareVersion != ""},
		RetryState:             string(params.RetryState),
		LastError:              sql.NullString{String: params.LastError, Valid: params.LastError != ""},
		MaxRetries:             params.MaxRetries,
	})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to mark firmware dispatch failure: %v", err)
	}
	return rows > 0, nil
}

func (s *SQLMinerChannelStore) MarkFirmwareDispatchHeld(ctx context.Context, params models.MarkFirmwareDispatchHeldParams) (bool, error) {
	rows, err := s.GetQueries(ctx).MarkFirmwareDispatchHeld(ctx, sqlc.MarkFirmwareDispatchHeldParams{
		OrgID:                  params.OrgID,
		DeviceIdentifier:       params.DeviceIdentifier,
		DesiredFirmwareFileID:  sql.NullString{String: params.DesiredFirmwareFileID, Valid: params.DesiredFirmwareFileID != ""},
		DesiredFirmwareVersion: sql.NullString{String: params.DesiredFirmwareVersion, Valid: params.DesiredFirmwareVersion != ""},
		RetryState:             string(params.RetryState),
		LastError:              sql.NullString{String: params.LastError, Valid: params.LastError != ""},
	})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to mark firmware dispatch held: %v", err)
	}
	return rows > 0, nil
}

func (s *SQLMinerChannelStore) IsCommandBatchFinished(ctx context.Context, batchUUID string) (bool, error) {
	finished, err := s.GetQueries(ctx).IsBatchFinished(ctx, batchUUID)
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to read command batch status: %v", err)
	}
	return finished, nil
}

func (s *SQLMinerChannelStore) UpsertMinerChannelReconcilerHeartbeat(ctx context.Context, lastTickAt time.Time, lastTickUUID uuid.UUID, durationMS *int32, activeDeviceCount int32) error {
	if err := s.GetQueries(ctx).UpsertMinerChannelReconcilerHeartbeat(ctx, sqlc.UpsertMinerChannelReconcilerHeartbeatParams{
		LastTickAt:         lastTickAt,
		LastTickUuid:       lastTickUUID,
		LastTickDurationMs: ptrToNullInt32(durationMS),
		ActiveDeviceCount:  activeDeviceCount,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert miner channel reconciler heartbeat: %v", err)
	}
	return nil
}

func (s *SQLMinerChannelStore) getMinerChannelWithQueries(ctx context.Context, q sqlc.Querier, orgID, minerChannelID int64) (*models.MinerChannel, error) {
	row, err := q.GetMinerChannel(ctx, sqlc.GetMinerChannelParams{ID: minerChannelID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("MinerChannel %d not found.", minerChannelID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get miner channel: %v", err)
	}
	minerChannel := minerChannelFromGetRow(row)
	members, err := q.ListMinerChannelMembers(ctx, sqlc.ListMinerChannelMembersParams{
		MinerChannelID: minerChannel.ID,
		OrgID:          minerChannel.OrgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list miner channel members: %v", err)
	}
	minerChannel.Members = make([]models.MinerChannelMember, 0, len(members))
	for _, row := range members {
		minerChannel.Members = append(minerChannel.Members, memberFromRow(row))
	}
	targets, err := q.ListMinerChannelFirmwareTargets(ctx, sqlc.ListMinerChannelFirmwareTargetsParams{
		MinerChannelID: minerChannel.ID,
		OrgID:          minerChannel.OrgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list miner channel firmware targets: %v", err)
	}
	minerChannel.FirmwareTargets = make([]models.MinerChannelFirmwareTarget, 0, len(targets))
	for _, row := range targets {
		minerChannel.FirmwareTargets = append(minerChannel.FirmwareTargets, firmwareTargetFromRow(row))
	}
	if err := s.loadFirmwareStatusesForMinerChannels(ctx, q, minerChannel.OrgID, []*models.MinerChannel{&minerChannel}); err != nil {
		return nil, err
	}
	if err := s.loadConfigStatusesForMinerChannels(ctx, q, minerChannel.OrgID, []*models.MinerChannel{&minerChannel}); err != nil {
		return nil, err
	}
	return &minerChannel, nil
}

type minerChannelMemberPayload struct {
	DeviceIdentifier string `json:"device_identifier"`
}

func (s *SQLMinerChannelStore) buildSelectedMinerChannelMemberPayload(ctx context.Context, q sqlc.Querier, params models.CreateMinerChannelParams) (json.RawMessage, int64, error) {
	selector := params.DeviceSelector
	if selector == nil {
		return nil, 0, fleeterror.NewInternalError("miner channel device selector is nil")
	}
	rows, err := q.ListDefaultMinerChannelDevices(ctx, sqlc.ListDefaultMinerChannelDevicesParams{
		OrgID:            params.OrgID,
		LimitCount:       selector.Count,
		ProductFilterSet: selector.Product != nil,
		Product:          ptrToNullString(selector.Product),
		ModelFilterSet:   selector.Model != nil,
		Model:            ptrToNullString(selector.Model),
	})
	if err != nil {
		return nil, 0, fleeterror.NewInternalErrorf("failed to select default miner channel devices: %v", err)
	}
	if len(rows) < int(selector.Count) {
		return nil, 0, newDefaultMinerChannelAvailabilityError(len(rows), selector)
	}

	payload := make([]minerChannelMemberPayload, 0, len(rows))
	for _, deviceIdentifier := range rows {
		payload = append(payload, minerChannelMemberPayload{
			DeviceIdentifier: deviceIdentifier,
		})
	}
	encoded, err := encodeMinerChannelMemberPayload(payload)
	if err != nil {
		return nil, 0, err
	}
	return encoded, int64(len(payload)), nil
}

func (s *SQLMinerChannelStore) buildMinerChannelMemberPayloadForIdentifiers(ctx context.Context, q sqlc.Querier, orgID int64, deviceIdentifiers []string) (json.RawMessage, error) {
	rows, err := q.ListDeviceIdentifiersForMinerChannelMembership(ctx, sqlc.ListDeviceIdentifiersForMinerChannelMembershipParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to resolve miner channel membership devices: %v", err)
	}
	foundIdentifiers := make(map[string]struct{}, len(rows))
	for _, deviceIdentifier := range rows {
		foundIdentifiers[deviceIdentifier] = struct{}{}
	}
	if len(foundIdentifiers) != len(deviceIdentifiers) {
		return nil, fleeterror.NewNotFoundErrorf("Device %q not found.", firstMissingIdentifier(deviceIdentifiers, foundIdentifiers))
	}

	payload := make([]minerChannelMemberPayload, 0, len(deviceIdentifiers))
	for _, id := range deviceIdentifiers {
		payload = append(payload, minerChannelMemberPayload{
			DeviceIdentifier: id,
		})
	}
	return encodeMinerChannelMemberPayload(payload)
}

func encodeMinerChannelMemberPayload(payload []minerChannelMemberPayload) (json.RawMessage, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to encode miner channel member payload: %v", err)
	}
	return encoded, nil
}

func firstMissingIdentifier(deviceIdentifiers []string, found map[string]struct{}) string {
	for _, id := range deviceIdentifiers {
		if _, ok := found[id]; !ok {
			return id
		}
	}
	return ""
}

func mapMinerChannelInsertError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation {
		switch pgErr.ConstraintName {
		case minerChannelActiveLabelUniqueIndex:
			return fleeterror.NewAlreadyExistsError("An active miner channel with this label already exists.")
		case minerChannelIdempotencyUniqueIndex:
			return fleeterror.NewAlreadyExistsError("A miner channel with this idempotency key already exists.")
		}
		return fleeterror.NewAlreadyExistsError("MinerChannel already exists.")
	}
	return fleeterror.NewInternalErrorf("failed to create miner channel: %v", err)
}

func mapMinerChannelUpdateError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation {
		if pgErr.ConstraintName == minerChannelActiveLabelUniqueIndex {
			return fleeterror.NewAlreadyExistsError("An active miner channel with this label already exists.")
		}
		return fleeterror.NewAlreadyExistsError("MinerChannel already exists.")
	}
	return fleeterror.NewInternalErrorf("failed to update miner channel: %v", err)
}

func mapMinerChannelMembershipError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation &&
		pgErr.ConstraintName == minerChannelMembershipUniqueConstraint {
		return fleeterror.NewPlainError("One or more miners already belong to another miner channel.", connect.CodeAlreadyExists).WithCallerStackTrace()
	}
	return fleeterror.NewInternalErrorf("failed to write miner channel membership: %v", err)
}

func newDefaultMinerChannelAvailabilityError(available int, selector *models.MinerChannelDeviceSelector) error {
	requested := 0
	if selector != nil {
		requested = int(selector.Count)
	}
	return fleeterror.NewAlreadyExistsErrorf(
		"Only %s %s available in the default miner channel%s. Requested %s.",
		formatMinerCount(available),
		formatAvailabilityVerb(available),
		formatSelectorAvailabilityScope(selector),
		formatMinerCount(requested),
	)
}

func formatSelectorAvailabilityScope(selector *models.MinerChannelDeviceSelector) string {
	if selector == nil {
		return ""
	}
	target := formatSelectorTarget(selector)
	if target != "" {
		return " for " + target
	}
	return ""
}

func formatSelectorTarget(selector *models.MinerChannelDeviceSelector) string {
	if selector == nil {
		return ""
	}
	product := ""
	model := ""
	if selector.Product != nil {
		product = strings.TrimSpace(*selector.Product)
	}
	if selector.Model != nil {
		model = strings.TrimSpace(*selector.Model)
	}
	switch {
	case product != "" && model != "":
		return product + " " + model
	case product != "":
		return "product " + product
	case model != "":
		return "model " + model
	default:
		return ""
	}
}

func formatMinerCount(count int) string {
	if count == 1 {
		return "1 miner"
	}
	return fmt.Sprintf("%d miners", count)
}

func formatAvailabilityVerb(count int) string {
	if count == 1 {
		return "is"
	}
	return "are"
}

func normalizeMinerChannelPageSize(pageSize int32) int32 {
	if pageSize <= 0 {
		return defaultMinerChannelPageSize
	}
	if pageSize > maxMinerChannelPageSize {
		return maxMinerChannelPageSize
	}
	return pageSize
}

func encodeMinerChannelPageCursor(cursor minerChannelPageCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to encode miner channel page token: %v", err)
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

func decodeMinerChannelPageCursor(token string) (*minerChannelPageCursor, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("Invalid page token: %v", err)
	}
	var cursor minerChannelPageCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("Invalid page token: %v", err)
	}
	if cursor.UpdatedAt.IsZero() || cursor.ID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("Invalid page token.")
	}
	return &cursor, nil
}

func encodeMinerChannelDevicePageCursor(cursor minerChannelDevicePageCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to encode miner channel device page token: %v", err)
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

func decodeMinerChannelDevicePageCursor(token string) (*minerChannelDevicePageCursor, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("Invalid page token: %v", err)
	}
	var cursor minerChannelDevicePageCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("Invalid page token: %v", err)
	}
	if strings.TrimSpace(cursor.DisplayName) == "" || strings.TrimSpace(cursor.DeviceIdentifier) == "" {
		return nil, fleeterror.NewInvalidArgumentError("Invalid page token.")
	}
	return &cursor, nil
}

func nullTimeFromCursor(cursor *minerChannelPageCursor) sql.NullTime {
	if cursor == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: cursor.UpdatedAt, Valid: true}
}

func nullInt64FromCursor(cursor *minerChannelPageCursor) sql.NullInt64 {
	if cursor == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: cursor.ID, Valid: true}
}

func cursorDisplayName(cursor *minerChannelDevicePageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.DisplayName
}

func cursorDeviceIdentifier(cursor *minerChannelDevicePageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.DeviceIdentifier
}

func minerChannelAssignmentStrings(assignments []models.MinerChannelDeviceAssignment) []string {
	out := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		switch assignment {
		case models.MinerChannelDeviceAssignmentAvailable, models.MinerChannelDeviceAssignmentReserved:
			out = append(out, string(assignment))
		}
	}
	return out
}

func trimStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func int64Slice(values []int64) []int64 {
	if values == nil {
		return []int64{}
	}
	return values
}

func int32Count(count int64) int32 {
	if count > math.MaxInt32 {
		return math.MaxInt32
	}
	if count < 0 {
		return 0
	}
	return int32(count)
}

func rawMessageToNull(raw json.RawMessage) pqtype.NullRawMessage {
	if len(raw) == 0 {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: raw, Valid: true}
}

func rawMessageFromNull(raw pqtype.NullRawMessage) json.RawMessage {
	if !raw.Valid || len(raw.RawMessage) == 0 {
		return nil
	}
	return raw.RawMessage
}

func desiredConfigFromNull(raw pqtype.NullRawMessage) *models.MinerChannelDesiredConfig {
	config, _ := models.ParseMinerChannelDesiredConfig(rawMessageFromNull(raw))
	return config
}

func minerChannelFromGetRow(row sqlc.GetMinerChannelRow) models.MinerChannel {
	return models.MinerChannel{
		ID:                  row.ID,
		OrgID:               row.OrgID,
		Label:               row.Label,
		IsDefault:           row.IsDefault,
		OwnerUserID:         nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:       ptrFromNullString(row.OwnerUsername),
		ExpiresAt:           nullTimeToPtr(row.ExpiresAt),
		DesiredConfig:       desiredConfigFromNull(row.DesiredConfigJsonb),
		DesiredConfigJSON:   rawMessageFromNull(row.DesiredConfigJsonb),
		State:               models.MinerChannelState(row.State),
		Purpose:             row.Purpose,
		SourceActorType:     models.SourceActorType(row.SourceActorType),
		SourceActorID:       ptrFromNullString(row.SourceActorID),
		IdempotencyKey:      ptrFromNullString(row.IdempotencyKey),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ExplicitMemberCount: row.ExplicitMemberCount,
	}
}

func minerChannelFromListRow(row sqlc.ListMinerChannelsRow) models.MinerChannel {
	return models.MinerChannel{
		ID:                  row.ID,
		OrgID:               row.OrgID,
		Label:               row.Label,
		IsDefault:           row.IsDefault,
		OwnerUserID:         nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:       ptrFromNullString(row.OwnerUsername),
		ExpiresAt:           nullTimeToPtr(row.ExpiresAt),
		DesiredConfig:       desiredConfigFromNull(row.DesiredConfigJsonb),
		DesiredConfigJSON:   rawMessageFromNull(row.DesiredConfigJsonb),
		State:               models.MinerChannelState(row.State),
		Purpose:             row.Purpose,
		SourceActorType:     models.SourceActorType(row.SourceActorType),
		SourceActorID:       ptrFromNullString(row.SourceActorID),
		IdempotencyKey:      ptrFromNullString(row.IdempotencyKey),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ExplicitMemberCount: row.ExplicitMemberCount,
	}
}

func minerChannelFromOwnerRow(row sqlc.ListMinerChannelsByOwnerRow) models.MinerChannel {
	return models.MinerChannel{
		ID:                  row.ID,
		OrgID:               row.OrgID,
		Label:               row.Label,
		IsDefault:           row.IsDefault,
		OwnerUserID:         nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:       ptrFromNullString(row.OwnerUsername),
		ExpiresAt:           nullTimeToPtr(row.ExpiresAt),
		DesiredConfig:       desiredConfigFromNull(row.DesiredConfigJsonb),
		DesiredConfigJSON:   rawMessageFromNull(row.DesiredConfigJsonb),
		State:               models.MinerChannelState(row.State),
		Purpose:             row.Purpose,
		SourceActorType:     models.SourceActorType(row.SourceActorType),
		SourceActorID:       ptrFromNullString(row.SourceActorID),
		IdempotencyKey:      ptrFromNullString(row.IdempotencyKey),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ExplicitMemberCount: row.ExplicitMemberCount,
	}
}

func minerChannelFromResolvedRow(row sqlc.ResolveEffectiveMinerChannelForDeviceRow) models.MinerChannel {
	return models.MinerChannel{
		ID:                  row.ID,
		OrgID:               row.OrgID,
		Label:               row.Label,
		IsDefault:           row.IsDefault,
		OwnerUserID:         nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:       ptrFromNullString(row.OwnerUsername),
		ExpiresAt:           nullTimeToPtr(row.ExpiresAt),
		DesiredConfig:       desiredConfigFromNull(row.DesiredConfigJsonb),
		DesiredConfigJSON:   rawMessageFromNull(row.DesiredConfigJsonb),
		State:               models.MinerChannelState(row.State),
		Purpose:             row.Purpose,
		SourceActorType:     models.SourceActorType(row.SourceActorType),
		SourceActorID:       ptrFromNullString(row.SourceActorID),
		IdempotencyKey:      ptrFromNullString(row.IdempotencyKey),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ExplicitMemberCount: row.ExplicitMemberCount,
	}
}

func minerChannelFromRow(row sqlc.MinerChannel, explicitMemberCount int64) models.MinerChannel {
	return models.MinerChannel{
		ID:                  row.ID,
		OrgID:               row.OrgID,
		Label:               row.Label,
		IsDefault:           row.IsDefault,
		OwnerUserID:         nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:       ptrFromNullString(row.OwnerUsername),
		ExpiresAt:           nullTimeToPtr(row.ExpiresAt),
		DesiredConfig:       desiredConfigFromNull(row.DesiredConfigJsonb),
		DesiredConfigJSON:   rawMessageFromNull(row.DesiredConfigJsonb),
		State:               models.MinerChannelState(row.State),
		Purpose:             row.Purpose,
		SourceActorType:     models.SourceActorType(row.SourceActorType),
		SourceActorID:       ptrFromNullString(row.SourceActorID),
		IdempotencyKey:      ptrFromNullString(row.IdempotencyKey),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ExplicitMemberCount: explicitMemberCount,
	}
}

func minerChannelFromDeviceRow(row sqlc.ListMinerChannelDevicesRow) models.MinerChannel {
	return models.MinerChannel{
		ID:                  row.ID,
		OrgID:               row.OrgID,
		Label:               row.Label,
		IsDefault:           row.IsDefault,
		OwnerUserID:         nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:       ptrFromNullString(row.OwnerUsername),
		ExpiresAt:           nullTimeToPtr(row.ExpiresAt),
		DesiredConfig:       desiredConfigFromNull(row.DesiredConfigJsonb),
		DesiredConfigJSON:   rawMessageFromNull(row.DesiredConfigJsonb),
		State:               models.MinerChannelState(row.State),
		Purpose:             row.Purpose,
		SourceActorType:     models.SourceActorType(row.SourceActorType),
		SourceActorID:       ptrFromNullString(row.SourceActorID),
		IdempotencyKey:      ptrFromNullString(row.IdempotencyKey),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ExplicitMemberCount: row.ExplicitMemberCount,
	}
}

func ownershipFromRow(deviceIdentifier string, minerChannelID int64, ownerUserID sql.NullInt64, ownerUsername sql.NullString) models.MinerChannelDeviceOwnership {
	return models.MinerChannelDeviceOwnership{
		DeviceIdentifier: deviceIdentifier,
		MinerChannelID:   minerChannelID,
		OwnerUserID:      nullInt64ToPtr(ownerUserID),
		OwnerUsername:    ptrFromNullString(ownerUsername),
	}
}

func firmwareTargetFromRow(row sqlc.MinerChannelFirmwareTarget) models.MinerChannelFirmwareTarget {
	return models.MinerChannelFirmwareTarget{
		MinerChannelID: row.MinerChannelID,
		OrgID:          row.OrgID,
		Manufacturer:   row.Manufacturer,
		Model:          row.Model,
		FirmwareFileID: ptrFromNullString(row.FirmwareFileID),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func firmwareStatusFromMinerChannelRow(row sqlc.ListMinerChannelFirmwareStatusesRow) models.MinerChannelFirmwareStatus {
	return models.MinerChannelFirmwareStatus{
		DeviceIdentifier:       row.DeviceIdentifier,
		TargetFirmwareFileID:   nullStringValue(row.TargetFirmwareFileID),
		TargetFirmwareVersion:  nullStringValue(row.StateDesiredFirmwareVersion),
		CurrentFirmwareVersion: row.CurrentFirmwareVersion,
		State:                  models.MinerChannelFirmwareRolloutStateUnknown,
		RetryCount:             int32FromNull(row.RetryCount),
		LastError:              ptrFromNullString(row.LastError),
		LastDispatchedAt:       nullTimeToPtr(row.LastDispatchedAt),
		ConfirmedAt:            nullTimeToPtr(row.ConfirmedAt),
		ObservedAt:             nullTimeToPtr(row.FirmwareObservedAt),
		EnforcementState:       enforcementStateFromNull(row.EnforcementState),
		DeviceStatus:           row.DeviceStatus,
	}
}

func firmwareStatusFromDeviceRow(row sqlc.ListMinerChannelFirmwareStatusesForDevicesRow) models.MinerChannelFirmwareStatus {
	return models.MinerChannelFirmwareStatus{
		DeviceIdentifier:       row.DeviceIdentifier,
		TargetFirmwareFileID:   nullStringValue(row.TargetFirmwareFileID),
		TargetFirmwareVersion:  nullStringValue(row.StateDesiredFirmwareVersion),
		CurrentFirmwareVersion: row.CurrentFirmwareVersion,
		State:                  models.MinerChannelFirmwareRolloutStateUnknown,
		RetryCount:             int32FromNull(row.RetryCount),
		LastError:              ptrFromNullString(row.LastError),
		LastDispatchedAt:       nullTimeToPtr(row.LastDispatchedAt),
		ConfirmedAt:            nullTimeToPtr(row.ConfirmedAt),
		ObservedAt:             nullTimeToPtr(row.FirmwareObservedAt),
		EnforcementState:       enforcementStateFromNull(row.EnforcementState),
		DeviceStatus:           row.DeviceStatus,
	}
}

func enforcementStateFromNull(value sql.NullString) *models.EnforcementState {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	state := models.EnforcementState(value.String)
	return &state
}

func int32FromNull(value sql.NullInt32) int32 {
	if !value.Valid {
		return 0
	}
	return value.Int32
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func firmwareEnforcementCandidateFromRow(row sqlc.ListFirmwareEnforcementCandidatesRow) models.FirmwareEnforcementCandidate {
	state := ptrFromNullString(row.EnforcementState)
	var typedState *models.EnforcementState
	if state != nil {
		next := models.EnforcementState(*state)
		typedState = &next
	}
	retryCount := int32(0)
	if row.RetryCount.Valid {
		retryCount = row.RetryCount.Int32
	}
	return models.FirmwareEnforcementCandidate{
		OrgID:                       row.OrgID,
		DeviceIdentifier:            row.DeviceIdentifier,
		Manufacturer:                row.Manufacturer,
		Model:                       row.Model,
		MinerChannelID:              row.MinerChannelID,
		OwnerUserID:                 nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:               ptrFromNullString(row.OwnerUsername),
		ActorUserID:                 row.ActorUserID,
		ActorExternalUserID:         row.ActorExternalUserID,
		ActorUsername:               row.ActorUsername,
		FirmwareFileID:              row.FirmwareFileID.String,
		StateDesiredFirmwareFileID:  ptrFromNullString(row.StateDesiredFirmwareFileID),
		StateDesiredFirmwareVersion: ptrFromNullString(row.StateDesiredFirmwareVersion),
		ObservedFirmwareVersion:     ptrFromNullString(row.ObservedFirmwareVersion),
		FirmwareObservedAt:          nullTimeToPtr(row.FirmwareObservedAt),
		State:                       typedState,
		RetryCount:                  retryCount,
		LastBatchUUID:               ptrFromNullString(row.LastBatchUuid),
		LastDispatchedAt:            nullTimeToPtr(row.LastDispatchedAt),
		ConfirmedAt:                 nullTimeToPtr(row.ConfirmedAt),
		LastError:                   ptrFromNullString(row.LastError),
	}
}

func memberFromRow(row sqlc.ListMinerChannelMembersRow) models.MinerChannelMember {
	return models.MinerChannelMember{
		MinerChannelID:   row.MinerChannelID,
		OrgID:            row.OrgID,
		DeviceIdentifier: row.DeviceIdentifier,
		AddedAt:          row.AddedAt,
		Display:          displayFromColumns(row.DisplayName, row.WorkerName, row.Manufacturer, row.Model, row.IpAddress, row.SerialNumber, row.FirmwareVersion),
	}
}

func displayFromColumns(displayName, workerName, manufacturer, model, ipAddress, serialNumber, firmwareVersion string) models.MinerChannelDeviceDisplay {
	return models.MinerChannelDeviceDisplay{
		Name:            displayName,
		WorkerName:      workerName,
		Manufacturer:    manufacturer,
		Model:           model,
		IPAddress:       ipAddress,
		SerialNumber:    serialNumber,
		FirmwareVersion: firmwareVersion,
	}
}
