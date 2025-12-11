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

// ============================================================================
// Query Methods
// ============================================================================

// QueryErrors retrieves errors with configurable filter logic (AND/OR).
func (s *SQLErrorStore) QueryErrors(ctx context.Context, opts *models.QueryOptions) ([]models.ErrorMessage, error) {
	q := s.getQueries(ctx)
	params := buildQueryParams(opts)

	rows, err := q.QueryErrors(ctx, params)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to query errors: %v", err)
	}

	return convertRowsToMessages(rows), nil
}

// CountErrors returns the total count of errors with configurable filter logic (AND/OR).
func (s *SQLErrorStore) CountErrors(ctx context.Context, opts *models.QueryOptions) (int64, error) {
	q := s.getQueries(ctx)
	params := buildCountParams(opts)

	count, err := q.CountErrors(ctx, params)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count errors: %v", err)
	}

	return count, nil
}

// ============================================================================
// Parameter Building Helpers
// ============================================================================

// filterParams is a generic holder for filter values that can be populated from QueryFilter.
// Filter flags use any to allow nil (SQL NULL) when filter is not active.
// When a filter is active, the flag is set to true; otherwise it remains nil.
type filterParams struct {
	DeviceFilter        any
	DeviceIdentifiers   []string
	DeviceTypeFilter    any
	DeviceTypes         []sql.NullString
	SeverityFilter      any
	Severities          []int32
	MinerErrorFilter    any
	MinerErrors         []int32
	ComponentTypeFilter any
	ComponentTypes      []sql.NullInt32
	ComponentIDFilter   any
	ComponentIds        []sql.NullString
}

// applyFilterToParams populates filter parameters from a QueryFilter.
// This extracts the common filter-building logic repeated 4 times in buildQueryParamsAND,
// buildQueryParamsOR, buildCountParamsAND, and buildCountParamsOR.
func applyFilterToParams(filter *models.QueryFilter) filterParams {
	var fp filterParams

	if len(filter.DeviceIdentifiers) > 0 {
		fp.DeviceFilter = true
		fp.DeviceIdentifiers = filter.DeviceIdentifiers
	}

	if len(filter.DeviceTypes) > 0 {
		fp.DeviceTypeFilter = true
		fp.DeviceTypes = stringsToNullStrings(filter.DeviceTypes)
	}

	if len(filter.Severities) > 0 {
		fp.SeverityFilter = true
		fp.Severities = severitiesToInt32s(filter.Severities)
	}

	if len(filter.MinerErrors) > 0 {
		fp.MinerErrorFilter = true
		fp.MinerErrors = minerErrorsToInt32s(filter.MinerErrors)
	}

	if len(filter.ComponentTypes) > 0 {
		fp.ComponentTypeFilter = true
		fp.ComponentTypes = componentTypesToNullInt32s(filter.ComponentTypes)
	}

	if len(filter.ComponentIDs) > 0 {
		fp.ComponentIDFilter = true
		fp.ComponentIds = stringsToNullStrings(filter.ComponentIDs)
	}

	return fp
}

// applyTimeFilter converts time filter values to sql.NullTime.
func applyTimeFilter(timeFrom, timeTo *time.Time) (sql.NullTime, sql.NullTime) {
	var fromNull, toNull sql.NullTime
	if timeFrom != nil {
		fromNull = sql.NullTime{Time: *timeFrom, Valid: true}
	}
	if timeTo != nil {
		toNull = sql.NullTime{Time: *timeTo, Valid: true}
	}
	return fromNull, toNull
}

// applyCursor converts page token to cursor parameters.
func applyCursor(pageToken string) (sql.NullInt32, sql.NullTime, sql.NullString) {
	cursor := parseCursor(pageToken)
	if cursor == nil {
		return sql.NullInt32{}, sql.NullTime{}, sql.NullString{}
	}
	return sql.NullInt32{Int32: int32(cursor.Severity), Valid: true}, // #nosec G115 -- Severity enum bounded (max 4)
		sql.NullTime{Time: cursor.LastSeenAt, Valid: true},
		sql.NullString{String: cursor.ErrorID, Valid: true}
}

