package sqlstores_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
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
	require.Equal(t, "proto", device1.DriverName)
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

func TestGetKnownSubnets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES
			(1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key'),
			(2, '00000000-0000-0000-0000-000000000002', 'Other Org', 'test-private-key')
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	for _, fixture := range []struct {
		discoveredID  int64
		deviceID      int64
		orgID         int64
		identifier    string
		ipAddress     string
		pairingStatus string
		deleted       bool
	}{
		{discoveredID: 501, deviceID: 501, orgID: 1, identifier: "known-subnet-1", ipAddress: "192.168.10.11", pairingStatus: "PAIRED"},
		{discoveredID: 502, deviceID: 502, orgID: 1, identifier: "known-subnet-2", ipAddress: "192.168.10.99", pairingStatus: "PAIRED"},
		{discoveredID: 503, deviceID: 503, orgID: 1, identifier: "known-subnet-3", ipAddress: "192.168.11.5", pairingStatus: "AUTHENTICATION_NEEDED"},
		{discoveredID: 504, deviceID: 504, orgID: 1, identifier: "ignored-unpaired", ipAddress: "172.16.1.5", pairingStatus: "UNPAIRED"},
		{discoveredID: 505, deviceID: 505, orgID: 2, identifier: "ignored-other-org", ipAddress: "192.168.99.5", pairingStatus: "PAIRED"},
		{discoveredID: 506, deviceID: 506, orgID: 1, identifier: "ignored-deleted-device", ipAddress: "192.168.12.5", pairingStatus: "PAIRED", deleted: true},
		{discoveredID: 507, deviceID: 507, orgID: 1, identifier: "known-ipv6-1", ipAddress: "fd00::1:100", pairingStatus: "PAIRED"},
		{discoveredID: 508, deviceID: 508, orgID: 1, identifier: "known-ipv6-2", ipAddress: "fd01::2:200", pairingStatus: "PAIRED"},
	} {
		_, err = conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
			VALUES ($1, $2, $3, 'test-model', 'test-manufacturer', 'proto', $4, '50051', 'grpc', TRUE)
		`, fixture.discoveredID, fixture.orgID, fixture.identifier, fixture.ipAddress)
		require.NoError(t, err)

		if fixture.deleted {
			_, err = conn.Exec(`
				INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address, deleted_at)
				VALUES ($1, $2, $3, $4, $5, NOW())
			`, fixture.deviceID, fixture.orgID, fixture.discoveredID, fixture.identifier, fmt.Sprintf("AA:BB:CC:DD:EE:%02d", fixture.deviceID%100))
		} else {
			_, err = conn.Exec(`
				INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
				VALUES ($1, $2, $3, $4, $5)
			`, fixture.deviceID, fixture.orgID, fixture.discoveredID, fixture.identifier, fmt.Sprintf("AA:BB:CC:DD:EE:%02d", fixture.deviceID%100))
		}
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, $2, NOW())
		`, fixture.deviceID, fixture.pairingStatus)
		require.NoError(t, err)
	}

	subnets24, err := store.GetKnownSubnets(ctx, 1, 24, true)
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.10.0/24", "192.168.11.0/24"}, subnets24)

	subnets16, err := store.GetKnownSubnets(ctx, 1, 16, true)
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.0.0/16"}, subnets16)

	// IPv4 query excludes IPv6 rows
	subnets24IPv4, err := store.GetKnownSubnets(ctx, 1, 24, true)
	require.NoError(t, err)
	require.NotContains(t, subnets24IPv4, "fd00::/24")
	require.NotContains(t, subnets24IPv4, "fd01::/24")

	// IPv6 query returns only IPv6 subnets
	subnets64, err := store.GetKnownSubnets(ctx, 1, 64, false)
	require.NoError(t, err)
	require.Equal(t, []string{"fd00::/64", "fd01::/64"}, subnets64)

	// IPv6 query excludes IPv4 rows
	require.NotContains(t, subnets64, "192.168.10.0/64")
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
		INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme)
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
// are included - ACTIVE device with INFO error should be Needs Attention (broken_count).
// INFO errors are advisory but still represent a known issue that should surface.
// Only UNSPECIFIED (0) severity is excluded.
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
			{deviceID: 1, orgID: 1, severity: 4, closed: false}, // INFO (included)
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	// ACTIVE + INFO error → Needs Attention (INFO severity included, only UNSPECIFIED excluded)
	require.Equal(t, int32(1), counts.BrokenCount, "broken_count should be 1")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
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

	// INACTIVE + AUTHENTICATION_NEEDED → broken (auth overrides sleeping)
	require.Equal(t, int32(1), counts.BrokenCount, "broken_count should be 1 (auth overrides sleeping)")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(0), counts.OfflineCount, "offline_count should be 0")
	require.Equal(t, int32(0), counts.SleepingCount, "sleeping_count should be 0")
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

	// offline: 2 (device-001 OFFLINE+error, device-002 OFFLINE+auth)
	// sleeping: 1 (device-003 INACTIVE+paired+error)
	// broken: 4 (device-004 MAINTENANCE+auth, device-005 ERROR, device-006 ACTIVE+auth, device-007 ACTIVE+error)
	require.Equal(t, int32(4), counts.BrokenCount, "broken_count should be 4 (includes MAINTENANCE+auth)")
	require.Equal(t, int32(0), counts.HashingCount, "hashing_count should be 0")
	require.Equal(t, int32(2), counts.OfflineCount, "offline_count should be 2")
	require.Equal(t, int32(1), counts.SleepingCount, "sleeping_count should be 1 (MAINTENANCE+auth now broken)")
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

