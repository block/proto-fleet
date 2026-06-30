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
	cohortActiveLabelUniqueIndex     = "uq_cohort_active_label_per_org"
	cohortIdempotencyUniqueIndex     = "uq_cohort_idempotency"
	maxDefaultCohortDeviceListLimit  = int32(2147483647)
	defaultCohortPageSize            = int32(50)
	maxCohortPageSize                = int32(500)
)

var _ interfaces.CohortStore = &SQLCohortStore{}

type SQLCohortStore struct {
	SQLConnectionManager
}

type cohortPageCursor struct {
	IsDefault bool      `json:"is_default,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	ID        int64     `json:"id"`
}

type cohortDevicePageCursor struct {
	DisplayName      string `json:"display_name"`
	DeviceIdentifier string `json:"device_identifier"`
}

func NewSQLCohortStore(conn *sql.DB) *SQLCohortStore {
	return &SQLCohortStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLCohortStore) CreateCohort(ctx context.Context, params models.CreateCohortParams) (*models.Cohort, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.Cohort, error) {
		row, err := q.CreateCohort(ctx, sqlc.CreateCohortParams{
			OrgID:                 params.OrgID,
			Label:                 params.Label,
			OwnerUserID:           ptrToNullInt64(params.OwnerUserID),
			OwnerUsername:         ptrToNullString(params.OwnerUsername),
			ExpiresAt:             ptrToNullTime(params.ExpiresAt),
			DesiredFirmwareFileID: ptrToNullString(params.DesiredFirmwareFileID),
			DesiredConfigJsonb:    rawMessageToNull(params.DesiredConfigJSON),
			Purpose:               params.Purpose,
			SourceActorType:       string(params.SourceActorType),
			SourceActorID:         ptrToNullString(params.SourceActorID),
			IdempotencyKey:        ptrToNullString(params.IdempotencyKey),
		})
		if err != nil {
			return nil, mapCohortInsertError(err)
		}
		switch {
		case params.DeviceSelector != nil:
			payload, selectedCount, err := s.buildSelectedCohortMemberPayload(ctx, q, params)
			if err != nil {
				return nil, err
			}
			inserted, err := q.BulkInsertCohortMemberships(ctx, sqlc.BulkInsertCohortMembershipsParams{
				CohortID:     row.ID,
				OrgID:        row.OrgID,
				MembersJsonb: payload,
			})
			if err != nil {
				return nil, mapCohortMembershipError(err)
			}
			if inserted != selectedCount {
				return nil, fleeterror.NewInternalErrorf("bulk insert wrote %d cohort members, expected %d", inserted, selectedCount)
			}
		case len(params.DeviceIdentifiers) > 0:
			payload, err := s.buildCohortMemberPayloadForIdentifiers(ctx, q, row.OrgID, params.DeviceIdentifiers)
			if err != nil {
				return nil, err
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
		cohort, err := s.getCohortWithQueries(ctx, q, row.OrgID, row.ID)
		if err != nil {
			return nil, err
		}
		if err := validateCohortSingleMinerType(cohort); err != nil {
			return nil, err
		}
		if err := validateCreateCohortFirmwareTarget(params, cohort); err != nil {
			return nil, err
		}
		if params.DesiredFirmwareFileID != nil {
			targetManufacturer := params.DesiredFirmwareTargetManufacturer
			targetModel := params.DesiredFirmwareTargetModel
			if strings.TrimSpace(targetManufacturer) == "" || strings.TrimSpace(targetModel) == "" {
				targetManufacturer, targetModel, err = cohortSingleMinerType(cohort)
				if err != nil {
					return nil, err
				}
			}
			if _, err := q.UpsertCohortFirmwareTarget(ctx, sqlc.UpsertCohortFirmwareTargetParams{
				CohortID:       row.ID,
				OrgID:          row.OrgID,
				Manufacturer:   targetManufacturer,
				Model:          targetModel,
				FirmwareFileID: ptrToNullString(params.DesiredFirmwareFileID),
			}); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to set cohort firmware target: %v", err)
			}
			return s.getCohortWithQueries(ctx, q, row.OrgID, row.ID)
		}
		return cohort, nil
	})
}

func validateCreateCohortFirmwareTarget(params models.CreateCohortParams, cohort *models.Cohort) error {
	if params.DesiredFirmwareFileID == nil {
		return nil
	}
	return validateCohortFirmwareTarget(params.DesiredFirmwareTargetManufacturer, params.DesiredFirmwareTargetModel, cohort)
}

func validateCohortSingleMinerType(cohort *models.Cohort) error {
	_, _, err := cohortSingleMinerType(cohort)
	return err
}

func validateCohortFirmwareTarget(targetManufacturer string, targetModel string, cohort *models.Cohort) error {
	targetManufacturer = strings.TrimSpace(targetManufacturer)
	targetModel = strings.TrimSpace(targetModel)
	if targetManufacturer == "" && targetModel == "" {
		return nil
	}
	memberManufacturer, memberModel, err := cohortSingleMinerType(cohort)
	if err != nil {
		return err
	}
	if memberManufacturer == "" && memberModel == "" {
		return nil
	}
	if memberManufacturer != targetManufacturer || memberModel != targetModel {
		return fleeterror.NewInvalidArgumentErrorf(
			"firmware target %s does not match cohort miner type %s",
			formatCohortMinerType(targetManufacturer, targetModel),
			formatCohortMinerType(memberManufacturer, memberModel),
		)
	}
	return nil
}

func cohortSingleMinerType(cohort *models.Cohort) (string, string, error) {
	if cohort == nil || len(cohort.Members) == 0 {
		return "", "", nil
	}
	var manufacturer string
	var model string
	for _, member := range cohort.Members {
		nextManufacturer := strings.TrimSpace(member.Display.Manufacturer)
		nextModel := strings.TrimSpace(member.Display.Model)
		if nextManufacturer == "" || nextModel == "" {
			return "", "", fleeterror.NewInvalidArgumentErrorf("cohort member %q is missing manufacturer or model", member.DeviceIdentifier)
		}
		if manufacturer == "" && model == "" {
			manufacturer = nextManufacturer
			model = nextModel
			continue
		}
		if nextManufacturer != manufacturer || nextModel != model {
			return "", "", fleeterror.NewInvalidArgumentError("cohort members must have a single manufacturer and model")
		}
	}
	return manufacturer, model, nil
}

func formatCohortMinerType(manufacturer, model string) string {
	manufacturer = strings.TrimSpace(manufacturer)
	model = strings.TrimSpace(model)
	switch {
	case manufacturer != "" && model != "":
		return manufacturer + " " + model
	case manufacturer != "":
		return manufacturer
	case model != "":
		return model
	default:
		return "unknown"
	}
}

func (s *SQLCohortStore) GetCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error) {
	return s.getCohortWithQueries(ctx, s.GetQueries(ctx), orgID, cohortID)
}

func (s *SQLCohortStore) ListCohorts(ctx context.Context, params models.ListCohortsParams) (models.PagedCohorts, error) {
	pageSize := normalizeCohortPageSize(params.PageSize)
	cursor, err := decodeCohortPageCursor(params.PageToken)
	if err != nil {
		return models.PagedCohorts{}, err
	}
	search := strings.TrimSpace(params.Search)
	q := s.GetQueries(ctx)
	rows, err := q.ListCohorts(ctx, sqlc.ListCohortsParams{
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
		return models.PagedCohorts{}, fleeterror.NewInternalErrorf("failed to list cohorts: %v", err)
	}
	total, err := q.CountCohorts(ctx, sqlc.CountCohortsParams{
		OrgID:           params.OrgID,
		IncludeReleased: params.IncludeReleased,
		SearchFilterSet: search != "",
		Search:          search,
	})
	if err != nil {
		return models.PagedCohorts{}, fleeterror.NewInternalErrorf("failed to count cohorts: %v", err)
	}
	var nextPageToken string
	if int32(len(rows)) > pageSize {
		last := rows[pageSize-1]
		nextPageToken, err = encodeCohortPageCursor(cohortPageCursor{
			IsDefault: last.IsDefault,
			UpdatedAt: last.UpdatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return models.PagedCohorts{}, err
		}
		rows = rows[:pageSize]
	}
	out := make([]*models.Cohort, 0, len(rows))
	for _, row := range rows {
		cohort := cohortFromListRow(row)
		out = append(out, &cohort)
	}
	return models.PagedCohorts{
		Cohorts:       out,
		NextPageToken: nextPageToken,
		TotalCount:    int32Count(total),
	}, nil
}

func (s *SQLCohortStore) ListCohortsByOwner(ctx context.Context, params models.ListCohortsByOwnerParams) (models.PagedCohorts, error) {
	pageSize := normalizeCohortPageSize(params.PageSize)
	cursor, err := decodeCohortPageCursor(params.PageToken)
	if err != nil {
		return models.PagedCohorts{}, err
	}
	search := strings.TrimSpace(params.Search)
	q := s.GetQueries(ctx)
	rows, err := q.ListCohortsByOwner(ctx, sqlc.ListCohortsByOwnerParams{
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
		return models.PagedCohorts{}, fleeterror.NewInternalErrorf("failed to list owned cohorts: %v", err)
	}
	total, err := q.CountCohortsByOwner(ctx, sqlc.CountCohortsByOwnerParams{
		OrgID:           params.OrgID,
		OwnerUserID:     sql.NullInt64{Int64: params.OwnerUserID, Valid: true},
		IncludeReleased: params.IncludeReleased,
		SearchFilterSet: search != "",
		Search:          search,
	})
	if err != nil {
		return models.PagedCohorts{}, fleeterror.NewInternalErrorf("failed to count owned cohorts: %v", err)
	}
	var nextPageToken string
	if int32(len(rows)) > pageSize {
		last := rows[pageSize-1]
		nextPageToken, err = encodeCohortPageCursor(cohortPageCursor{
			UpdatedAt: last.UpdatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return models.PagedCohorts{}, err
		}
		rows = rows[:pageSize]
	}
	out := make([]*models.Cohort, 0, len(rows))
	for _, row := range rows {
		cohort := cohortFromOwnerRow(row)
		out = append(out, &cohort)
	}
	return models.PagedCohorts{
		Cohorts:       out,
		NextPageToken: nextPageToken,
		TotalCount:    int32Count(total),
	}, nil
}

func (s *SQLCohortStore) UpdateCohort(ctx context.Context, params models.UpdateCohortParams) (*models.Cohort, error) {
	row, err := s.GetQueries(ctx).UpdateCohort(ctx, sqlc.UpdateCohortParams{
		ID:                       params.CohortID,
		OrgID:                    params.OrgID,
		Label:                    ptrToNullString(params.Label),
		Purpose:                  ptrToNullString(params.Purpose),
		ExpiresAt:                ptrToNullTime(params.ExpiresAt),
		ClearExpiresAt:           params.ClearExpiresAt,
		DesiredFirmwareFileID:    ptrToNullString(params.DesiredFirmwareFileID),
		DesiredFirmwareFileIDSet: params.DesiredFirmwareFileIDSet,
		DesiredConfigJsonb:       rawMessageToNull(params.DesiredConfigJSON),
		DesiredConfigJsonbSet:    params.DesiredConfigJSONSet,
		ClearDesiredConfig:       params.ClearDesiredConfig,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("active cohort %d not found", params.CohortID)
		}
		return nil, mapCohortUpdateError(err)
	}
	return s.getCohortWithQueries(ctx, s.GetQueries(ctx), row.OrgID, row.ID)
}

func (s *SQLCohortStore) UpdateDefaultCohortFirmware(ctx context.Context, params models.UpdateCohortParams) (*models.Cohort, error) {
	row, err := s.GetQueries(ctx).UpdateDefaultCohortFirmware(ctx, sqlc.UpdateDefaultCohortFirmwareParams{
		ID:                    params.CohortID,
		OrgID:                 params.OrgID,
		DesiredFirmwareFileID: ptrToNullString(params.DesiredFirmwareFileID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("active default cohort %d not found", params.CohortID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to update default cohort firmware: %v", err)
	}
	return s.getCohortWithQueries(ctx, s.GetQueries(ctx), row.OrgID, row.ID)
}

func (s *SQLCohortStore) SetCohortFirmwareTarget(ctx context.Context, params models.SetCohortFirmwareTargetParams) (*models.Cohort, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.Cohort, error) {
		target, err := q.GetCohort(ctx, sqlc.GetCohortParams{ID: params.CohortID, OrgID: params.OrgID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fleeterror.NewNotFoundErrorf("cohort %d not found", params.CohortID)
			}
			return nil, fleeterror.NewInternalErrorf("failed to get cohort: %v", err)
		}
		if models.CohortState(target.State) != models.CohortStateActive {
			return nil, fleeterror.NewInvalidArgumentErrorf("cohort %d is not active", params.CohortID)
		}

		if params.FirmwareFileID == nil {
			if _, err := q.DeleteCohortFirmwareTarget(ctx, sqlc.DeleteCohortFirmwareTargetParams{
				CohortID:     params.CohortID,
				OrgID:        params.OrgID,
				Manufacturer: params.Manufacturer,
				Model:        params.Model,
			}); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to clear cohort firmware target: %v", err)
			}
		} else if _, err := q.UpsertCohortFirmwareTarget(ctx, sqlc.UpsertCohortFirmwareTargetParams{
			CohortID:       params.CohortID,
			OrgID:          params.OrgID,
			Manufacturer:   params.Manufacturer,
			Model:          params.Model,
			FirmwareFileID: ptrToNullString(params.FirmwareFileID),
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to set cohort firmware target: %v", err)
		}

		if !target.IsDefault {
			if _, err := q.UpdateCohort(ctx, sqlc.UpdateCohortParams{
				ID:                       params.CohortID,
				OrgID:                    params.OrgID,
				DesiredFirmwareFileID:    ptrToNullString(params.FirmwareFileID),
				DesiredFirmwareFileIDSet: true,
			}); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, fleeterror.NewNotFoundErrorf("active cohort %d not found", params.CohortID)
				}
				return nil, fleeterror.NewInternalErrorf("failed to mirror cohort firmware target: %v", err)
			}
		}
		return s.getCohortWithQueries(ctx, q, params.OrgID, params.CohortID)
	})
}

func (s *SQLCohortStore) MoveDevicesToCohort(ctx context.Context, params models.MembershipMutationParams) (*models.Cohort, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.Cohort, error) {
		target, err := q.GetCohort(ctx, sqlc.GetCohortParams{ID: params.CohortID, OrgID: params.OrgID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fleeterror.NewNotFoundErrorf("cohort %d not found", params.CohortID)
			}
			return nil, fleeterror.NewInternalErrorf("failed to get target cohort: %v", err)
		}
		if target.IsDefault || models.CohortState(target.State) != models.CohortStateActive {
			return nil, fleeterror.NewInvalidArgumentErrorf("cohort %d is not an active non-default cohort", params.CohortID)
		}

		if _, err := q.DeleteCohortMembershipsByDevice(ctx, sqlc.DeleteCohortMembershipsByDeviceParams{
			OrgID:             params.OrgID,
			DeviceIdentifiers: params.DeviceIdentifiers,
		}); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to clear existing cohort memberships: %v", err)
		}
		payload, err := s.buildCohortMemberPayloadForIdentifiers(ctx, q, params.OrgID, params.DeviceIdentifiers)
		if err != nil {
			return nil, err
		}
		inserted, err := q.BulkInsertCohortMemberships(ctx, sqlc.BulkInsertCohortMembershipsParams{
			CohortID:     params.CohortID,
			OrgID:        params.OrgID,
			MembersJsonb: payload,
		})
		if err != nil {
			return nil, mapCohortMembershipError(err)
		}
		if inserted != int64(len(params.DeviceIdentifiers)) {
			return nil, fleeterror.NewInternalErrorf("bulk insert wrote %d cohort members, expected %d", inserted, len(params.DeviceIdentifiers))
		}
		cohort, err := s.getCohortWithQueries(ctx, q, params.OrgID, params.CohortID)
		if err != nil {
			return nil, err
		}
		if err := validateCohortSingleMinerType(cohort); err != nil {
			return nil, err
		}
		if err := validateCohortFirmwareTarget(params.DesiredFirmwareTargetManufacturer, params.DesiredFirmwareTargetModel, cohort); err != nil {
			return nil, err
		}
		return cohort, nil
	})
}

func (s *SQLCohortStore) RemoveDevicesAndGetCohort(ctx context.Context, params models.MembershipMutationParams) (*models.Cohort, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.Cohort, error) {
		if _, err := q.GetCohort(ctx, sqlc.GetCohortParams{ID: params.CohortID, OrgID: params.OrgID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fleeterror.NewNotFoundErrorf("cohort %d not found", params.CohortID)
			}
			return nil, fleeterror.NewInternalErrorf("failed to get cohort: %v", err)
		}
		matched, err := q.CountCohortMemberships(ctx, sqlc.CountCohortMembershipsParams{
			CohortID:          params.CohortID,
			OrgID:             params.OrgID,
			DeviceIdentifiers: params.DeviceIdentifiers,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to validate cohort memberships: %v", err)
		}
		if matched != int64(len(params.DeviceIdentifiers)) {
			return nil, fleeterror.NewNotFoundErrorf("found %d of %d requested cohort members", matched, len(params.DeviceIdentifiers))
		}
		deleted, err := q.DeleteCohortMemberships(ctx, sqlc.DeleteCohortMembershipsParams{
			CohortID:          params.CohortID,
			OrgID:             params.OrgID,
			DeviceIdentifiers: params.DeviceIdentifiers,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to delete cohort memberships: %v", err)
		}
		if deleted != int64(len(params.DeviceIdentifiers)) {
			return nil, fleeterror.NewInternalErrorf("deleted %d cohort members, expected %d", deleted, len(params.DeviceIdentifiers))
		}
		return s.getCohortWithQueries(ctx, q, params.OrgID, params.CohortID)
	})
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

func (s *SQLCohortStore) SweepExpiredCohorts(ctx context.Context) ([]*models.Cohort, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) ([]*models.Cohort, error) {
		expired, err := q.ListExpiredActiveCohorts(ctx)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to list expired cohorts: %v", err)
		}
		out := make([]*models.Cohort, 0, len(expired))
		for _, row := range expired {
			released, err := q.ReleaseCohort(ctx, sqlc.ReleaseCohortParams{ID: row.ID, OrgID: row.OrgID})
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to release expired cohort %d: %v", row.ID, err)
			}
			if _, err := q.DeleteCohortMembershipsByCohort(ctx, sqlc.DeleteCohortMembershipsByCohortParams{
				CohortID: row.ID,
				OrgID:    row.OrgID,
			}); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to clear expired cohort %d memberships: %v", row.ID, err)
			}
			cohort := cohortFromRow(released, 0)
			out = append(out, &cohort)
		}
		return out, nil
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
	rows, err := s.GetQueries(ctx).ListDefaultCohortDevices(ctx, sqlc.ListDefaultCohortDevicesParams{
		OrgID:      orgID,
		LimitCount: maxDefaultCohortDeviceListLimit,
	})
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

func (s *SQLCohortStore) ListCohortDeviceOwnership(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.CohortDeviceOwnership, error) {
	rows, err := s.GetQueries(ctx).ListCohortDeviceOwnership(ctx, sqlc.ListCohortDeviceOwnershipParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list cohort device ownership: %v", err)
	}
	out := make([]models.CohortDeviceOwnership, 0, len(rows))
	for _, row := range rows {
		out = append(out, ownershipFromRow(row.DeviceIdentifier, row.CohortID, row.OwnerUserID, row.OwnerUsername))
	}
	return out, nil
}

func (s *SQLCohortStore) ListActiveOwnedCohortMemberships(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.CohortDeviceOwnership, error) {
	rows, err := s.GetQueries(ctx).ListActiveOwnedCohortMemberships(ctx, sqlc.ListActiveOwnedCohortMembershipsParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list active owned cohort memberships: %v", err)
	}
	out := make([]models.CohortDeviceOwnership, 0, len(rows))
	for _, row := range rows {
		out = append(out, ownershipFromRow(row.DeviceIdentifier, row.CohortID, row.OwnerUserID, row.OwnerUsername))
	}
	return out, nil
}

func (s *SQLCohortStore) ListDevices(ctx context.Context, params models.ListDevicesParams) (models.PagedCohortDevices, error) {
	pageSize := normalizeCohortPageSize(params.PageSize)
	cursor, err := decodeCohortDevicePageCursor(params.PageToken)
	if err != nil {
		return models.PagedCohortDevices{}, err
	}
	search := strings.TrimSpace(params.Filter.Search)
	queryParams := sqlc.ListCohortDevicesParams{
		Assignments:            cohortAssignmentStrings(params.Filter.Assignments),
		CohortIds:              int64Slice(params.Filter.CohortIDs),
		OwnerUserIds:           int64Slice(params.Filter.OwnerUserIDs),
		IncludeUnowned:         params.Filter.IncludeUnowned,
		Manufacturers:          trimStrings(params.Filter.Manufacturers),
		Models:                 trimStrings(params.Filter.Models),
		SiteIds:                int64Slice(params.Filter.SiteIDs),
		IncludeUnassignedSite:  params.Filter.IncludeUnassignedSite,
		SearchFilterSet:        search != "",
		Search:                 search,
		CursorSet:              cursor != nil,
		CursorDisplayName:      cursorDisplayName(cursor),
		CursorDeviceIdentifier: cursorDeviceIdentifier(cursor),
		LimitCount:             pageSize + 1,
		OrgID:                  params.OrgID,
		LegacySiteIDFilterSet:  params.SiteID != nil,
		LegacySiteID:           ptrToNullInt64(params.SiteID),
	}
	q := s.GetQueries(ctx)
	rows, err := q.ListCohortDevices(ctx, queryParams)
	if err != nil {
		return models.PagedCohortDevices{}, fleeterror.NewInternalErrorf("failed to list cohort devices: %v", err)
	}
	counts, err := q.CountCohortDevices(ctx, sqlc.CountCohortDevicesParams{
		Assignments:           queryParams.Assignments,
		CohortIds:             queryParams.CohortIds,
		OwnerUserIds:          queryParams.OwnerUserIds,
		IncludeUnowned:        queryParams.IncludeUnowned,
		Manufacturers:         queryParams.Manufacturers,
		Models:                queryParams.Models,
		SiteIds:               queryParams.SiteIds,
		IncludeUnassignedSite: queryParams.IncludeUnassignedSite,
		SearchFilterSet:       queryParams.SearchFilterSet,
		Search:                queryParams.Search,
		OrgID:                 queryParams.OrgID,
		LegacySiteIDFilterSet: queryParams.LegacySiteIDFilterSet,
		LegacySiteID:          queryParams.LegacySiteID,
	})
	if err != nil {
		return models.PagedCohortDevices{}, fleeterror.NewInternalErrorf("failed to count cohort devices: %v", err)
	}
	var nextPageToken string
	if int32(len(rows)) > pageSize {
		last := rows[pageSize-1]
		nextPageToken, err = encodeCohortDevicePageCursor(cohortDevicePageCursor{
			DisplayName:      last.DisplayName,
			DeviceIdentifier: last.DeviceIdentifier,
		})
		if err != nil {
			return models.PagedCohortDevices{}, err
		}
		rows = rows[:pageSize]
	}
	out := make([]models.CohortDevice, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.CohortDevice{
			DeviceIdentifier: row.DeviceIdentifier,
			SiteID:           nullInt64ToPtr(row.DeviceSiteID),
			EffectiveCohort:  cohortFromDeviceRow(row),
			Display:          displayFromColumns(row.DisplayName, row.WorkerName, row.Manufacturer, row.Model, row.IpAddress, row.SerialNumber, row.SiteLabel),
		})
	}
	return models.PagedCohortDevices{
		Devices:        out,
		NextPageToken:  nextPageToken,
		TotalCount:     int32Count(counts.TotalCount),
		AvailableCount: int32Count(counts.AvailableCount),
		ReservedCount:  int32Count(counts.ReservedCount),
	}, nil
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
	targets, err := q.ListCohortFirmwareTargets(ctx, sqlc.ListCohortFirmwareTargetsParams{
		CohortID: cohort.ID,
		OrgID:    cohort.OrgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list cohort firmware targets: %v", err)
	}
	cohort.FirmwareTargets = make([]models.CohortFirmwareTarget, 0, len(targets))
	for _, row := range targets {
		cohort.FirmwareTargets = append(cohort.FirmwareTargets, firmwareTargetFromRow(row))
	}
	return &cohort, nil
}

type cohortMemberPayload struct {
	DeviceIdentifier string `json:"device_identifier"`
	SiteID           *int64 `json:"site_id"`
}

func (s *SQLCohortStore) buildSelectedCohortMemberPayload(ctx context.Context, q *sqlc.Queries, params models.CreateCohortParams) (json.RawMessage, int64, error) {
	selector := params.DeviceSelector
	if selector == nil {
		return nil, 0, fleeterror.NewInternalError("cohort device selector is nil")
	}
	rows, err := q.ListDefaultCohortDevices(ctx, sqlc.ListDefaultCohortDevicesParams{
		OrgID:            params.OrgID,
		LimitCount:       selector.Count,
		ProductFilterSet: selector.Product != nil,
		Product:          ptrToNullString(selector.Product),
		ModelFilterSet:   selector.Model != nil,
		Model:            ptrToNullString(selector.Model),
		SiteIDFilterSet:  selector.SiteID != nil,
		SiteID:           ptrToNullInt64(selector.SiteID),
	})
	if err != nil {
		return nil, 0, fleeterror.NewInternalErrorf("failed to select default cohort devices: %v", err)
	}
	if len(rows) < int(selector.Count) {
		return nil, 0, fleeterror.NewAlreadyExistsErrorf("only %d default-cohort rigs available%s, requested %d", len(rows), formatSelectorFilter(selector), selector.Count)
	}

	payload := make([]cohortMemberPayload, 0, len(rows))
	for _, row := range rows {
		payload = append(payload, cohortMemberPayload{
			DeviceIdentifier: row.DeviceIdentifier,
			SiteID:           nullInt64ToPtr(row.SiteID),
		})
	}
	encoded, err := encodeCohortMemberPayload(payload)
	if err != nil {
		return nil, 0, err
	}
	return encoded, int64(len(payload)), nil
}

func (s *SQLCohortStore) buildCohortMemberPayloadForIdentifiers(ctx context.Context, q *sqlc.Queries, orgID int64, deviceIdentifiers []string) (json.RawMessage, error) {
	rows, err := q.ListDeviceSitesForIdentifiers(ctx, sqlc.ListDeviceSitesForIdentifiersParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to resolve device sites: %v", err)
	}
	siteByIdentifier := make(map[string]*int64, len(rows))
	for _, row := range rows {
		siteByIdentifier[row.DeviceIdentifier] = nullInt64ToPtr(row.SiteID)
	}
	if len(siteByIdentifier) != len(deviceIdentifiers) {
		return nil, fleeterror.NewNotFoundErrorf("device %q not found", firstMissingIdentifier(deviceIdentifiers, siteByIdentifier))
	}

	payload := make([]cohortMemberPayload, 0, len(deviceIdentifiers))
	for _, id := range deviceIdentifiers {
		payload = append(payload, cohortMemberPayload{
			DeviceIdentifier: id,
			SiteID:           siteByIdentifier[id],
		})
	}
	return encodeCohortMemberPayload(payload)
}

func encodeCohortMemberPayload(payload []cohortMemberPayload) (json.RawMessage, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to encode cohort member payload: %v", err)
	}
	return encoded, nil
}

func firstMissingIdentifier(deviceIdentifiers []string, found map[string]*int64) string {
	for _, id := range deviceIdentifiers {
		if _, ok := found[id]; !ok {
			return id
		}
	}
	return ""
}

func mapCohortInsertError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation {
		switch pgErr.ConstraintName {
		case cohortActiveLabelUniqueIndex:
			return fleeterror.NewAlreadyExistsError("an active cohort with this label already exists")
		case cohortIdempotencyUniqueIndex:
			return fleeterror.NewAlreadyExistsError("cohort with this idempotency key already exists")
		}
		return fleeterror.NewAlreadyExistsError("cohort already exists")
	}
	return fleeterror.NewInternalErrorf("failed to create cohort: %v", err)
}

func mapCohortUpdateError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation {
		if pgErr.ConstraintName == cohortActiveLabelUniqueIndex {
			return fleeterror.NewAlreadyExistsError("an active cohort with this label already exists")
		}
		return fleeterror.NewAlreadyExistsError("cohort already exists")
	}
	return fleeterror.NewInternalErrorf("failed to update cohort: %v", err)
}

func mapCohortMembershipError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == db.PGUniqueViolation &&
		pgErr.ConstraintName == cohortMembershipUniqueConstraint {
		return fleeterror.NewPlainError("one or more devices already belong to another cohort", connect.CodeAlreadyExists).WithCallerStackTrace()
	}
	return fleeterror.NewInternalErrorf("failed to write cohort membership: %v", err)
}

func formatSelectorFilter(selector *models.CohortDeviceSelector) string {
	if selector == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if selector.Product != nil {
		parts = append(parts, fmt.Sprintf("product %q", *selector.Product))
	}
	if selector.Model != nil {
		parts = append(parts, fmt.Sprintf("model %q", *selector.Model))
	}
	if selector.SiteID != nil {
		parts = append(parts, fmt.Sprintf("site_id %d", *selector.SiteID))
	}
	if len(parts) == 0 {
		return ""
	}
	return " for " + strings.Join(parts, ", ")
}

func normalizeCohortPageSize(pageSize int32) int32 {
	if pageSize <= 0 {
		return defaultCohortPageSize
	}
	if pageSize > maxCohortPageSize {
		return maxCohortPageSize
	}
	return pageSize
}

func encodeCohortPageCursor(cursor cohortPageCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to encode cohort page token: %v", err)
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

func decodeCohortPageCursor(token string) (*cohortPageCursor, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid page token: %v", err)
	}
	var cursor cohortPageCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid page token: %v", err)
	}
	if cursor.UpdatedAt.IsZero() || cursor.ID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("invalid page token")
	}
	return &cursor, nil
}

func encodeCohortDevicePageCursor(cursor cohortDevicePageCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to encode cohort device page token: %v", err)
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

func decodeCohortDevicePageCursor(token string) (*cohortDevicePageCursor, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid page token: %v", err)
	}
	var cursor cohortDevicePageCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid page token: %v", err)
	}
	if strings.TrimSpace(cursor.DisplayName) == "" || strings.TrimSpace(cursor.DeviceIdentifier) == "" {
		return nil, fleeterror.NewInvalidArgumentError("invalid page token")
	}
	return &cursor, nil
}

func nullTimeFromCursor(cursor *cohortPageCursor) sql.NullTime {
	if cursor == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: cursor.UpdatedAt, Valid: true}
}

func nullInt64FromCursor(cursor *cohortPageCursor) sql.NullInt64 {
	if cursor == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: cursor.ID, Valid: true}
}

func cursorDisplayName(cursor *cohortDevicePageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.DisplayName
}

func cursorDeviceIdentifier(cursor *cohortDevicePageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.DeviceIdentifier
}

func cohortAssignmentStrings(assignments []models.CohortDeviceAssignment) []string {
	out := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		switch assignment {
		case models.CohortDeviceAssignmentAvailable, models.CohortDeviceAssignmentReserved:
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

func cohortFromGetRow(row sqlc.GetCohortRow) models.Cohort {
	return models.Cohort{
		ID:                    row.ID,
		OrgID:                 row.OrgID,
		Label:                 row.Label,
		IsDefault:             row.IsDefault,
		OwnerUserID:           nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:         ptrFromNullString(row.OwnerUsername),
		ExpiresAt:             nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareFileID: ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:     rawMessageFromNull(row.DesiredConfigJsonb),
		State:                 models.CohortState(row.State),
		Purpose:               row.Purpose,
		SourceActorType:       models.SourceActorType(row.SourceActorType),
		SourceActorID:         ptrFromNullString(row.SourceActorID),
		IdempotencyKey:        ptrFromNullString(row.IdempotencyKey),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		ExplicitMemberCount:   row.ExplicitMemberCount,
	}
}

func cohortFromListRow(row sqlc.ListCohortsRow) models.Cohort {
	return models.Cohort{
		ID:                    row.ID,
		OrgID:                 row.OrgID,
		Label:                 row.Label,
		IsDefault:             row.IsDefault,
		OwnerUserID:           nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:         ptrFromNullString(row.OwnerUsername),
		ExpiresAt:             nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareFileID: ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:     rawMessageFromNull(row.DesiredConfigJsonb),
		State:                 models.CohortState(row.State),
		Purpose:               row.Purpose,
		SourceActorType:       models.SourceActorType(row.SourceActorType),
		SourceActorID:         ptrFromNullString(row.SourceActorID),
		IdempotencyKey:        ptrFromNullString(row.IdempotencyKey),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		ExplicitMemberCount:   row.ExplicitMemberCount,
	}
}

func cohortFromOwnerRow(row sqlc.ListCohortsByOwnerRow) models.Cohort {
	return models.Cohort{
		ID:                    row.ID,
		OrgID:                 row.OrgID,
		Label:                 row.Label,
		IsDefault:             row.IsDefault,
		OwnerUserID:           nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:         ptrFromNullString(row.OwnerUsername),
		ExpiresAt:             nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareFileID: ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:     rawMessageFromNull(row.DesiredConfigJsonb),
		State:                 models.CohortState(row.State),
		Purpose:               row.Purpose,
		SourceActorType:       models.SourceActorType(row.SourceActorType),
		SourceActorID:         ptrFromNullString(row.SourceActorID),
		IdempotencyKey:        ptrFromNullString(row.IdempotencyKey),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		ExplicitMemberCount:   row.ExplicitMemberCount,
	}
}

func cohortFromResolvedRow(row sqlc.ResolveEffectiveCohortForDeviceRow) models.Cohort {
	return models.Cohort{
		ID:                    row.ID,
		OrgID:                 row.OrgID,
		Label:                 row.Label,
		IsDefault:             row.IsDefault,
		OwnerUserID:           nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:         ptrFromNullString(row.OwnerUsername),
		ExpiresAt:             nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareFileID: ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:     rawMessageFromNull(row.DesiredConfigJsonb),
		State:                 models.CohortState(row.State),
		Purpose:               row.Purpose,
		SourceActorType:       models.SourceActorType(row.SourceActorType),
		SourceActorID:         ptrFromNullString(row.SourceActorID),
		IdempotencyKey:        ptrFromNullString(row.IdempotencyKey),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		ExplicitMemberCount:   row.ExplicitMemberCount,
	}
}

func cohortFromRow(row sqlc.Cohort, explicitMemberCount int64) models.Cohort {
	return models.Cohort{
		ID:                    row.ID,
		OrgID:                 row.OrgID,
		Label:                 row.Label,
		IsDefault:             row.IsDefault,
		OwnerUserID:           nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:         ptrFromNullString(row.OwnerUsername),
		ExpiresAt:             nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareFileID: ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:     rawMessageFromNull(row.DesiredConfigJsonb),
		State:                 models.CohortState(row.State),
		Purpose:               row.Purpose,
		SourceActorType:       models.SourceActorType(row.SourceActorType),
		SourceActorID:         ptrFromNullString(row.SourceActorID),
		IdempotencyKey:        ptrFromNullString(row.IdempotencyKey),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		ExplicitMemberCount:   explicitMemberCount,
	}
}

func cohortFromDeviceRow(row sqlc.ListCohortDevicesRow) models.Cohort {
	return models.Cohort{
		ID:                    row.ID,
		OrgID:                 row.OrgID,
		Label:                 row.Label,
		IsDefault:             row.IsDefault,
		OwnerUserID:           nullInt64ToPtr(row.OwnerUserID),
		OwnerUsername:         ptrFromNullString(row.OwnerUsername),
		ExpiresAt:             nullTimeToPtr(row.ExpiresAt),
		DesiredFirmwareFileID: ptrFromNullString(row.DesiredFirmwareFileID),
		DesiredConfigJSON:     rawMessageFromNull(row.DesiredConfigJsonb),
		State:                 models.CohortState(row.State),
		Purpose:               row.Purpose,
		SourceActorType:       models.SourceActorType(row.SourceActorType),
		SourceActorID:         ptrFromNullString(row.SourceActorID),
		IdempotencyKey:        ptrFromNullString(row.IdempotencyKey),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		ExplicitMemberCount:   row.ExplicitMemberCount,
	}
}

func ownershipFromRow(deviceIdentifier string, cohortID int64, ownerUserID sql.NullInt64, ownerUsername sql.NullString) models.CohortDeviceOwnership {
	return models.CohortDeviceOwnership{
		DeviceIdentifier: deviceIdentifier,
		CohortID:         cohortID,
		OwnerUserID:      nullInt64ToPtr(ownerUserID),
		OwnerUsername:    ptrFromNullString(ownerUsername),
	}
}

func firmwareTargetFromRow(row sqlc.CohortFirmwareTarget) models.CohortFirmwareTarget {
	return models.CohortFirmwareTarget{
		CohortID:       row.CohortID,
		OrgID:          row.OrgID,
		Manufacturer:   row.Manufacturer,
		Model:          row.Model,
		FirmwareFileID: ptrFromNullString(row.FirmwareFileID),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func memberFromRow(row sqlc.ListCohortMembersRow) models.CohortMember {
	return models.CohortMember{
		CohortID:         row.CohortID,
		OrgID:            row.OrgID,
		DeviceIdentifier: row.DeviceIdentifier,
		SiteID:           nullInt64ToPtr(row.SiteID),
		AddedAt:          row.AddedAt,
		Display:          displayFromColumns(row.DisplayName, row.WorkerName, row.Manufacturer, row.Model, row.IpAddress, row.SerialNumber, row.SiteLabel),
	}
}

func displayFromColumns(displayName, workerName, manufacturer, model, ipAddress, serialNumber, siteLabel string) models.CohortDeviceDisplay {
	return models.CohortDeviceDisplay{
		Name:         displayName,
		WorkerName:   workerName,
		Manufacturer: manufacturer,
		Model:        model,
		IPAddress:    ipAddress,
		SerialNumber: serialNumber,
		SiteLabel:    siteLabel,
	}
}
