package sqlstores_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
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

func TestSQLErrorStore_UpsertError_ShouldInsertNewError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange
	errMsg := createTestErrorMessage(deviceIdentifier)

	// Act
	result, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ErrorID, "ErrorID should be populated")
	assert.Equal(t, models.HashboardOverTemperature, result.MinerError)
	assert.Equal(t, models.SeverityMajor, result.Severity)
	assert.Equal(t, "Test hashboard over temperature", result.Summary)
	assert.Equal(t, "Reduced hashrate", result.Impact)
	assert.Equal(t, "Cooling system degraded", result.CauseSummary)
	assert.Equal(t, "Check fans and airflow", result.RecommendedAction)
	assert.Equal(t, "E001", result.VendorCode)
	assert.Equal(t, "v1.2.3", result.Firmware)
	assert.Nil(t, result.ClosedAt, "ClosedAt should be nil for open error")
	assert.False(t, result.FirstSeenAt.IsZero())
	assert.False(t, result.LastSeenAt.IsZero())
}

func TestSQLErrorStore_UpsertError_ShouldUpdateExistingOpenError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Insert first error
	errMsg := createTestErrorMessage(deviceIdentifier)
	first, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
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
	updated, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

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

	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, observedAt, time.Minute, false))

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
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, time.Minute, false))
	require.Equal(t, initial, readError(), "an unchanged error inside the heartbeat must not rewrite its row")

	errMsg.LastSeenAt = observedAt.Add(60 * time.Second)
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, time.Minute, false))
	afterHeartbeat := readError()
	require.NotEqual(t, initial.tuple, afterHeartbeat.tuple)
	require.True(t, afterHeartbeat.lastSeenAt.Equal(errMsg.LastSeenAt))

	errMsg.Summary = "Changed before the next heartbeat"
	errMsg.LastSeenAt = observedAt.Add(61 * time.Second)
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, time.Minute, false))
	afterChange := readError()
	require.NotEqual(t, afterHeartbeat.tuple, afterChange.tuple, "changed details must be written immediately")
	require.Equal(t, errMsg.Summary, afterChange.summary)
	require.True(t, afterChange.lastSeenAt.Equal(errMsg.LastSeenAt))
}

