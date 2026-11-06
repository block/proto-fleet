package sqlstores_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupErrorTestData creates an organization and device for error store tests.
// Uses testutil.DatabaseService for consistency with other integration tests.
func setupErrorTestData(t *testing.T) (db *sql.DB, orgID int64, deviceIdentifier string) {
	t.Helper()
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	device := testContext.DatabaseService.CreateDevice(adminUser.OrganizationID, "proto")
	return testContext.DatabaseService.DB, adminUser.OrganizationID, device.ID
}

// newErrorStore creates an ErrorStore with a transactor for integration tests.
func newErrorStore(db *sql.DB) *sqlstores.SQLErrorStore {
	transactor := sqlstores.NewSQLTransactor(db)
	return sqlstores.NewSQLErrorStore(db, transactor)
}

func upsertSingleError(
	ctx context.Context,
	db *sql.DB,
	store *sqlstores.SQLErrorStore,
	orgID int64,
	deviceIdentifier string,
	errMsg *models.ErrorMessage,
) (*models.ErrorMessage, error) {
	if err := store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, 0); err != nil {
		return nil, err
	}

	var componentID sql.NullString
	if errMsg.ComponentID != nil {
		componentID = sql.NullString{String: *errMsg.ComponentID, Valid: true}
	}
	componentType := sql.NullInt32{Int32: int32(errMsg.ComponentType), Valid: errMsg.ComponentType != models.ComponentTypeUnspecified} // #nosec G115 -- ComponentType enum values bounded (max 4)
	var errorID string
	err := db.QueryRowContext(ctx, `
		SELECT e.error_id
		FROM errors e
		JOIN device d ON d.id = e.device_id
		WHERE e.org_id = $1
		  AND d.device_identifier = $2
		  AND e.miner_error = $3
		  AND e.component_id IS NOT DISTINCT FROM $4::text
		  AND e.component_type IS NOT DISTINCT FROM $5::int
		ORDER BY e.id DESC
		LIMIT 1`, orgID, deviceIdentifier, int32(errMsg.MinerError), componentID, componentType).Scan(&errorID) // #nosec G115 -- MinerError enum bounded by protobuf
	if err != nil {
		return nil, fmt.Errorf("locate upserted error: %w", err)
	}
	return store.GetErrorByErrorID(ctx, orgID, errorID)
}

type errorRow struct {
	errorID           string
	minerError        models.MinerError
	severity          models.Severity
	summary           string
	impact            sql.NullString
	causeSummary      sql.NullString
	recommendedAction sql.NullString
	firstSeenAt       time.Time
	lastSeenAt        time.Time
	closedAt          sql.NullTime
	componentID       sql.NullString
	componentType     sql.NullInt32
	vendorCode        sql.NullString
	firmware          sql.NullString
	extra             []byte
	siteID            sql.NullInt64
}

