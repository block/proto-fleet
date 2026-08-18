package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	channel "github.com/block/proto-fleet/server/internal/domain/channel"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type SQLChannelEnforcementStore struct {
	conn *sql.DB
}

var (
	_ channel.AuthorityStore    = (*SQLChannelEnforcementStore)(nil)
	_ channel.DesiredStateStore = (*SQLChannelEnforcementStore)(nil)
	_ channel.EnforcementStore  = (*SQLChannelEnforcementStore)(nil)
)

func NewSQLChannelEnforcementStore(conn *sql.DB) *SQLChannelEnforcementStore {
	return &SQLChannelEnforcementStore{conn: conn}
}

func (s *SQLChannelEnforcementStore) CreateAuthority(
	ctx context.Context,
	params channel.CreateAuthorityParams,
) (channel.Authority, error) {
	row, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.ChannelFirmwareAuthority, error) {
		return q.CreateChannelFirmwareAuthority(ctx, sqlc.CreateChannelFirmwareAuthorityParams{
			ID:                 params.ID,
			OrgID:              params.OrgID,
			AuthorityType:      params.Type,
			AuthorityReference: params.Reference,
			CreatedByUserID:    params.CreatedByUserID,
		})
	})
	if err != nil {
		return channel.Authority{}, fmt.Errorf("create channel firmware authority: %w", err)
	}
	return authorityFromSQL(row), nil
}

func (s *SQLChannelEnforcementStore) AdvanceAuthorityRevision(
	ctx context.Context,
	authorityID uuid.UUID,
	orgID int64,
	expectedRevision int64,
) (channel.Authority, error) {
	row, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.ChannelFirmwareAuthority, error) {
		return q.AdvanceChannelFirmwareAuthorityRevision(ctx, sqlc.AdvanceChannelFirmwareAuthorityRevisionParams{
			AuthorityID:      authorityID,
			OrgID:            orgID,
			ExpectedRevision: expectedRevision,
		})
	})
	if errors.Is(err, sql.ErrNoRows) {
		return channel.Authority{}, channel.ErrCASConflict
	}
	if err != nil {
		return channel.Authority{}, fmt.Errorf("advance channel firmware authority revision: %w", err)
	}
	return authorityFromSQL(row), nil
}

func (s *SQLChannelEnforcementStore) HaltAuthority(
	ctx context.Context,
	authorityID uuid.UUID,
	orgID int64,
	expectedRevision int64,
) (channel.Authority, error) {
	row, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.ChannelFirmwareAuthority, error) {
		return q.HaltChannelFirmwareAuthority(ctx, sqlc.HaltChannelFirmwareAuthorityParams{
			AuthorityID:      authorityID,
			OrgID:            orgID,
			ExpectedRevision: expectedRevision,
		})
	})
	if errors.Is(err, sql.ErrNoRows) {
		return channel.Authority{}, channel.ErrCASConflict
	}
	if err != nil {
		return channel.Authority{}, fmt.Errorf("halt channel firmware authority: %w", err)
	}
	return authorityFromSQL(row), nil
}

func (s *SQLChannelEnforcementStore) CreateEnforcement(
	ctx context.Context,
	params channel.CreateEnforcementParams,
) (channel.Enforcement, error) {
	row, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.GetChannelFirmwareEnforcementRow, error) {
		created, err := q.CreateChannelFirmwareEnforcement(ctx, sqlc.CreateChannelFirmwareEnforcementParams{
			CauseType:         params.CauseType,
			CauseReference:    ptrToNullString(params.CauseReference),
			DeviceID:          params.DeviceID,
			ReleaseTargetID:   params.ReleaseTargetID,
			AuthorityID:       params.AuthorityID,
			OrgID:             params.OrgID,
			AuthorityRevision: params.AuthorityRevision,
		})
		if err != nil {
			return sqlc.GetChannelFirmwareEnforcementRow{}, err
		}
		return q.GetChannelFirmwareEnforcement(ctx, created.ID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return channel.Enforcement{}, channel.ErrCASConflict
	}
	if err != nil {
		return channel.Enforcement{}, fmt.Errorf("create channel firmware enforcement: %w", err)
	}
	return enforcementFromGetRow(row), nil
}