func TestSQLErrorStore_UpsertErrors_ShouldRefreshBeforeConfiguredStaleness(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	queries := sqlc.New(db)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	initialObservedAt := now.Add(-30 * time.Second)
	errMsg := createTestErrorMessage(deviceIdentifier)
	errMsg.FirstSeenAt = initialObservedAt.Add(-time.Hour)
	errMsg.LastSeenAt = initialObservedAt
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, initialObservedAt, 10*time.Second, false))

	recentObservedAt := now.Add(-15 * time.Second)
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, recentObservedAt, 10*time.Second, false))

	deviceID, err := queries.GetDeviceIDByIdentifier(ctx, sqlc.GetDeviceIDByIdentifierParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	require.NoError(t, err)
	require.NoError(t, queries.UpsertDeviceStatus(ctx, sqlc.UpsertDeviceStatusParams{
		DeviceID:        deviceID,
		StatusTimestamp: sql.NullTime{Time: now, Valid: true},
		Status:          sqlc.DeviceStatusEnumACTIVE,
		StatusDetails:   sql.NullString{String: "{}", Valid: true},
	}))

	closed, err := store.CloseStaleErrors(ctx, 20*time.Second)
	require.NoError(t, err)
	require.Zero(t, closed)

	var lastSeenAt time.Time
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT last_seen_at
		FROM errors
		WHERE org_id = $1 AND miner_error = $2 AND closed_at IS NULL`, orgID, errMsg.MinerError).
		Scan(&lastSeenAt))
	require.True(t, lastSeenAt.Equal(recentObservedAt))
}

func TestSQLErrorStore_UpsertErrors_ShouldRefreshAllOpenErrorsForPartialReport(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	reported := createTestErrorMessage(deviceIdentifier)
	reported.FirstSeenAt = observedAt.Add(-time.Hour)
	reported.LastSeenAt = observedAt
	omitted := createTestErrorMessage(deviceIdentifier)
	omitted.MinerError = models.FanFailed
	omitted.FirstSeenAt = observedAt.Add(-time.Hour)
	omitted.LastSeenAt = observedAt

	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*reported, *omitted}, observedAt, time.Minute, false))

	partialObservedAt := observedAt.Add(30 * time.Second)
	reported.LastSeenAt = partialObservedAt
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*reported}, partialObservedAt, time.Minute, true))

	rows, err := db.QueryContext(ctx, `
		SELECT e.miner_error, e.last_seen_at
		FROM errors e
		JOIN device d ON d.id = e.device_id
		WHERE e.org_id = $1
		  AND d.device_identifier = $2
		  AND e.closed_at IS NULL
		ORDER BY e.miner_error`, orgID, deviceIdentifier)
	require.NoError(t, err)
	defer rows.Close()

	lastSeenByError := make(map[models.MinerError]time.Time)
	for rows.Next() {
		var minerError models.MinerError
		var lastSeenAt time.Time
		require.NoError(t, rows.Scan(&minerError, &lastSeenAt))
		lastSeenByError[minerError] = lastSeenAt
	}
	require.NoError(t, rows.Err())
	require.True(t, lastSeenByError[reported.MinerError].Equal(partialObservedAt))
	require.True(t, lastSeenByError[omitted.MinerError].Equal(partialObservedAt), "partial reports must keep omitted open errors alive")
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

	require.NoError(t, store.UpsertErrors(
		ctx,
		orgID,
		deviceIdentifier,
		[]models.ErrorMessage{*first, last},
		observedAt,
		time.Minute,
		false,
	))

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

func TestSQLErrorStore_UpsertErrors_ShouldKeepDifferentComponentTypesDistinct(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	componentID := "0"
	hashboard := createTestErrorMessage(deviceIdentifier)
	hashboard.ComponentID = &componentID
	hashboard.ComponentType = models.ComponentTypeHashBoards
	hashboard.FirstSeenAt = observedAt.Add(-time.Hour)
	hashboard.LastSeenAt = observedAt
	controlBoard := *hashboard
	controlBoard.ComponentType = models.ComponentTypeControlBoard
	controlBoard.Summary = "Control board temperature"

	require.NoError(t, store.UpsertErrors(
		ctx,
		orgID,
		deviceIdentifier,
		[]models.ErrorMessage{*hashboard, controlBoard},
		observedAt,
		time.Minute,
		false,
	))

	type persistedError struct {
		errorID string
		summary string
	}
	loadErrors := func() map[models.ComponentType]persistedError {
		rows, err := db.QueryContext(ctx, `
			SELECT error_id, component_type, summary
			FROM errors
			WHERE org_id = $1
			  AND miner_error = $2
			  AND component_id = $3
			  AND closed_at IS NULL`, orgID, hashboard.MinerError, componentID)
		require.NoError(t, err)
		defer rows.Close()

		persisted := make(map[models.ComponentType]persistedError)
		for rows.Next() {
			var componentType models.ComponentType
			var occurrence persistedError
			require.NoError(t, rows.Scan(&occurrence.errorID, &componentType, &occurrence.summary))
			persisted[componentType] = occurrence
		}
		require.NoError(t, rows.Err())
		return persisted
	}

	original := loadErrors()
	require.Len(t, original, 2)
	require.Contains(t, original, models.ComponentTypeHashBoards)
	require.Contains(t, original, models.ComponentTypeControlBoard)

	updatedAt := observedAt.Add(30 * time.Second)
	hashboard.LastSeenAt = updatedAt
	hashboard.Summary = "Updated hashboard temperature"
	controlBoard.LastSeenAt = updatedAt
	controlBoard.Summary = "Updated control board temperature"
	require.NoError(t, store.UpsertErrors(
		ctx,
		orgID,
		deviceIdentifier,
		[]models.ErrorMessage{controlBoard, *hashboard},
		updatedAt,
		time.Minute,
		false,
	))

	updated := loadErrors()
	require.Len(t, updated, 2)
	require.Equal(t, original[models.ComponentTypeHashBoards].errorID, updated[models.ComponentTypeHashBoards].errorID)
	require.Equal(t, hashboard.Summary, updated[models.ComponentTypeHashBoards].summary)
	require.Equal(t, original[models.ComponentTypeControlBoard].errorID, updated[models.ComponentTypeControlBoard].errorID)
	require.Equal(t, controlBoard.Summary, updated[models.ComponentTypeControlBoard].summary)
}

func TestSQLErrorStore_UpsertErrors_ShouldPreserveCloseAndReopenLifecycle(t *testing.T) {
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
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, observedAt, time.Minute, false))

	var originalErrorID string
	var originalFirstSeenAt time.Time
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT error_id, first_seen_at
		FROM errors
		WHERE org_id = $1 AND miner_error = $2 AND closed_at IS NULL`, orgID, errMsg.MinerError).
		Scan(&originalErrorID, &originalFirstSeenAt))

	closedAt := observedAt.Add(time.Second)
	errMsg.LastSeenAt = closedAt
	errMsg.ClosedAt = &closedAt
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, closedAt, time.Minute, false))

	reopenedAt := closedAt.Add(time.Second)
	errMsg.FirstSeenAt = reopenedAt
	errMsg.LastSeenAt = reopenedAt
	errMsg.ClosedAt = nil
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, reopenedAt, time.Minute, false))

	rows, err := db.QueryContext(ctx, `
		SELECT error_id, first_seen_at, closed_at
		FROM errors
		WHERE org_id = $1 AND miner_error = $2
		ORDER BY id`, orgID, errMsg.MinerError)
	require.NoError(t, err)
	defer rows.Close()

	type occurrence struct {
		errorID     string
		firstSeenAt time.Time
		closedAt    sql.NullTime
	}
	var occurrences []occurrence
	for rows.Next() {
		var occurrence occurrence
		require.NoError(t, rows.Scan(&occurrence.errorID, &occurrence.firstSeenAt, &occurrence.closedAt))
		occurrences = append(occurrences, occurrence)
	}
	require.NoError(t, rows.Err())
	require.Len(t, occurrences, 2)
	require.Equal(t, originalErrorID, occurrences[0].errorID)
	require.True(t, occurrences[0].firstSeenAt.Equal(originalFirstSeenAt))
	require.True(t, occurrences[0].closedAt.Valid)
	require.NotEqual(t, originalErrorID, occurrences[1].errorID)
	require.True(t, occurrences[1].firstSeenAt.Equal(reopenedAt))
	require.False(t, occurrences[1].closedAt.Valid)
}

