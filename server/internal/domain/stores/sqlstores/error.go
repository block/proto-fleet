package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/id"
)

const (
	// minValidEnumValue represents the minimum valid value for enum types
	minValidEnumValue = 0
	// unsetDatabaseID represents an uninitialized or invalid database ID
	unsetDatabaseID = 0
)

var _ interfaces.ErrorStore = &SQLErrorStore{}

type SQLErrorStore struct {
	SQLConnectionManager
}

func NewSQLErrorStore(conn *sql.DB) *SQLErrorStore {
	return &SQLErrorStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLErrorStore) getQueries(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

// UpsertError inserts a new error or updates an existing open error with the same dedup key.
func (s *SQLErrorStore) UpsertError(ctx context.Context, orgID int64, deviceIdentifier string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error) {
	q := s.getQueries(ctx)

	// Resolve device_identifier to device_id
	deviceID, err := q.GetDeviceIDByIdentifier(ctx, sqlc.GetDeviceIDByIdentifierParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %s", deviceIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("failed to resolve device identifier: %v", err)
	}

	// Prepare nullable fields for dedup lookup
	componentID := sql.NullString{String: "", Valid: false}
	if errMsg.ComponentID != nil {
		componentID = sql.NullString{String: *errMsg.ComponentID, Valid: true}
	}

	componentType := sql.NullInt32{Int32: unsetDatabaseID, Valid: false}
	if errMsg.ComponentType != models.ComponentTypeUnspecified {
		// #nosec G115 -- ComponentType enum values are bounded (max 4), safe for int32
		componentType = sql.NullInt32{Int32: int32(errMsg.ComponentType), Valid: true}
	}

	// Check for existing open error with same dedup key using NULL-safe equality (<=>)
	existingError, dbErr := q.GetOpenErrorByDedupKey(ctx, sqlc.GetOpenErrorByDedupKeyParams{
		OrgID:         orgID,
		DeviceID:      deviceID,
		MinerError:    int32(errMsg.MinerError), // #nosec G115 -- MinerError enum values bounded by protobuf (max ~9000)
		ComponentID:   componentID,
		ComponentType: componentType,
	})

	if dbErr != nil && !errors.Is(dbErr, sql.ErrNoRows) {
		return nil, fleeterror.NewInternalErrorf("failed to check for existing error: %v", dbErr)
	}
	noOpenErrorExists := errors.Is(dbErr, sql.ErrNoRows) || existingError.ID == unsetDatabaseID

	// Prepare extra JSON from VendorAttributes
	extra, err := json.Marshal(errMsg.VendorAttributes)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to marshal vendor attributes: %v", err)
	}

	if noOpenErrorExists {
		// No open error exists - insert new error
		return s.insertNewError(ctx, q, orgID, deviceID, errMsg, componentID, componentType, extra)
	}

	// Open error exists - update mutable fields
	return s.updateExistingError(ctx, q, orgID, &existingError, errMsg, extra)
}

