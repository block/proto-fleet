package ipscanner_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/ipscanner"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
)

// mockDiscoverer implements minerdiscovery.Discoverer for testing
type mockDiscoverer struct {
	minerType   miner.Type
	devicesByIP map[string]*minerdiscovery.DiscoveredDevice
}

func (m *mockDiscoverer) GetMinerType() miner.Type {
	return m.minerType
}

func (m *mockDiscoverer) Discover(ctx context.Context, ipAddress, port string) (*minerdiscovery.DiscoveredDevice, error) {
	key := ipAddress + ":" + port
	if device, ok := m.devicesByIP[key]; ok {
		return device, nil
	}
	return nil, errors.New("device not found")
}

func TestIPScannerService_RediscoverOfflineDeviceAtNewIP(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Get test database connection (migrations already applied)
	conn := testutil.GetTestDB(t)

	// Create real device store
	deviceStore := sqlstores.NewSQLDeviceStore(conn)

	// Seed test data - two offline devices on same subnet
	setupTestData(t, conn)

	// Set up mock discoverer to find both devices at new IPs
	mockDisc := &mockDiscoverer{
		minerType: miner.TypeProto,
		devicesByIP: map[string]*minerdiscovery.DiscoveredDevice{
			"192.168.1.150:50051": {
				Device: pb.Device{
					IpAddress:  "192.168.1.150",
					Port:       "50051",
					UrlScheme:  "grpc",
					MacAddress: "AA:BB:CC:DD:EE:01", // First device moved here
				},
			},
			"192.168.1.151:50051": {
				Device: pb.Device{
					IpAddress:  "192.168.1.151",
					Port:       "50051",
					UrlScheme:  "grpc",
					MacAddress: "AA:BB:CC:DD:EE:02", // Second device moved here
				},
			},
		},
	}

	// Create discovery service with mock discoverer
	discoveryService, err := minerdiscovery.NewService(mockDisc)
	require.NoError(t, err)

	// Configure service with short intervals for testing
	config := ipscanner.Config{
		Enabled:                       true,
		ScanInterval:                  100 * time.Millisecond,
		MaxConcurrentSubnetScans:      5,
		MaxConcurrentIPScansPerSubnet: 20,
		ScanTimeout:                   2 * time.Second,
		SubnetMaskBits:                24,
	}

	logger := slog.Default()

	// Create and start the service
	service := ipscanner.NewIPScannerService(config, deviceStore, discoveryService, logger)

	testCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err = service.Start(testCtx)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, service.Stop())
	}()

	// Wait for the service to complete at least one scan cycle
	time.Sleep(500 * time.Millisecond)

	// Verify both devices were updated in the database
	verifyIPAssignmentUpdated(t, conn, 1, "192.168.1.150", "50051", "grpc")
	verifyIPAssignmentUpdated(t, conn, 2, "192.168.1.151", "50051", "grpc")

	t.Log("Successfully rediscovered both offline devices and updated database")
}

// setupTestData creates test data in the database
func setupTestData(t *testing.T, conn *sql.DB) {
	t.Helper()

	// Insert organization
	_, err := conn.Exec(`
		INSERT INTO organization (id, org_id, name, miner_auth_private_key)
		VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org', 'test-private-key')
	`)
	require.NoError(t, err)

	// Insert two devices
	_, err = conn.Exec(`
		INSERT INTO device (id, org_id, device_identifier, mac_address, type, is_active)
		VALUES
			(1, 1, 'test-miner-001', 'AA:BB:CC:DD:EE:01', 'proto', 1),
			(2, 1, 'test-miner-002', 'AA:BB:CC:DD:EE:02', 'proto', 1)
	`)
	require.NoError(t, err)

	// Insert device pairings (all PAIRED)
	_, err = conn.Exec(`
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		VALUES
			(1, 'PAIRED', NOW()),
			(2, 'PAIRED', NOW())
	`)
	require.NoError(t, err)

	// Insert device status - both OFFLINE
	_, err = conn.Exec(`
		INSERT INTO device_status (device_id, status, status_timestamp)
		VALUES
			(1, 'OFFLINE', NOW()),
			(2, 'OFFLINE', NOW())
	`)
	require.NoError(t, err)

	// Insert IP assignments at old IPs (where devices WERE)
	_, err = conn.Exec(`
		INSERT INTO device_ip_assignment (device_id, ip_address, port, url_scheme, is_current)
		VALUES
			(1, '192.168.1.100', '50051', 'grpc', 1),
			(2, '192.168.1.101', '50051', 'grpc', 1)
	`)
	require.NoError(t, err)
}

// verifyIPAssignmentUpdated checks that the device's IP assignment was updated
func verifyIPAssignmentUpdated(t *testing.T, conn *sql.DB, deviceID int64, expectedIP, expectedPort, expectedScheme string) {
	t.Helper()

	var ipAddress, port, urlScheme string
	var isCurrent bool

	err := conn.QueryRow(`
		SELECT ip_address, port, url_scheme, is_current
		FROM device_ip_assignment
		WHERE device_id = ? AND ip_address = ?
		LIMIT 1
	`, deviceID, expectedIP).Scan(&ipAddress, &port, &urlScheme, &isCurrent)

	require.NoError(t, err, "IP assignment should exist in database")
	require.Equal(t, expectedIP, ipAddress, "IP address should match")
	require.Equal(t, expectedPort, port, "Port should match")
	require.Equal(t, expectedScheme, urlScheme, "URL scheme should match")
	require.True(t, isCurrent, "IP assignment should be current")

	// Verify old IP is marked as not current
	var oldIPCurrent bool
	err = conn.QueryRow(`
		SELECT is_current
		FROM device_ip_assignment
		WHERE device_id = ? AND ip_address != ?
		LIMIT 1
	`, deviceID, expectedIP).Scan(&oldIPCurrent)

	if err == nil {
		require.False(t, oldIPCurrent, "Old IP assignment should not be current")
	}
}
