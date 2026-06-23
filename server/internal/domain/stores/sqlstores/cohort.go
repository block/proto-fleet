package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sqlc-dev/pqtype"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const (
	cohortMembershipUniqueConstraint = "uq_cohort_membership_one_per_device"
	cohortIdempotencyUniqueIndex     = "uq_cohort_idempotency"
)

var _ interfaces.CohortStore = &SQLCohortStore{}

type SQLCohortStore struct {
	SQLConnectionManager
}

func NewSQLCohortStore(conn *sql.DB) *SQLCohortStore {
	return &SQLCohortStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLCohortStore) CreateCohort(ctx context.Context, params models.CreateCohortParams) (*models.Cohort, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.Cohort, error) {
		row, err := q.CreateCohort(ctx, sqlc.CreateCohortParams{
			OrgID:                  params.OrgID,
			Label:                  params.Label,
			OwnerUserID:            ptrToNullInt64(params.OwnerUserID),
			OwnerUsername:          ptrToNullString(params.OwnerUsername),
			ExpiresAt:              ptrToNullTime(params.ExpiresAt),
			DesiredFirmwareChannel: ptrToNullString(params.DesiredFirmwareChannel),
			DesiredFirmwareFileID:  ptrToNullString(params.DesiredFirmwareFileID),
			DesiredConfigJsonb:     rawMessageToNull(params.DesiredConfigJSON),
			Purpose:                params.Purpose,
			SourceActorType:        string(params.SourceActorType),
			SourceActorID:          ptrToNullString(params.SourceActorID),
			IdempotencyKey:         ptrToNullString(params.IdempotencyKey),
		})
		if err != nil {
			return nil, mapCohortInsertError(err)
		}
		if len(params.DeviceIdentifiers) > 0 {
			payload, err := buildCohortMemberPayload(params.DeviceIdentifiers)
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to encode cohort member payload: %v", err)
			}
			inserted, err := q.BulkInsertCohortMemberships(ctx, sqlc.BulkInsertCohortMembershipsParams{
				CohortID:     row.ID,
				OrgID:        row.OrgID,
				MembersJsonb: payload,
			})
			if err != nil {
				return nil, mapCohortMembershipError(err)
			}
			if inserted != int64(len(params.DeviceIdentifiers)) {
				return nil, fleeterror.NewInternalErrorf("bulk insert wrote %d cohort members, expected %d", inserted, len(params.DeviceIdentifiers))
			}
		}
		return s.getCohortWithQueries(ctx, q, row.OrgID, row.ID)
	})
}

func (s *SQLCohortStore) GetCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error) {
	return s.getCohortWithQueries(ctx, s.GetQueries(ctx), orgID, cohortID)
}

func (s *SQLCohortStore) ListCohorts(ctx context.Context, params models.ListCohortsParams) ([]*models.Cohort, error) {
	rows, err := s.GetQueries(ctx).ListCohorts(ctx, sqlc.ListCohortsParams{
		OrgID:           params.OrgID,
		IncludeReleased: params.IncludeReleased,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list cohorts: %v", err)
	}
	out := make([]*models.Cohort, 0, len(rows))
	for _, row := range rows {
		cohort := cohortFromListRow(row)
		out = append(out, &cohort)
	}
	return out, nil
}

func (s *SQLCohortStore) ListCohortsByOwner(ctx context.Context, params models.ListCohortsByOwnerParams) ([]*models.Cohort, error) {
	rows, err := s.GetQueries(ctx).ListCohortsByOwner(ctx, sqlc.ListCohortsByOwnerParams{
		OrgID:           params.OrgID,
		OwnerUserID:     sql.NullInt64{Int64: params.OwnerUserID, Valid: true},
		IncludeReleased: params.IncludeReleased,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list owned cohorts: %v", err)
	}
	out := make([]*models.Cohort, 0, len(rows))
	for _, row := range rows {
		cohort := cohortFromOwnerRow(row)
		out = append(out, &cohort)
	}
	return out, nil
}

func (s *SQLCohortStore) ReleaseCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.Cohort, error) {
		row, err := q.ReleaseCohort(ctx, sqlc.ReleaseCohortParams{ID: cohortID, OrgID: orgID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fleeterror.NewNotFoundErrorf("cohort %d not found", cohortID)
			}
			return nil, fleeterror.NewInternalErrorf("failed to release cohort: %v", err)
		}
		if _, err := q.DeleteCohortMembershipsByCohort(ctx, sqlc.DeleteCohortMembershipsByCohortParams{
			CohortID: cohortID,
			OrgID:    orgID,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to clear cohort memberships: %v", err)
		}
		return s.getCohortWithQueries(ctx, q, row.OrgID, row.ID)
	})
}