func (s *SQLChannelEnforcementStore) GetEnforcement(
	ctx context.Context,
	enforcementID int64,
) (channel.Enforcement, error) {
	row, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.GetChannelFirmwareEnforcementRow, error) {
		return q.GetChannelFirmwareEnforcement(ctx, enforcementID)
	})
	if err != nil {
		return channel.Enforcement{}, fmt.Errorf("get channel firmware enforcement: %w", err)
	}
	return enforcementFromGetRow(row), nil
}

func (s *SQLChannelEnforcementStore) ListForReconcile(
	ctx context.Context,
	limit int32,
) ([]channel.Enforcement, error) {
	rows, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) ([]sqlc.ListChannelFirmwareEnforcementsForReconcileRow, error) {
		return q.ListChannelFirmwareEnforcementsForReconcile(ctx, limit)
	})
	if err != nil {
		return nil, fmt.Errorf("list channel firmware enforcements: %w", err)
	}
	result := make([]channel.Enforcement, 0, len(rows))
	for _, row := range rows {
		result = append(result, enforcementFromListRow(row))
	}
	return result, nil
}

func (s *SQLChannelEnforcementStore) ListChannelManagedDeviceIdentifiers(
	ctx context.Context,
	orgID int64,
	deviceIdentifiers []string,
) ([]string, error) {
	rows, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) ([]string, error) {
		return q.ListChannelManagedDeviceIdentifiers(ctx, sqlc.ListChannelManagedDeviceIdentifiersParams{
			OrgID:             orgID,
			DeviceIdentifiers: deviceIdentifiers,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list channel-managed devices: %w", err)
	}
	return rows, nil
}

func (s *SQLChannelEnforcementStore) Claim(
	ctx context.Context,
	enforcement channel.Enforcement,
	commandBatchUUID string,
	claimedAt time.Time,
) (channel.Enforcement, error) {
	rows, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (int64, error) {
		return q.ClaimChannelFirmwareEnforcement(ctx, sqlc.ClaimChannelFirmwareEnforcementParams{
			CommandBatchUuid:  sql.NullString{String: commandBatchUUID, Valid: true},
			ClaimedAt:         sql.NullTime{Time: claimedAt, Valid: true},
			EnforcementID:     enforcement.ID,
			ExpectedRevision:  enforcement.Revision,
			AuthorityID:       enforcement.AuthorityID,
			OrgID:             enforcement.OrgID,
			AuthorityRevision: enforcement.AuthorityRevision,
		})
	})
	if err != nil {
		return channel.Enforcement{}, fmt.Errorf("claim channel firmware enforcement: %w", err)
	}
	if rows == 0 {
		return channel.Enforcement{}, channel.ErrCASConflict
	}
	enforcement.State = channel.EnforcementStateDispatching
	enforcement.CommandBatchUUID = commandBatchUUID
	enforcement.ClaimedAt = &claimedAt
	enforcement.HeldAt = nil
	enforcement.LastError = nil
	enforcement.Revision++
	return enforcement, nil
}

func (s *SQLChannelEnforcementStore) Hold(
	ctx context.Context,
	enforcement channel.Enforcement,
	reason string,
	heldAt time.Time,
) error {
	return s.requireTransition(ctx, "hold", func(q sqlc.Querier) (int64, error) {
		return q.HoldChannelFirmwareEnforcement(ctx, sqlc.HoldChannelFirmwareEnforcementParams{
			HeldAt:            sql.NullTime{Time: heldAt, Valid: true},
			Reason:            sql.NullString{String: reason, Valid: reason != ""},
			EnforcementID:     enforcement.ID,
			ExpectedRevision:  enforcement.Revision,
			ExpectedBatchUuid: emptyToNullString(enforcement.CommandBatchUUID),
		})
	})
}

func (s *SQLChannelEnforcementStore) ReturnPending(
	ctx context.Context,
	enforcement channel.Enforcement,
	reason string,
) error {
	return s.requireTransition(ctx, "return pending", func(q sqlc.Querier) (int64, error) {
		return q.ReturnChannelFirmwareEnforcementPending(ctx, sqlc.ReturnChannelFirmwareEnforcementPendingParams{
			Reason:            sql.NullString{String: reason, Valid: reason != ""},
			EnforcementID:     enforcement.ID,
			ExpectedRevision:  enforcement.Revision,
			ExpectedBatchUuid: emptyToNullString(enforcement.CommandBatchUUID),
		})
	})
}