// buildQueryParams converts QueryOptions to sqlc.QueryErrorsParams.
// TODO(DASH-1048): Re-add UseOrLogic parameter to support OR filter logic.
func buildQueryParams(opts *models.QueryOptions) sqlc.QueryErrorsParams {
	filter := opts.Filter
	if filter == nil {
		filter = &models.QueryFilter{}
	}

	params := sqlc.QueryErrorsParams{
		OrgID:         opts.OrgID,
		IncludeClosed: filter.IncludeClosed,
		Limit:         normalizeLimit(opts.PageSize),
	}

	params.TimeFrom, params.TimeTo = applyTimeFilter(filter.TimeFrom, filter.TimeTo)

	fp := applyFilterToParams(filter)
	params.DeviceFilter = fp.DeviceFilter
	params.DeviceIdentifiers = fp.DeviceIdentifiers
	params.DeviceTypeFilter = fp.DeviceTypeFilter
	params.DeviceTypes = fp.DeviceTypes
	params.SeverityFilter = fp.SeverityFilter
	params.Severities = fp.Severities
	params.MinerErrorFilter = fp.MinerErrorFilter
	params.MinerErrors = fp.MinerErrors
	params.ComponentTypeFilter = fp.ComponentTypeFilter
	params.ComponentTypes = fp.ComponentTypes
	params.ComponentIDFilter = fp.ComponentIDFilter
	params.ComponentIds = fp.ComponentIds

	params.CursorSeverity, params.CursorLastSeen, params.CursorErrorID = applyCursor(opts.PageToken)

	return params
}

// buildCountParams converts QueryOptions to sqlc.CountErrorsParams.
// TODO(DASH-1048): Re-add UseOrLogic parameter to support OR filter logic.
func buildCountParams(opts *models.QueryOptions) sqlc.CountErrorsParams {
	filter := opts.Filter
	if filter == nil {
		filter = &models.QueryFilter{}
	}

	params := sqlc.CountErrorsParams{
		OrgID:         opts.OrgID,
		IncludeClosed: filter.IncludeClosed,
	}

	params.TimeFrom, params.TimeTo = applyTimeFilter(filter.TimeFrom, filter.TimeTo)

	fp := applyFilterToParams(filter)
	params.DeviceFilter = fp.DeviceFilter
	params.DeviceIdentifiers = fp.DeviceIdentifiers
	params.DeviceTypeFilter = fp.DeviceTypeFilter
	params.DeviceTypes = fp.DeviceTypes
	params.SeverityFilter = fp.SeverityFilter
	params.Severities = fp.Severities
	params.MinerErrorFilter = fp.MinerErrorFilter
	params.MinerErrors = fp.MinerErrors
	params.ComponentTypeFilter = fp.ComponentTypeFilter
	params.ComponentTypes = fp.ComponentTypes
	params.ComponentIDFilter = fp.ComponentIDFilter
	params.ComponentIds = fp.ComponentIds

	return params
}

// ============================================================================
// Row Conversion Helpers
// ============================================================================

// convertRowsToMessages converts sqlc query results to domain models.
func convertRowsToMessages(rows []sqlc.QueryErrorsRow) []models.ErrorMessage {
	messages := make([]models.ErrorMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, queryRowToMessage(
			row.ErrorID,
			row.MinerError,
			row.Severity,
			row.Summary,
			row.Impact,
			row.CauseSummary,
			row.RecommendedAction,
			row.FirstSeenAt,
			row.LastSeenAt,
			row.ClosedAt,
			row.ComponentID,
			row.ComponentType,
			row.VendorCode,
			row.Firmware,
			row.Extra,
			row.DeviceIdentifier,
			row.DeviceType,
		))
	}
	return messages
}

// queryRowToMessage converts individual row fields to an ErrorMessage.
func queryRowToMessage(
	errorID string,
	minerError int32,
	severity int32,
	summary string,
	impact sql.NullString,
	causeSummary sql.NullString,
	recommendedAction sql.NullString,
	firstSeenAt time.Time,
	lastSeenAt time.Time,
	closedAt sql.NullTime,
	componentID sql.NullString,
	componentType sql.NullInt32,
	vendorCode sql.NullString,
	firmware sql.NullString,
	extra json.RawMessage,
	deviceIdentifier string,
	deviceType sql.NullString,
) models.ErrorMessage {
	var closedAtPtr *time.Time
	if closedAt.Valid {
		closedAtPtr = &closedAt.Time
	}

	var componentIDPtr *string
	if componentID.Valid {
		componentIDPtr = &componentID.String
	}

	var vendorAttrs map[string]string
	if len(extra) > 0 {
		if err := json.Unmarshal(extra, &vendorAttrs); err != nil {
			slog.Warn("failed to unmarshal vendor attributes", "error_id", errorID, "error", err)
		}
	}

	return models.ErrorMessage{
		ErrorID:           errorID,
		MinerError:        safeInt32ToMinerError(minerError),
		Severity:          safeInt32ToSeverity(severity),
		Summary:           summary,
		Impact:            impact.String,
		CauseSummary:      causeSummary.String,
		RecommendedAction: recommendedAction.String,
		FirstSeenAt:       firstSeenAt,
		LastSeenAt:        lastSeenAt,
		ClosedAt:          closedAtPtr,
		DeviceID:          deviceIdentifier,
		DeviceType:        deviceType.String,
		ComponentID:       componentIDPtr,
		ComponentType:     safeInt32ToComponentType(componentType.Int32),
		VendorCode:        vendorCode.String,
		Firmware:          firmware.String,
		VendorAttributes:  vendorAttrs,
	}
}