func loadErrorRows(t *testing.T, ctx context.Context, db *sql.DB, orgID int64) []errorRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT error_id, miner_error, severity, summary, impact, cause_summary, recommended_action,
		       first_seen_at, last_seen_at, closed_at, component_id, component_type,
		       vendor_code, firmware, extra, site_id
		FROM errors
		WHERE org_id = $1
		ORDER BY id`, orgID)
	require.NoError(t, err)
	defer rows.Close()

	var result []errorRow
	for rows.Next() {
		var row errorRow
		require.NoError(t, rows.Scan(
			&row.errorID, &row.minerError, &row.severity, &row.summary, &row.impact, &row.causeSummary,
			&row.recommendedAction, &row.firstSeenAt, &row.lastSeenAt, &row.closedAt, &row.componentID,
			&row.componentType, &row.vendorCode, &row.firmware, &row.extra, &row.siteID,
		))
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

func errorRowsByMinerError(rows []errorRow) map[models.MinerError]errorRow {
	byMinerError := make(map[models.MinerError]errorRow, len(rows))
	for _, row := range rows {
		byMinerError[row.minerError] = row
	}
	return byMinerError
}

func requireDatabaseLockWait(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		var waiting bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND pid <> pg_backend_pid()
				  AND wait_event_type = 'Lock'
			)`).Scan(&waiting)
		require.NoError(t, err)
		if waiting {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for competing database operation to block: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

// createTestErrorMessage builds an ErrorMessage with default test values.
func createTestErrorMessage(deviceID string) *models.ErrorMessage {
	now := time.Now().Truncate(time.Microsecond)
	return &models.ErrorMessage{
		MinerError:        models.HashboardOverTemperature,
		Severity:          models.SeverityMajor,
		Summary:           "Test hashboard over temperature",
		Impact:            "Reduced hashrate",
		CauseSummary:      "Cooling system degraded",
		RecommendedAction: "Check fans and airflow",
		FirstSeenAt:       now.Add(-time.Hour),
		LastSeenAt:        now,
		ClosedAt:          nil,
		DeviceID:          deviceID,
		ComponentID:       nil,
		ComponentType:     models.ComponentTypeUnspecified,
		VendorCode:        "E001",
		Firmware:          "v1.2.3",
		VendorAttributes:  map[string]string{"key1": "value1"},
	}
}

// setupMultiDeviceErrorData creates an org with multiple devices for query testing.
// Uses testutil.DatabaseService for consistency with other integration tests.
func setupMultiDeviceErrorData(t *testing.T, deviceCount int) (db *sql.DB, orgID int64, deviceIdentifiers []string) {
	t.Helper()
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIdentifiers = make([]string, deviceCount)
	for i := range deviceCount {
		device := testContext.DatabaseService.CreateDevice(adminUser.OrganizationID, "proto")
		deviceIdentifiers[i] = device.ID
	}
	return testContext.DatabaseService.DB, adminUser.OrganizationID, deviceIdentifiers
}

// testMinerErrors provides distinct MinerError values for test data generation.
// Using explicit enum values avoids G115 integer conversion warnings.
var testMinerErrors = []models.MinerError{
	models.HashboardOverTemperature,
	models.PSUOverTemperature,
	models.HashboardASICUnderTemperature,
	models.FanSpeedDeviation,
	models.DeviceOverTemperature,
}

// testSeverities provides distinct Severity values for test data generation.
var testSeverities = []models.Severity{
	models.SeverityCritical,
	models.SeverityMajor,
	models.SeverityMinor,
}

// createErrorWithSeverity creates an ErrorMessage with specific severity and miner error.
func createErrorWithSeverity(deviceID string, severity models.Severity, minerError models.MinerError, componentID *string) *models.ErrorMessage {
	now := time.Now().Truncate(time.Microsecond)
	componentType := models.ComponentTypeUnspecified
	if componentID != nil {
		componentType = models.ComponentTypeHashBoards
	}
	return &models.ErrorMessage{
		MinerError:        minerError,
		Severity:          severity,
		Summary:           fmt.Sprintf("Test error with severity %d", severity),
		Impact:            "Test impact",
		CauseSummary:      "Test cause",
		RecommendedAction: "Test action",
		FirstSeenAt:       now.Add(-time.Hour),
		LastSeenAt:        now,
		ClosedAt:          nil,
		DeviceID:          deviceID,
		ComponentID:       componentID,
		ComponentType:     componentType,
		VendorCode:        "TEST",
		Firmware:          "v1.0.0",
		VendorAttributes:  nil,
	}
}

// encodeCursor encodes a PageCursor to a base64 token for pagination tests.
func encodeCursor(severity models.Severity, lastSeenAt time.Time, errorID string) string {
	data := struct {
		Severity   int32     `json:"s"`
		LastSeenAt time.Time `json:"t"`
		ErrorID    string    `json:"e"`
	}{
		Severity:   int32(severity), // #nosec G115 -- Severity enum bounded (max 4)
		LastSeenAt: lastSeenAt,
		ErrorID:    errorID,
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(jsonBytes)
}

// encodeDeviceCursor encodes a device cursor to a base64 token for pagination tests.
func encodeDeviceCursor(severity models.Severity, deviceID int64) string {
	data := struct {
		Severity int32 `json:"s"`
		DeviceID int64 `json:"d"`
	}{
		Severity: int32(severity), // #nosec G115 -- Severity enum bounded (max 4)
		DeviceID: deviceID,
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(jsonBytes)
}

func TestSQLErrorStore_UpsertErrors_ShouldUpdateExistingOpenError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Insert first error
	errMsg := createTestErrorMessage(deviceIdentifier)
	first, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	originalErrorID := first.ErrorID
	originalFirstSeenAt := first.FirstSeenAt

	// Arrange: Update with same dedup key but different mutable fields
	time.Sleep(10 * time.Millisecond) // Ensure time difference
	errMsg.Severity = models.SeverityCritical
	errMsg.Summary = "Updated summary"
	errMsg.Impact = "Updated impact"
	errMsg.LastSeenAt = time.Now().Truncate(time.Microsecond)

	// Act
	updated, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, originalErrorID, updated.ErrorID, "ErrorID should be preserved")
	assert.Equal(t, originalFirstSeenAt.Unix(), updated.FirstSeenAt.Unix(), "FirstSeenAt should be preserved")
	assert.Equal(t, models.SeverityCritical, updated.Severity, "Severity should be updated")
	assert.Equal(t, "Updated summary", updated.Summary, "Summary should be updated")
	assert.Equal(t, "Updated impact", updated.Impact, "Impact should be updated")
	assert.True(t, updated.LastSeenAt.After(first.LastSeenAt), "LastSeenAt should be updated")
}

func TestSQLErrorStore_UpsertErrors_ShouldCoalesceUnchangedErrorsUntilHeartbeat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	errMsg := createTestErrorMessage(deviceIdentifier)
	errMsg.FirstSeenAt = observedAt.Add(-time.Hour)
	errMsg.LastSeenAt = observedAt

	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, observedAt, time.Minute))

	type persistedError struct {
		lastSeenAt time.Time
		summary    string
		tuple      string
	}
	readError := func() persistedError {
		t.Helper()
		var result persistedError
		err := db.QueryRowContext(ctx, `
			SELECT e.last_seen_at, e.summary, e.ctid::text
			FROM errors e
			JOIN device d ON d.id = e.device_id
			WHERE e.org_id = $1
			  AND d.device_identifier = $2
			  AND e.miner_error = $3
			  AND e.closed_at IS NULL`, orgID, deviceIdentifier, errMsg.MinerError).
			Scan(&result.lastSeenAt, &result.summary, &result.tuple)
		require.NoError(t, err)
		return result
	}

	initial := readError()
	errMsg.LastSeenAt = observedAt.Add(30 * time.Second)
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, time.Minute))
	require.Equal(t, initial, readError(), "an unchanged error inside the heartbeat must not rewrite its row")

	errMsg.LastSeenAt = observedAt.Add(60 * time.Second)
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, time.Minute))
	afterHeartbeat := readError()
	require.NotEqual(t, initial.tuple, afterHeartbeat.tuple, "an unchanged error at the heartbeat cutoff must be rewritten")
	require.True(t, afterHeartbeat.lastSeenAt.Equal(errMsg.LastSeenAt))

	errMsg.Summary = "Changed before the next heartbeat"
	errMsg.LastSeenAt = observedAt.Add(61 * time.Second)
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, time.Minute))
	afterChange := readError()
	require.NotEqual(t, afterHeartbeat.tuple, afterChange.tuple, "changed details must be written immediately")
	require.Equal(t, errMsg.Summary, afterChange.summary)
	require.True(t, afterChange.lastSeenAt.Equal(errMsg.LastSeenAt))
}

func TestSQLErrorStore_UpsertErrors_ShouldPreserveFirstDuplicateObservation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	first := createTestErrorMessage(deviceIdentifier)
	first.FirstSeenAt = observedAt.Add(-2 * time.Hour)
	first.LastSeenAt = observedAt
	last := *first
	last.FirstSeenAt = observedAt.Add(-time.Hour)
	last.Severity = models.SeverityCritical
	last.Summary = "Final duplicate details"

	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*first, last}, observedAt, time.Minute))

	var count int
	var firstSeenAt time.Time
	var severity models.Severity
	var summary string
	err := db.QueryRowContext(ctx, `
		SELECT count(*) OVER (), first_seen_at, severity, summary
		FROM errors
		WHERE org_id = $1 AND miner_error = $2 AND closed_at IS NULL`, orgID, first.MinerError).
		Scan(&count, &firstSeenAt, &severity, &summary)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.True(t, firstSeenAt.Equal(first.FirstSeenAt))
	require.Equal(t, last.Severity, severity)
	require.Equal(t, last.Summary, summary)
}

func TestSQLErrorStore_UpsertErrors_ShouldKeepComponentIdentitiesDistinct(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()
	observedAt := time.Now().UTC().Truncate(time.Microsecond)

	cases := []struct {
		name          string
		componentID   *string
		componentType models.ComponentType
	}{
		{name: "no component", componentID: nil, componentType: models.ComponentTypeUnspecified},
		{name: "nil id with type", componentID: nil, componentType: models.ComponentTypeHashBoards},
		{name: "empty id", componentID: strPtr(""), componentType: models.ComponentTypeHashBoards},
		{name: "id 0", componentID: strPtr("0"), componentType: models.ComponentTypeHashBoards},
		{name: "id 1", componentID: strPtr("1"), componentType: models.ComponentTypeHashBoards},
		{name: "id 0 other type", componentID: strPtr("0"), componentType: models.ComponentTypeControlBoard},
	}
	buildBatch := func(summarySuffix string, lastSeenAt time.Time) []models.ErrorMessage {
		batch := make([]models.ErrorMessage, 0, len(cases))
		for _, tt := range cases {
			errMsg := createTestErrorMessage(deviceIdentifier)
			errMsg.ComponentID = tt.componentID
			errMsg.ComponentType = tt.componentType
			errMsg.Summary = tt.name + summarySuffix
			errMsg.FirstSeenAt = observedAt.Add(-time.Hour)
			errMsg.LastSeenAt = lastSeenAt
			batch = append(batch, *errMsg)
		}
		return batch
	}
	rowsBySummary := func(rows []errorRow) map[string]errorRow {
		bySummary := make(map[string]errorRow, len(rows))
		for _, row := range rows {
			bySummary[row.summary] = row
		}
		return bySummary
	}

	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, buildBatch("", observedAt), observedAt, time.Minute))
	original := rowsBySummary(loadErrorRows(t, ctx, db, orgID))
	require.Len(t, original, len(cases), "every component identity must have its own open row")

	updatedAt := observedAt.Add(30 * time.Second)
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, buildBatch(" updated", updatedAt), updatedAt, time.Minute))
	updated := rowsBySummary(loadErrorRows(t, ctx, db, orgID))
	require.Len(t, updated, len(cases), "re-reporting the same identities must not add rows")

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			before, ok := original[tt.name]
			require.True(t, ok)
			after, ok := updated[tt.name+" updated"]
			require.True(t, ok)

			wantComponentID := sql.NullString{}
			if tt.componentID != nil {
				wantComponentID = sql.NullString{String: *tt.componentID, Valid: true}
			}
			assert.Equal(t, wantComponentID, before.componentID)
			assert.Equal(t, tt.componentType != models.ComponentTypeUnspecified, before.componentType.Valid)
			assert.False(t, before.closedAt.Valid)
			assert.Equal(t, before.errorID, after.errorID, "error_id must be stable across upserts")
		})
	}
}

