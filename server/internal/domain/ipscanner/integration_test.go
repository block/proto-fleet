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
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
)

// mockDiscoverer implements minerdiscovery.Discoverer for testing
type mockDiscoverer struct {
	minerType   miner.Type
	devicesByIP map[string]*discoverymodels.DiscoveredDevice
}

func (m *mockDiscoverer) GetMinerType() miner.Type {
	return m.minerType
}

func (m *mockDiscoverer) Discover(ctx context.Context, ipAddress, port string) (*discoverymodels.DiscoveredDevice, error) {
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
		devicesByIP: map[string]*discoverymodels.DiscoveredDevice{
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

	// Insert discovered devices
	_, err = conn.Exec(`
		INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme)
		VALUES
			(1, 1, 'test-miner-001', 'proto', 'test-manufacturer', 'proto', '192.168.1.100', '50051', 'grpc'),
			(2, 1, 'test-miner-002', 'proto', 'test-manufacturer', 'proto', '192.168.1.101', '50051', 'grpc')
	`)
	require.NoError(t, err)

	// Insert two devices
	_, err = conn.Exec(`
		INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
		VALUES
			(1, 1, 1, 'test-miner-001', 'AA:BB:CC:DD:EE:01'),
			(2, 1, 2, 'test-miner-002', 'AA:BB:CC:DD:EE:02')
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
}

// verifyIPAssignmentUpdated checks that the device's IP assignment was updated
func verifyIPAssignmentUpdated(t *testing.T, conn *sql.DB, deviceID int64, expectedIP, expectedPort, expectedScheme string) {
	t.Helper()

	var ipAddress, port, urlScheme string

	err := conn.QueryRow(`
		SELECT dd.ip_address, dd.port, dd.url_scheme
		FROM device d
		JOIN discovered_device dd ON d.discovered_device_id = dd.id
		WHERE d.id = ?
		LIMIT 1
	`, deviceID).Scan(&ipAddress, &port, &urlScheme)

	require.NoError(t, err, "IP assignment should exist in database")
	require.Equal(t, expectedIP, ipAddress, "IP address should match")
	require.Equal(t, expectedPort, port, "Port should match")
	require.Equal(t, expectedScheme, urlScheme, "URL scheme should match")
}
