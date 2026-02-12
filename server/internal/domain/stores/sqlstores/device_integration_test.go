package sqlstores_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	minermodels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
)

// TestGetOfflineDevices_DatabaseIntegration tests the GetOfflineDevices query
// against a real PostgreSQL database to validate SQL syntax and JOIN conditions
func TestGetOfflineDevices_DatabaseIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Get test database connection (migrations already applied)
	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	// Create store
	store := sqlstores.NewSQLDeviceStore(conn)

	// Seed test data
	setupOfflineDeviceTestData(t, conn)

	// Execute the ACTUAL query - this would have caught the JOIN bug
	devices, err := store.GetOfflineDevices(ctx, 10)
	require.NoError(t, err, "GetOfflineDevices query should succeed")

	// Validate results
	require.Len(t, devices, 2, "Should return 2 offline devices")

	// Verify first device
	device1 := findDeviceByIdentifier(devices, "test-device-001")
	require.NotNil(t, device1, "Should find test-device-001")
	require.Equal(t, "AA:BB:CC:DD:EE:01", device1.MacAddress)
	require.Equal(t, "proto", device1.DeviceType)
	require.Equal(t, "192.168.1.100", device1.LastKnownIP)
	require.Equal(t, "50051", device1.LastKnownPort)
	require.Equal(t, "grpc", device1.LastKnownURLScheme)

	// Verify second device
	device2 := findDeviceByIdentifier(devices, "test-device-002")
	require.NotNil(t, device2, "Should find test-device-002")
	require.Equal(t, "AA:BB:CC:DD:EE:02", device2.MacAddress)
}

// TestGetOfflineDevices_NoResults ensures query works even with no offline devices
func TestGetOfflineDevices_NoResults(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Get test database connection (migrations already applied)
	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	store := sqlstores.NewSQLDeviceStore(conn)

	// Don't seed any data - test empty result
	devices, err := store.GetOfflineDevices(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, devices, "Should return empty slice when no offline devices")
}

// TestGetOfflineDevices_InvalidLimit validates that invalid limit values return errors
func TestGetOfflineDevices_InvalidLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Get test database connection (migrations already applied)
	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	store := sqlstores.NewSQLDeviceStore(conn)

	tests := []struct {
		name  string
		limit int
	}{
		{"zero limit", 0},
		{"negative limit", -1},
		{"large negative limit", -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			devices, err := store.GetOfflineDevices(ctx, tt.limit)
			require.Error(t, err, "Should return error for limit %d", tt.limit)
			require.Nil(t, devices, "Should return nil devices for invalid limit")
			require.Contains(t, err.Error(), "limit must be at least 1")
		})
	}
}