func TestSQLErrorStore_UpsertErrors_ShouldRecoverWhenCloserLocksFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	queries := sqlc.New(db)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	deviceID, err := queries.GetDeviceIDByIdentifier(ctx, sqlc.GetDeviceIDByIdentifierParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	require.NoError(t, err)

	staleAt := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Microsecond)
	errMsg := createTestErrorMessage(deviceIdentifier)
	errMsg.FirstSeenAt = staleAt.Add(-time.Hour)
	errMsg.LastSeenAt = staleAt
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, staleAt, time.Minute))
	require.NoError(t, queries.UpsertDeviceStatus(ctx, sqlc.UpsertDeviceStatusParams{
		DeviceID:        deviceID,
		StatusTimestamp: sql.NullTime{Time: time.Now().UTC(), Valid: true},
		Status:          sqlc.DeviceStatusEnumACTIVE,
		StatusDetails:   sql.NullString{String: "{}", Valid: true},
	}))

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	closeResult, err := sqlc.New(tx).CloseStaleErrors(ctx, sqlc.CloseStaleErrorsParams{
		CutoffTime:       time.Now().UTC().Add(-3 * time.Minute),
		StatusCutoffTime: sql.NullTime{Time: time.Now().UTC().Add(-3 * time.Minute), Valid: true},
	})
	require.NoError(t, err)
	closed, err := closeResult.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), closed)

	errMsg.LastSeenAt = time.Now().UTC().Truncate(time.Microsecond)
	upsertDone := make(chan error, 1)
	go func() {
		upsertDone <- store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, time.Minute)
	}()

	requireDatabaseLockWait(t, ctx, db)
	require.NoError(t, tx.Commit())
	select {
	case err := <-upsertDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatalf("upsert did not finish after closer committed: %v", ctx.Err())
	}

	var openCount int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM errors
		WHERE org_id = $1 AND miner_error = $2 AND closed_at IS NULL`, orgID, errMsg.MinerError).
		Scan(&openCount))
	require.Equal(t, 1, openCount)
}

func TestSQLErrorStore_UpsertErrors_ShouldCloseErrorWhenClosedAtSet(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Insert open error
	errMsg := createTestErrorMessage(deviceIdentifier)
	first, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	require.Nil(t, first.ClosedAt)
	originalErrorID := first.ErrorID

	// Arrange: Close via upsert with revised details
	closedAt := time.Now().Truncate(time.Microsecond)
	errMsg.ClosedAt = &closedAt
	errMsg.LastSeenAt = closedAt
	errMsg.Severity = models.SeverityCritical
	errMsg.Summary = "Revised summary"
	errMsg.Impact = "Revised impact"
	errMsg.CauseSummary = "Revised cause"
	errMsg.RecommendedAction = "Revised action"
	errMsg.VendorCode = "E002"
	errMsg.Firmware = "v2.0.0"
	errMsg.VendorAttributes = map[string]string{"key2": "value2"}

	// Act
	closed, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, closed)
	assert.Equal(t, originalErrorID, closed.ErrorID, "ErrorID should be preserved")
	require.NotNil(t, closed.ClosedAt, "ClosedAt should be set")
	assert.Equal(t, closedAt.Unix(), closed.ClosedAt.Unix(), "ClosedAt should match")
	assert.Equal(t, errMsg.Severity, closed.Severity)
	assert.Equal(t, errMsg.Summary, closed.Summary)
	assert.Equal(t, errMsg.Impact, closed.Impact)
	assert.Equal(t, errMsg.CauseSummary, closed.CauseSummary)
	assert.Equal(t, errMsg.RecommendedAction, closed.RecommendedAction)
	assert.Equal(t, errMsg.VendorCode, closed.VendorCode)
	assert.Equal(t, errMsg.Firmware, closed.Firmware)
	assert.Equal(t, errMsg.VendorAttributes, closed.VendorAttributes)
}

func TestSQLErrorStore_UpsertErrors_ShouldInsertAlreadyClosedError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create error with ClosedAt already set (historical import)
	errMsg := createTestErrorMessage(deviceIdentifier)
	closedAt := time.Now().Add(-24 * time.Hour).Truncate(time.Microsecond)
	errMsg.ClosedAt = &closedAt

	// Act
	result, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ErrorID, "ErrorID should be generated")
	require.NotNil(t, result.ClosedAt, "ClosedAt should be populated")
	assert.Equal(t, closedAt.Unix(), result.ClosedAt.Unix())
}

func TestSQLErrorStore_UpsertErrors_ShouldCreateNewErrorWhenExistingIsClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Step 1 - Create open error
	errMsg := createTestErrorMessage(deviceIdentifier)
	errorA, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	errorAID := errorA.ErrorID

	// Arrange: Step 2 - Close error A via upsert
	closedAt := time.Now().Truncate(time.Microsecond)
	errMsg.ClosedAt = &closedAt
	errMsg.LastSeenAt = closedAt
	_, err = upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)

	// Arrange: Step 3 - Create new occurrence (same dedup key, ClosedAt=nil)
	errMsg.ClosedAt = nil
	errMsg.FirstSeenAt = time.Now().Truncate(time.Microsecond)
	errMsg.LastSeenAt = time.Now().Truncate(time.Microsecond)

	// Act
	errorB, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, errorB)
	assert.NotEqual(t, errorAID, errorB.ErrorID, "Should have NEW ErrorID")
	assert.Nil(t, errorB.ClosedAt, "New error should be open")

	// Assert: Verify both records exist in DB
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM errors WHERE org_id = $1", orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "Both error A and B should exist in DB")
}

func TestSQLErrorStore_UpsertErrors_ShouldReturnNotFoundForUnavailableDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	owner := testContext.DatabaseService.CreateSuperAdminUser()
	otherOwner := testContext.DatabaseService.CreateSuperAdminUser2()
	db := testContext.DatabaseService.DB
	store := newErrorStore(db)
	ctx := t.Context()

	softDeleted := testContext.DatabaseService.CreateDevice(owner.OrganizationID, "proto")
	_, err := db.ExecContext(ctx, `UPDATE device SET deleted_at = NOW() WHERE id = $1`, softDeleted.DatabaseID)
	require.NoError(t, err)
	otherOrgDevice := testContext.DatabaseService.CreateDevice(otherOwner.OrganizationID, "proto")

	tests := []struct {
		name             string
		deviceIdentifier string
	}{
		{name: "unknown device", deviceIdentifier: "non-existent-device"},
		{name: "soft-deleted device", deviceIdentifier: softDeleted.ID},
		{name: "device in another organization", deviceIdentifier: otherOrgDevice.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := createTestErrorMessage(tt.deviceIdentifier)

			err := store.UpsertErrors(ctx, owner.OrganizationID, tt.deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, time.Minute)

			require.Error(t, err)
			assert.True(t, fleeterror.IsNotFoundError(err), "expected NotFound, got %v", err)
			assert.Empty(t, loadErrorRows(t, ctx, db, owner.OrganizationID))
			assert.Empty(t, loadErrorRows(t, ctx, db, otherOwner.OrganizationID))
		})
	}
}

func TestSQLErrorStore_UpsertErrors_ShouldPersistEveryField(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	site, err := sqlc.New(db).CreateSite(ctx, sqlc.CreateSiteParams{OrgID: orgID, Name: "error-site", Slug: "error-site"})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE device SET site_id = $1 WHERE device_identifier = $2 AND org_id = $3`, site.ID, deviceIdentifier, orgID)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Microsecond)
	openComponentID := "psu-0"
	open := models.ErrorMessage{
		MinerError:        models.PSUOverTemperature,
		Severity:          models.SeverityCritical,
		Summary:           "PSU over temperature warning",
		Impact:            "Device may shut down",
		CauseSummary:      "PSU fan failure",
		RecommendedAction: "Replace PSU fan",
		FirstSeenAt:       now.Add(-2 * time.Hour),
		LastSeenAt:        now.Add(-time.Minute),
		DeviceID:          deviceIdentifier,
		ComponentID:       &openComponentID,
		ComponentType:     models.ComponentTypePSU,
		VendorCode:        "PSU_TEMP_HIGH",
		Firmware:          "v2.0.1",
		VendorAttributes:  map[string]string{"temp": "95", "threshold": "85"},
	}
	closedComponentID := "fan-1"
	closedAt := now.Add(-24 * time.Hour)
	alreadyClosed := models.ErrorMessage{
		MinerError:        models.FanFailed,
		Severity:          models.SeverityMajor,
		Summary:           "Fan failed and was replaced",
		Impact:            "Increased temperature",
		CauseSummary:      "Fan motor failure",
		RecommendedAction: "None",
		FirstSeenAt:       now.Add(-48 * time.Hour),
		LastSeenAt:        closedAt,
		ClosedAt:          &closedAt,
		DeviceID:          deviceIdentifier,
		ComponentID:       &closedComponentID,
		ComponentType:     models.ComponentTypeFans,
		VendorCode:        "FAN_STALL",
		Firmware:          "v1.9.0",
		VendorAttributes:  map[string]string{"rpm": "0"},
	}

	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{open, alreadyClosed}, now, time.Minute))

	rows := errorRowsByMinerError(loadErrorRows(t, ctx, db, orgID))
	require.Len(t, rows, 2)

	openRow := rows[models.PSUOverTemperature]
	assert.NotEmpty(t, openRow.errorID)
	assert.Equal(t, models.SeverityCritical, openRow.severity)
	assert.Equal(t, open.Summary, openRow.summary)
	assert.Equal(t, sql.NullString{String: open.Impact, Valid: true}, openRow.impact)
	assert.Equal(t, sql.NullString{String: open.CauseSummary, Valid: true}, openRow.causeSummary)
	assert.Equal(t, sql.NullString{String: open.RecommendedAction, Valid: true}, openRow.recommendedAction)
	assert.True(t, openRow.firstSeenAt.Equal(open.FirstSeenAt))
	assert.True(t, openRow.lastSeenAt.Equal(open.LastSeenAt))
	assert.False(t, openRow.closedAt.Valid)
	assert.Equal(t, sql.NullString{String: openComponentID, Valid: true}, openRow.componentID)
	assert.Equal(t, sql.NullInt32{Int32: int32(models.ComponentTypePSU), Valid: true}, openRow.componentType) // #nosec G115 -- ComponentType enum values bounded (max 4)
	assert.Equal(t, sql.NullString{String: open.VendorCode, Valid: true}, openRow.vendorCode)
	assert.Equal(t, sql.NullString{String: open.Firmware, Valid: true}, openRow.firmware)
	assert.JSONEq(t, `{"temp":"95","threshold":"85"}`, string(openRow.extra))
	assert.Equal(t, sql.NullInt64{Int64: site.ID, Valid: true}, openRow.siteID)

	closedRow := rows[models.FanFailed]
	assert.NotEmpty(t, closedRow.errorID)
	assert.NotEqual(t, openRow.errorID, closedRow.errorID)
	assert.Equal(t, models.SeverityMajor, closedRow.severity)
	assert.Equal(t, alreadyClosed.Summary, closedRow.summary)
	assert.Equal(t, sql.NullString{String: alreadyClosed.Impact, Valid: true}, closedRow.impact)
	assert.Equal(t, sql.NullString{String: alreadyClosed.CauseSummary, Valid: true}, closedRow.causeSummary)
	assert.Equal(t, sql.NullString{String: alreadyClosed.RecommendedAction, Valid: true}, closedRow.recommendedAction)
	assert.True(t, closedRow.firstSeenAt.Equal(alreadyClosed.FirstSeenAt))
	assert.True(t, closedRow.lastSeenAt.Equal(closedAt))
	require.True(t, closedRow.closedAt.Valid)
	assert.True(t, closedRow.closedAt.Time.Equal(closedAt))
	assert.Equal(t, sql.NullString{String: closedComponentID, Valid: true}, closedRow.componentID)
	assert.Equal(t, sql.NullInt32{Int32: int32(models.ComponentTypeFans), Valid: true}, closedRow.componentType) // #nosec G115 -- ComponentType enum values bounded (max 4)
	assert.Equal(t, sql.NullString{String: alreadyClosed.VendorCode, Valid: true}, closedRow.vendorCode)
	assert.Equal(t, sql.NullString{String: alreadyClosed.Firmware, Valid: true}, closedRow.firmware)
	assert.JSONEq(t, `{"rpm":"0"}`, string(closedRow.extra))
	assert.Equal(t, sql.NullInt64{Int64: site.ID, Valid: true}, closedRow.siteID)

	mapped, err := store.GetErrorByErrorID(ctx, orgID, openRow.errorID)
	require.NoError(t, err)
	assert.Equal(t, deviceIdentifier, mapped.DeviceID)
	assert.Equal(t, open.MinerError, mapped.MinerError)
	assert.Equal(t, open.ComponentType, mapped.ComponentType)
	require.NotNil(t, mapped.ComponentID)
	assert.Equal(t, openComponentID, *mapped.ComponentID)
	assert.Equal(t, open.Impact, mapped.Impact)
	assert.Equal(t, open.CauseSummary, mapped.CauseSummary)
	assert.Equal(t, open.RecommendedAction, mapped.RecommendedAction)
	assert.Equal(t, open.VendorCode, mapped.VendorCode)
	assert.Equal(t, open.Firmware, mapped.Firmware)
	assert.Equal(t, open.VendorAttributes, mapped.VendorAttributes)
	assert.Nil(t, mapped.ClosedAt)

	mappedClosed, err := store.GetErrorByErrorID(ctx, orgID, closedRow.errorID)
	require.NoError(t, err)
	require.NotNil(t, mappedClosed.ClosedAt)
	assert.True(t, mappedClosed.ClosedAt.Equal(closedAt))
}