// ============================================================================
// Type Conversion Helpers
// ============================================================================

// normalizeLimit ensures page size is within valid bounds.
func normalizeLimit(pageSize int) int32 {
	if pageSize <= 0 {
		return int32(models.DefaultPageSize)
	}
	if pageSize > models.MaxPageSize {
		return int32(models.MaxPageSize)
	}
	return int32(pageSize) // #nosec G115 -- Validated within bounds (1-1000)
}

// stringsToNullStrings converts a string slice to sql.NullString slice.
func stringsToNullStrings(strs []string) []sql.NullString {
	result := make([]sql.NullString, len(strs))
	for i, s := range strs {
		result[i] = sql.NullString{String: s, Valid: true}
	}
	return result
}

// severitiesToInt32s converts Severity slice to int32 slice.
func severitiesToInt32s(severities []models.Severity) []int32 {
	result := make([]int32, len(severities))
	for i, s := range severities {
		result[i] = int32(s) // #nosec G115 -- Severity enum bounded (max 4)
	}
	return result
}

// minerErrorsToInt32s converts MinerError slice to int32 slice.
func minerErrorsToInt32s(errors []models.MinerError) []int32 {
	result := make([]int32, len(errors))
	for i, e := range errors {
		result[i] = int32(e) // #nosec G115 -- MinerError enum bounded by protobuf (max ~9000)
	}
	return result
}

// componentTypesToNullInt32s converts ComponentType slice to sql.NullInt32 slice.
func componentTypesToNullInt32s(types []models.ComponentType) []sql.NullInt32 {
	result := make([]sql.NullInt32, len(types))
	for i, t := range types {
		result[i] = sql.NullInt32{Int32: int32(t), Valid: true} // #nosec G115 -- ComponentType enum bounded (max 4)
	}
	return result
}

// parseCursor decodes a page token into cursor components.
// Returns nil if token is empty or invalid.
func parseCursor(token string) *models.PageCursor {
	if token == "" {
		return nil
	}

	cursor, err := models.DecodeCursor(token)
	if err != nil {
		slog.Warn("failed to decode cursor", "error", err)
		return nil
	}

	return cursor
}

// ============================================================================
// Entity-Based Pagination Methods
// ============================================================================

// QueryDeviceKeys retrieves distinct device keys (ID + worst severity) with errors matching filter criteria.
func (s *SQLErrorStore) QueryDeviceKeys(ctx context.Context, opts *models.QueryOptions) ([]models.DeviceKey, error) {
	q := s.getQueries(ctx)
	params := buildDeviceQueryParams(opts)

	rows, err := q.QueryDeviceIDsWithErrors(ctx, params)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to query device keys: %v", err)
	}

	keys := make([]models.DeviceKey, len(rows))
	for i, row := range rows {
		// WorstSeverity from MIN() aggregate returns interface{}, extract as int32
		var worstSeverity models.Severity
		if severity, ok := row.WorstSeverity.(int64); ok {
			worstSeverity = safeInt32ToEnum(int32(severity), models.SeverityUnspecified) // #nosec G115 -- Severity bounded 0-4
		}
		keys[i] = models.DeviceKey{
			DeviceID:         row.DeviceID,
			DeviceIdentifier: row.DeviceIdentifier,
			WorstSeverity:    worstSeverity,
		}
	}
	return keys, nil
}

// CountDevices returns the count of distinct devices with errors matching filter criteria.
func (s *SQLErrorStore) CountDevices(ctx context.Context, opts *models.QueryOptions) (int64, error) {
	q := s.getQueries(ctx)
	params := buildDeviceCountParams(opts)

	count, err := q.CountDevicesWithErrors(ctx, params)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count devices: %v", err)
	}
	return count, nil
}