// setupOfflineDeviceTestData creates test data in the database
func setupOfflineDeviceTestData(t *testing.T, conn *sql.DB) {
	t.Helper()

	// Insert organization
	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
	`)
	require.NoError(t, err)

	// Insert discovered devices
	_, err = conn.Exec(`
		INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme)
		VALUES
			(1, 1, 'test-device-001', 'proto', 'test-manufacturer', 'proto', '192.168.1.100', '50051', 'grpc'),
			(2, 1, 'test-device-002', 'proto', 'test-manufacturer', 'proto', '192.168.1.101', '50051', 'grpc'),
			(3, 1, 'test-device-003', 'proto', 'test-manufacturer', 'proto', '192.168.1.102', '50051', 'grpc')
	`)
	require.NoError(t, err)

	// Insert devices
	require.NoError(t, err)
	// Insert devices
	_, err = conn.Exec(`
		INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
		VALUES
			(1, 1, 1, 'test-device-001', 'AA:BB:CC:DD:EE:01'),
			(2, 1, 2, 'test-device-002', 'AA:BB:CC:DD:EE:02'),
			(3, 1, 3, 'test-device-003', 'AA:BB:CC:DD:EE:03')
	`)
	require.NoError(t, err)

	// Insert device pairings (all PAIRED)
	_, err = conn.Exec(`
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		VALUES
			(1, 'PAIRED', NOW()),
			(2, 'PAIRED', NOW()),
			(3, 'PAIRED', NOW())
	`)
	require.NoError(t, err)

	// Insert device status
	_, err = conn.Exec(`
		INSERT INTO device_status (device_id, status, status_timestamp)
		VALUES
			(1, 'OFFLINE', NOW()),
			(2, 'OFFLINE', NOW()),
			(3, 'ACTIVE', NOW())
	`)
	require.NoError(t, err)
}

// Helper function to find device by identifier
func findDeviceByIdentifier(devices []interfaces.OfflineDeviceInfo, identifier string) *interfaces.OfflineDeviceInfo {
	for i := range devices {
		if devices[i].DeviceIdentifier == identifier {
			return &devices[i]
		}
	}
	return nil
}

// =============================================================================
// CountMinersByState Tests - Error-Based Fleet Health Buckets
// =============================================================================

// TestCountMinersByState_ActiveWithNoErrors_ReturnsHealthyCount verifies baseline behavior:
// ACTIVE device with no errors should go to Healthy (hashing_count) bucket
func TestCountMinersByState_ActiveWithNoErrors_ReturnsHealthyCount(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	// Seed single ACTIVE device with no errors
	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// ACTIVE + no errors → Healthy
	require.Equal(t, int32(0), counts.BrokenCount, "broken_count should be 0")
	require.Equal(t, int32(1), counts.HashingCount, "hashing_count should be 1")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(0), counts.SleepingCount, "sleeping_count should be 0")
}

// TestCountMinersByState_ActiveWithCriticalError verifies error priority:
// ACTIVE device with CRITICAL error should go to Needs Attention (broken_count) bucket
func TestCountMinersByState_ActiveWithCriticalError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 1, closed: false}, // CRITICAL
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// ACTIVE + CRITICAL error → Needs Attention (error takes precedence)
	require.Equal(t, int32(1), counts.BrokenCount, "broken_count should be 1")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(0), counts.SleepingCount, "sleeping_count should be 0")
}

// TestCountMinersByState_ActiveWithMajorError verifies MAJOR severity errors
// trigger Needs Attention bucket even for ACTIVE devices
func TestCountMinersByState_ActiveWithMajorError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 2, closed: false}, // MAJOR
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// ACTIVE + MAJOR error → Needs Attention
	require.Equal(t, int32(1), counts.BrokenCount, "broken_count should be 1")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(0), counts.SleepingCount, "sleeping_count should be 0")
}

// TestCountMinersByState_ActiveWithMinorError verifies MINOR severity errors
// trigger Needs Attention bucket even for ACTIVE devices
func TestCountMinersByState_ActiveWithMinorError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 3, closed: false}, // MINOR
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// ACTIVE + MINOR error → Needs Attention
	require.Equal(t, int32(1), counts.BrokenCount, "broken_count should be 1")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(0), counts.SleepingCount, "sleeping_count should be 0")
}

// TestCountMinersByState_ActiveWithInfoError verifies INFO severity errors
// are excluded - ACTIVE device with INFO error should still be Healthy
func TestCountMinersByState_ActiveWithInfoError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 4, closed: false}, // INFO (excluded)
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// ACTIVE + INFO error → Healthy (INFO severity excluded)
	require.Equal(t, int32(0), counts.BrokenCount, "broken_count should be 0")
	require.Equal(t, int32(1), counts.HashingCount, "hashing_count should be 1")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(0), counts.SleepingCount, "sleeping_count should be 0")
}

// TestCountMinersByState_ActiveWithClosedError verifies closed errors
// are excluded - ACTIVE device with closed error should be Healthy
func TestCountMinersByState_ActiveWithClosedError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 1, closed: true}, // CRITICAL but closed
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// ACTIVE + closed error → Healthy (closed errors excluded)
	require.Equal(t, int32(0), counts.BrokenCount, "broken_count should be 0")
	require.Equal(t, int32(1), counts.HashingCount, "hashing_count should be 1")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(0), counts.SleepingCount, "sleeping_count should be 0")
}

// TestCountMinersByState_SleepingWithError verifies sleeping status takes precedence
// over errors - device should remain in Sleeping bucket
func TestCountMinersByState_SleepingWithError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "MAINTENANCE", pairingStatus: "PAIRED"},
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 2, closed: false}, // MAJOR
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// MAINTENANCE + error → Sleeping (status takes precedence)
	require.Equal(t, int32(0), counts.BrokenCount, "broken_count should be 0")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(1), counts.SleepingCount, "sleeping_count should be 1")
}

// TestCountMinersByState_ErrorStatusNoDBErrors verifies existing ERROR status
// logic still works independently - device with ERROR status but no DB errors
func TestCountMinersByState_ErrorStatusNoDBErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ERROR", pairingStatus: "PAIRED"},
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// ERROR status (no DB errors) → Needs Attention (existing logic preserved)
	require.Equal(t, int32(1), counts.BrokenCount, "broken_count should be 1")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(0), counts.SleepingCount, "sleeping_count should be 0")
}

// TestCountMinersByState_MixedFleet verifies complex scenarios with multiple
// devices in different states with various error conditions
func TestCountMinersByState_MixedFleet(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},      // Healthy
			{id: 2, identifier: "device-002", status: "ACTIVE", pairingStatus: "PAIRED"},      // Needs Attention (error)
			{id: 3, identifier: "device-003", status: "OFFLINE", pairingStatus: "PAIRED"},     // Offline
			{id: 4, identifier: "device-004", status: "MAINTENANCE", pairingStatus: "PAIRED"}, // Sleeping
		},
		errors: []testError{
			{deviceID: 2, orgID: 1, severity: 1, closed: false}, // CRITICAL on device-002
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// Expected: 1 broken (device-002), 1 hashing (device-001), 1 offline (device-003), 1 sleeping (device-004)
	require.Equal(t, int32(1), counts.BrokenCount, "broken_count should be 1")
	require.Equal(t, int32(1), counts.HashingCount, "hashing_count should be 1")
	require.Equal(t, int32(1), counts.OfflineCount, "offline_count should be 1")
	require.Equal(t, int32(1), counts.SleepingCount, "sleeping_count should be 1")
}

// TestCountMinersByState_MutualExclusivity verifies each device appears in
// exactly one bucket - sum of all buckets should equal total devices
func TestCountMinersByState_MutualExclusivity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
			{id: 2, identifier: "device-002", status: "ACTIVE", pairingStatus: "PAIRED"},
			{id: 3, identifier: "device-003", status: "OFFLINE", pairingStatus: "PAIRED"},
			{id: 4, identifier: "device-004", status: "MAINTENANCE", pairingStatus: "PAIRED"},
			{id: 5, identifier: "device-005", status: "ERROR", pairingStatus: "PAIRED"},
			{id: 6, identifier: "device-006", status: "ACTIVE", pairingStatus: "AUTHENTICATION_NEEDED"},
		},
		errors: []testError{
			{deviceID: 2, orgID: 1, severity: 1, closed: false}, // Error on one ACTIVE device
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// Verify mutual exclusivity: sum of all buckets = total devices (6)
	totalDevices := counts.BrokenCount + counts.HashingCount + counts.OfflineCount + counts.SleepingCount
	require.Equal(t, int32(6), totalDevices, "sum of all buckets should equal total devices")

	// Expected distribution:
	// - broken: 3 (device-002 with error, device-005 ERROR status, device-006 AUTHENTICATION_NEEDED)
	// - hashing: 1 (device-001)
	// - offline: 1 (device-003)
	// - sleeping: 1 (device-004)
	require.Equal(t, int32(3), counts.BrokenCount, "broken_count should be 3")
	require.Equal(t, int32(1), counts.HashingCount, "hashing_count should be 1")
	require.Equal(t, int32(1), counts.OfflineCount, "offline_count should be 1")
	require.Equal(t, int32(1), counts.SleepingCount, "sleeping_count should be 1")
}

// TestCountMinersByState_OfflineWithError verifies offline status takes precedence
// over errors - device should go to Offline bucket, not Needs Attention
func TestCountMinersByState_OfflineWithError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "OFFLINE", pairingStatus: "PAIRED"},
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 1, closed: false}, // CRITICAL
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// OFFLINE + error → Offline (status takes precedence)
	require.Equal(t, int32(0), counts.BrokenCount, "broken_count should be 0")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(1), counts.OfflineCount, "offline_count should be 1")
	require.Equal(t, int32(0), counts.SleepingCount, "sleeping_count should be 0")
}

// TestCountMinersByState_SleepingWithErrorAndAuth verifies sleeping status
// takes precedence over both errors and authentication status
func TestCountMinersByState_SleepingWithErrorAndAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "INACTIVE", pairingStatus: "AUTHENTICATION_NEEDED"},
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 2, closed: false}, // MAJOR
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// INACTIVE + AUTHENTICATION_NEEDED + error → Sleeping (status takes precedence)
	require.Equal(t, int32(0), counts.BrokenCount, "broken_count should be 0")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(1), counts.SleepingCount, "sleeping_count should be 1")
}

// TestCountMinersByState_ComplexPriority verifies the complete priority hierarchy
func TestCountMinersByState_ComplexPriority(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "OFFLINE", pairingStatus: "PAIRED"},
			{id: 2, identifier: "device-002", status: "OFFLINE", pairingStatus: "AUTHENTICATION_NEEDED"},
			{id: 3, identifier: "device-003", status: "INACTIVE", pairingStatus: "PAIRED"},
			{id: 4, identifier: "device-004", status: "MAINTENANCE", pairingStatus: "AUTHENTICATION_NEEDED"},
			{id: 5, identifier: "device-005", status: "ERROR", pairingStatus: "PAIRED"},
			{id: 6, identifier: "device-006", status: "ACTIVE", pairingStatus: "AUTHENTICATION_NEEDED"},
			{id: 7, identifier: "device-007", status: "ACTIVE", pairingStatus: "PAIRED"},
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 1, closed: false}, // OFFLINE with error
			{deviceID: 3, orgID: 1, severity: 2, closed: false}, // INACTIVE with error
			{deviceID: 7, orgID: 1, severity: 1, closed: false}, // ACTIVE with error
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// Expected distribution:
	// - offline: 2 (device-001 with error, device-002 auth needed)
	// - sleeping: 2 (device-003 with error, device-004 auth needed)
	// - broken: 3 (device-005 ERROR status, device-006 auth needed, device-007 with error)
	// - hashing: 0
	require.Equal(t, int32(3), counts.BrokenCount, "broken_count should be 3")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(2), counts.OfflineCount, "offline_count should be 2")
	require.Equal(t, int32(2), counts.SleepingCount, "sleeping_count should be 2")
}

// TestCountMinersByState_FilterConsistency verifies that filtering by needs attention
// returns exactly the devices counted in broken_count (not offline/sleeping devices with errors)
func TestCountMinersByState_FilterConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "OFFLINE", pairingStatus: "PAIRED"},               // Offline with error - should NOT be in needs attention
			{id: 2, identifier: "device-002", status: "INACTIVE", pairingStatus: "PAIRED"},              // Sleeping with error - should NOT be in needs attention
			{id: 3, identifier: "device-003", status: "ERROR", pairingStatus: "PAIRED"},                 // Error status - should be in needs attention
			{id: 4, identifier: "device-004", status: "ACTIVE", pairingStatus: "PAIRED"},                // Active with error - should be in needs attention
			{id: 5, identifier: "device-005", status: "ACTIVE", pairingStatus: "AUTHENTICATION_NEEDED"}, // Auth needed - should be in needs attention
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 1, closed: false}, // OFFLINE with error
			{deviceID: 2, orgID: 1, severity: 2, closed: false}, // INACTIVE with error
			{deviceID: 4, orgID: 1, severity: 1, closed: false}, // ACTIVE with error
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)

	// Get counts - should show 3 in broken_count
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)
	require.Equal(t, int32(3), counts.BrokenCount, "broken_count should be 3")
	require.Equal(t, int32(1), counts.OfflineCount, "offline_count should be 1")
	require.Equal(t, int32(1), counts.SleepingCount, "sleeping_count should be 1")

	// Filter by needs attention - should return exactly 3 devices
	filter := &interfaces.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusError},
	}
	miners, _, total, err := store.ListMinerStateSnapshots(ctx, 1, "", 50, filter, nil)
	require.NoError(t, err)
	require.Equal(t, int64(3), total, "total filtered count should match broken_count")
	require.Len(t, miners, 3, "filtered list should contain exactly 3 miners")

	// Verify the filtered list contains the correct devices (not offline/sleeping)
	identifiers := make(map[string]bool)
	for _, miner := range miners {
		identifiers[miner.DeviceIdentifier] = true
	}
	require.True(t, identifiers["device-003"], "should include ERROR status device")
	require.True(t, identifiers["device-004"], "should include ACTIVE device with error")
	require.True(t, identifiers["device-005"], "should include AUTHENTICATION_NEEDED device")
	require.False(t, identifiers["device-001"], "should NOT include OFFLINE device with error")
	require.False(t, identifiers["device-002"], "should NOT include INACTIVE device with error")
}

// =============================================================================
// Test Helpers for CountMinersByState
// =============================================================================

type testDevice struct {
	id            int64
	identifier    string
	status        string
	pairingStatus string
}

type testError struct {
	deviceID int64
	orgID    int64
	severity int32
	closed   bool
}

type countMinersByStateTestSetup struct {
	devices []testDevice
	errors  []testError
}

// setupCountMinersByStateTestData seeds database with test data for CountMinersByState tests
func setupCountMinersByStateTestData(t *testing.T, conn *sql.DB, setup *countMinersByStateTestSetup) {
	t.Helper()

	// Insert organization
	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
	`)
	require.NoError(t, err)

	// Insert discovered devices
	for i, device := range setup.devices {
		// Use unique IP for each device to avoid unique constraint violations on (org_id, ip_address, port)
		ipAddress := fmt.Sprintf("192.168.1.%d", 100+i)
		_, err := conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, 'proto', 'test-manufacturer', 'proto', $3, '50051', 'grpc', TRUE)
		`, device.id, device.identifier, ipAddress)
		require.NoError(t, err)
	}

	// Insert devices
	for _, device := range setup.devices {
		_, err := conn.Exec(`
			INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
			VALUES ($1, 1, $2, $3, 'AA:BB:CC:DD:EE:FF')
		`, device.id, device.id, device.identifier)
		require.NoError(t, err)
	}

	// Insert device pairings
	for _, device := range setup.devices {
		_, err := conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, $2, NOW())
		`, device.id, device.pairingStatus)
		require.NoError(t, err)
	}

	// Insert device statuses
	for _, device := range setup.devices {
		_, err := conn.Exec(`
			INSERT INTO device_status (device_id, status, status_timestamp)
			VALUES ($1, $2, NOW())
		`, device.id, device.status)
		require.NoError(t, err)
	}

	// Insert errors if provided
	for _, errData := range setup.errors {
		insertTestError(t, conn, errData.deviceID, errData.orgID, errData.severity, errData.closed)
	}
}