func (s *SQLCohortStore) InsertCohortMember(ctx context.Context, params models.InsertCohortMemberParams) error {
	err := s.GetQueries(ctx).InsertCohortMembership(ctx, sqlc.InsertCohortMembershipParams{
		CohortID:         params.CohortID,
		OrgID:            params.OrgID,
		DeviceIdentifier: params.DeviceIdentifier,
		SiteID:           ptrToNullInt64(params.SiteID),
	})
	if err != nil {
		return mapCohortMembershipError(err)
	}
	return nil
}

func (s *SQLCohortStore) DeleteCohortMemberships(ctx context.Context, orgID, cohortID int64, deviceIdentifiers []string) (int64, error) {
	count, err := s.GetQueries(ctx).DeleteCohortMemberships(ctx, sqlc.DeleteCohortMembershipsParams{
		CohortID:          cohortID,
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to delete cohort memberships: %v", err)
	}
	return count, nil
}

func (s *SQLCohortStore) ListCohortMembers(ctx context.Context, orgID, cohortID int64) ([]models.CohortMember, error) {
	rows, err := s.GetQueries(ctx).ListCohortMembers(ctx, sqlc.ListCohortMembersParams{
		CohortID: cohortID,
		OrgID:    orgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list cohort members: %v", err)
	}
	out := make([]models.CohortMember, 0, len(rows))
	for _, row := range rows {
		out = append(out, memberFromRow(row))
	}
	return out, nil
}

func (s *SQLCohortStore) ResolveEffectiveCohortForDevice(ctx context.Context, orgID int64, deviceIdentifier string) (*models.Cohort, error) {
	row, err := s.GetQueries(ctx).ResolveEffectiveCohortForDevice(ctx, sqlc.ResolveEffectiveCohortForDeviceParams{
		OrgID:            orgID,
		DeviceIdentifier: deviceIdentifier,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device %q not found", deviceIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("failed to resolve effective cohort: %v", err)
	}
	cohort := cohortFromResolvedRow(row)
	return &cohort, nil
}

func (s *SQLCohortStore) ListDefaultCohortDevices(ctx context.Context, orgID int64) ([]models.DefaultCohortDevice, error) {
	rows, err := s.GetQueries(ctx).ListDefaultCohortDevices(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list default cohort devices: %v", err)
	}
	out := make([]models.DefaultCohortDevice, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.DefaultCohortDevice{
			DeviceIdentifier: row.DeviceIdentifier,
			SiteID:           nullInt64ToPtr(row.SiteID),
		})
	}
	return out, nil
}

func (s *SQLCohortStore) getCohortWithQueries(ctx context.Context, q *sqlc.Queries, orgID, cohortID int64) (*models.Cohort, error) {
	row, err := q.GetCohort(ctx, sqlc.GetCohortParams{ID: cohortID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("cohort %d not found", cohortID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get cohort: %v", err)
	}
	cohort := cohortFromGetRow(row)
	members, err := q.ListCohortMembers(ctx, sqlc.ListCohortMembersParams{
		CohortID: cohort.ID,
		OrgID:    cohort.OrgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list cohort members: %v", err)
	}
	cohort.Members = make([]models.CohortMember, 0, len(members))
	for _, row := range members {
		cohort.Members = append(cohort.Members, memberFromRow(row))
	}
	return &cohort, nil
}

type cohortMemberPayload struct {
	DeviceIdentifier string `json:"device_identifier"`
	SiteID           *int64 `json:"site_id"`
}

func buildCohortMemberPayload(deviceIdentifiers []string) (json.RawMessage, error) {
	payload := make([]cohortMemberPayload, 0, len(deviceIdentifiers))
	for _, id := range deviceIdentifiers {
		payload = append(payload, cohortMemberPayload{DeviceIdentifier: id})
	}
	return json.Marshal(payload)
}

func mapCohortInsertError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation {
		if pgErr.ConstraintName == cohortIdempotencyUniqueIndex {
			return fleeterror.NewAlreadyExistsError("cohort with this idempotency key already exists")
		}
		return fleeterror.NewAlreadyExistsError("cohort already exists")
	}
	return fleeterror.NewInternalErrorf("failed to create cohort: %v", err)
}

func mapCohortMembershipError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation &&
		pgErr.ConstraintName == cohortMembershipUniqueConstraint {
		return fleeterror.NewPlainError("one or more devices already belong to another cohort", connect.CodeAlreadyExists).WithCallerStackTrace()
	}
	return fleeterror.NewInternalErrorf("failed to write cohort membership: %v", err)
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
	return json.RawMessage(raw.RawMessage)
}

func cohortFromGetRow(row sqlc.GetCohortRow) models.Cohort {
	return models.Cohort{
		ID:                     row.ID,
		OrgID:                  row.OrgID,
		Label:                  row.Label,
		IsDefault:              row.IsDefault,
		OwnerUserID:            nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:          ptrFromNullString(row.OwnerUsername),
		ExpiresAt:              nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareChannel: ptrFromNullString(row.DesiredFirmwareChannel),
		DesiredFirmwareFileID:  ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:      rawMessageFromNull(row.DesiredConfigJsonb),
		State:                  models.CohortState(row.State),
		Purpose:                row.Purpose,
		SourceActorType:        models.SourceActorType(row.SourceActorType),
		SourceActorID:          ptrFromNullString(row.SourceActorID),
		IdempotencyKey:         ptrFromNullString(row.IdempotencyKey),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
		ExplicitMemberCount:    row.ExplicitMemberCount,
	}
}

func cohortFromListRow(row sqlc.ListCohortsRow) models.Cohort {
	return models.Cohort{
		ID:                     row.ID,
		OrgID:                  row.OrgID,
		Label:                  row.Label,
		IsDefault:              row.IsDefault,
		OwnerUserID:            nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:          ptrFromNullString(row.OwnerUsername),
		ExpiresAt:              nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareChannel: ptrFromNullString(row.DesiredFirmwareChannel),
		DesiredFirmwareFileID:  ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:      rawMessageFromNull(row.DesiredConfigJsonb),
		State:                  models.CohortState(row.State),
		Purpose:                row.Purpose,
		SourceActorType:        models.SourceActorType(row.SourceActorType),
		SourceActorID:          ptrFromNullString(row.SourceActorID),
		IdempotencyKey:         ptrFromNullString(row.IdempotencyKey),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
		ExplicitMemberCount:    row.ExplicitMemberCount,
	}
}

func cohortFromOwnerRow(row sqlc.ListCohortsByOwnerRow) models.Cohort {
	return models.Cohort{
		ID:                     row.ID,
		OrgID:                  row.OrgID,
		Label:                  row.Label,
		IsDefault:              row.IsDefault,
		OwnerUserID:            nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:          ptrFromNullString(row.OwnerUsername),
		ExpiresAt:              nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareChannel: ptrFromNullString(row.DesiredFirmwareChannel),
		DesiredFirmwareFileID:  ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:      rawMessageFromNull(row.DesiredConfigJsonb),
		State:                  models.CohortState(row.State),
		Purpose:                row.Purpose,
		SourceActorType:        models.SourceActorType(row.SourceActorType),
		SourceActorID:          ptrFromNullString(row.SourceActorID),
		IdempotencyKey:         ptrFromNullString(row.IdempotencyKey),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
		ExplicitMemberCount:    row.ExplicitMemberCount,
	}
}

func cohortFromResolvedRow(row sqlc.ResolveEffectiveCohortForDeviceRow) models.Cohort {
	return models.Cohort{
		ID:                     row.ID,
		OrgID:                  row.OrgID,
		Label:                  row.Label,
		IsDefault:              row.IsDefault,
		OwnerUserID:            nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:          ptrFromNullString(row.OwnerUsername),
		ExpiresAt:              nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareChannel: ptrFromNullString(row.DesiredFirmwareChannel),
		DesiredFirmwareFileID:  ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:      rawMessageFromNull(row.DesiredConfigJsonb),
		State:                  models.CohortState(row.State),
		Purpose:                row.Purpose,
		SourceActorType:        models.SourceActorType(row.SourceActorType),
		SourceActorID:          ptrFromNullString(row.SourceActorID),
		IdempotencyKey:         ptrFromNullString(row.IdempotencyKey),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
		ExplicitMemberCount:    row.ExplicitMemberCount,
	}
}

func memberFromRow(row sqlc.CohortMembership) models.CohortMember {
	return models.CohortMember{
		CohortID:         row.CohortID,
		OrgID:            row.OrgID,
		DeviceIdentifier: row.DeviceIdentifier,
		SiteID:           nullInt64ToPtr(row.SiteID),
		AddedAt:          row.AddedAt,
	}
}