// TestCountMinersByState_AuthNeededNullStatus verifies auth-needed miners with
// NULL device_status go to broken_count, not offline_count.
func TestCountMinersByState_AuthNeededNullStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{devices: nullStatusDashboardFixture()})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	require.Equal(t, int32(1), counts.BrokenCount, "auth-needed with NULL status should be broken")
	require.Equal(t, int32(1), counts.OfflineCount, "paired with NULL status should be offline")
	require.Equal(t, int32(1), counts.HashingCount, "active paired should be hashing")
	require.Equal(t, int32(0), counts.SleepingCount, "no sleeping devices")
}

// TestCountMinersByState_AuthNeededInactiveStatus verifies auth-needed miners with
// INACTIVE device_status go to broken_count, not sleeping_count.
func TestCountMinersByState_AuthNeededInactiveStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "INACTIVE", pairingStatus: "AUTHENTICATION_NEEDED"}, // auth + inactive → broken
			{id: 2, identifier: "device-002", status: "INACTIVE", pairingStatus: "PAIRED"},                // paired + inactive → sleeping
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	require.Equal(t, int32(1), counts.BrokenCount, "auth-needed with INACTIVE should be broken")
	require.Equal(t, int32(1), counts.SleepingCount, "paired with INACTIVE should be sleeping")
	require.Equal(t, int32(0), counts.OfflineCount, "no offline")
	require.Equal(t, int32(0), counts.HashingCount, "no hashing")
}

// TestCountMinersByState_NullStatusFilterConsistency verifies that list filters
// return the same miners the dashboard counts as offline/needs-attention for
// NULL-status devices.
func TestCountMinersByState_NullStatusFilterConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{devices: nullStatusDashboardFixture()})

	store := sqlstores.NewSQLDeviceStore(conn)

	// Verify counts first
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), counts.BrokenCount)
	require.Equal(t, int32(1), counts.OfflineCount)
	require.Equal(t, int32(1), counts.HashingCount)

	// Filter by needs attention — should include auth-needed NULL-status miner
	needsAttentionFilter := &interfaces.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusError},
	}
	miners, _, total, err := store.ListMinerStateSnapshots(ctx, 1, "", 50, needsAttentionFilter, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), total, "needs attention filter should match 1 miner")
	require.Len(t, miners, 1)
	require.Equal(t, "device-001", miners[0].DeviceIdentifier, "should include auth-needed NULL-status miner")

	// Filter by offline — should include paired NULL-status miner
	offlineFilter := &interfaces.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusOffline},
	}
	miners, _, total, err = store.ListMinerStateSnapshots(ctx, 1, "", 50, offlineFilter, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), total, "offline filter should match 1 miner")
	require.Len(t, miners, 1)
	require.Equal(t, "device-002", miners[0].DeviceIdentifier, "should include paired NULL-status miner")
}