// insertTestError inserts an error record into the errors table for testing
func insertTestError(t *testing.T, conn *sql.DB, deviceID, orgID int64, severity int32, closed bool) {
	t.Helper()

	// Generate ULID for error_id
	errorID := ulid.Make().String()
	now := time.Now()

	var closedAt sql.NullTime
	if closed {
		closedAt = sql.NullTime{Time: now, Valid: true}
	}

	_, err := conn.Exec(`
		INSERT INTO errors (error_id, org_id, device_id, miner_error, severity, summary, first_seen_at, last_seen_at, closed_at)
		VALUES ($1, $2, $3, 1000, $4, 'Test error', $5, $6, $7)
	`, errorID, orgID, deviceID, severity, now, now, closedAt)
	require.NoError(t, err)
}

// =============================================================================
// UpsertDeviceStatuses Tests - Bulk Status Update
// =============================================================================

// TestUpsertDeviceStatuses_SuccessfulBulkUpsert verifies bulk upsert of multiple devices
func TestUpsertDeviceStatuses_SuccessfulBulkUpsert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	setupUpsertDeviceStatusesTestData(t, conn, []testDevice{
		{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
		{id: 2, identifier: "device-002", status: "ACTIVE", pairingStatus: "PAIRED"},
		{id: 3, identifier: "device-003", status: "OFFLINE", pairingStatus: "PAIRED"},
	})
	store := sqlstores.NewSQLDeviceStore(conn)
	updates := []interfaces.DeviceStatusUpdate{
		{DeviceIdentifier: "device-001", Status: minermodels.MinerStatusOffline},
		{DeviceIdentifier: "device-002", Status: minermodels.MinerStatusMaintenance},
		{DeviceIdentifier: "device-003", Status: minermodels.MinerStatusActive},
	}

	// Act
	err := store.UpsertDeviceStatuses(ctx, updates)

	// Assert
	require.NoError(t, err)
	require.Equal(t, "OFFLINE", getDeviceStatusFromDB(t, conn, 1))
	require.Equal(t, "MAINTENANCE", getDeviceStatusFromDB(t, conn, 2))
	require.Equal(t, "ACTIVE", getDeviceStatusFromDB(t, conn, 3))
}