func (s *SQLChannelEnforcementStore) MarkDispatched(
	ctx context.Context,
	enforcement channel.Enforcement,
	enqueuedAt time.Time,
) error {
	return s.requireTransition(ctx, "mark dispatched", func(q sqlc.Querier) (int64, error) {
		return q.MarkChannelFirmwareEnforcementDispatched(ctx, sqlc.MarkChannelFirmwareEnforcementDispatchedParams{
			EnqueuedAt:        sql.NullTime{Time: enqueuedAt, Valid: true},
			EnforcementID:     enforcement.ID,
			ExpectedRevision:  enforcement.Revision,
			ExpectedBatchUuid: emptyToNullString(enforcement.CommandBatchUUID),
		})
	})
}

func (s *SQLChannelEnforcementStore) CommandOutcome(
	ctx context.Context,
	enforcement channel.Enforcement,
) (channel.CommandOutcome, error) {
	row, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.GetChannelFirmwareCommandOutcomeRow, error) {
		return q.GetChannelFirmwareCommandOutcome(ctx, sqlc.GetChannelFirmwareCommandOutcomeParams{
			BatchUuid: enforcement.CommandBatchUUID,
			DeviceID:  enforcement.DeviceID,
		})
	})
	if errors.Is(err, sql.ErrNoRows) {
		return channel.CommandOutcome{Status: channel.CommandOutcomeMissing}, nil
	}
	if err != nil {
		return channel.CommandOutcome{}, fmt.Errorf("get channel firmware command outcome: %w", err)
	}
	outcome := channel.CommandOutcome{
		Status:      channel.CommandOutcomeStatus(strings.ToLower(string(row.Status))),
		CompletedAt: row.UpdatedAt,
	}
	if row.ErrorInfo.Valid {
		outcome.Error = row.ErrorInfo.String
	}
	return outcome, nil
}

func (s *SQLChannelEnforcementStore) MarkVerifying(
	ctx context.Context,
	enforcement channel.Enforcement,
	commandCompletedAt time.Time,
) error {
	return s.requireTransition(ctx, "mark verifying", func(q sqlc.Querier) (int64, error) {
		return q.MarkChannelFirmwareEnforcementVerifying(ctx, sqlc.MarkChannelFirmwareEnforcementVerifyingParams{
			CommandCompletedAt: sql.NullTime{Time: commandCompletedAt, Valid: true},
			EnforcementID:      enforcement.ID,
			ExpectedRevision:   enforcement.Revision,
			ExpectedBatchUuid:  emptyToNullString(enforcement.CommandBatchUUID),
		})
	})
}

func (s *SQLChannelEnforcementStore) RecordObservation(
	ctx context.Context,
	enforcement channel.Enforcement,
	observation channel.Observation,
) error {
	return s.requireTransition(ctx, "record observation", func(q sqlc.Querier) (int64, error) {
		params := sqlc.RecordChannelFirmwareObservationParams{
			FirmwareVersion:    sql.NullString{String: observation.FirmwareVersion, Valid: observation.FirmwareVersion != ""},
			FirmwareObservedAt: sql.NullTime{Time: observation.ObservedAt, Valid: !observation.ObservedAt.IsZero()},
			ObservationError:   sql.NullString{String: observation.Error, Valid: observation.Error != ""},
			EnforcementID:      enforcement.ID,
			ExpectedRevision:   enforcement.Revision,
			ExpectedBatchUuid:  emptyToNullString(enforcement.CommandBatchUUID),
		}
		if observation.HashrateHS != nil {
			params.HashrateHs = sql.NullFloat64{Float64: *observation.HashrateHS, Valid: true}
			params.HashingObservedAt = sql.NullTime{Time: observation.ObservedAt, Valid: true}
		}
		return q.RecordChannelFirmwareObservation(ctx, params)
	})
}