func TestSQLErrorStore_UpsertErrors_ShouldNeverMoveLastSeenAtBackwards(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()
	storedAt := time.Now().UTC().Truncate(time.Microsecond)

	errMsg := createTestErrorMessage(deviceIdentifier)
	errMsg.FirstSeenAt = storedAt.Add(-time.Hour)
	errMsg.LastSeenAt = storedAt
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, storedAt, time.Minute))

	errMsg.LastSeenAt = storedAt.Add(-10 * time.Minute)
	errMsg.Summary = "Changed with an older report"
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, storedAt.Add(-5*time.Minute), time.Minute))

	rows := loadErrorRows(t, ctx, db, orgID)
	require.Len(t, rows, 1)
	assert.Equal(t, errMsg.Summary, rows[0].summary, "changed details are written even when timestamps are older")
	assert.True(t, rows[0].lastSeenAt.Equal(storedAt), "last_seen_at moved to %s", rows[0].lastSeenAt)
}

func TestSQLErrorStore_RefreshOpenErrorsLastSeen_ShouldRefreshOnlyOpenErrorsForDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 2)
	store := newErrorStore(db)
	ctx := t.Context()
	seededAt := time.Now().UTC().Truncate(time.Microsecond)
	device, otherDevice := deviceIdentifiers[0], deviceIdentifiers[1]

	build := func(deviceID string, minerError models.MinerError) models.ErrorMessage {
		errMsg := createTestErrorMessage(deviceID)
		errMsg.MinerError = minerError
		errMsg.FirstSeenAt = seededAt.Add(-time.Hour)
		errMsg.LastSeenAt = seededAt
		return *errMsg
	}
	closedLastSeenAt := seededAt.Add(-time.Minute)
	closed := build(device, models.PSUOverTemperature)
	closed.ClosedAt = &closedLastSeenAt
	closed.LastSeenAt = closedLastSeenAt

	require.NoError(t, store.UpsertErrors(ctx, orgID, device, []models.ErrorMessage{
		build(device, models.HashboardOverTemperature),
		build(device, models.FanFailed),
		closed,
	}, seededAt, time.Minute))
	require.NoError(t, store.UpsertErrors(ctx, orgID, otherDevice, []models.ErrorMessage{build(otherDevice, models.DeviceOverTemperature)}, seededAt, time.Minute))

	refreshedAt := seededAt.Add(10 * time.Second)
	refreshed, err := store.RefreshOpenErrorsLastSeen(ctx, orgID, device, refreshedAt)
	require.NoError(t, err)
	assert.Equal(t, int64(2), refreshed, "only the device's open errors are refreshed")

	rows := errorRowsByMinerError(loadErrorRows(t, ctx, db, orgID))
	require.Len(t, rows, 4)
	assert.True(t, rows[models.HashboardOverTemperature].lastSeenAt.Equal(refreshedAt))
	assert.True(t, rows[models.FanFailed].lastSeenAt.Equal(refreshedAt))
	assert.True(t, rows[models.PSUOverTemperature].lastSeenAt.Equal(closedLastSeenAt), "closed errors must not be refreshed")
	assert.True(t, rows[models.DeviceOverTemperature].lastSeenAt.Equal(seededAt), "other devices must not be refreshed")

	_, err = store.RefreshOpenErrorsLastSeen(ctx, orgID, device, seededAt.Add(-5*time.Minute))
	require.NoError(t, err)
	rows = errorRowsByMinerError(loadErrorRows(t, ctx, db, orgID))
	assert.True(t, rows[models.HashboardOverTemperature].lastSeenAt.Equal(refreshedAt), "last_seen_at must never move backwards")
	assert.True(t, rows[models.FanFailed].lastSeenAt.Equal(refreshedAt), "last_seen_at must never move backwards")

	refreshed, err = store.RefreshOpenErrorsLastSeen(ctx, orgID, "non-existent-device", refreshedAt)
	require.NoError(t, err)
	assert.Zero(t, refreshed)
}