// TestUpsertDeviceStatuses_AllDevicesNotFound verifies error when all devices are unknown
func TestUpsertDeviceStatuses_AllDevicesNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	updates := []interfaces.DeviceStatusUpdate{
		{DeviceIdentifier: "nonexistent-device-1", Status: minermodels.MinerStatusActive},
		{DeviceIdentifier: "nonexistent-device-2", Status: minermodels.MinerStatusOffline},
	}

	// Act
	err := store.UpsertDeviceStatuses(ctx, updates)

	// Assert
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// TestUpsertDeviceStatuses_PartialDevicesFound verifies partial success when some devices exist
func TestUpsertDeviceStatuses_PartialDevicesFound(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	setupUpsertDeviceStatusesTestData(t, conn, []testDevice{
		{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
	})
	store := sqlstores.NewSQLDeviceStore(conn)
	updates := []interfaces.DeviceStatusUpdate{
		{DeviceIdentifier: "device-001", Status: minermodels.MinerStatusOffline},
		{DeviceIdentifier: "nonexistent-device", Status: minermodels.MinerStatusActive},
	}

	// Act
	err := store.UpsertDeviceStatuses(ctx, updates)

	// Assert
	require.NoError(t, err)
	require.Equal(t, "OFFLINE", getDeviceStatusFromDB(t, conn, 1))
}

// TestUpsertDeviceStatuses_DuplicateDeviceIdentifiers verifies last-write-wins for duplicates
func TestUpsertDeviceStatuses_DuplicateDeviceIdentifiers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	setupUpsertDeviceStatusesTestData(t, conn, []testDevice{
		{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
	})
	store := sqlstores.NewSQLDeviceStore(conn)
	updates := []interfaces.DeviceStatusUpdate{
		{DeviceIdentifier: "device-001", Status: minermodels.MinerStatusOffline},
		{DeviceIdentifier: "device-001", Status: minermodels.MinerStatusMaintenance},
		{DeviceIdentifier: "device-001", Status: minermodels.MinerStatusActive},
	}

	// Act
	err := store.UpsertDeviceStatuses(ctx, updates)

	// Assert
	require.NoError(t, err)
	require.Equal(t, "ACTIVE", getDeviceStatusFromDB(t, conn, 1))
}

// TestUpsertDeviceStatuses_Insert verifies the insert path when no status exists
func TestUpsertDeviceStatuses_Insert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	setupUpsertDeviceStatusesTestDataNoStatus(t, conn, []testDevice{
		{id: 1, identifier: "device-001", pairingStatus: "PAIRED"},
		{id: 2, identifier: "device-002", pairingStatus: "PAIRED"},
	})
	store := sqlstores.NewSQLDeviceStore(conn)
	updates := []interfaces.DeviceStatusUpdate{
		{DeviceIdentifier: "device-001", Status: minermodels.MinerStatusActive},
		{DeviceIdentifier: "device-002", Status: minermodels.MinerStatusOffline},
	}

	// Act
	err := store.UpsertDeviceStatuses(ctx, updates)

	// Assert
	require.NoError(t, err)
	require.Equal(t, "ACTIVE", getDeviceStatusFromDB(t, conn, 1))
	require.Equal(t, "OFFLINE", getDeviceStatusFromDB(t, conn, 2))
}

