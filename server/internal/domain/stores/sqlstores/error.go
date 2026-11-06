package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sqlc-dev/pqtype"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

const (
	// minValidEnumValue represents the minimum valid value for enum types
	minValidEnumValue = 0
)

var _ interfaces.ErrorStore = &SQLErrorStore{}

type SQLErrorStore struct {
	SQLConnectionManager
	transactor interfaces.Transactor
}

func NewSQLErrorStore(conn *sql.DB, transactor interfaces.Transactor) *SQLErrorStore {
	return &SQLErrorStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
		transactor:           transactor,
	}
}

func (s *SQLErrorStore) getQueries(ctx context.Context) sqlc.Querier {
	return s.GetQueries(ctx)
}

type errorBatchReport struct {
	ErrorID           string            `json:"error_id"`
	MinerError        int32             `json:"miner_error"`
	Severity          int32             `json:"severity"`
	Summary           string            `json:"summary"`
	Impact            *string           `json:"impact"`
	CauseSummary      *string           `json:"cause_summary"`
	RecommendedAction *string           `json:"recommended_action"`
	FirstSeenAt       time.Time         `json:"first_seen_at"`
	LastSeenAt        time.Time         `json:"last_seen_at"`
	ComponentID       *string           `json:"component_id"`
	ComponentType     *int32            `json:"component_type"`
	VendorCode        *string           `json:"vendor_code"`
	Firmware          *string           `json:"firmware"`
	Extra             map[string]string `json:"extra"`
	ClosedAt          *time.Time        `json:"closed_at"`
}

type errorBatchDedupKey struct {
	MinerError    models.MinerError
	ComponentID   string
	HasComponent  bool
	ComponentType models.ComponentType
}

// UpsertErrors persists one device's error snapshot in one database round trip.
// Error detail changes are immediate; stable rows refresh on the supplied heartbeat.
func (s *SQLErrorStore) UpsertErrors(
	ctx context.Context,
	orgID int64,
	deviceIdentifier string,
	errorsToUpsert []models.ErrorMessage,
	observedAt time.Time,
	refreshHeartbeat time.Duration,
	refreshAllOpen bool,
) error {
	reports := make([]errorBatchReport, 0, len(errorsToUpsert))
	reportIndexes := make(map[errorBatchDedupKey]int, len(errorsToUpsert))
	for i := range errorsToUpsert {
		errMsg := &errorsToUpsert[i]
		var componentID *string
		if errMsg.ComponentID != nil {
			sanitized := sanitizeBoundedText(*errMsg.ComponentID)
			componentID = &sanitized
		}
		key := errorBatchDedupKey{
			MinerError:    errMsg.MinerError,
			ComponentType: errMsg.ComponentType,
		}
		if componentID != nil {
			key.ComponentID = *componentID
			key.HasComponent = true
		}

		report := errorBatchReport{
			ErrorID:           id.GenerateID(),
			MinerError:        int32(errMsg.MinerError), // #nosec G115 -- MinerError enum bounded by protobuf
			Severity:          int32(errMsg.Severity),   // #nosec G115 -- Severity enum bounded (max 4)
			Summary:           stripNUL(errMsg.Summary),
			Impact:            nonEmptyStringPtr(stripNUL(errMsg.Impact)),
			CauseSummary:      nonEmptyStringPtr(stripNUL(errMsg.CauseSummary)),
			RecommendedAction: nonEmptyStringPtr(stripNUL(errMsg.RecommendedAction)),
			FirstSeenAt:       errMsg.FirstSeenAt,
			LastSeenAt:        errMsg.LastSeenAt,
			ComponentID:       componentID,
			ComponentType:     nullableComponentType(errMsg.ComponentType),
			VendorCode:        nonEmptyStringPtr(sanitizeBoundedText(errMsg.VendorCode)),
			Firmware:          nonEmptyStringPtr(sanitizeBoundedText(errMsg.Firmware)),
			Extra:             sanitizeVendorAttributes(errMsg.VendorAttributes),
			ClosedAt:          errMsg.ClosedAt,
		}
		if index, ok := reportIndexes[key]; ok {
			report.FirstSeenAt = reports[index].FirstSeenAt
			reports[index] = report
			continue
		}
		reportIndexes[key] = len(reports)
		reports = append(reports, report)
	}

	reportsJSON, err := json.Marshal(reports)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to marshal error reports: %v", err)
	}

	params := sqlc.UpsertDeviceErrorsParams{
		OrgID:            orgID,
		DeviceIdentifier: deviceIdentifier,
		ErrorReports:     reportsJSON,
		ObservedAt:       observedAt,
		HeartbeatCutoff:  observedAt.Add(-refreshHeartbeat),
		RefreshAllOpen:   refreshAllOpen,
	}
	var deviceFound bool
	err = s.retryQuery(ctx, "UpsertDeviceErrors", func() error {
		var queryErr error
		deviceFound, queryErr = s.getQueries(ctx).UpsertDeviceErrors(ctx, params)
		return queryErr
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert device errors: %w", err)
	}
	if !deviceFound {
		return fleeterror.NewNotFoundErrorf("device not found: %s", deviceIdentifier)
	}

	return nil
}