// TestCountMinersByState_FilteredCountsMatchList verifies that GetMinerStateCounts
// (which populates total_state_counts) agrees with ListMinerStateSnapshots total
// when a status filter is applied. Regression guard for the bucket/list disagreement
// flagged by Codex Security review.
func TestCountMinersByState_FilteredCountsMatchList(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "OFFLINE", pairingStatus: "PAIRED"},               // offline
			{id: 2, identifier: "device-002", status: "", pairingStatus: "PAIRED"},                      // NULL+paired → offline
			{id: 3, identifier: "device-003", status: "ACTIVE", pairingStatus: "PAIRED"},                // hashing
			{id: 4, identifier: "device-004", status: "", pairingStatus: "AUTHENTICATION_NEEDED"},       // auth+NULL → broken
			{id: 5, identifier: "device-005", status: "ERROR", pairingStatus: "PAIRED"},                 // broken
			{id: 6, identifier: "device-006", status: "ACTIVE", pairingStatus: "AUTHENTICATION_NEEDED"}, // auth+ACTIVE → broken
			{id: 7, identifier: "device-007", status: "ACTIVE", pairingStatus: "PAIRED"},                // ACTIVE+paired+error → broken (excluded from Hashing)
		},
		errors: []testError{
			{deviceID: 7, orgID: 1, severity: 1, closed: false}, // CRITICAL on device-007
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)

	// Offline filter: list total and offline_count in state counts must match.
	offlineFilter := &interfaces.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusOffline},
	}
	_, _, listTotal, err := store.ListMinerStateSnapshots(ctx, 1, "", 50, offlineFilter, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), listTotal, "offline list should match OFFLINE + NULL+paired")

	stateCounts, err := store.GetMinerStateCounts(ctx, 1, offlineFilter)
	require.NoError(t, err)
	require.Equal(t, listTotal, int64(stateCounts.OfflineCount),
		"offline_count must equal list total when filtering by Offline")
	require.Equal(t, int32(0), stateCounts.BrokenCount, "no broken in offline-filtered view")
	require.Equal(t, int32(0), stateCounts.HashingCount, "no hashing in offline-filtered view")
	require.Equal(t, int32(0), stateCounts.SleepingCount, "no sleeping in offline-filtered view")

	// Needs Attention filter: list total and broken_count in state counts must match.
	needsAttentionFilter := &interfaces.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusError},
	}
	_, _, listTotal, err = store.ListMinerStateSnapshots(ctx, 1, "", 50, needsAttentionFilter, nil)
	require.NoError(t, err)
	require.Equal(t, int64(4), listTotal,
		"needs-attention includes ERROR + auth-needed + paired-ACTIVE-with-error")

	stateCounts, err = store.GetMinerStateCounts(ctx, 1, needsAttentionFilter)
	require.NoError(t, err)
	require.Equal(t, listTotal, int64(stateCounts.BrokenCount),
		"broken_count must equal list total when filtering by Needs Attention")

	// Hashing filter: ACTIVE+error miners must be excluded from BOTH the list and
	// the state counts. Pre-fix, CountMinersByState admitted ACTIVE+error via the
	// bare ANY(['ACTIVE']) predicate and bucketed them as broken, causing
	// sum(buckets) > listTotal. Regression guard for that divergence.
	hashingFilter := &interfaces.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusActive},
	}
	_, _, listTotal, err = store.ListMinerStateSnapshots(ctx, 1, "", 50, hashingFilter, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), listTotal,
		"hashing list should exclude ACTIVE+error (device-007); includes device-003 and device-006")

	stateCounts, err = store.GetMinerStateCounts(ctx, 1, hashingFilter)
	require.NoError(t, err)
	require.Equal(t, int32(1), stateCounts.HashingCount, "paired+ACTIVE+no-error only (device-003)")
	require.Equal(t, int32(1), stateCounts.BrokenCount, "auth+ACTIVE+no-error (device-006); auth overrides hashing")
	require.Equal(t, int32(0), stateCounts.OfflineCount, "no offline in hashing-filtered view")
	require.Equal(t, int32(0), stateCounts.SleepingCount, "no sleeping in hashing-filtered view")
	require.Equal(t, listTotal, int64(stateCounts.HashingCount+stateCounts.BrokenCount),
		"hashing+broken buckets must sum to filtered list total")
}

// TestCountMinersByState_NullPairedWithErrorStaysOffline verifies that a PAIRED
// miner with NULL device_status and an open error is bucketed as offline
// (not Needs Attention) by both the list filter and the state counts.
func TestCountMinersByState_NullPairedWithErrorStaysOffline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "", pairingStatus: "PAIRED"}, // NULL+paired with open error
		},
		errors: []testError{
			{deviceID: 1, orgID: 1, severity: 1, closed: false},
		},
	})

	store := sqlstores.NewSQLDeviceStore(conn)

	// Offline filter matches the miner; state counts agree.
	offlineFilter := &interfaces.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusOffline},
	}
	_, _, total, err := store.ListMinerStateSnapshots(ctx, 1, "", 50, offlineFilter, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), total, "NULL+paired (with error) should be in offline filter")

	counts, err := store.GetMinerStateCounts(ctx, 1, offlineFilter)
	require.NoError(t, err)
	require.Equal(t, int32(1), counts.OfflineCount, "NULL+paired (with error) should count as offline")
	require.Equal(t, int32(0), counts.BrokenCount, "NULL+paired should NOT count as broken")

	// Needs Attention filter excludes the miner (offline trumps errors).
	needsAttentionFilter := &interfaces.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusError},
	}
	_, _, total, err = store.ListMinerStateSnapshots(ctx, 1, "", 50, needsAttentionFilter, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), total, "NULL+paired (with error) should NOT be in needs-attention filter")
}

// TestGetMinerStateCountsByCollections_AuthNeededNullStatus verifies per-collection
// counts match dashboard bucket rules for NULL device_status.
func TestGetMinerStateCountsByCollections_AuthNeededNullStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	devices := nullStatusDashboardFixture()
	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{devices: devices})

	const collectionID int64 = 100
	setupCollectionMembership(t, conn, collectionID, "group", devices)

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCountsByCollections(ctx, 1, []int64{collectionID})
	require.NoError(t, err)
	require.Contains(t, counts, collectionID)

	c := counts[collectionID]
	require.Equal(t, int32(1), c.BrokenCount, "auth-needed with NULL status should be broken")
	require.Equal(t, int32(1), c.OfflineCount, "paired with NULL status should be offline")
	require.Equal(t, int32(1), c.HashingCount, "active paired should be hashing")
	require.Equal(t, int32(0), c.SleepingCount, "no sleeping devices")
}

