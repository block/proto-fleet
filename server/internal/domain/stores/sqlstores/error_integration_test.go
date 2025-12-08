package sqlstores_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupErrorTestData creates an organization and device for error store tests.
// Returns orgID and deviceIdentifier for use in tests.
func setupErrorTestData(t *testing.T, db *sql.DB) (orgID int64, deviceIdentifier string) {
	t.Helper()
	ctx := t.Context()
	queries := sqlc.New(db)

	// Create organization
	orgResult, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name: "Test Error Org",
	})
	require.NoError(t, err)
	orgID, err = orgResult.LastInsertId()
	require.NoError(t, err)

	// Create discovered_device (required for device FK)
	deviceIdentifier = "test-error-device-123"
	ddResult, err := db.ExecContext(ctx, `
		INSERT INTO discovered_device (org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme)
		VALUES (?, ?, 'proto', 'test-manufacturer', 'proto', '192.168.1.100', '50051', 'grpc')
	`, orgID, deviceIdentifier)
	require.NoError(t, err)
	discoveredDeviceID, err := ddResult.LastInsertId()
	require.NoError(t, err)

	// Create device (required to resolve device_identifier)
	_, err = db.ExecContext(ctx, `
		INSERT INTO device (org_id, discovered_device_id, device_identifier, mac_address, serial_number)
		VALUES (?, ?, ?, 'AA:BB:CC:DD:EE:FF', 'SN-ERROR-TEST-001')
	`, orgID, discoveredDeviceID, deviceIdentifier)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM errors WHERE org_id = ?", orgID)
		_, _ = db.ExecContext(ctx, "DELETE FROM device WHERE org_id = ?", orgID)
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = ?", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	return orgID, deviceIdentifier
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

func TestSQLErrorStore_UpsertError_ShouldInsertNewError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLErrorStore(db)
	ctx := t.Context()
	orgID, deviceIdentifier := setupErrorTestData(t, db)

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

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLErrorStore(db)
	ctx := t.Context()
	orgID, deviceIdentifier := setupErrorTestData(t, db)

	// Insert first error
	errMsg := createTestErrorMessage(deviceIdentifier)
	first, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	originalErrorID := first.ErrorID
	originalFirstSeenAt := first.FirstSeenAt

	// Update with same dedup key but different mutable fields
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

func TestSQLErrorStore_UpsertError_ShouldCloseErrorWhenClosedAtSet(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLErrorStore(db)
	ctx := t.Context()
	orgID, deviceIdentifier := setupErrorTestData(t, db)

	// Insert open error
	errMsg := createTestErrorMessage(deviceIdentifier)
	first, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	require.Nil(t, first.ClosedAt)
	originalErrorID := first.ErrorID

	// Close via upsert
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

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLErrorStore(db)
	ctx := t.Context()
	orgID, deviceIdentifier := setupErrorTestData(t, db)

	// Create error with ClosedAt already set (historical import)
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

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLErrorStore(db)
	ctx := t.Context()
	orgID, deviceIdentifier := setupErrorTestData(t, db)

	// Step 1: Create open error
	errMsg := createTestErrorMessage(deviceIdentifier)
	errorA, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	errorAID := errorA.ErrorID

	// Step 2: Close error A via upsert
	closedAt := time.Now().Truncate(time.Microsecond)
	errMsg.ClosedAt = &closedAt
	errMsg.LastSeenAt = closedAt
	_, err = store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)

	// Step 3: Create new occurrence (same dedup key, ClosedAt=nil)
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

	// Verify both records exist in DB
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM errors WHERE org_id = ?", orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "Both error A and B should exist in DB")
}

func TestSQLErrorStore_UpsertError_ShouldDedupWithNullComponents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLErrorStore(db)
	ctx := t.Context()
	orgID, deviceIdentifier := setupErrorTestData(t, db)

	// Insert error with NULL component_id and Unspecified component_type
	errMsg := createTestErrorMessage(deviceIdentifier)
	errMsg.ComponentID = nil
	errMsg.ComponentType = models.ComponentTypeUnspecified

	first, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	originalErrorID := first.ErrorID

	// Update with same dedup key (both components NULL)
	errMsg.Summary = "Updated with null components"

	// Act
	updated, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, originalErrorID, updated.ErrorID, "Should update existing error")
	assert.Equal(t, "Updated with null components", updated.Summary)

	// Verify only one record exists
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM errors WHERE org_id = ?", orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Should have only one error record")
}

func TestSQLErrorStore_UpsertError_ShouldNotDedupWhenComponentsDiffer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLErrorStore(db)
	ctx := t.Context()
	orgID, deviceIdentifier := setupErrorTestData(t, db)

	// Insert error with ComponentID="hashboard-0"
	errMsg := createTestErrorMessage(deviceIdentifier)
	componentID := "hashboard-0"
	errMsg.ComponentID = &componentID
	errMsg.ComponentType = models.ComponentTypeHashBoards

	first, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)
	require.NoError(t, err)
	firstErrorID := first.ErrorID

	// Insert error with ComponentID=nil (different dedup key)
	errMsg.ComponentID = nil
	errMsg.ComponentType = models.ComponentTypeUnspecified

	// Act
	second, err := store.UpsertError(ctx, orgID, deviceIdentifier, errMsg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.NotEqual(t, firstErrorID, second.ErrorID, "Should create NEW error (different dedup key)")

	// Verify two records exist
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM errors WHERE org_id = ?", orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "Should have two error records")
}

func TestSQLErrorStore_UpsertError_ShouldReturnErrorForUnknownDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLErrorStore(db)
	ctx := t.Context()

	// Create org only, no device
	queries := sqlc.New(db)
	orgResult, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name: "Test Org No Device",
	})
	require.NoError(t, err)
	orgID, err := orgResult.LastInsertId()
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

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLErrorStore(db)
	ctx := t.Context()
	orgID, deviceIdentifier := setupErrorTestData(t, db)

	// Create error with ALL fields populated
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

	// Verify all fields
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

	// Verify VendorAttributes stored in DB as JSON
	var extraJSON []byte
	err = db.QueryRowContext(ctx,
		"SELECT extra FROM errors WHERE error_id = ?", result.ErrorID).Scan(&extraJSON)
	require.NoError(t, err)
	assert.Contains(t, string(extraJSON), `"temp"`)
	assert.Contains(t, string(extraJSON), `"95"`)
	assert.Contains(t, string(extraJSON), `"threshold"`)
	assert.Contains(t, string(extraJSON), `"85"`)
}