// maxBoundedTextRunes matches the varchar(255) columns, which count characters.
const maxBoundedTextRunes = 255

// stripNUL removes NUL characters, which jsonb and text columns reject.
func stripNUL(value string) string {
	if !strings.ContainsRune(value, 0) {
		return value
	}
	return strings.ReplaceAll(value, "\x00", "")
}

func sanitizeBoundedText(value string) string {
	value = stripNUL(value)
	if utf8.RuneCountInString(value) <= maxBoundedTextRunes {
		return value
	}
	return string([]rune(value)[:maxBoundedTextRunes])
}

func sanitizeVendorAttributes(attributes map[string]string) map[string]string {
	if attributes == nil {
		return nil
	}
	sanitized := make(map[string]string, len(attributes))
	for key, value := range attributes {
		sanitized[stripNUL(key)] = stripNUL(value)
	}
	return sanitized
}

func nullableComponentType(componentType models.ComponentType) *int32 {
	if componentType == models.ComponentTypeUnspecified {
		return nil
	}
	value := int32(componentType) // #nosec G115 -- ComponentType enum values bounded (max 4)
	return &value
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
	Extra             pqtype.NullRawMessage
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

	componentID := nullStringToPtr(fields.ComponentID)

	var vendorAttrs map[string]string
	if fields.Extra.Valid && len(fields.Extra.RawMessage) > 0 {
		if err := json.Unmarshal(fields.Extra.RawMessage, &vendorAttrs); err != nil {
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

// safeInt32ToMinerError converts int32 from DB to MinerError, returning Unspecified for negative values.
// MinerError is a proto type (int32) so we can cast directly.
func safeInt32ToMinerError(val int32) models.MinerError {
	if val < minValidEnumValue {
		return models.MinerErrorUnspecified
	}
	return models.MinerError(val)
}

// safeInt32ToSeverity converts int32 from DB to Severity, returning Unspecified for negative values.
// Severity is a proto type (int32) so we can cast directly.
func safeInt32ToSeverity(val int32) models.Severity {
	if val < minValidEnumValue {
		return models.SeverityUnspecified
	}
	return models.Severity(val)
}

// safeInt32ToComponentType converts int32 from DB to ComponentType, returning Unspecified for negative values.
// ComponentType is a domain type (uint) so we need to convert through uint.
func safeInt32ToComponentType(val int32) models.ComponentType {
	if val < minValidEnumValue {
		return models.ComponentTypeUnspecified
	}
	return models.ComponentType(uint(val))
}

// nullStringToPtr converts a sql.NullString to a *string pointer.
// Returns nil if the NullString is not valid, otherwise returns a pointer to the string value.
// This helper consolidates the repeated pattern of converting database nullable strings to Go pointers.
func nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
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
	DeviceFilter        sql.NullString
	DeviceIdentifiers   []string
	DeviceTypeFilter    sql.NullString
	DeviceTypes         []string
	SeverityFilter      sql.NullString
	Severities          []int32
	MinerErrorFilter    sql.NullString
	MinerErrors         []int32
	ComponentTypeFilter sql.NullString
	ComponentTypes      []int32
	ComponentIDFilter   sql.NullString
	ComponentIds        []string
}

// applyFilterToParams populates filter parameters from a QueryFilter.
// This extracts the common filter-building logic repeated in multiple parameter builders.
func applyFilterToParams(filter *models.QueryFilter) filterParams {
	var fp filterParams

	if len(filter.DeviceIdentifiers) > 0 {
		fp.DeviceFilter = sql.NullString{String: "active", Valid: true}
		fp.DeviceIdentifiers = filter.DeviceIdentifiers
	}

	if len(filter.DeviceTypes) > 0 {
		fp.DeviceTypeFilter = sql.NullString{String: "active", Valid: true}
		fp.DeviceTypes = filter.DeviceTypes
	}

	if len(filter.Severities) > 0 {
		fp.SeverityFilter = sql.NullString{String: "active", Valid: true}
		fp.Severities = severitiesToInt32s(filter.Severities)
	}

	if len(filter.MinerErrors) > 0 {
		fp.MinerErrorFilter = sql.NullString{String: "active", Valid: true}
		fp.MinerErrors = minerErrorsToInt32s(filter.MinerErrors)
	}

	if len(filter.ComponentTypes) > 0 {
		fp.ComponentTypeFilter = sql.NullString{String: "active", Valid: true}
		fp.ComponentTypes = componentTypesToInt32s(filter.ComponentTypes)
	}

	if len(filter.ComponentIDs) > 0 {
		fp.ComponentIDFilter = sql.NullString{String: "active", Valid: true}
		fp.ComponentIds = filter.ComponentIDs
	}

	return fp
}

// assignFilterParams assigns filterParams fields to a params struct.
// This helper consolidates the repeated 12-line assignment block that appears 6 times
// in buildQueryParams, buildCountParams, buildDeviceQueryParams, buildDeviceCountParams,
// buildComponentQueryParams, and buildComponentCountParams.
func assignFilterParams(fp filterParams, target interface{}) {
	switch p := target.(type) {
	case *sqlc.QueryErrorsParams:
		p.DeviceFilter = fp.DeviceFilter
		p.DeviceIdentifiers = fp.DeviceIdentifiers
		p.DeviceTypeFilter = fp.DeviceTypeFilter
		p.DeviceTypes = fp.DeviceTypes
		p.SeverityFilter = fp.SeverityFilter
		p.Severities = fp.Severities
		p.MinerErrorFilter = fp.MinerErrorFilter
		p.MinerErrors = fp.MinerErrors
		p.ComponentTypeFilter = fp.ComponentTypeFilter
		p.ComponentTypes = fp.ComponentTypes
		p.ComponentIDFilter = fp.ComponentIDFilter
		p.ComponentIds = fp.ComponentIds
	case *sqlc.CountErrorsParams:
		p.DeviceFilter = fp.DeviceFilter
		p.DeviceIdentifiers = fp.DeviceIdentifiers
		p.DeviceTypeFilter = fp.DeviceTypeFilter
		p.DeviceTypes = fp.DeviceTypes
		p.SeverityFilter = fp.SeverityFilter
		p.Severities = fp.Severities
		p.MinerErrorFilter = fp.MinerErrorFilter
		p.MinerErrors = fp.MinerErrors
		p.ComponentTypeFilter = fp.ComponentTypeFilter
		p.ComponentTypes = fp.ComponentTypes
		p.ComponentIDFilter = fp.ComponentIDFilter
		p.ComponentIds = fp.ComponentIds
	case *sqlc.QueryDeviceIDsWithErrorsParams:
		p.DeviceFilter = fp.DeviceFilter
		p.DeviceIdentifiers = fp.DeviceIdentifiers
		p.DeviceTypeFilter = fp.DeviceTypeFilter
		p.DeviceTypes = fp.DeviceTypes
		p.SeverityFilter = fp.SeverityFilter
		p.Severities = fp.Severities
		p.MinerErrorFilter = fp.MinerErrorFilter
		p.MinerErrors = fp.MinerErrors
		p.ComponentTypeFilter = fp.ComponentTypeFilter
		p.ComponentTypes = fp.ComponentTypes
		p.ComponentIDFilter = fp.ComponentIDFilter
		p.ComponentIds = fp.ComponentIds
	case *sqlc.CountDevicesWithErrorsParams:
		p.DeviceFilter = fp.DeviceFilter
		p.DeviceIdentifiers = fp.DeviceIdentifiers
		p.DeviceTypeFilter = fp.DeviceTypeFilter
		p.DeviceTypes = fp.DeviceTypes
		p.SeverityFilter = fp.SeverityFilter
		p.Severities = fp.Severities
		p.MinerErrorFilter = fp.MinerErrorFilter
		p.MinerErrors = fp.MinerErrors
		p.ComponentTypeFilter = fp.ComponentTypeFilter
		p.ComponentTypes = fp.ComponentTypes
		p.ComponentIDFilter = fp.ComponentIDFilter
		p.ComponentIds = fp.ComponentIds
	case *sqlc.QueryComponentKeysWithErrorsParams:
		p.DeviceFilter = fp.DeviceFilter
		p.DeviceIdentifiers = fp.DeviceIdentifiers
		p.DeviceTypeFilter = fp.DeviceTypeFilter
		p.DeviceTypes = fp.DeviceTypes
		p.SeverityFilter = fp.SeverityFilter
		p.Severities = fp.Severities
		p.MinerErrorFilter = fp.MinerErrorFilter
		p.MinerErrors = fp.MinerErrors
		p.ComponentTypeFilter = fp.ComponentTypeFilter
		p.ComponentTypes = fp.ComponentTypes
		p.ComponentIDFilter = fp.ComponentIDFilter
		p.ComponentIds = fp.ComponentIds
	case *sqlc.CountComponentsWithErrorsParams:
		p.DeviceFilter = fp.DeviceFilter
		p.DeviceIdentifiers = fp.DeviceIdentifiers
		p.DeviceTypeFilter = fp.DeviceTypeFilter
		p.DeviceTypes = fp.DeviceTypes
		p.SeverityFilter = fp.SeverityFilter
		p.Severities = fp.Severities
		p.MinerErrorFilter = fp.MinerErrorFilter
		p.MinerErrors = fp.MinerErrors
		p.ComponentTypeFilter = fp.ComponentTypeFilter
		p.ComponentTypes = fp.ComponentTypes
		p.ComponentIDFilter = fp.ComponentIDFilter
		p.ComponentIds = fp.ComponentIds
	}
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
// TODO: Re-add UseOrLogic parameter to support OR filter logic.
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
	assignFilterParams(applyFilterToParams(filter), &params)
	params.CursorSeverity, params.CursorLastSeen, params.CursorErrorID = applyCursor(opts.PageToken)

	return params
}

// buildCountParams converts QueryOptions to sqlc.CountErrorsParams.
// TODO: Re-add UseOrLogic parameter to support OR filter logic.
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
	assignFilterParams(applyFilterToParams(filter), &params)

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
			row.Extra.RawMessage,
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

	componentIDPtr := nullStringToPtr(componentID)

	var vendorAttrs map[string]string
	if len(extra) > 0 {
		if err := json.Unmarshal(extra, &vendorAttrs); err != nil {
			slog.Warn("failed to unmarshal vendor attributes", "error_id", errorID, "error", err)
		}
	}

	var compType models.ComponentType
	if componentType.Valid {
		compType = safeInt32ToComponentType(componentType.Int32)
	} else {
		compType = models.ComponentTypeUnspecified
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
		ComponentType:     compType,
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
func componentTypesToInt32s(types []models.ComponentType) []int32 {
	result := make([]int32, len(types))
	for i, t := range types {
		result[i] = int32(t) // #nosec G115 -- ComponentType enum bounded (max 4)
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
			worstSeverity = safeInt32ToSeverity(int32(severity)) // #nosec G115 -- Severity bounded 0-4
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
		componentID := nullStringToPtr(row.ComponentID)
		// WorstSeverity from MIN() aggregate returns interface{}, extract as int32
		var worstSeverity models.Severity
		if severity, ok := row.WorstSeverity.(int64); ok {
			worstSeverity = safeInt32ToSeverity(int32(severity)) // #nosec G115 -- Severity bounded 0-4
		}
		// ComponentType comes from SQL as sql.NullInt32
		var componentType models.ComponentType
		if row.ComponentType.Valid {
			componentType = models.ComponentType(row.ComponentType.Int32) // #nosec G115 -- ComponentType enum bounded (0-6)
		}
		keys[i] = models.ComponentKey{
			DeviceID:         row.DeviceID,
			DeviceIdentifier: row.DeviceIdentifier,
			ComponentType:    componentType,
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
// TODO: Re-add UseOrLogic parameter to support OR filter logic.
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
	assignFilterParams(applyFilterToParams(filter), &params)

	// Apply device cursor (severity, device_id)
	cursorSeverity, cursorDeviceID, _ := models.DecodeDeviceCursor(opts.PageToken)
	if cursorDeviceID > 0 {
		params.CursorSeverity = sql.NullInt32{Int32: int32(cursorSeverity), Valid: true} // #nosec G115 -- Severity enum bounded (max 4)
		params.CursorDeviceID = sql.NullInt64{Int64: cursorDeviceID, Valid: true}
	}

	return params
}

// buildDeviceCountParams converts QueryOptions to sqlc.CountDevicesWithErrorsParams.
// TODO: Re-add UseOrLogic parameter to support OR filter logic.
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
	assignFilterParams(applyFilterToParams(filter), &params)

	return params
}

// buildComponentQueryParams converts QueryOptions to sqlc.QueryComponentKeysWithErrorsParams.
// TODO: Re-add UseOrLogic parameter to support OR filter logic.
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
	assignFilterParams(applyFilterToParams(filter), &params)

	// Apply component cursor (severity, device_id, component_type, component_id)
	cursorSeverity, cursorDeviceID, cursorComponentType, cursorComponentID, _ := models.DecodeComponentCursor(opts.PageToken)
	if cursorDeviceID > 0 {
		params.CursorSeverity = sql.NullInt32{Int32: int32(cursorSeverity), Valid: true} // #nosec G115 -- Severity enum bounded (max 4)
		params.CursorDeviceID = sql.NullInt64{Int64: cursorDeviceID, Valid: true}
		params.CursorComponentType = sql.NullInt32{Int32: int32(cursorComponentType), Valid: true} // #nosec G115 -- ComponentType enum bounded (max 4)
		if cursorComponentID != nil {
			params.CursorComponentID = sql.NullString{String: *cursorComponentID, Valid: true}
		}
		// If cursorComponentID is nil, CursorComponentID stays as zero value (Valid: false = NULL)
	}

	return params
}

// buildComponentCountParams converts QueryOptions to sqlc.CountComponentsWithErrorsParams.
// TODO: Re-add UseOrLogic parameter to support OR filter logic.
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
	assignFilterParams(applyFilterToParams(filter), &params)

	return params
}

// ============================================================================
// Error Lifecycle Management
// ============================================================================

// CloseStaleErrors closes all open errors where last_seen_at is older than the threshold.
// This is a bulk operation that operates globally across all organizations.
func (s *SQLErrorStore) CloseStaleErrors(ctx context.Context, threshold time.Duration) (int64, error) {
	q := s.getQueries(ctx)

	cutoffTime := time.Now().Add(-threshold)

	result, err := q.CloseStaleErrors(ctx, sqlc.CloseStaleErrorsParams{
		CutoffTime:       cutoffTime,
		StatusCutoffTime: sql.NullTime{Time: cutoffTime, Valid: true},
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to close stale errors: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to get rows affected: %v", err)
	}

	return rowsAffected, nil
}