// TestGetMinerStateCountsByCollections_AuthNeededInactiveStatus verifies auth-needed
// miners with INACTIVE status go to broken_count, not sleeping_count.
func TestGetMinerStateCountsByCollections_AuthNeededInactiveStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	devices := []testDevice{
		{id: 1, identifier: "device-001", status: "INACTIVE", pairingStatus: "AUTHENTICATION_NEEDED"}, // auth + inactive → broken
		{id: 2, identifier: "device-002", status: "INACTIVE", pairingStatus: "PAIRED"},                // paired + inactive → sleeping
	}
	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{devices: devices})

	const collectionID int64 = 101
	setupCollectionMembership(t, conn, collectionID, "group", devices)

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCountsByCollections(ctx, 1, []int64{collectionID})
	require.NoError(t, err)
	require.Contains(t, counts, collectionID)

	c := counts[collectionID]
	require.Equal(t, int32(1), c.BrokenCount, "auth-needed with INACTIVE should be broken")
	require.Equal(t, int32(1), c.SleepingCount, "paired with INACTIVE should be sleeping")
	require.Equal(t, int32(0), c.OfflineCount, "no offline devices")
	require.Equal(t, int32(0), c.HashingCount, "no hashing devices")
}

// TestCountMinersByState_ExcludesSoftDeletedDiscoveredDevice verifies that
// soft-deleted discovered_device rows are excluded from the dashboard counts,
// matching the scope used by GetTotalMinerStateSnapshots and
// GetMinerStateCountsByCollections. Without the `dd.deleted_at IS NULL` guard
// the dashboard would disagree with the list and the per-collection counts
// whenever a discovered_device is soft-deleted independently of its device row.
func TestCountMinersByState_ExcludesSoftDeletedDiscoveredDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{
		devices: []testDevice{
			{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"}, // active — should be counted
			{id: 2, identifier: "device-002", status: "ACTIVE", pairingStatus: "PAIRED"}, // active but discovered_device soft-deleted — should be excluded
		},
	})

	// Soft-delete the second device's discovered_device row while leaving the
	// device row intact. This is the shape produced by reconciliation paths that
	// soft-delete a stale discovered_device without touching the device row.
	_, err := conn.Exec(`UPDATE discovered_device SET deleted_at = NOW() WHERE id = $1`, int64(2))
	require.NoError(t, err)

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCounts(ctx, 1, nil)
	require.NoError(t, err)

	require.Equal(t, int32(1), counts.HashingCount, "only the non-deleted active paired device should count")
	require.Equal(t, int32(0), counts.BrokenCount, "no broken devices")
	require.Equal(t, int32(0), counts.OfflineCount, "no offline devices")
	require.Equal(t, int32(0), counts.SleepingCount, "no sleeping devices")
}