// QueryComponentKeys retrieves distinct (device_id, component_id) pairs with worst severity.
func (s *SQLErrorStore) QueryComponentKeys(ctx context.Context, opts *models.QueryOptions) ([]models.ComponentKey, error) {
	q := s.getQueries(ctx)
	params := buildComponentQueryParams(opts)

	rows, err := q.QueryComponentKeysWithErrors(ctx, params)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to query component keys: %v", err)
	}

	keys := make([]models.ComponentKey, len(rows))
	for i, row := range rows {
		var componentID *string
		if row.ComponentID.Valid {
			componentID = &row.ComponentID.String
		}
		// WorstSeverity from MIN() aggregate returns interface{}, extract as int32
		var worstSeverity models.Severity
		if severity, ok := row.WorstSeverity.(int64); ok {
			worstSeverity = safeInt32ToEnum(int32(severity), models.SeverityUnspecified) // #nosec G115 -- Severity bounded 0-4
		}
		keys[i] = models.ComponentKey{
			DeviceID:         row.DeviceID,
			DeviceIdentifier: row.DeviceIdentifier,
			ComponentID:      componentID,
			WorstSeverity:    worstSeverity,
		}
	}
	return keys, nil
}

// CountComponents returns the count of distinct components with errors matching filter criteria.
func (s *SQLErrorStore) CountComponents(ctx context.Context, opts *models.QueryOptions) (int64, error) {
	q := s.getQueries(ctx)
	params := buildComponentCountParams(opts)

	count, err := q.CountComponentsWithErrors(ctx, params)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count components: %v", err)
	}
	return count, nil
}

// ============================================================================
// Entity-Based Parameter Builders
// ============================================================================

// buildDeviceQueryParams converts QueryOptions to sqlc.QueryDeviceIDsWithErrorsParams.
// TODO(DASH-1048): Re-add UseOrLogic parameter to support OR filter logic.
func buildDeviceQueryParams(opts *models.QueryOptions) sqlc.QueryDeviceIDsWithErrorsParams {
	filter := opts.Filter
	if filter == nil {
		filter = &models.QueryFilter{}
	}

	params := sqlc.QueryDeviceIDsWithErrorsParams{
		OrgID:         opts.OrgID,
		IncludeClosed: filter.IncludeClosed,
		Limit:         normalizeLimit(opts.PageSize),
	}

	params.TimeFrom, params.TimeTo = applyTimeFilter(filter.TimeFrom, filter.TimeTo)

	fp := applyFilterToParams(filter)
	params.DeviceFilter = fp.DeviceFilter
	params.DeviceIdentifiers = fp.DeviceIdentifiers
	params.DeviceTypeFilter = fp.DeviceTypeFilter
	params.DeviceTypes = fp.DeviceTypes
	params.SeverityFilter = fp.SeverityFilter
	params.Severities = fp.Severities
	params.MinerErrorFilter = fp.MinerErrorFilter
	params.MinerErrors = fp.MinerErrors
	params.ComponentTypeFilter = fp.ComponentTypeFilter
	params.ComponentTypes = fp.ComponentTypes
	params.ComponentIDFilter = fp.ComponentIDFilter
	params.ComponentIds = fp.ComponentIds

	// Apply device cursor (severity, device_id)
	cursorSeverity, cursorDeviceID, _ := models.DecodeDeviceCursor(opts.PageToken)
	if cursorDeviceID > 0 {
		params.CursorSeverity = sql.NullInt32{Int32: int32(cursorSeverity), Valid: true} // #nosec G115 -- Severity enum bounded (max 4)
		params.CursorDeviceID = sql.NullInt64{Int64: cursorDeviceID, Valid: true}
	}

	return params
}