func TestSQLErrorStore_UpsertErrors_ShouldSanitizeUnstorableReports(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()
	observedAt := time.Now().UTC().Truncate(time.Microsecond)

	good := createTestErrorMessage(deviceIdentifier)
	good.FirstSeenAt = observedAt.Add(-time.Hour)
	good.LastSeenAt = observedAt

	oversized := strings.Repeat("é", 300)
	componentID := "fan\x00-" + oversized
	bad := createTestErrorMessage(deviceIdentifier)
	bad.MinerError = models.FanFailed
	bad.ComponentID = &componentID
	bad.ComponentType = models.ComponentTypeFans
	bad.Summary = "Fan\x00 failed"
	bad.Impact = "Impact\x00"
	bad.CauseSummary = "Cause\x00"
	bad.RecommendedAction = "Action\x00"
	bad.VendorCode = "code\x00" + oversized
	bad.Firmware = oversized
	bad.VendorAttributes = map[string]string{"key\x00": "value\x00"}
	bad.FirstSeenAt = observedAt.Add(-time.Hour)
	bad.LastSeenAt = observedAt

	batch := []models.ErrorMessage{*good, *bad}
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, batch, observedAt, time.Minute))

	original := loadErrorRows(t, ctx, db, orgID)
	rows := errorRowsByMinerError(original)
	require.Len(t, rows, 2)
	assert.Equal(t, good.Summary, rows[good.MinerError].summary)

	fan := rows[models.FanFailed]
	assert.Equal(t, "Fan failed", fan.summary)
	assert.Equal(t, sql.NullString{String: "Impact", Valid: true}, fan.impact)
	assert.Equal(t, sql.NullString{String: "Cause", Valid: true}, fan.causeSummary)
	assert.Equal(t, sql.NullString{String: "Action", Valid: true}, fan.recommendedAction)
	assert.Equal(t, sql.NullString{String: "fan-" + strings.Repeat("é", 251), Valid: true}, fan.componentID)
	assert.Equal(t, sql.NullString{String: "code" + strings.Repeat("é", 251), Valid: true}, fan.vendorCode)
	assert.Equal(t, sql.NullString{String: strings.Repeat("é", 255), Valid: true}, fan.firmware)
	assert.JSONEq(t, `{"key":"value"}`, string(fan.extra))

	later := observedAt.Add(30 * time.Second)
	batch[1].Summary = "Fan\x00 still failed"
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, batch, later, time.Minute))
	updated := errorRowsByMinerError(loadErrorRows(t, ctx, db, orgID))
	require.Len(t, updated, 2)
	assert.Equal(t, fan.errorID, updated[models.FanFailed].errorID, "a truncated component id must keep matching its open row")
	assert.Equal(t, "Fan still failed", updated[models.FanFailed].summary)
}