// TestUpsertDeviceStatuses_Update verifies the update path when status already exists
func TestUpsertDeviceStatuses_Update(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	setupUpsertDeviceStatusesTestData(t, conn, []testDevice{
		{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"},
		{id: 2, identifier: "device-002", status: "ACTIVE", pairingStatus: "PAIRED"},
	})
	store := sqlstores.NewSQLDeviceStore(conn)
	updates := []interfaces.DeviceStatusUpdate{
		{DeviceIdentifier: "device-001", Status: minermodels.MinerStatusOffline},
		{DeviceIdentifier: "device-002", Status: minermodels.MinerStatusMaintenance},
	}

	// Act
	err := store.UpsertDeviceStatuses(ctx, updates)

	// Assert
	require.NoError(t, err)
	require.Equal(t, "OFFLINE", getDeviceStatusFromDB(t, conn, 1))
	require.Equal(t, "MAINTENANCE", getDeviceStatusFromDB(t, conn, 2))
}

// =============================================================================
// Test Helpers for UpsertDeviceStatuses
// =============================================================================

// setupUpsertDeviceStatusesTestData seeds database with test data including device status
func setupUpsertDeviceStatusesTestData(t *testing.T, conn *sql.DB, devices []testDevice) {
	t.Helper()

	// Insert organization
	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
	`)
	require.NoError(t, err)

	// Insert discovered devices and devices
	for i, device := range devices {
		ipAddress := fmt.Sprintf("192.168.1.%d", 100+i)
		_, err := conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, 'proto', 'test-manufacturer', 'proto', $3, '50051', 'grpc', TRUE)
		`, device.id, device.identifier, ipAddress)
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
			VALUES ($1, 1, $2, $3, 'AA:BB:CC:DD:EE:FF')
		`, device.id, device.id, device.identifier)
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, $2, NOW())
		`, device.id, device.pairingStatus)
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device_status (device_id, status, status_timestamp)
			VALUES ($1, $2, NOW())
		`, device.id, device.status)
		require.NoError(t, err)
	}
}

// setupUpsertDeviceStatusesTestDataNoStatus seeds database without initial device status
func setupUpsertDeviceStatusesTestDataNoStatus(t *testing.T, conn *sql.DB, devices []testDevice) {
	t.Helper()

	// Insert organization
	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
	`)
	require.NoError(t, err)

	// Insert discovered devices and devices (no status)
	for i, device := range devices {
		ipAddress := fmt.Sprintf("192.168.1.%d", 100+i)
		_, err := conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, 'proto', 'test-manufacturer', 'proto', $3, '50051', 'grpc', TRUE)
		`, device.id, device.identifier, ipAddress)
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
			VALUES ($1, 1, $2, $3, 'AA:BB:CC:DD:EE:FF')
		`, device.id, device.id, device.identifier)
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, $2, NOW())
		`, device.id, device.pairingStatus)
		require.NoError(t, err)
	}
}

// getDeviceStatusFromDB retrieves device status directly from database for test verification
func getDeviceStatusFromDB(t *testing.T, conn *sql.DB, deviceID int64) string {
	t.Helper()
	var status string
	err := conn.QueryRow(`SELECT status FROM device_status WHERE device_id = $1`, deviceID).Scan(&status)
	require.NoError(t, err)
	return status
}

// =============================================================================
// GetFilteredDeviceIds Tests - Filter-Based Device Selection
// =============================================================================

// TestGetFilteredDeviceIds_WithDeviceStatusFilter verifies filtering by device status only
func TestGetFilteredDeviceIds_WithDeviceStatusFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	queries := sqlc.New(conn)

	// Setup test data with mixed statuses
	setupFilteredDeviceIdsTestData(t, conn)

	tests := []struct {
		name          string
		deviceStatus  sqlc.DeviceStatusEnum
		expectedCount int
		expectedIDs   []int64
	}{
		{
			name:          "Filter by NEEDS_MINING_POOL status",
			deviceStatus:  sqlc.DeviceStatusEnumNEEDSMININGPOOL,
			expectedCount: 1,
			expectedIDs:   []int64{1}, // Only device 1 (PAIRED), device 4 is AUTHENTICATION_NEEDED
		},
		{
			name:          "Filter by ACTIVE status",
			deviceStatus:  sqlc.DeviceStatusEnumACTIVE,
			expectedCount: 1,
			expectedIDs:   []int64{2},
		},
		{
			name:          "Filter by OFFLINE status",
			deviceStatus:  sqlc.DeviceStatusEnumOFFLINE,
			expectedCount: 1,
			expectedIDs:   []int64{3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := sqlc.GetFilteredDeviceIdsParams{
				OrgID: 1,
				DeviceStatus: sql.NullString{
					String: string(tt.deviceStatus),
					Valid:  true,
				},
				PairingStatus: sql.NullString{Valid: false},
			}

			deviceIDs, err := queries.GetFilteredDeviceIds(ctx, params)
			require.NoError(t, err)
			require.Len(t, deviceIDs, tt.expectedCount)
			require.ElementsMatch(t, tt.expectedIDs, deviceIDs)
		})
	}
}

// TestGetFilteredDeviceIds_WithPairingStatusFilter verifies filtering by pairing status only
func TestGetFilteredDeviceIds_WithPairingStatusFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	queries := sqlc.New(conn)

	setupFilteredDeviceIdsTestData(t, conn)

	tests := []struct {
		name          string
		pairingStatus sqlc.PairingStatusEnum
		expectedCount int
		expectedIDs   []int64
	}{
		{
			name:          "Filter by PAIRED status",
			pairingStatus: sqlc.PairingStatusEnumPAIRED,
			expectedCount: 3,
			expectedIDs:   []int64{1, 2, 3},
		},
		{
			name:          "Filter by AUTHENTICATION_NEEDED status",
			pairingStatus: sqlc.PairingStatusEnumAUTHENTICATIONNEEDED,
			expectedCount: 1,
			expectedIDs:   []int64{4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := sqlc.GetFilteredDeviceIdsParams{
				OrgID:        1,
				DeviceStatus: sql.NullString{Valid: false},
				PairingStatus: sql.NullString{
					String: string(tt.pairingStatus),
					Valid:  true,
				},
			}

			deviceIDs, err := queries.GetFilteredDeviceIds(ctx, params)
			require.NoError(t, err)
			require.Len(t, deviceIDs, tt.expectedCount)
			require.ElementsMatch(t, tt.expectedIDs, deviceIDs)
		})
	}
}

// TestGetFilteredDeviceIds_WithBothFilters verifies filtering by both device and pairing status
func TestGetFilteredDeviceIds_WithBothFilters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	queries := sqlc.New(conn)

	setupFilteredDeviceIdsTestData(t, conn)

	tests := []struct {
		name          string
		deviceStatus  sqlc.DeviceStatusEnum
		pairingStatus sqlc.PairingStatusEnum
		expectedCount int
		expectedIDs   []int64
	}{
		{
			name:          "NEEDS_MINING_POOL and PAIRED",
			deviceStatus:  sqlc.DeviceStatusEnumNEEDSMININGPOOL,
			pairingStatus: sqlc.PairingStatusEnumPAIRED,
			expectedCount: 1,
			expectedIDs:   []int64{1},
		},
		{
			name:          "NEEDS_MINING_POOL and AUTHENTICATION_NEEDED",
			deviceStatus:  sqlc.DeviceStatusEnumNEEDSMININGPOOL,
			pairingStatus: sqlc.PairingStatusEnumAUTHENTICATIONNEEDED,
			expectedCount: 1,
			expectedIDs:   []int64{4},
		},
		{
			name:          "ACTIVE and PAIRED",
			deviceStatus:  sqlc.DeviceStatusEnumACTIVE,
			pairingStatus: sqlc.PairingStatusEnumPAIRED,
			expectedCount: 1,
			expectedIDs:   []int64{2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := sqlc.GetFilteredDeviceIdsParams{
				OrgID: 1,
				DeviceStatus: sql.NullString{
					String: string(tt.deviceStatus),
					Valid:  true,
				},
				PairingStatus: sql.NullString{
					String: string(tt.pairingStatus),
					Valid:  true,
				},
			}

			deviceIDs, err := queries.GetFilteredDeviceIds(ctx, params)
			require.NoError(t, err)
			require.Len(t, deviceIDs, tt.expectedCount)
			require.ElementsMatch(t, tt.expectedIDs, deviceIDs)
		})
	}
}

// TestGetFilteredDeviceIds_NoFilters verifies returning all paired devices when no filters provided
func TestGetFilteredDeviceIds_NoFilters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	queries := sqlc.New(conn)

	setupFilteredDeviceIdsTestData(t, conn)

	params := sqlc.GetFilteredDeviceIdsParams{
		OrgID:         1,
		DeviceStatus:  sql.NullString{Valid: false},
		PairingStatus: sql.NullString{Valid: false},
	}

	deviceIDs, err := queries.GetFilteredDeviceIds(ctx, params)
	require.NoError(t, err)
	// Should return all 4 devices with PAIRED status (device 4 is AUTHENTICATION_NEEDED, excluded)
	require.Len(t, deviceIDs, 3)
	require.ElementsMatch(t, []int64{1, 2, 3}, deviceIDs)
}

// TestGetFilteredDeviceIds_NoResults verifies empty result when no devices match filters
func TestGetFilteredDeviceIds_NoResults(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	queries := sqlc.New(conn)

	setupFilteredDeviceIdsTestData(t, conn)

	// Filter for a status that doesn't exist in test data
	params := sqlc.GetFilteredDeviceIdsParams{
		OrgID: 1,
		DeviceStatus: sql.NullString{
			String: string(sqlc.DeviceStatusEnumERROR),
			Valid:  true,
		},
		PairingStatus: sql.NullString{Valid: false},
	}

	deviceIDs, err := queries.GetFilteredDeviceIds(ctx, params)
	require.NoError(t, err)
	require.Empty(t, deviceIDs)
}

// TestGetFilteredDeviceIds_OnlyPairedByDefault verifies default PAIRED filter in query
func TestGetFilteredDeviceIds_OnlyPairedByDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	queries := sqlc.New(conn)

	setupFilteredDeviceIdsTestData(t, conn)

	// No filters provided - should only return PAIRED devices
	params := sqlc.GetFilteredDeviceIdsParams{
		OrgID:         1,
		DeviceStatus:  sql.NullString{Valid: false},
		PairingStatus: sql.NullString{Valid: false},
	}

	deviceIDs, err := queries.GetFilteredDeviceIds(ctx, params)
	require.NoError(t, err)
	// Should NOT include device 4 (AUTHENTICATION_NEEDED)
	require.Len(t, deviceIDs, 3)
	require.ElementsMatch(t, []int64{1, 2, 3}, deviceIDs)
	require.NotContains(t, deviceIDs, int64(4))
}

// =============================================================================
// Test Helpers for GetFilteredDeviceIds
// =============================================================================

// setupFilteredDeviceIdsTestData creates test data with mixed device and pairing statuses
func setupFilteredDeviceIdsTestData(t *testing.T, conn *sql.DB) {
	t.Helper()

	// Insert organization
	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
	`)
	require.NoError(t, err)

	// Insert discovered devices
	devices := []struct {
		id         int64
		identifier string
		ipAddress  string
	}{
		{1, "device-001", "192.168.1.101"},
		{2, "device-002", "192.168.1.102"},
		{3, "device-003", "192.168.1.103"},
		{4, "device-004", "192.168.1.104"},
	}

	for _, d := range devices {
		_, err := conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, 'proto', 'test-manufacturer', 'proto', $3, '50051', 'grpc', TRUE)
		`, d.id, d.identifier, d.ipAddress)
		require.NoError(t, err)
	}

	// Insert devices
	for _, d := range devices {
		_, err := conn.Exec(`
			INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
			VALUES ($1, 1, $2, $3, 'AA:BB:CC:DD:EE:FF')
		`, d.id, d.id, d.identifier)
		require.NoError(t, err)
	}

	// Insert device pairings with mixed statuses
	pairings := []struct {
		deviceID int64
		status   string
	}{
		{1, "PAIRED"},
		{2, "PAIRED"},
		{3, "PAIRED"},
		{4, "AUTHENTICATION_NEEDED"},
	}

	for _, p := range pairings {
		_, err := conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, $2, NOW())
		`, p.deviceID, p.status)
		require.NoError(t, err)
	}

	// Insert device statuses with mixed values
	statuses := []struct {
		deviceID int64
		status   string
	}{
		{1, "NEEDS_MINING_POOL"},
		{2, "ACTIVE"},
		{3, "OFFLINE"},
		{4, "NEEDS_MINING_POOL"},
	}

	for _, s := range statuses {
		_, err := conn.Exec(`
			INSERT INTO device_status (device_id, status, status_timestamp)
			VALUES ($1, $2, NOW())
		`, s.deviceID, s.status)
		require.NoError(t, err)
	}
}

