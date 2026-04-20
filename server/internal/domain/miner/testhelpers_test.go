package miner_test

import (
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/token"
	"github.com/block/proto-fleet/server/internal/testutil"

	"github.com/block/proto-fleet/server/internal/infrastructure/files"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

// Global counter for generating unique test IPs
// Using atomic operations ensures uniqueness even in parallel tests
var testDeviceIPCounter uint32

func setupTestDB(t *testing.T) (*sql.DB, *encrypt.Service, *files.Service, *token.Service) {
	t.Helper()

	testConfig, err := testutil.GetTestConfig()
	require.NoError(t, err, "Failed to get test config")

	db := testutil.GetTestDB(t)

	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: testConfig.ServiceMasterKey,
	})
	require.NoError(t, err, "Failed to create encrypt service")

	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err, "Failed to create files service")

	tokenService, err := token.NewService(token.Config{
		ClientToken: token.AuthTokenConfig{
			SecretKey:        testConfig.AuthTokenSecretKey,
			ExpirationPeriod: 5 * time.Minute,
		},
		MinerTokenExpirationPeriod: 5 * time.Minute,
	})
	require.NoError(t, err, "Failed to create token service")

	return db, encryptService, filesService, tokenService
}

func createDiscoveredDevice(t *testing.T, db *sql.DB, model string, manufacturer string, deviceType string) int64 {
	t.Helper()

	orgID := int64(1)
	queries := sqlc.New(db)

	// Ensure organization exists
	var exists bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM organization WHERE id = $1)`, orgID).Scan(&exists)
	require.NoError(t, err, "Failed to check organization existence")
	if !exists {
		_, err := db.Exec(`INSERT INTO organization (id, org_id, name, miner_auth_private_key) VALUES ($1, $2, $3, $4)`,
			orgID, fmt.Sprintf("test-org-%d", orgID), fmt.Sprintf("Test Organization %d", orgID), "dummy-key-for-testing")
		require.NoError(t, err, "Failed to insert organization")
	}

	// Generate a unique device_identifier for the discovered device
	deviceIdentifier := fmt.Sprintf("test-discovered-%s-%d", deviceType, time.Now().UnixNano())

	// Use unique IP to avoid constraint violations on (org_id, ip_address, port)
	// Use an atomic counter to ensure unique IPs even in parallel tests
	counter := atomic.AddUint32(&testDeviceIPCounter, 1)
	uniqueIP := fmt.Sprintf("192.168.%d.%d", (counter>>8)&0xFF, counter&0xFF)

	// Use standard ports for each device type
	port := "4028" // default
	if deviceType == "proto" {
		port = "8080"
	}

	discoveredDeviceID, err := queries.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID:            orgID,
		DeviceIdentifier: deviceIdentifier,
		Model:            sql.NullString{String: model, Valid: true},
		Manufacturer:     sql.NullString{String: manufacturer, Valid: true},
		DriverName:       deviceType,
		IpAddress:        uniqueIP,
		Port:             port,
		UrlScheme:        "https",
		IsActive:         true,
	})
	require.NoError(t, err)

	return discoveredDeviceID
}

func createTestDevice(t *testing.T, db *sql.DB, deviceIdentifier string) int64 {
	t.Helper()

	orgID := int64(1)

	discoveredDeviceID := createDiscoveredDevice(t, db, "TestMiner", "TestCorp", "antminer")

	queries := sqlc.New(db)

	deviceID, err := queries.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID:              orgID,
		DiscoveredDeviceID: discoveredDeviceID,
		DeviceIdentifier:   deviceIdentifier,
		MacAddress:         fmt.Sprintf("00:11:22:33:44:%02x", len(deviceIdentifier)%256),
		SerialNumber:       sql.NullString{String: fmt.Sprintf("SN-%s", deviceIdentifier), Valid: true},
	})
	require.NoError(t, err)

	// Create device pairing record with PAIRED status
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      deviceID,
		PairingStatus: "PAIRED",
	})
	require.NoError(t, err)

	// Generate unique IP based on device ID to avoid duplicate IP constraint violations
	uniqueIP := fmt.Sprintf("192.168.1.%d", 100+(deviceID%150))

	err = queries.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{
		IpAddress: uniqueIP,
		Port:      "4028",
		UrlScheme: "https",
		ID:        deviceID,
	})
	require.NoError(t, err)

	return deviceID
}

func createTestMinerCredentials(t *testing.T, db *sql.DB, encryptService *encrypt.Service, deviceID int64) {
	t.Helper()

	queries := sqlc.New(db)

	encryptedUsername, err := encryptService.Encrypt([]byte("testuser"))
	require.NoError(t, err)

	encryptedPassword, err := encryptService.Encrypt([]byte("testpass"))
	require.NoError(t, err)

	err = queries.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{
		DeviceID:    deviceID,
		UsernameEnc: encryptedUsername,
		PasswordEnc: encryptedPassword,
	})
	require.NoError(t, err)
}

func createTestDeviceWithCredentials(t *testing.T, db *sql.DB, encryptService *encrypt.Service, deviceIdentifier string) {
	t.Helper()

	deviceID := createTestDevice(t, db, deviceIdentifier)
	createTestMinerCredentials(t, db, encryptService, deviceID)
}

func createTestProtoMinerWithToken(t *testing.T, db *sql.DB, deviceIdentifier string) int64 {
	t.Helper()

	discoveredDeviceID := createDiscoveredDevice(t, db, "ProtoMiner", "ProtoCorp", "proto")

	queries := sqlc.New(db)

	deviceID, err := queries.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID:              1,
		DiscoveredDeviceID: discoveredDeviceID,
		DeviceIdentifier:   deviceIdentifier,
		MacAddress:         fmt.Sprintf("00:11:22:33:44:%02x", len(deviceIdentifier)%256),
		SerialNumber:       sql.NullString{String: fmt.Sprintf("SN-%s", deviceIdentifier), Valid: true},
	})
	require.NoError(t, err)

	// Create device pairing record with PAIRED status
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      deviceID,
		PairingStatus: "PAIRED",
	})
	require.NoError(t, err)

	// Generate unique IP based on device ID to avoid duplicate IP constraint violations
	uniqueIP := fmt.Sprintf("192.168.2.%d", 100+(deviceID%150))

	err = queries.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{
		IpAddress: uniqueIP,
		Port:      "8080",
		UrlScheme: "https",
		ID:        deviceID,
	})
	require.NoError(t, err)

	return deviceID
}