func (s *SQLChannelEnforcementStore) Confirm(
	ctx context.Context,
	enforcement channel.Enforcement,
	observation channel.Observation,
) error {
	if observation.HashrateHS == nil {
		return fmt.Errorf("confirm channel firmware enforcement: hashing observation is required")
	}
	return s.requireTransition(ctx, "confirm", func(q sqlc.Querier) (int64, error) {
		return q.ConfirmChannelFirmwareEnforcement(ctx, sqlc.ConfirmChannelFirmwareEnforcementParams{
			FirmwareVersion:   sql.NullString{String: observation.FirmwareVersion, Valid: true},
			ObservedAt:        sql.NullTime{Time: observation.ObservedAt, Valid: true},
			HashrateHs:        sql.NullFloat64{Float64: *observation.HashrateHS, Valid: true},
			ConfirmedAt:       sql.NullTime{Time: observation.ObservedAt, Valid: true},
			EnforcementID:     enforcement.ID,
			ExpectedRevision:  enforcement.Revision,
			ExpectedBatchUuid: emptyToNullString(enforcement.CommandBatchUUID),
		})
	})
}

func (s *SQLChannelEnforcementStore) MarkAttentionRequired(
	ctx context.Context,
	enforcement channel.Enforcement,
	reason string,
	at time.Time,
) error {
	return s.requireTransition(ctx, "mark attention required", func(q sqlc.Querier) (int64, error) {
		return q.MarkChannelFirmwareEnforcementAttentionRequired(ctx, sqlc.MarkChannelFirmwareEnforcementAttentionRequiredParams{
			AttentionRequiredAt: sql.NullTime{Time: at, Valid: true},
			Reason:              sql.NullString{String: reason, Valid: true},
			EnforcementID:       enforcement.ID,
			ExpectedRevision:    enforcement.Revision,
			ExpectedState:       string(enforcement.State),
			ExpectedBatchUuid:   emptyToNullString(enforcement.CommandBatchUUID),
		})
	})
}

func (s *SQLChannelEnforcementStore) requireTransition(
	ctx context.Context,
	name string,
	transition func(sqlc.Querier) (int64, error),
) error {
	rows, err := db.WithTransaction(ctx, s.conn, transition)
	if err != nil {
		return fmt.Errorf("%s channel firmware enforcement: %w", name, err)
	}
	if rows == 0 {
		return channel.ErrCASConflict
	}
	return nil
}