// TestGetMinerStateCountsByCollections_ExcludesSoftDeletedDiscoveredDevice verifies
// that soft-deleted discovered_device rows are excluded from collection counts.
func TestGetMinerStateCountsByCollections_ExcludesSoftDeletedDiscoveredDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()

	devices := []testDevice{
		{id: 1, identifier: "device-001", status: "ACTIVE", pairingStatus: "PAIRED"}, // active — should be counted
		{id: 2, identifier: "device-002", status: "ACTIVE", pairingStatus: "PAIRED"}, // active but discovered_device soft-deleted — should be excluded
	}
	setupCountMinersByStateTestData(t, conn, &countMinersByStateTestSetup{devices: devices})

	const collectionID int64 = 102
	setupCollectionMembership(t, conn, collectionID, "group", devices)

	// Soft-delete the second device's discovered_device row after seeding so the
	// membership row still references it but the row should not contribute to counts.
	_, err := conn.Exec(`UPDATE discovered_device SET deleted_at = NOW() WHERE id = $1`, int64(2))
	require.NoError(t, err)

	store := sqlstores.NewSQLDeviceStore(conn)
	counts, err := store.GetMinerStateCountsByCollections(ctx, 1, []int64{collectionID})
	require.NoError(t, err)
	require.Contains(t, counts, collectionID)

	c := counts[collectionID]
	require.Equal(t, int32(1), c.HashingCount, "only the non-deleted active paired device should count")
	require.Equal(t, int32(0), c.BrokenCount, "no broken devices")
	require.Equal(t, int32(0), c.OfflineCount, "no offline devices")
	require.Equal(t, int32(0), c.SleepingCount, "no sleeping devices")
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
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
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

	// Insert device statuses (skip if status is empty to simulate NULL device_status)
	for _, device := range setup.devices {
		if device.status == "" {
			continue
		}
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

// nullStatusDashboardFixture returns the canonical three-device fixture used to
// verify NULL device_status bucketing: auth-needed (→ broken), paired (→ offline),
// and an active paired control (→ hashing).
func nullStatusDashboardFixture() []testDevice {
	return []testDevice{
		{id: 1, identifier: "device-001", status: "", pairingStatus: "AUTHENTICATION_NEEDED"},
		{id: 2, identifier: "device-002", status: "", pairingStatus: "PAIRED"},
		{id: 3, identifier: "device-003", status: "ACTIVE", pairingStatus: "PAIRED"},
	}
}

// setupCollectionMembership inserts a device_set row and membership entries linking
// the given devices to it. Devices must already exist (e.g. via
// setupCountMinersByStateTestData). Uses org_id=1 to match that fixture.
// setType is the device_set_type enum value ('group' or 'rack').
func setupCollectionMembership(t *testing.T, conn *sql.DB, collectionID int64, setType string, devices []testDevice) {
	t.Helper()

	_, err := conn.Exec(`
		INSERT INTO device_set (id, org_id, type, label)
		VALUES ($1, 1, $2::device_set_type, $3)
	`, collectionID, setType, fmt.Sprintf("collection-%d", collectionID))
	require.NoError(t, err)

	for _, device := range devices {
		_, err := conn.Exec(`
			INSERT INTO device_set_membership (org_id, device_set_id, device_set_type, device_id, device_identifier)
			VALUES (1, $1, $2::device_set_type, $3, $4)
		`, collectionID, setType, device.id, device.identifier)
		require.NoError(t, err)
	}
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
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
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
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
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

// TestGetFilteredDeviceIds_WithManufacturerFilter verifies manufacturer-based filtering
// prevents cross-manufacturer command targeting when model names collide.
func TestGetFilteredDeviceIds_WithManufacturerFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	queries := sqlc.New(conn)

	setupManufacturerFilterTestData(t, conn)

	tests := []struct {
		name               string
		modelFilter        sql.NullString
		manufacturerFilter sql.NullString
		expectedIDs        []int64
	}{
		{
			name:               "Manufacturer-only filter returns only that manufacturer's devices",
			manufacturerFilter: sql.NullString{String: "Virtual", Valid: true},
			modelFilter:        sql.NullString{Valid: false},
			expectedIDs:        []int64{1},
		},
		{
			name:               "Model-only filter returns all manufacturers with that model",
			manufacturerFilter: sql.NullString{Valid: false},
			modelFilter:        sql.NullString{String: "S21", Valid: true},
			expectedIDs:        []int64{1, 2},
		},
		{
			name:               "Combined manufacturer+model filter prevents cross-manufacturer collision",
			manufacturerFilter: sql.NullString{String: "Bitmain", Valid: true},
			modelFilter:        sql.NullString{String: "S21", Valid: true},
			expectedIDs:        []int64{2},
		},
		{
			name:               "No filters returns all paired devices",
			manufacturerFilter: sql.NullString{Valid: false},
			modelFilter:        sql.NullString{Valid: false},
			expectedIDs:        []int64{1, 2, 3},
		},
		{
			name:               "Multiple manufacturers in filter",
			manufacturerFilter: sql.NullString{String: "Virtual,Bitmain", Valid: true},
			modelFilter:        sql.NullString{Valid: false},
			expectedIDs:        []int64{1, 2, 3},
		},
		{
			name:               "Non-existent manufacturer returns empty",
			manufacturerFilter: sql.NullString{String: "NonExistent", Valid: true},
			modelFilter:        sql.NullString{Valid: false},
			expectedIDs:        []int64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := sqlc.GetFilteredDeviceIdsParams{
				OrgID:              1,
				DeviceStatus:       sql.NullString{Valid: false},
				PairingStatus:      sql.NullString{Valid: false},
				ModelFilter:        tt.modelFilter,
				ManufacturerFilter: tt.manufacturerFilter,
			}

			deviceIDs, err := queries.GetFilteredDeviceIds(ctx, params)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.expectedIDs, deviceIDs)
		})
	}
}