func TestSQLErrorStore_GetErrorByErrorID_ShouldReturnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	errMsg := createTestErrorMessage(deviceIdentifier)
	inserted, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)

	// Act
	result, err := store.GetErrorByErrorID(ctx, orgID, inserted.ErrorID)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, deviceIdentifier, result.DeviceID)
	assert.Equal(t, inserted.ErrorID, result.ErrorID)
	assert.Equal(t, inserted.MinerError, result.MinerError)
	assert.Equal(t, inserted.Summary, result.Summary)
	assert.Equal(t, inserted.CauseSummary, result.CauseSummary)
	assert.Equal(t, inserted.RecommendedAction, result.RecommendedAction)
	assert.Equal(t, inserted.FirstSeenAt, result.FirstSeenAt)
	assert.Equal(t, inserted.LastSeenAt, result.LastSeenAt)
	assert.Equal(t, inserted.ClosedAt, result.ClosedAt)
	assert.Equal(t, inserted.VendorAttributes, result.VendorAttributes)
	assert.Equal(t, inserted.VendorCode, result.VendorCode)
	assert.Equal(t, inserted.Firmware, result.Firmware)
}

// ============================================================================
// QueryErrors Tests
// ============================================================================

func TestSQLErrorStore_QueryErrors_ShouldReturnAllOpenErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 3)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create 3 errors on different devices
	for i, identifier := range deviceIdentifiers {
		errMsg := createErrorWithSeverity(identifier, testSeverities[i], testMinerErrors[i], nil)
		_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
		require.NoError(t, err)
	}

	// Act
	opts := &models.QueryOptions{OrgID: orgID, PageSize: 100}
	results, err := store.QueryErrors(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Len(t, results, 3, "Should return all 3 open errors")
}

func TestSQLErrorStore_QueryErrors_WithSeverityFilter_ShouldFilterBySeverity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 3)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create errors with different severities
	// Device 0: CRITICAL, Device 1: MAJOR, Device 2: MINOR
	severities := []models.Severity{models.SeverityCritical, models.SeverityMajor, models.SeverityMinor}
	for i, identifier := range deviceIdentifiers {
		errMsg := createErrorWithSeverity(identifier, severities[i], testMinerErrors[i], nil)
		_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
		require.NoError(t, err)
	}

	// Act: Filter by CRITICAL only
	opts := &models.QueryOptions{
		OrgID:    orgID,
		PageSize: 100,
		Filter: &models.QueryFilter{
			Severities: []models.Severity{models.SeverityCritical},
		},
	}
	results, err := store.QueryErrors(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Len(t, results, 1, "Should return only CRITICAL error")
	assert.Equal(t, models.SeverityCritical, results[0].Severity)
}

func TestSQLErrorStore_QueryErrors_WithDeviceFilter_ShouldFilterByDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 3)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create one error per device
	for i, identifier := range deviceIdentifiers {
		errMsg := createErrorWithSeverity(identifier, models.SeverityMajor, testMinerErrors[i], nil)
		_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
		require.NoError(t, err)
	}

	// Act: Filter by first device only
	opts := &models.QueryOptions{
		OrgID:    orgID,
		PageSize: 100,
		Filter: &models.QueryFilter{
			DeviceIdentifiers: []string{deviceIdentifiers[0]},
		},
	}
	results, err := store.QueryErrors(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Len(t, results, 1, "Should return only errors from filtered device")
	assert.Equal(t, deviceIdentifiers[0], results[0].DeviceID)
}

// TestSQLErrorStore_QueryErrors_WithMultipleFilters_ShouldMatchAll verifies that
// when multiple filter criteria are provided, ALL must match (AND logic).
// TODO: Add OR logic test when OR filter support is implemented.
func TestSQLErrorStore_QueryErrors_WithMultipleFilters_ShouldMatchAll(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 3)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange:
	// Device 0: CRITICAL error
	// Device 1: MAJOR error
	// Device 2: MINOR error
	severities := []models.Severity{models.SeverityCritical, models.SeverityMajor, models.SeverityMinor}
	for i, identifier := range deviceIdentifiers {
		errMsg := createErrorWithSeverity(identifier, severities[i], testMinerErrors[i], nil)
		_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
		require.NoError(t, err)
	}

	// Act: Filter for CRITICAL AND device 1 (AND logic - no device matches both)
	opts := &models.QueryOptions{
		OrgID:    orgID,
		PageSize: 100,
		Filter: &models.QueryFilter{
			Severities:        []models.Severity{models.SeverityCritical},
			DeviceIdentifiers: []string{deviceIdentifiers[1]},
		},
	}
	results, err := store.QueryErrors(ctx, opts)

	// Assert: Should return nothing (no CRITICAL error on device 1)
	require.NoError(t, err)
	assert.Len(t, results, 0, "Should return no errors (no match for both criteria)")
}

func TestSQLErrorStore_QueryErrors_WithCursor_ShouldPaginate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 5)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create 5 errors, one per device with same severity for predictable ordering
	for i, identifier := range deviceIdentifiers {
		errMsg := createErrorWithSeverity(identifier, models.SeverityMajor, testMinerErrors[i], nil)
		_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure different last_seen_at for stable cursor
	}

	// Act: Get page 1 (limit 2)
	opts := &models.QueryOptions{OrgID: orgID, PageSize: 2}
	page1, err := store.QueryErrors(ctx, opts)
	require.NoError(t, err)
	assert.Len(t, page1, 2, "Page 1 should have 2 errors")

	// Build cursor from last error
	opts.PageToken = encodeCursor(page1[1].Severity, page1[1].LastSeenAt, page1[1].ErrorID)

	// Act: Get page 2
	page2, err := store.QueryErrors(ctx, opts)
	require.NoError(t, err)
	assert.Len(t, page2, 2, "Page 2 should have 2 errors")

	// Build cursor from last error of page 2
	opts.PageToken = encodeCursor(page2[1].Severity, page2[1].LastSeenAt, page2[1].ErrorID)

	// Act: Get page 3 (last page)
	page3, err := store.QueryErrors(ctx, opts)
	require.NoError(t, err)
	assert.Len(t, page3, 1, "Page 3 should have 1 error")

	// Assert: All 5 errors returned with no duplicates
	allErrorIDs := make(map[string]bool)
	for _, e := range page1 {
		allErrorIDs[e.ErrorID] = true
	}
	for _, e := range page2 {
		allErrorIDs[e.ErrorID] = true
	}
	for _, e := range page3 {
		allErrorIDs[e.ErrorID] = true
	}
	assert.Len(t, allErrorIDs, 5, "Should have 5 unique errors across all pages")
}