func (s *SQLErrorStore) insertNewError(
	ctx context.Context,
	q *sqlc.Queries,
	orgID int64,
	deviceID int64,
	errMsg *models.ErrorMessage,
	componentID sql.NullString,
	componentType sql.NullInt32,
	extra json.RawMessage,
) (*models.ErrorMessage, error) {
	errorID := id.GenerateID()

	result, err := q.InsertError(ctx, sqlc.InsertErrorParams{
		ErrorID:           errorID,
		OrgID:             orgID,
		DeviceID:          deviceID,
		MinerError:        int32(errMsg.MinerError), // #nosec G115 -- MinerError enum bounded by protobuf
		Severity:          int32(errMsg.Severity),   // #nosec G115 -- Severity enum bounded (max 4)
		Summary:           errMsg.Summary,
		Impact:            toNullString(errMsg.Impact),
		CauseSummary:      toNullString(errMsg.CauseSummary),
		RecommendedAction: toNullString(errMsg.RecommendedAction),
		FirstSeenAt:       errMsg.FirstSeenAt,
		LastSeenAt:        errMsg.LastSeenAt,
		ComponentID:       componentID,
		ComponentType:     componentType,
		VendorCode:        toNullString(errMsg.VendorCode),
		Firmware:          toNullString(errMsg.Firmware),
		Extra:             extra,
		ClosedAt:          toNullTime(errMsg.ClosedAt),
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to insert error: %v", err)
	}

	insertedID, err := result.LastInsertId()
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get last insert ID: %v", err)
	}

	// Fetch and return the complete record
	dbError, err := q.GetErrorByID(ctx, sqlc.GetErrorByIDParams{
		ID:    insertedID,
		OrgID: orgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to fetch inserted error: %v", err)
	}

	return toErrorMessage(&dbError, errMsg.DeviceID), nil
}

func (s *SQLErrorStore) updateExistingError(
	ctx context.Context,
	q *sqlc.Queries,
	orgID int64,
	existingError *sqlc.Error,
	errMsg *models.ErrorMessage,
	extra json.RawMessage,
) (*models.ErrorMessage, error) {
	err := q.UpdateOpenError(ctx, sqlc.UpdateOpenErrorParams{
		LastSeenAt:        errMsg.LastSeenAt,
		Severity:          int32(errMsg.Severity), // #nosec G115 -- Severity enum values bounded (max 4)
		Summary:           errMsg.Summary,
		Impact:            toNullString(errMsg.Impact),
		CauseSummary:      toNullString(errMsg.CauseSummary),
		RecommendedAction: toNullString(errMsg.RecommendedAction),
		VendorCode:        toNullString(errMsg.VendorCode),
		Firmware:          toNullString(errMsg.Firmware),
		Extra:             extra,
		ClosedAt:          toNullTime(errMsg.ClosedAt),
		ID:                existingError.ID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to update error: %v", err)
	}

	// Fetch and return the updated record
	dbError, err := q.GetErrorByID(ctx, sqlc.GetErrorByIDParams{
		ID:    existingError.ID,
		OrgID: orgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to fetch updated error: %v", err)
	}

	return toErrorMessage(&dbError, errMsg.DeviceID), nil
}

// toErrorMessage converts a sqlc.Error to a domain models.ErrorMessage.
// Design note: NULL database values for optional string fields (CauseSummary, RecommendedAction,
// Impact, VendorCode, Firmware) are intentionally converted to empty strings. This is symmetric
// with toNullString which treats empty strings as NULL when writing to the database.
func toErrorMessage(dbError *sqlc.Error, deviceIdentifier string) *models.ErrorMessage {
	var closedAt *time.Time
	if dbError.ClosedAt.Valid {
		closedAt = &dbError.ClosedAt.Time
	}

	var componentID *string
	if dbError.ComponentID.Valid {
		componentID = &dbError.ComponentID.String
	}

	var vendorAttrs map[string]string
	if len(dbError.Extra) > 0 {
		if err := json.Unmarshal(dbError.Extra, &vendorAttrs); err != nil {
			slog.Warn("failed to unmarshal vendor attributes", "error_id", dbError.ErrorID, "error", err)
		}
	}

	return &models.ErrorMessage{
		ErrorID:           dbError.ErrorID,
		MinerError:        safeInt32ToMinerError(dbError.MinerError),
		CauseSummary:      dbError.CauseSummary.String,
		RecommendedAction: dbError.RecommendedAction.String,
		Severity:          safeInt32ToSeverity(dbError.Severity),
		FirstSeenAt:       dbError.FirstSeenAt,
		LastSeenAt:        dbError.LastSeenAt,
		ClosedAt:          closedAt,
		VendorAttributes:  vendorAttrs,
		DeviceID:          deviceIdentifier,
		ComponentID:       componentID,
		ComponentType:     safeInt32ToComponentType(dbError.ComponentType.Int32),
		Impact:            dbError.Impact.String,
		Summary:           dbError.Summary,
		VendorCode:        dbError.VendorCode.String,
		Firmware:          dbError.Firmware.String,
	}
}

// safeInt32ToMinerError converts int32 from DB to MinerError, returning Unspecified for negative values.
func safeInt32ToMinerError(val int32) models.MinerError {
	if val < minValidEnumValue {
		return models.MinerErrorUnspecified
	}
	// #nosec G115 -- Validated non-negative; DB values come from our controlled inserts
	return models.MinerError(val)
}

// safeInt32ToSeverity converts int32 from DB to Severity, returning Unspecified for negative values.
func safeInt32ToSeverity(val int32) models.Severity {
	if val < minValidEnumValue {
		return models.SeverityUnspecified
	}
	// #nosec G115 -- Validated non-negative; DB values come from our controlled inserts
	return models.Severity(val)
}

// safeInt32ToComponentType converts int32 from DB to ComponentType, returning Unspecified for negative values.
func safeInt32ToComponentType(val int32) models.ComponentType {
	if val < minValidEnumValue {
		return models.ComponentTypeUnspecified
	}
	// #nosec G115 -- Validated non-negative; DB values come from our controlled inserts
	return models.ComponentType(val)
}

// toNullTime converts a *time.Time to sql.NullTime for database operations.
func toNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