// buildDeviceCountParams converts QueryOptions to sqlc.CountDevicesWithErrorsParams.
// TODO(DASH-1048): Re-add UseOrLogic parameter to support OR filter logic.
func buildDeviceCountParams(opts *models.QueryOptions) sqlc.CountDevicesWithErrorsParams {
	filter := opts.Filter
	if filter == nil {
		filter = &models.QueryFilter{}
	}

	params := sqlc.CountDevicesWithErrorsParams{
		OrgID:         opts.OrgID,
		IncludeClosed: filter.IncludeClosed,
	}

	params.TimeFrom, params.TimeTo = applyTimeFilter(filter.TimeFrom, filter.TimeTo)

	fp := applyFilterToParams(filter)
	params.DeviceFilter = fp.DeviceFilter
	params.DeviceIdentifiers = fp.DeviceIdentifiers
	params.DeviceTypeFilter = fp.DeviceTypeFilter
	params.DeviceTypes = fp.DeviceTypes
	params.SeverityFilter = fp.SeverityFilter
	params.Severities = fp.Severities
	params.MinerErrorFilter = fp.MinerErrorFilter
	params.MinerErrors = fp.MinerErrors
	params.ComponentTypeFilter = fp.ComponentTypeFilter
	params.ComponentTypes = fp.ComponentTypes
	params.ComponentIDFilter = fp.ComponentIDFilter
	params.ComponentIds = fp.ComponentIds

	return params
}

// buildComponentQueryParams converts QueryOptions to sqlc.QueryComponentKeysWithErrorsParams.
// TODO(DASH-1048): Re-add UseOrLogic parameter to support OR filter logic.
func buildComponentQueryParams(opts *models.QueryOptions) sqlc.QueryComponentKeysWithErrorsParams {
	filter := opts.Filter
	if filter == nil {
		filter = &models.QueryFilter{}
	}

	params := sqlc.QueryComponentKeysWithErrorsParams{
		OrgID:         opts.OrgID,
		IncludeClosed: filter.IncludeClosed,
		Limit:         normalizeLimit(opts.PageSize),
	}

	params.TimeFrom, params.TimeTo = applyTimeFilter(filter.TimeFrom, filter.TimeTo)

	fp := applyFilterToParams(filter)
	params.DeviceFilter = fp.DeviceFilter
	params.DeviceIdentifiers = fp.DeviceIdentifiers
	params.DeviceTypeFilter = fp.DeviceTypeFilter
	params.DeviceTypes = fp.DeviceTypes
	params.SeverityFilter = fp.SeverityFilter
	params.Severities = fp.Severities
	params.MinerErrorFilter = fp.MinerErrorFilter
	params.MinerErrors = fp.MinerErrors
	params.ComponentTypeFilter = fp.ComponentTypeFilter
	params.ComponentTypes = fp.ComponentTypes
	params.ComponentIDFilter = fp.ComponentIDFilter
	params.ComponentIds = fp.ComponentIds

	// Apply component cursor (severity, device_id, component_id)
	cursorSeverity, cursorDeviceID, cursorComponentID, _ := models.DecodeComponentCursor(opts.PageToken)
	if cursorDeviceID > 0 {
		params.CursorSeverity = sql.NullInt32{Int32: int32(cursorSeverity), Valid: true} // #nosec G115 -- Severity enum bounded (max 4)
		params.CursorDeviceID = sql.NullInt64{Int64: cursorDeviceID, Valid: true}
		if cursorComponentID != nil {
			params.CursorComponentID = sql.NullString{String: *cursorComponentID, Valid: true}
		}
		// If cursorComponentID is nil, CursorComponentID stays as zero value (Valid: false = NULL)
	}

	return params
}

// buildComponentCountParams converts QueryOptions to sqlc.CountComponentsWithErrorsParams.
// TODO(DASH-1048): Re-add UseOrLogic parameter to support OR filter logic.
func buildComponentCountParams(opts *models.QueryOptions) sqlc.CountComponentsWithErrorsParams {
	filter := opts.Filter
	if filter == nil {
		filter = &models.QueryFilter{}
	}

	params := sqlc.CountComponentsWithErrorsParams{
		OrgID:         opts.OrgID,
		IncludeClosed: filter.IncludeClosed,
	}

	params.TimeFrom, params.TimeTo = applyTimeFilter(filter.TimeFrom, filter.TimeTo)

	fp := applyFilterToParams(filter)
	params.DeviceFilter = fp.DeviceFilter
	params.DeviceIdentifiers = fp.DeviceIdentifiers
	params.DeviceTypeFilter = fp.DeviceTypeFilter
	params.DeviceTypes = fp.DeviceTypes
	params.SeverityFilter = fp.SeverityFilter
	params.Severities = fp.Severities
	params.MinerErrorFilter = fp.MinerErrorFilter
	params.MinerErrors = fp.MinerErrors
	params.ComponentTypeFilter = fp.ComponentTypeFilter
	params.ComponentTypes = fp.ComponentTypes
	params.ComponentIDFilter = fp.ComponentIDFilter
	params.ComponentIds = fp.ComponentIds

	return params
}