// setupManufacturerFilterTestData creates test data with multiple manufacturers
// sharing model names to test cross-manufacturer filtering.
func setupManufacturerFilterTestData(t *testing.T, conn *sql.DB) {
	t.Helper()

	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
	`)
	require.NoError(t, err)

	devices := []struct {
		id           int64
		identifier   string
		ipAddress    string
		model        string
		manufacturer string
	}{
		{1, "device-001", "192.168.1.101", "S21", "Virtual"},
		{2, "device-002", "192.168.1.102", "S21", "Bitmain"},
		{3, "device-003", "192.168.1.103", "S19", "Bitmain"},
	}

	for _, d := range devices {
		_, err := conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, $3, $4, 'test-driver', $5, '50051', 'grpc', TRUE)
		`, d.id, d.identifier, d.model, d.manufacturer, d.ipAddress)
		require.NoError(t, err)
	}

	for _, d := range devices {
		_, err := conn.Exec(`
			INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
			VALUES ($1, 1, $2, $3, 'AA:BB:CC:DD:EE:FF')
		`, d.id, d.id, d.identifier)
		require.NoError(t, err)
	}

	for _, d := range devices {
		_, err := conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, 'PAIRED', NOW())
		`, d.id)
		require.NoError(t, err)
	}
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
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
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
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
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

// =============================================================================
// IP Address Sorting Integration Tests
// =============================================================================

func TestListMinerStateSnapshots_SortByIPAddress_NumericOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	setupIPAddressSortingTestData(t, conn)
	sortConfig := &interfaces.SortConfig{
		Field:     interfaces.SortFieldIPAddress,
		Direction: interfaces.SortDirectionAsc,
	}

	// Act
	miners, _, _, err := store.ListMinerStateSnapshots(ctx, 1, "", 50, nil, sortConfig)

	// Assert - numeric order: 2.x < 10.x < 192.x (lexicographic would be: 10.x < 192.x < 2.x)
	require.NoError(t, err)
	require.Len(t, miners, 3)
	require.Equal(t, "2.0.0.1", miners[0].IpAddress)
	require.Equal(t, "10.0.0.1", miners[1].IpAddress)
	require.Equal(t, "192.168.1.1", miners[2].IpAddress)
}

func TestListMinerStateSnapshots_SortByIPAddress_Descending(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	setupIPAddressSortingTestData(t, conn)
	sortConfig := &interfaces.SortConfig{
		Field:     interfaces.SortFieldIPAddress,
		Direction: interfaces.SortDirectionDesc,
	}

	// Act
	miners, _, _, err := store.ListMinerStateSnapshots(ctx, 1, "", 50, nil, sortConfig)

	// Assert
	require.NoError(t, err)
	require.Len(t, miners, 3)
	require.Equal(t, "192.168.1.1", miners[0].IpAddress)
	require.Equal(t, "10.0.0.1", miners[1].IpAddress)
	require.Equal(t, "2.0.0.1", miners[2].IpAddress)
}

func TestListMinerStateSnapshots_SortByIPAddress_KeysetPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)
	setupIPAddressSortingTestData(t, conn)
	sortConfig := &interfaces.SortConfig{
		Field:     interfaces.SortFieldIPAddress,
		Direction: interfaces.SortDirectionAsc,
	}

	// Act - fetch first page (limit 2)
	page1, cursor1, _, err := store.ListMinerStateSnapshots(ctx, 1, "", 2, nil, sortConfig)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, cursor1, "cursor should be returned for pagination")

	// Act - fetch second page using cursor
	page2, _, _, err := store.ListMinerStateSnapshots(ctx, 1, cursor1, 2, nil, sortConfig)
	require.NoError(t, err)

	// Assert - page1 has first two IPs, page2 has the third
	require.Equal(t, "2.0.0.1", page1[0].IpAddress)
	require.Equal(t, "10.0.0.1", page1[1].IpAddress)
	require.Len(t, page2, 1)
	require.Equal(t, "192.168.1.1", page2[0].IpAddress)
}

func TestGetPairedDeviceByMACAddress_LegacyDashFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
		VALUES (401, 1, 'legacy-mac-device', 'test-model', 'test-manufacturer', 'proto', '192.168.10.10', '50051', 'grpc', TRUE)
	`)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
		VALUES (401, 1, 401, 'legacy-mac-device', 'AA:BB:CC:DD:EE:FF')
	`)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		VALUES (401, 'PAIRED', NOW())
	`)
	require.NoError(t, err)

	pairedDevice, err := store.GetPairedDeviceByMACAddress(ctx, "AA:BB:CC:DD:EE:FF", 1)
	require.NoError(t, err)
	require.Equal(t, "legacy-mac-device", pairedDevice.DeviceIdentifier)
	require.Equal(t, "AA:BB:CC:DD:EE:FF", pairedDevice.MacAddress)
	require.Equal(t, int64(401), pairedDevice.DiscoveredDeviceID)
}

