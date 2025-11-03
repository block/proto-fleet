package sqlstores_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
)

// TestGetOfflineDevices_DatabaseIntegration tests the GetOfflineDevices query
// against a real MySQL database to validate SQL syntax and JOIN conditions
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
