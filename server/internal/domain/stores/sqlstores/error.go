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

	componentID := sql.NullString{String: "", Valid: false}
	if errMsg.ComponentID != nil {
		componentID = sql.NullString{String: *errMsg.ComponentID, Valid: true}
	}

	componentType := sql.NullInt32{Int32: unsetDatabaseID, Valid: false}
	if errMsg.ComponentType != models.ComponentTypeUnspecified {
		componentType = sql.NullInt32{Int32: int32(errMsg.ComponentType), Valid: true} // #nosec G115 -- ComponentType enum values bounded (max 4), safe for int32
	}

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

	extra, err := json.Marshal(errMsg.VendorAttributes)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to marshal vendor attributes: %v", err)
	}

	if noOpenErrorExists {
		return s.insertNewError(ctx, q, orgID, deviceID, errMsg, componentID, componentType, extra)
	}

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

	dbError, err := q.GetErrorByID(ctx, sqlc.GetErrorByIDParams{
		ID:    existingError.ID,
		OrgID: orgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to fetch updated error: %v", err)
	}

	return toErrorMessage(&dbError, errMsg.DeviceID), nil
}

// GetErrorByErrorID retrieves a single error by its error_id (ULID).
func (s *SQLErrorStore) GetErrorByErrorID(ctx context.Context, orgID int64, errorID string) (*models.ErrorMessage, error) {
	q := s.getQueries(ctx)

	row, err := q.GetErrorByErrorID(ctx, sqlc.GetErrorByErrorIDParams{
		ErrorID: errorID,
		OrgID:   orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("error not found: %s", errorID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get error: %v", err)
	}

	return toErrorMessageFromRow(&row), nil
}

// errorFields holds the common database fields needed to construct an ErrorMessage.
// This struct abstracts the differences between sqlc.Error and sqlc.GetErrorByErrorIDRow.
type errorFields struct {
	ErrorID           string
	MinerError        int32
	CauseSummary      sql.NullString
	RecommendedAction sql.NullString
	Severity          int32
	FirstSeenAt       time.Time
	LastSeenAt        time.Time
	ClosedAt          sql.NullTime
	Extra             json.RawMessage
	ComponentID       sql.NullString
	ComponentType     sql.NullInt32
	Impact            sql.NullString
	Summary           string
	VendorCode        sql.NullString
	Firmware          sql.NullString
	DeviceIdentifier  string
}

// buildErrorMessage converts common error fields to a domain models.ErrorMessage.
// Design note: NULL database values for optional string fields (CauseSummary, RecommendedAction,
// Impact, VendorCode, Firmware) are intentionally converted to empty strings. This is symmetric
// with toNullString which treats empty strings as NULL when writing to the database.
func buildErrorMessage(fields errorFields) *models.ErrorMessage {
	var closedAt *time.Time
	if fields.ClosedAt.Valid {
		closedAt = &fields.ClosedAt.Time
	}

	var componentID *string
	if fields.ComponentID.Valid {
		componentID = &fields.ComponentID.String
	}

	var vendorAttrs map[string]string
	if len(fields.Extra) > 0 {
		if err := json.Unmarshal(fields.Extra, &vendorAttrs); err != nil {
			slog.Warn("failed to unmarshal vendor attributes", "error_id", fields.ErrorID, "error", err)
		}
	}

	var componentType models.ComponentType
	if fields.ComponentType.Valid {
		componentType = safeInt32ToComponentType(fields.ComponentType.Int32)
	} else {
		componentType = models.ComponentTypeUnspecified
	}

	return &models.ErrorMessage{
		ErrorID:           fields.ErrorID,
		MinerError:        safeInt32ToMinerError(fields.MinerError),
		CauseSummary:      fields.CauseSummary.String,
		RecommendedAction: fields.RecommendedAction.String,
		Severity:          safeInt32ToSeverity(fields.Severity),
		FirstSeenAt:       fields.FirstSeenAt,
		LastSeenAt:        fields.LastSeenAt,
		ClosedAt:          closedAt,
		VendorAttributes:  vendorAttrs,
		DeviceID:          fields.DeviceIdentifier,
		ComponentID:       componentID,
		ComponentType:     componentType,
		Impact:            fields.Impact.String,
		Summary:           fields.Summary,
		VendorCode:        fields.VendorCode.String,
		Firmware:          fields.Firmware.String,
	}
}

// toErrorMessage converts a sqlc.Error to a domain models.ErrorMessage.
func toErrorMessage(dbError *sqlc.Error, deviceIdentifier string) *models.ErrorMessage {
	return buildErrorMessage(errorFields{
		ErrorID:           dbError.ErrorID,
		MinerError:        dbError.MinerError,
		CauseSummary:      dbError.CauseSummary,
		RecommendedAction: dbError.RecommendedAction,
		Severity:          dbError.Severity,
		FirstSeenAt:       dbError.FirstSeenAt,
		LastSeenAt:        dbError.LastSeenAt,
		ClosedAt:          dbError.ClosedAt,
		Extra:             dbError.Extra,
		ComponentID:       dbError.ComponentID,
		ComponentType:     dbError.ComponentType,
		Impact:            dbError.Impact,
		Summary:           dbError.Summary,
		VendorCode:        dbError.VendorCode,
		Firmware:          dbError.Firmware,
		DeviceIdentifier:  deviceIdentifier,
	})
}

// toErrorMessageFromRow converts a GetErrorByErrorIDRow to a domain models.ErrorMessage.
func toErrorMessageFromRow(row *sqlc.GetErrorByErrorIDRow) *models.ErrorMessage {
	return buildErrorMessage(errorFields{
		ErrorID:           row.ErrorID,
		MinerError:        row.MinerError,
		CauseSummary:      row.CauseSummary,
		RecommendedAction: row.RecommendedAction,
		Severity:          row.Severity,
		FirstSeenAt:       row.FirstSeenAt,
		LastSeenAt:        row.LastSeenAt,
		ClosedAt:          row.ClosedAt,
		Extra:             row.Extra,
		ComponentID:       row.ComponentID,
		ComponentType:     row.ComponentType,
		Impact:            row.Impact,
		Summary:           row.Summary,
		VendorCode:        row.VendorCode,
		Firmware:          row.Firmware,
		DeviceIdentifier:  row.DeviceIdentifier,
	})
}

// safeInt32ToEnum converts int32 from DB to any enum type, returning the specified default for negative values.
// The generic type constraint ~uint allows this to work with any uint-based enum types.
func safeInt32ToEnum[T ~uint](val int32, defaultValue T) T {
	if val < minValidEnumValue {
		return defaultValue
	}
	return T(val) // #nosec G115 -- Validated non-negative; DB values come from our controlled inserts
}

// safeInt32ToMinerError converts int32 from DB to MinerError, returning Unspecified for negative values.
func safeInt32ToMinerError(val int32) models.MinerError {
	return safeInt32ToEnum(val, models.MinerErrorUnspecified)
}

// safeInt32ToSeverity converts int32 from DB to Severity, returning Unspecified for negative values.
func safeInt32ToSeverity(val int32) models.Severity {
	return safeInt32ToEnum(val, models.SeverityUnspecified)
}

// safeInt32ToComponentType converts int32 from DB to ComponentType, returning Unspecified for negative values.
func safeInt32ToComponentType(val int32) models.ComponentType {
	return safeInt32ToEnum(val, models.ComponentTypeUnspecified)
}

// toNullTime converts a *time.Time to sql.NullTime for database operations.
func toNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