func TestGetPairedDeviceByMACAddress_BareInput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
		VALUES (405, 1, 'bare-mac-device', 'test-model', 'test-manufacturer', 'proto', '192.168.10.15', '50051', 'grpc', TRUE)
	`)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
		VALUES (405, 1, 405, 'bare-mac-device', 'AA:BB:CC:DD:EE:FF')
	`)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		VALUES (405, 'PAIRED', NOW())
	`)
	require.NoError(t, err)

	pairedDevice, err := store.GetPairedDeviceByMACAddress(ctx, "AABBCCDDEEFF", 1)
	require.NoError(t, err)
	require.Equal(t, "bare-mac-device", pairedDevice.DeviceIdentifier)
	require.Equal(t, "AA:BB:CC:DD:EE:FF", pairedDevice.MacAddress)
	require.Equal(t, int64(405), pairedDevice.DiscoveredDeviceID)
}

func TestUpdateWorkerName_StoresWorkerNameWithoutTouchingPoolSyncStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
		VALUES (406, 1, 'paired-discovered-id', 'test-model', 'test-manufacturer', 'proto', '192.168.10.16', '50051', 'grpc', TRUE)
	`)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
		VALUES (406, 1, 406, 'paired-id', 'AA:BB:CC:DD:EE:16')
	`)
	require.NoError(t, err)

	err = store.UpdateWorkerName(ctx, minermodels.DeviceIdentifier("paired-id"), "worker-16")
	require.NoError(t, err)

	var workerName sql.NullString
	var syncStatus sql.NullString
	err = conn.QueryRowContext(ctx, `
		SELECT worker_name, worker_name_pool_sync_status::text
		FROM device
		WHERE id = 406
	`).Scan(&workerName, &syncStatus)
	require.NoError(t, err)
	require.True(t, workerName.Valid)
	require.Equal(t, "worker-16", workerName.String)
	require.False(t, syncStatus.Valid)
}

func TestGetPairedDeviceByMACAddress_AmbiguousMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	for _, fixture := range []struct {
		discoveredID int64
		deviceID     int64
		identifier   string
		macAddress   string
	}{
		{discoveredID: 411, deviceID: 411, identifier: "duplicate-mac-1", macAddress: "AA:BB:CC:DD:EE:99"},
		{discoveredID: 412, deviceID: 412, identifier: "duplicate-mac-2", macAddress: "AA:BB:CC:DD:EE:99"},
	} {
		_, err = conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, 'test-model', 'test-manufacturer', 'proto', '192.168.10.10', '50051', 'grpc', TRUE)
		`, fixture.discoveredID, fixture.identifier)
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
			VALUES ($1, 1, $2, $3, $4)
		`, fixture.deviceID, fixture.discoveredID, fixture.identifier, fixture.macAddress)
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, 'PAIRED', NOW())
		`, fixture.deviceID)
		require.NoError(t, err)
	}

	_, err = store.GetPairedDeviceByMACAddress(ctx, "AA:BB:CC:DD:EE:99", 1)
	require.Error(t, err)
	require.ErrorContains(t, err, "multiple paired devices found")
}

func setupIPAddressSortingTestData(t *testing.T, conn *sql.DB) {
	t.Helper()

	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	devices := []struct {
		id         int64
		identifier string
		ipAddress  string
	}{
		{301, "device-ip-1", "192.168.1.1"},
		{302, "device-ip-2", "2.0.0.1"},
		{303, "device-ip-3", "10.0.0.1"},
	}

	for _, d := range devices {
		_, err := conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
			VALUES ($1, 1, $2, 'test-model', 'test-manufacturer', 'proto', $3, '50051', 'grpc', TRUE)
		`, d.id, d.identifier, d.ipAddress)
		require.NoError(t, err)
	}
}

// availableFiltersFixture seeds device rows with configurable pairing statuses
// so the GetAvailableModels / GetAvailableFirmwareVersions tests can verify
// which statuses are surfaced to the filter dropdowns.
type availableFiltersFixture struct {
	id              int64
	identifier      string
	model           string
	firmwareVersion string
	pairingStatus   string
}

func seedAvailableFiltersFixtures(t *testing.T, conn *sql.DB, fixtures []availableFiltersFixture) {
	t.Helper()

	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	for _, f := range fixtures {
		_, err := conn.Exec(`
			INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, firmware_version, is_active)
			VALUES ($1, 1, $2, $3, 'test-manufacturer', 'proto', '192.168.1.1', '50051', 'grpc', $4, TRUE)
		`, f.id, f.identifier, f.model, f.firmwareVersion)
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
			VALUES ($1, 1, $1, $2, $3)
		`, f.id, f.identifier, fmt.Sprintf("AA:BB:CC:DD:EE:%02d", f.id%100))
		require.NoError(t, err)

		_, err = conn.Exec(`
			INSERT INTO device_pairing (device_id, pairing_status, paired_at)
			VALUES ($1, $2, NOW())
		`, f.id, f.pairingStatus)
		require.NoError(t, err)
	}
}

// TestGetAvailableModels_PairedOnly verifies models on PAIRED devices are returned.
func TestGetAvailableModels_PairedOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	seedAvailableFiltersFixtures(t, conn, []availableFiltersFixture{
		{id: 601, identifier: "paired-1", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "PAIRED"},
		{id: 602, identifier: "paired-2", model: "S21", firmwareVersion: "2.0.0", pairingStatus: "PAIRED"},
	})

	models, err := store.GetAvailableModels(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"S19", "S21"}, models)
}