// ============================================================================
// CountErrors Tests
// ============================================================================

func TestSQLErrorStore_CountErrors_ShouldCountMatchingErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 3)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create 3 errors
	for i, identifier := range deviceIdentifiers {
		errMsg := createErrorWithSeverity(identifier, models.SeverityMajor, testMinerErrors[i], nil)
		_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
		require.NoError(t, err)
	}

	// Act
	opts := &models.QueryOptions{OrgID: orgID}
	count, err := store.CountErrors(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestSQLErrorStore_CountErrors_WithFilters_ShouldCountFiltered(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 3)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create errors with different severities
	severities := []models.Severity{models.SeverityCritical, models.SeverityCritical, models.SeverityMinor}
	for i, identifier := range deviceIdentifiers {
		errMsg := createErrorWithSeverity(identifier, severities[i], testMinerErrors[i], nil)
		_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
		require.NoError(t, err)
	}

	// Act: Count only CRITICAL errors
	opts := &models.QueryOptions{
		OrgID: orgID,
		Filter: &models.QueryFilter{
			Severities: []models.Severity{models.SeverityCritical},
		},
	}
	count, err := store.CountErrors(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "Should count only CRITICAL errors")
}

// ============================================================================
// Device Pagination Tests
// ============================================================================

func TestSQLErrorStore_QueryDeviceKeys_ShouldReturnUniqueDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 3)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create multiple errors per device
	for _, identifier := range deviceIdentifiers {
		// 2 errors per device with different miner errors
		for j := range 2 {
			errMsg := createErrorWithSeverity(identifier, models.SeverityMajor, testMinerErrors[j], nil)
			_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
			require.NoError(t, err)
		}
	}

	// Act
	opts := &models.QueryOptions{OrgID: orgID, PageSize: 100}
	keys, err := store.QueryDeviceKeys(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Len(t, keys, 3, "Should return 3 unique devices (not 6 errors)")
}

func TestSQLErrorStore_QueryDeviceKeys_ShouldPaginateByDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 5)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create errors with varying severities per device for predictable ordering
	// Lower severity value = worse (CRITICAL=1, MAJOR=2, etc.)
	severities := []models.Severity{
		models.SeverityCritical, // device 0: worst
		models.SeverityMajor,    // device 1
		models.SeverityMajor,    // device 2
		models.SeverityMinor,    // device 3
		models.SeverityInfo,     // device 4: best
	}
	for i, identifier := range deviceIdentifiers {
		errMsg := createErrorWithSeverity(identifier, severities[i], testMinerErrors[i], nil)
		_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
		require.NoError(t, err)
	}

	// Act: Get page 1 (limit 2)
	opts := &models.QueryOptions{OrgID: orgID, PageSize: 2}
	page1, err := store.QueryDeviceKeys(ctx, opts)
	require.NoError(t, err)
	assert.Len(t, page1, 2)

	// Build device cursor from last key
	opts.PageToken = encodeDeviceCursor(page1[1].WorstSeverity, page1[1].DeviceID)

	// Act: Get page 2
	page2, err := store.QueryDeviceKeys(ctx, opts)
	require.NoError(t, err)
	assert.Len(t, page2, 2)

	// Build device cursor
	opts.PageToken = encodeDeviceCursor(page2[1].WorstSeverity, page2[1].DeviceID)

	// Act: Get page 3 (last)
	page3, err := store.QueryDeviceKeys(ctx, opts)
	require.NoError(t, err)
	assert.Len(t, page3, 1)

	// Assert: All 5 devices returned
	allDeviceIDs := make(map[int64]bool)
	for _, k := range page1 {
		allDeviceIDs[k.DeviceID] = true
	}
	for _, k := range page2 {
		allDeviceIDs[k.DeviceID] = true
	}
	for _, k := range page3 {
		allDeviceIDs[k.DeviceID] = true
	}
	assert.Len(t, allDeviceIDs, 5, "Should have 5 unique devices across all pages")
}

func TestSQLErrorStore_CountDevices_ShouldCountUniqueDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupMultiDeviceErrorData(t, 3)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create multiple errors per device (6 errors total, 3 devices)
	for _, identifier := range deviceIdentifiers {
		for j := range 2 {
			errMsg := createErrorWithSeverity(identifier, models.SeverityMajor, testMinerErrors[j], nil)
			_, err := upsertSingleError(ctx, db, store, orgID, identifier, errMsg)
			require.NoError(t, err)
		}
	}

	// Act
	opts := &models.QueryOptions{OrgID: orgID}
	count, err := store.CountDevices(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(3), count, "Should count 3 unique devices")
}

// ============================================================================
// Component Pagination Tests
// ============================================================================

func TestSQLErrorStore_QueryComponentKeys_ShouldReturnUniqueComponents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create errors for different components on same device
	components := []*string{nil, strPtr("hashboard-0"), strPtr("hashboard-1")}
	minerErrors := []models.MinerError{models.PSUNotPresent, models.HashboardOverTemperature, models.FanFailed}
	for i, comp := range components {
		errMsg := createErrorWithSeverity(deviceIdentifier, models.SeverityMajor, minerErrors[i], comp)
		_, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)
		require.NoError(t, err)
	}

	// Act
	opts := &models.QueryOptions{OrgID: orgID, PageSize: 100}
	keys, err := store.QueryComponentKeys(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Len(t, keys, 3, "Should return 3 unique component keys")
}

func TestSQLErrorStore_QueryComponentKeys_ShouldHandleNullComponentID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create device-level error (nil ComponentID) and component-level error
	// Device-level error
	errDevice := createErrorWithSeverity(deviceIdentifier, models.SeverityMajor, models.PSUNotPresent, nil)
	_, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errDevice)
	require.NoError(t, err)

	// Component-level error
	componentID := "hashboard-0"
	errComponent := createErrorWithSeverity(deviceIdentifier, models.SeverityMinor, models.HashboardOverTemperature, &componentID)
	_, err = upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errComponent)
	require.NoError(t, err)

	// Act
	opts := &models.QueryOptions{OrgID: orgID, PageSize: 100}
	keys, err := store.QueryComponentKeys(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Len(t, keys, 2, "Should return 2 component keys (device-level + component-level)")

	// Verify one has nil ComponentID and one has non-nil
	var hasNilComponent, hasNonNilComponent bool
	for _, k := range keys {
		if k.ComponentID == nil {
			hasNilComponent = true
		} else {
			hasNonNilComponent = true
		}
	}
	assert.True(t, hasNilComponent, "Should have device-level error (nil ComponentID)")
	assert.True(t, hasNonNilComponent, "Should have component-level error")
}

func TestSQLErrorStore_CountComponents_ShouldCountUniqueComponents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create 3 unique components, with multiple errors per component
	components := []*string{nil, strPtr("hashboard-0"), strPtr("hashboard-1")}
	minerErrors := []models.MinerError{models.PSUNotPresent, models.HashboardOverTemperature, models.FanFailed}
	for i, comp := range components {
		// 2 errors per component
		for j := range 2 {
			errMsg := createErrorWithSeverity(deviceIdentifier, models.SeverityMajor, minerErrors[(i+j)%3], comp)
			_, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, errMsg)
			require.NoError(t, err)
		}
	}

	// Act
	opts := &models.QueryOptions{OrgID: orgID}
	count, err := store.CountComponents(ctx, opts)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(3), count, "Should count 3 unique components")
}