// =============================================================================
// Telemetry/Issues Sorting Integration Tests
// =============================================================================

// TestListMinerStateSnapshots_SortByHashrate verifies telemetry-based sorting works
// against the actual device_metrics table.
func TestListMinerStateSnapshots_SortByHashrate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	setupTelemetrySortingTestData(t, conn)

	sortConfig := &interfaces.SortConfig{
		Field:     interfaces.SortFieldHashrate,
		Direction: interfaces.SortDirectionDesc,
	}

	// Act
	miners, _, _, err := store.ListMinerStateSnapshots(ctx, 1, "", 50, nil, sortConfig)

	// Assert
	require.NoError(t, err)
	require.Len(t, miners, 3)
	// Device with highest hashrate should be first (descending order)
	require.Equal(t, "device-high-hash", miners[0].DeviceIdentifier, "highest hashrate should be first")
	require.Equal(t, "device-mid-hash", miners[1].DeviceIdentifier, "medium hashrate should be second")
	require.Equal(t, "device-low-hash", miners[2].DeviceIdentifier, "lowest hashrate should be third")
}

// TestListMinerStateSnapshots_SortByIssues verifies error count sorting works
// against the actual errors table.
func TestListMinerStateSnapshots_SortByIssues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	setupIssuesSortingTestData(t, conn)

	sortConfig := &interfaces.SortConfig{
		Field:     interfaces.SortFieldIssues,
		Direction: interfaces.SortDirectionDesc,
	}

	// Act
	miners, _, _, err := store.ListMinerStateSnapshots(ctx, 1, "", 50, nil, sortConfig)

	// Assert
	require.NoError(t, err)
	require.Len(t, miners, 3)
	// Device with most open errors should be first (descending order)
	require.Equal(t, "device-many-errors", miners[0].DeviceIdentifier, "device with 3 errors should be first")
	require.Equal(t, "device-one-error", miners[1].DeviceIdentifier, "device with 1 error should be second")
	require.Equal(t, "device-no-errors", miners[2].DeviceIdentifier, "device with 0 errors should be last")
}