func TestSQLErrorStore_UpsertErrors_ShouldRecoverWhenCloserLocksFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	for _, refreshAllOpen := range []bool{false, true} {
		t.Run(fmt.Sprintf("refresh_all_open=%t", refreshAllOpen), func(t *testing.T) {
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
			require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, staleAt, time.Minute, false))
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
				upsertDone <- store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, errMsg.LastSeenAt, time.Minute, refreshAllOpen)
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
		})
	}
}

func TestSQLErrorStore_UpsertErrors_ShouldPreventConcurrentCloserAfterRefresh(t *testing.T) {
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
	require.NoError(t, store.UpsertErrors(ctx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, staleAt, time.Minute, false))
	require.NoError(t, queries.UpsertDeviceStatus(ctx, sqlc.UpsertDeviceStatusParams{
		DeviceID:        deviceID,
		StatusTimestamp: sql.NullTime{Time: time.Now().UTC(), Valid: true},
		Status:          sqlc.DeviceStatusEnumACTIVE,
		StatusDetails:   sql.NullString{String: "{}", Valid: true},
	}))

	refreshedAt := time.Now().UTC().Truncate(time.Microsecond)
	errMsg.LastSeenAt = refreshedAt
	refreshComplete := make(chan struct{})
	releaseRefresh := make(chan struct{})
	upsertDone := make(chan error, 1)
	transactor := sqlstores.NewSQLTransactor(db)
	go func() {
		upsertDone <- transactor.RunInTxNoRetry(ctx, func(txCtx context.Context) error {
			if err := store.UpsertErrors(txCtx, orgID, deviceIdentifier, []models.ErrorMessage{*errMsg}, refreshedAt, time.Minute, false); err != nil {
				return err
			}
			close(refreshComplete)
			select {
			case <-releaseRefresh:
				return nil
			case <-txCtx.Done():
				return txCtx.Err()
			}
		})
	}()
	select {
	case <-refreshComplete:
	case err := <-upsertDone:
		require.NoError(t, err)
		t.Fatal("refresh transaction ended before the competing closer started")
	case <-ctx.Done():
		t.Fatalf("refresh transaction did not acquire its row lock: %v", ctx.Err())
	}

	type closeResult struct {
		count int64
		err   error
	}
	closerDone := make(chan closeResult, 1)
	go func() {
		count, err := store.CloseStaleErrors(ctx, 3*time.Minute)
		closerDone <- closeResult{count: count, err: err}
	}()

	requireDatabaseLockWait(t, ctx, db)
	close(releaseRefresh)
	select {
	case err := <-upsertDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatalf("refresh transaction did not finish after release: %v", ctx.Err())
	}
	var result closeResult
	select {
	case result = <-closerDone:
	case <-ctx.Done():
		t.Fatalf("closer did not finish after refresh committed: %v", ctx.Err())
	}
	require.NoError(t, result.err)
	require.Zero(t, result.count)

	var openCount int
	var lastSeenAt time.Time
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT count(*) OVER (), last_seen_at
		FROM errors
		WHERE org_id = $1 AND miner_error = $2 AND closed_at IS NULL`, orgID, errMsg.MinerError).
		Scan(&openCount, &lastSeenAt))
	require.Equal(t, 1, openCount)
	require.True(t, lastSeenAt.Equal(refreshedAt))
}

func TestSQLErrorStore_UpsertErrors_ShouldRetryCompleteBatchAfterDeadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	first := createTestErrorMessage(deviceIdentifier)
	first.FirstSeenAt = observedAt.Add(-time.Hour)
	first.LastSeenAt = observedAt
	second := *first
	second.MinerError = models.FanFailed
	second.Summary = "Fan failed"
	require.NoError(t, store.UpsertErrors(
		ctx,
		orgID,
		deviceIdentifier,
		[]models.ErrorMessage{*first, second},
		observedAt,
		time.Minute,
		false,
	))

	_, err := db.ExecContext(ctx, `
		CREATE SEQUENCE error_batch_retry_attempt;
		CREATE FUNCTION fail_first_error_batch_update() RETURNS trigger AS $$
		BEGIN
			IF nextval('error_batch_retry_attempt') = 1 THEN
				RAISE EXCEPTION 'injected deadlock victim' USING ERRCODE = '40P01';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER error_batch_retry
		BEFORE UPDATE ON errors
		FOR EACH ROW EXECUTE FUNCTION fail_first_error_batch_update();`)
	require.NoError(t, err)

	retriedAt := observedAt.Add(30 * time.Second)
	first.LastSeenAt = retriedAt
	first.Summary = "Hashboard recovered after retry"
	second.LastSeenAt = retriedAt
	second.Summary = "Fan recovered after retry"
	require.NoError(t, store.UpsertErrors(
		ctx,
		orgID,
		deviceIdentifier,
		[]models.ErrorMessage{second, *first},
		retriedAt,
		time.Minute,
		false,
	))

	rows, err := db.QueryContext(ctx, `
		SELECT miner_error, summary
		FROM errors
		WHERE org_id = $1 AND closed_at IS NULL`, orgID)
	require.NoError(t, err)
	defer rows.Close()

	summaries := make(map[models.MinerError]string)
	for rows.Next() {
		var minerError models.MinerError
		var summary string
		require.NoError(t, rows.Scan(&minerError, &summary))
		summaries[minerError] = summary
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[models.MinerError]string{
		first.MinerError:  first.Summary,
		second.MinerError: second.Summary,
	}, summaries)
}

func TestSQLErrorStore_UpsertError_ShouldCloseErrorWhenClosedAtSet(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Insert open error
	errMsg := createTestErrorMessage(deviceIdentifier)
	first, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	require.Nil(t, first.ClosedAt)
	originalErrorID := first.ErrorID

	// Arrange: Close via upsert
	closedAt := time.Now().Truncate(time.Microsecond)
	errMsg.ClosedAt = &closedAt
	errMsg.LastSeenAt = closedAt

	// Act
	closed, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, closed)
	assert.Equal(t, originalErrorID, closed.ErrorID, "ErrorID should be preserved")
	require.NotNil(t, closed.ClosedAt, "ClosedAt should be set")
	assert.Equal(t, closedAt.Unix(), closed.ClosedAt.Unix(), "ClosedAt should match")
}

func TestSQLErrorStore_UpsertError_ShouldInsertAlreadyClosedError(t *testing.T) {
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
	result, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ErrorID, "ErrorID should be generated")
	require.NotNil(t, result.ClosedAt, "ClosedAt should be populated")
	assert.Equal(t, closedAt.Unix(), result.ClosedAt.Unix())
}

func TestSQLErrorStore_UpsertError_ShouldCreateNewErrorWhenExistingIsClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Step 1 - Create open error
	errMsg := createTestErrorMessage(deviceIdentifier)
	errorA, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	errorAID := errorA.ErrorID

	// Arrange: Step 2 - Close error A via upsert
	closedAt := time.Now().Truncate(time.Microsecond)
	errMsg.ClosedAt = &closedAt
	errMsg.LastSeenAt = closedAt
	_, err = store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)

	// Arrange: Step 3 - Create new occurrence (same dedup key, ClosedAt=nil)
	errMsg.ClosedAt = nil
	errMsg.FirstSeenAt = time.Now().Truncate(time.Microsecond)
	errMsg.LastSeenAt = time.Now().Truncate(time.Microsecond)

	// Act
	errorB, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

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

func TestSQLErrorStore_UpsertError_ShouldDedupWithNullComponents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Insert error with NULL component_id and Unspecified component_type
	errMsg := createTestErrorMessage(deviceIdentifier)
	errMsg.ComponentID = nil
	errMsg.ComponentType = models.ComponentTypeUnspecified

	first, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	originalErrorID := first.ErrorID

	// Arrange: Update with same dedup key (both components NULL)
	errMsg.Summary = "Updated with null components"

	// Act
	updated, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, originalErrorID, updated.ErrorID, "Should update existing error")
	assert.Equal(t, "Updated with null components", updated.Summary)

	// Assert: Verify only one record exists
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM errors WHERE org_id = $1", orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Should have only one error record")
}

func TestSQLErrorStore_UpsertError_ShouldNotDedupWhenComponentsDiffer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Insert error with ComponentID="hashboard-0"
	errMsg := createTestErrorMessage(deviceIdentifier)
	componentID := "hashboard-0"
	errMsg.ComponentID = &componentID
	errMsg.ComponentType = models.ComponentTypeHashBoards

	first, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	firstErrorID := first.ErrorID

	// Arrange: Insert error with ComponentID=nil (different dedup key)
	errMsg.ComponentID = nil
	errMsg.ComponentType = models.ComponentTypeUnspecified

	// Act
	second, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.NotEqual(t, firstErrorID, second.ErrorID, "Should create NEW error (different dedup key)")

	// Assert: Verify two records exist
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM errors WHERE org_id = $1", orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "Should have two error records")
}

func TestSQLErrorStore_UpsertError_ShouldReturnErrorForUnknownDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create org only, no device
	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID: "test-org-no-device",
		Name:  "Test Org No Device",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	errMsg := createTestErrorMessage("non-existent-device")

	// Act
	result, err := store.UpsertError(ctx, orgID, "non-existent-device", errMsg)

	// Assert
	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, fleeterror.IsNotFoundError(err), "Should return NotFound error")
}

func TestSQLErrorStore_UpsertError_ShouldMapAllFieldsCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifier := setupErrorTestData(t)
	store := newErrorStore(db)
	ctx := t.Context()

	// Arrange: Create error with ALL fields populated
	componentID := "psu-0"
	errMsg := &models.ErrorMessage{
		MinerError:        models.PSUOverTemperature,
		Severity:          models.SeverityCritical,
		Summary:           "PSU over temperature warning",
		Impact:            "Device may shut down",
		CauseSummary:      "PSU fan failure",
		RecommendedAction: "Replace PSU fan",
		FirstSeenAt:       time.Now().Add(-2 * time.Hour).Truncate(time.Microsecond),
		LastSeenAt:        time.Now().Truncate(time.Microsecond),
		ClosedAt:          nil,
		DeviceID:          deviceIdentifier,
		ComponentID:       &componentID,
		ComponentType:     models.ComponentTypePSU,
		VendorCode:        "PSU_TEMP_HIGH",
		Firmware:          "v2.0.1",
		VendorAttributes:  map[string]string{"temp": "95", "threshold": "85"},
	}

	// Act
	result, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)

	// Assert: Verify all fields
	assert.NotEmpty(t, result.ErrorID)
	assert.Equal(t, models.PSUOverTemperature, result.MinerError)
	assert.Equal(t, models.SeverityCritical, result.Severity)
	assert.Equal(t, "PSU over temperature warning", result.Summary)
	assert.Equal(t, "Device may shut down", result.Impact)
	assert.Equal(t, "PSU fan failure", result.CauseSummary)
	assert.Equal(t, "Replace PSU fan", result.RecommendedAction)
	assert.Equal(t, deviceIdentifier, result.DeviceID)
	require.NotNil(t, result.ComponentID)
	assert.Equal(t, "psu-0", *result.ComponentID)
	assert.Equal(t, models.ComponentTypePSU, result.ComponentType)
	assert.Equal(t, "PSU_TEMP_HIGH", result.VendorCode)
	assert.Equal(t, "v2.0.1", result.Firmware)
	assert.Nil(t, result.ClosedAt)

	// Assert: Verify VendorAttributes stored in DB as JSON
	var extraJSON []byte
	err = db.QueryRowContext(ctx,
		"SELECT extra FROM errors WHERE error_id = $1", result.ErrorID).Scan(&extraJSON)
	require.NoError(t, err)
	assert.Contains(t, string(extraJSON), `"temp"`)
	assert.Contains(t, string(extraJSON), `"95"`)
	assert.Contains(t, string(extraJSON), `"threshold"`)
	assert.Contains(t, string(extraJSON), `"85"`)
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
	inserted, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
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
		_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
		_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
		_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
		_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
		_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
		_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
		_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
			_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
		_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
			_, err := store.UpsertError(ctx, orgID, identifier, errMsg)
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
		_, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
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
	_, err := store.UpsertError(ctx, orgID, deviceIdentifier, errDevice)
	require.NoError(t, err)

	// Component-level error
	componentID := "hashboard-0"
	errComponent := createErrorWithSeverity(deviceIdentifier, models.SeverityMinor, models.HashboardOverTemperature, &componentID)
	_, err = store.UpsertError(ctx, orgID, deviceIdentifier, errComponent)
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
			_, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
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
	inserted, err := store.UpsertError(ctx, orgID, deviceIdentifier, staleError)
	require.NoError(t, err)

	// Arrange: Insert recent device status (1 minute ago) - confirms device was polled
	recentStatus := time.Now().Add(-1 * time.Minute).Truncate(time.Microsecond)
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
	assert.Equal(t, int64(1), closed, "Should close 1 stale error")

	// Assert: Verify error is now closed
	closedError, err := store.GetErrorByErrorID(ctx, orgID, inserted.ErrorID)
	require.NoError(t, err)
	assert.NotNil(t, closedError.ClosedAt, "Error should be closed")
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
	inserted, err := store.UpsertError(ctx, orgID, deviceIdentifier, staleError)
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
	inserted, err := store.UpsertError(ctx, orgID, deviceIdentifier, recentError)
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