// TestGetAvailableModels_AuthenticationNeededOnly verifies models from devices
// stuck in AUTHENTICATION_NEEDED still appear in the dropdown — the bug fix.
func TestGetAvailableModels_AuthenticationNeededOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	seedAvailableFiltersFixtures(t, conn, []availableFiltersFixture{
		{id: 611, identifier: "auth-1", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "AUTHENTICATION_NEEDED"},
		{id: 612, identifier: "auth-2", model: "T19", firmwareVersion: "1.5.0", pairingStatus: "AUTHENTICATION_NEEDED"},
	})

	models, err := store.GetAvailableModels(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"S19", "T19"}, models)
}

// TestGetAvailableModels_PairedAndAuthenticationNeeded verifies combined
// PAIRED + AUTHENTICATION_NEEDED rows surface a deduplicated, sorted list.
func TestGetAvailableModels_PairedAndAuthenticationNeeded(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	seedAvailableFiltersFixtures(t, conn, []availableFiltersFixture{
		{id: 621, identifier: "paired", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "PAIRED"},
		{id: 622, identifier: "auth", model: "T19", firmwareVersion: "1.5.0", pairingStatus: "AUTHENTICATION_NEEDED"},
		{id: 623, identifier: "paired-dup", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "PAIRED"},
	})

	models, err := store.GetAvailableModels(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"S19", "T19"}, models)
}

// TestGetAvailableModels_ExcludesOtherStatuses verifies PENDING / UNPAIRED / FAILED
// devices are not surfaced in the dropdown.
func TestGetAvailableModels_ExcludesOtherStatuses(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	seedAvailableFiltersFixtures(t, conn, []availableFiltersFixture{
		{id: 631, identifier: "pending", model: "PENDING_MODEL", firmwareVersion: "0.0.1", pairingStatus: "PENDING"},
		{id: 632, identifier: "unpaired", model: "UNPAIRED_MODEL", firmwareVersion: "0.0.2", pairingStatus: "UNPAIRED"},
		{id: 633, identifier: "failed", model: "FAILED_MODEL", firmwareVersion: "0.0.3", pairingStatus: "FAILED"},
		{id: 634, identifier: "paired", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "PAIRED"},
	})

	models, err := store.GetAvailableModels(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"S19"}, models)
}

// TestGetAvailableFirmwareVersions_PairedOnly verifies firmware versions on
// PAIRED devices are returned.
func TestGetAvailableFirmwareVersions_PairedOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	seedAvailableFiltersFixtures(t, conn, []availableFiltersFixture{
		{id: 701, identifier: "paired-1", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "PAIRED"},
		{id: 702, identifier: "paired-2", model: "S21", firmwareVersion: "2.0.0", pairingStatus: "PAIRED"},
	})

	versions, err := store.GetAvailableFirmwareVersions(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0", "2.0.0"}, versions)
}

// TestGetAvailableFirmwareVersions_AuthenticationNeededOnly verifies firmware
// versions from devices stuck in AUTHENTICATION_NEEDED still appear — the fix.
func TestGetAvailableFirmwareVersions_AuthenticationNeededOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	seedAvailableFiltersFixtures(t, conn, []availableFiltersFixture{
		{id: 711, identifier: "auth-1", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "AUTHENTICATION_NEEDED"},
		{id: 712, identifier: "auth-2", model: "T19", firmwareVersion: "1.5.0", pairingStatus: "AUTHENTICATION_NEEDED"},
	})

	versions, err := store.GetAvailableFirmwareVersions(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0", "1.5.0"}, versions)
}

// TestGetAvailableFirmwareVersions_PairedAndAuthenticationNeeded verifies
// combined PAIRED + AUTHENTICATION_NEEDED rows produce a deduplicated, sorted list.
func TestGetAvailableFirmwareVersions_PairedAndAuthenticationNeeded(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	seedAvailableFiltersFixtures(t, conn, []availableFiltersFixture{
		{id: 721, identifier: "paired", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "PAIRED"},
		{id: 722, identifier: "auth", model: "T19", firmwareVersion: "1.5.0", pairingStatus: "AUTHENTICATION_NEEDED"},
		{id: 723, identifier: "paired-dup", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "PAIRED"},
	})

	versions, err := store.GetAvailableFirmwareVersions(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0", "1.5.0"}, versions)
}

// TestGetAvailableFirmwareVersions_ExcludesOtherStatuses verifies PENDING /
// UNPAIRED / FAILED devices are not surfaced.
func TestGetAvailableFirmwareVersions_ExcludesOtherStatuses(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLDeviceStore(conn)

	seedAvailableFiltersFixtures(t, conn, []availableFiltersFixture{
		{id: 731, identifier: "pending", model: "S19", firmwareVersion: "9.9.1", pairingStatus: "PENDING"},
		{id: 732, identifier: "unpaired", model: "S19", firmwareVersion: "9.9.2", pairingStatus: "UNPAIRED"},
		{id: 733, identifier: "failed", model: "S19", firmwareVersion: "9.9.3", pairingStatus: "FAILED"},
		{id: 734, identifier: "paired", model: "S19", firmwareVersion: "1.0.0", pairingStatus: "PAIRED"},
	})

	versions, err := store.GetAvailableFirmwareVersions(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, versions)
}