// setupTelemetrySortingTestData creates test devices with different hashrates for telemetry sorting tests.
func setupTelemetrySortingTestData(t *testing.T, conn *sql.DB) {
	t.Helper()

	// Insert organization
	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	// Define test devices with different hashrates
	devices := []struct {
		id         int64
		identifier string
		hashRate   float64
	}{
		{101, "device-low-hash", 100_000_000},    // 100 MH/s
		{102, "device-mid-hash", 500_000_000},    // 500 MH/s
		{103, "device-high-hash", 1_000_000_000}, // 1 TH/s
	}

	for i, d := range devices {
		// Insert discovered device
		_, err := conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, 'test-model', 'test-manufacturer', 'proto', $3, '50051', 'grpc', TRUE)
		`, d.id, d.identifier, fmt.Sprintf("192.168.100.%d", 100+i))
		require.NoError(t, err)

		// Insert device
		_, err = conn.Exec(`
			INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
			VALUES ($1, 1, $2, $3, $4)
		`, d.id, d.id, d.identifier, fmt.Sprintf("AA:BB:CC:DD:%02d:01", i))
		require.NoError(t, err)

		// Insert device pairing
		_, err = conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, 'PAIRED', NOW())
		`, d.id)
		require.NoError(t, err)

		// Insert device metrics (telemetry)
		_, err = conn.Exec(`
			INSERT INTO device_metrics (time, device_identifier, hash_rate_hs, temp_c, power_w, efficiency_jh)
			VALUES (NOW(), $1, $2, 72.5, 1500.0, 15.0)
		`, d.identifier, d.hashRate)
		require.NoError(t, err)
	}
}