// strPtr is a helper to create string pointers for component IDs.
func strPtr(s string) *string {
	return &s
}

// ============================================================================
// CloseStaleErrors Tests
// ============================================================================

func TestSQLErrorStore_CloseStaleErrors_ShouldCloseOnlyStaleErrorsWithRecentPoll(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	for _, tc := range []struct {
		name      string
		threshold time.Duration
		statusAge time.Duration
	}{
		{"default freshness", 2 * time.Minute, 5 * time.Minute},
		{"short freshness", 20 * time.Second, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, orgID, deviceIdentifier := setupErrorTestData(t)
			store := newErrorStore(db)
			queries := sqlc.New(db)
			ctx := t.Context()

			// Get device ID for status insertion
			deviceID, err := queries.GetDeviceIDByIdentifier(ctx, sqlc.GetDeviceIDByIdentifierParams{
				DeviceIdentifier: deviceIdentifier,
				OrgID:            orgID,
			})
			require.NoError(t, err)

			// Arrange: Create a stale error (last seen 5 minutes ago)
			staleError := createTestErrorMessage(deviceIdentifier)
			staleTime := time.Now().Add(-5 * time.Minute).Truncate(time.Microsecond)
			staleError.FirstSeenAt = staleTime.Add(-time.Hour)
			staleError.LastSeenAt = staleTime
			inserted, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, staleError)
			require.NoError(t, err)

			// An unchanged status must still refresh periodically so the closer has
			// recent evidence of polling, even when the device stays ACTIVE indefinitely.
			oldStatus := time.Now().Add(-tc.statusAge).Truncate(time.Microsecond)
			err = queries.UpsertDeviceStatus(ctx, sqlc.UpsertDeviceStatusParams{
				DeviceID:        deviceID,
				StatusTimestamp: sql.NullTime{Time: oldStatus, Valid: true},
				Status:          sqlc.DeviceStatusEnumACTIVE,
				StatusDetails:   sql.NullString{String: "{}", Valid: true},
			})
			require.NoError(t, err)

			threshold := tc.threshold
			closed, err := store.CloseStaleErrors(ctx, threshold)
			require.NoError(t, err)
			require.Zero(t, closed, "old status cannot confirm a recent poll")

			deviceStore := sqlstores.NewSQLDeviceStore(db).WithStatusFreshnessThreshold(threshold)
			updates := []interfaces.DeviceStatusUpdate{{DeviceIdentifier: minermodels.DeviceIdentifier(deviceIdentifier), Status: minermodels.MinerStatusActive}}
			require.NoError(t, deviceStore.UpsertDeviceStatuses(ctx, updates))
			// A coalesced follow-up poll must also report success.
			require.NoError(t, deviceStore.UpsertDeviceStatuses(ctx, updates))

			// Act: Close stale errors using the refreshed unchanged status.
			closed, err = store.CloseStaleErrors(ctx, threshold)

			// Assert
			require.NoError(t, err)
			assert.Equal(t, int64(1), closed, "Should close 1 stale error")

			// Assert: Verify error is now closed
			closedError, err := store.GetErrorByErrorID(ctx, orgID, inserted.ErrorID)
			require.NoError(t, err)
			assert.NotNil(t, closedError.ClosedAt, "Error should be closed")
		})
	}
}

func TestSQLErrorStore_CloseStaleErrors_ShouldNotCloseIfNoRecentPoll(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	queries := sqlc.New(db)
	ctx := t.Context()

	// Get device ID for status insertion
	deviceID, err := queries.GetDeviceIDByIdentifier(ctx, sqlc.GetDeviceIDByIdentifierParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	require.NoError(t, err)

	// Arrange: Create a stale error (last seen 5 minutes ago)
	staleError := createTestErrorMessage(deviceIdentifier)
	staleTime := time.Now().Add(-5 * time.Minute).Truncate(time.Microsecond)
	staleError.FirstSeenAt = staleTime.Add(-time.Hour)
	staleError.LastSeenAt = staleTime
	inserted, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, staleError)
	require.NoError(t, err)

	// Arrange: Insert OLD device status (10 minutes ago) - no recent poll confirmation
	oldStatus := time.Now().Add(-10 * time.Minute).Truncate(time.Microsecond)
	err = queries.UpsertDeviceStatus(ctx, sqlc.UpsertDeviceStatusParams{
		DeviceID:        deviceID,
		StatusTimestamp: sql.NullTime{Time: oldStatus, Valid: true},
		Status:          sqlc.DeviceStatusEnumACTIVE,
		StatusDetails:   sql.NullString{String: "{}", Valid: true},
	})
	require.NoError(t, err)

	// Act: Close errors stale for more than 3 minutes
	threshold := 3 * time.Minute
	closed, err := store.CloseStaleErrors(ctx, threshold)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(0), closed, "Should NOT close error without recent poll")

	// Assert: Verify error is still open
	openError, err := store.GetErrorByErrorID(ctx, orgID, inserted.ErrorID)
	require.NoError(t, err)
	assert.Nil(t, openError.ClosedAt, "Error should still be open")
}

func TestSQLErrorStore_CloseStaleErrors_ShouldNotCloseRecentErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	queries := sqlc.New(db)
	ctx := t.Context()

	// Get device ID for status insertion
	deviceID, err := queries.GetDeviceIDByIdentifier(ctx, sqlc.GetDeviceIDByIdentifierParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	require.NoError(t, err)

	// Arrange: Create a RECENT error (last seen 1 minute ago)
	recentError := createTestErrorMessage(deviceIdentifier)
	recentTime := time.Now().Add(-1 * time.Minute).Truncate(time.Microsecond)
	recentError.FirstSeenAt = recentTime.Add(-time.Hour)
	recentError.LastSeenAt = recentTime
	inserted, err := upsertSingleError(ctx, db, store, orgID, deviceIdentifier, recentError)
	require.NoError(t, err)

	// Arrange: Insert recent device status
	recentStatus := time.Now().Add(-30 * time.Second).Truncate(time.Microsecond)
	err = queries.UpsertDeviceStatus(ctx, sqlc.UpsertDeviceStatusParams{
		DeviceID:        deviceID,
		StatusTimestamp: sql.NullTime{Time: recentStatus, Valid: true},
		Status:          sqlc.DeviceStatusEnumACTIVE,
		StatusDetails:   sql.NullString{String: "{}", Valid: true},
	})
	require.NoError(t, err)

	// Act: Close errors stale for more than 3 minutes
	threshold := 3 * time.Minute
	closed, err := store.CloseStaleErrors(ctx, threshold)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(0), closed, "Should NOT close recent error")

	// Assert: Verify error is still open
	openError, err := store.GetErrorByErrorID(ctx, orgID, inserted.ErrorID)
	require.NoError(t, err)
	assert.Nil(t, openError.ClosedAt, "Error should still be open")
}