func authorityFromSQL(row sqlc.ChannelFirmwareAuthority) channel.Authority {
	return channel.Authority{
		ID:              row.ID,
		OrgID:           row.OrgID,
		Type:            row.AuthorityType,
		Reference:       row.AuthorityReference,
		Revision:        row.Revision,
		HaltedAt:        nullTimeToPtr(row.HaltedAt),
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func enforcementFromGetRow(row sqlc.GetChannelFirmwareEnforcementRow) channel.Enforcement {
	return enforcementFromSQL(sqlc.ChannelFirmwareEnforcement{
		ID:                          row.ID,
		OrgID:                       row.OrgID,
		DeviceID:                    row.DeviceID,
		DesiredReleaseSetID:         row.DesiredReleaseSetID,
		DesiredReleaseTargetID:      row.DesiredReleaseTargetID,
		DesiredFirmwareFileID:       row.DesiredFirmwareFileID,
		DesiredFirmwareVersion:      row.DesiredFirmwareVersion,
		CauseType:                   row.CauseType,
		CauseReference:              row.CauseReference,
		AuthorityID:                 row.AuthorityID,
		AuthorityRevision:           row.AuthorityRevision,
		State:                       row.State,
		AttemptCount:                row.AttemptCount,
		CommandBatchUuid:            row.CommandBatchUuid,
		Revision:                    row.Revision,
		DesiredAt:                   row.DesiredAt,
		HeldAt:                      row.HeldAt,
		ClaimedAt:                   row.ClaimedAt,
		EnqueuedAt:                  row.EnqueuedAt,
		CommandCompletedAt:          row.CommandCompletedAt,
		LastObservedFirmwareVersion: row.LastObservedFirmwareVersion,
		FirmwareObservedAt:          row.FirmwareObservedAt,
		LastObservedHashrateHs:      row.LastObservedHashrateHs,
		HashingObservedAt:           row.HashingObservedAt,
		ConfirmedAt:                 row.ConfirmedAt,
		AttentionRequiredAt:         row.AttentionRequiredAt,
		LastError:                   row.LastError,
		CreatedAt:                   row.CreatedAt,
		UpdatedAt:                   row.UpdatedAt,
	}, row.DeviceIdentifier, row.CreatedByUserID)
}

func enforcementFromListRow(row sqlc.ListChannelFirmwareEnforcementsForReconcileRow) channel.Enforcement {
	return enforcementFromSQL(sqlc.ChannelFirmwareEnforcement{
		ID:                          row.ID,
		OrgID:                       row.OrgID,
		DeviceID:                    row.DeviceID,
		DesiredReleaseSetID:         row.DesiredReleaseSetID,
		DesiredReleaseTargetID:      row.DesiredReleaseTargetID,
		DesiredFirmwareFileID:       row.DesiredFirmwareFileID,
		DesiredFirmwareVersion:      row.DesiredFirmwareVersion,
		CauseType:                   row.CauseType,
		CauseReference:              row.CauseReference,
		AuthorityID:                 row.AuthorityID,
		AuthorityRevision:           row.AuthorityRevision,
		State:                       row.State,
		AttemptCount:                row.AttemptCount,
		CommandBatchUuid:            row.CommandBatchUuid,
		Revision:                    row.Revision,
		DesiredAt:                   row.DesiredAt,
		HeldAt:                      row.HeldAt,
		ClaimedAt:                   row.ClaimedAt,
		EnqueuedAt:                  row.EnqueuedAt,
		CommandCompletedAt:          row.CommandCompletedAt,
		LastObservedFirmwareVersion: row.LastObservedFirmwareVersion,
		FirmwareObservedAt:          row.FirmwareObservedAt,
		LastObservedHashrateHs:      row.LastObservedHashrateHs,
		HashingObservedAt:           row.HashingObservedAt,
		ConfirmedAt:                 row.ConfirmedAt,
		AttentionRequiredAt:         row.AttentionRequiredAt,
		LastError:                   row.LastError,
		CreatedAt:                   row.CreatedAt,
		UpdatedAt:                   row.UpdatedAt,
	}, row.DeviceIdentifier, row.CreatedByUserID)
}

func enforcementFromSQL(
	row sqlc.ChannelFirmwareEnforcement,
	deviceIdentifier string,
	createdByUserID int64,
) channel.Enforcement {
	return channel.Enforcement{
		ID:                          row.ID,
		OrgID:                       row.OrgID,
		DeviceID:                    row.DeviceID,
		DeviceIdentifier:            deviceIdentifier,
		DesiredReleaseSetID:         row.DesiredReleaseSetID,
		DesiredReleaseTargetID:      row.DesiredReleaseTargetID,
		DesiredFirmwareFileID:       row.DesiredFirmwareFileID,
		DesiredFirmwareVersion:      row.DesiredFirmwareVersion,
		CauseType:                   row.CauseType,
		CauseReference:              nullStringToPtr(row.CauseReference),
		AuthorityID:                 row.AuthorityID,
		AuthorityRevision:           row.AuthorityRevision,
		State:                       channel.EnforcementState(row.State),
		AttemptCount:                row.AttemptCount,
		CommandBatchUUID:            row.CommandBatchUuid.String,
		Revision:                    row.Revision,
		DesiredAt:                   row.DesiredAt,
		HeldAt:                      nullTimeToPtr(row.HeldAt),
		ClaimedAt:                   nullTimeToPtr(row.ClaimedAt),
		EnqueuedAt:                  nullTimeToPtr(row.EnqueuedAt),
		CommandCompletedAt:          nullTimeToPtr(row.CommandCompletedAt),
		LastObservedFirmwareVersion: nullStringToPtr(row.LastObservedFirmwareVersion),
		FirmwareObservedAt:          nullTimeToPtr(row.FirmwareObservedAt),
		LastObservedHashrateHS:      nullFloat64ToPtr(row.LastObservedHashrateHs),
		HashingObservedAt:           nullTimeToPtr(row.HashingObservedAt),
		ConfirmedAt:                 nullTimeToPtr(row.ConfirmedAt),
		AttentionRequiredAt:         nullTimeToPtr(row.AttentionRequiredAt),
		LastError:                   nullStringToPtr(row.LastError),
		CreatedAt:                   row.CreatedAt,
		UpdatedAt:                   row.UpdatedAt,
		CreatedByUserID:             createdByUserID,
	}
}