// setupIssuesSortingTestData creates test devices with different error counts for issues sorting tests.
func setupIssuesSortingTestData(t *testing.T, conn *sql.DB) {
	t.Helper()

	// Insert organization
	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	// Define test devices with different error counts
	devices := []struct {
		id         int64
		identifier string
		errorCount int
	}{
		{201, "device-no-errors", 0},
		{202, "device-one-error", 1},
		{203, "device-many-errors", 3},
	}

	for i, d := range devices {
		// Insert discovered device
		_, err := conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, 'test-model', 'test-manufacturer', 'proto', $3, '50051', 'grpc', TRUE)
		`, d.id, d.identifier, fmt.Sprintf("192.168.200.%d", 100+i))
		require.NoError(t, err)

		// Insert device
		_, err = conn.Exec(`
			INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
			VALUES ($1, 1, $2, $3, $4)
		`, d.id, d.id, d.identifier, fmt.Sprintf("BB:CC:DD:EE:%02d:01", i))
		require.NoError(t, err)

		// Insert device pairing
		_, err = conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, 'PAIRED', NOW())
		`, d.id)
		require.NoError(t, err)

		// Insert open errors for this device
		for range d.errorCount {
			errorID := ulid.Make().String()
			//nolint:dupword // SQL syntax requires NOW() twice for first_seen_at and last_seen_at
			_, err = conn.Exec(`
				INSERT INTO errors (org_id, device_id, error_id, miner_error, severity, summary, first_seen_at, last_seen_at, closed_at)
				VALUES (1, $1, $2, 1, 1, 'Test error', NOW(), NOW(), NULL)
			`, d.id, errorID)
			require.NoError(t, err)
		}
	}
}
