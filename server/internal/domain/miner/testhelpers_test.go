package miner_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migrateMySQL "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
)

var (
	testContainer        *mysql.MySQLContainer
	testEncryptService   *encrypt.Service
	testFilesService     *files.Service
	testTokenService     *token.Service
	testConnectionString string
	setupOnce            sync.Once
	setupError           error
)

func setupTestInfrastructure() error {
	setupOnce.Do(func() {
		ctx := context.Background()

		testEncryptService, setupError = createTestEncryptService()
		if setupError != nil {
			return
		}

		var err error
		testContainer, err = mysql.Run(ctx,
			"mysql:8.0",
			mysql.WithDatabase("testdb"),
			mysql.WithUsername("testuser"),
			mysql.WithPassword("testpass"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("port: 3306  MySQL Community Server - GPL").
					WithOccurrence(1).
					WithStartupTimeout(60*time.Second)),
		)
		if err != nil {
			setupError = fmt.Errorf("could not start MySQL container: %w", err)
			return
		}

		testConnectionString, err = testContainer.ConnectionString(ctx, "parseTime=true&multiStatements=true")
		if err != nil {
			setupError = fmt.Errorf("could not get connection string: %w", err)
			return
		}

		tempDB, err := sql.Open("mysql", testConnectionString)
		if err != nil {
			setupError = fmt.Errorf("could not connect to database: %w", err)
			return
		}
		defer tempDB.Close()

		if err = tempDB.Ping(); err != nil {
			setupError = fmt.Errorf("could not ping database: %w", err)
			return
		}

		if err := runMigrations(tempDB); err != nil {
			setupError = fmt.Errorf("could not run migrations: %w", err)
			return
		}

		testFilesService, setupError = files.NewService()
		if setupError != nil {
			return
		}

		testConfig, setupError := testutil.GetTestConfig()
		if setupError != nil {
			return
		}

		testTokenService, setupError = token.NewService(token.Config{ClientToken: token.AuthTokenConfig{SecretKey: testConfig.AuthTokenSecretKey, ExpirationPeriod: time.Minute * 5}, MinerTokenExpirationPeriod: time.Minute * 5})
		if setupError != nil {
			return
		}
	})

	return setupError
}

func cleanupTestInfrastructure() error {
	if testContainer != nil {
		ctx := context.Background()
		if err := testContainer.Terminate(ctx); err != nil {
			return fmt.Errorf("could not terminate MySQL container: %w", err)
		}
		testContainer = nil
	}

	testEncryptService = nil
	testConnectionString = ""
	setupOnce = sync.Once{}
	setupError = nil

	return nil
}

func createTestEncryptService() (*encrypt.Service, error) {
	testKey := "MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI=" // 32-byte key: "12345678901234567890123456789012"

	config := &encrypt.Config{
		ServiceMasterKey: testKey,
	}

	return encrypt.NewService(config)
}

func runMigrations(db *sql.DB) error {
	migrationsDir, err := filepath.Abs("../../../migrations")
	if err != nil {
		return fmt.Errorf("failed to get migrations directory: %w", err)
	}

	driver, err := migrateMySQL.WithInstance(db, &migrateMySQL.Config{})
	if err != nil {
		return fmt.Errorf("failed to create migrate driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		fmt.Sprintf("file://%s", migrationsDir),
		"mysql",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func setupTestDB(t *testing.T) (*sql.DB, *encrypt.Service, *files.Service, *token.Service) {
	t.Helper()

	if err := setupTestInfrastructure(); err != nil {
		t.Fatalf("Failed to setup test infrastructure: %v", err)
	}

	if testConnectionString == "" {
		t.Fatal("Test connection string not initialized after setup")
	}

	if testEncryptService == nil {
		t.Fatal("Test encrypt service not initialized after setup")
	}

	if testTokenService == nil {
		t.Fatal("Test token service not initialized after setup")
	}

	if testFilesService == nil {
		t.Fatal("Test files service not initialized after setup")
	}

	db, err := sql.Open("mysql", testConnectionString)
	if err != nil {
		t.Fatalf("Failed to create database connection: %v", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("Failed to ping database: %v", err)
	}

	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
	})

	cleanupTestData(t, db)

	return db, testEncryptService, testFilesService, testTokenService
}

func cleanupTestData(t *testing.T, db *sql.DB) {
	t.Helper()

	queries := []string{
		"DELETE FROM miner_credentials WHERE device_id IN (SELECT id FROM device WHERE device_identifier LIKE 'test-%')",
		"DELETE FROM device_pairing WHERE device_id IN (SELECT id FROM device WHERE device_identifier LIKE 'test-%')",
		"DELETE FROM discovered_device WHERE id IN (SELECT discovered_device_id FROM device WHERE device_identifier LIKE 'test-%')",
		"DELETE FROM device WHERE device_identifier LIKE 'test-%'",
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			t.Logf("Warning: failed to cleanup test data: %v", err)
		}
	}
}

func createDiscoveredDevice(t *testing.T, db *sql.DB, model string, manufacturer string, deviceType string) int64 {
	t.Helper()

	orgID := int64(1)
	var exists bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM organization WHERE id = ?)`, orgID).Scan(&exists)
	require.NoError(t, err, "Failed to check organization existence")
	if !exists {
		_, err := db.Exec(`INSERT INTO organization (id, org_id, name, miner_auth_private_key) VALUES (?, ?, ?, ?)`,
			orgID, fmt.Sprintf("test-org-%d", orgID), fmt.Sprintf("Test Organization %d", orgID), "dummy-key-for-testing")
		require.NoError(t, err, "Failed to insert organization")
	}

	// Generate a unique device_identifier for the discovered device
	deviceIdentifier := fmt.Sprintf("test-discovered-%s-%d", deviceType, time.Now().UnixNano())

	discoveredDeviceResult, err := db.Exec(`
		INSERT INTO discovered_device (org_id, device_identifier, model, manufacturer, type, ip_address, port, url_scheme)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, deviceIdentifier, model, manufacturer, deviceType, "192.168.1.100", "4028", "https")
	require.NoError(t, err)

	discoveredDeviceID, err := discoveredDeviceResult.LastInsertId()
	require.NoError(t, err)

	return discoveredDeviceID
}

func createTestDevice(t *testing.T, db *sql.DB, deviceIdentifier string) int64 {
	t.Helper()

	orgID := int64(1)

	discoveredDeviceID := createDiscoveredDevice(t, db, "TestMiner", "TestCorp", "antminer")

	queries := sqlc.New(db)

	result, err := queries.UpsertDevice(t.Context(), sqlc.UpsertDeviceParams{
		OrgID:              orgID,
		DiscoveredDeviceID: discoveredDeviceID,
		DeviceIdentifier:   deviceIdentifier,
		MacAddress:         fmt.Sprintf("00:11:22:33:44:%02x", len(deviceIdentifier)%256),
		SerialNumber:       sql.NullString{String: fmt.Sprintf("SN-%s", deviceIdentifier), Valid: true},
	})
	require.NoError(t, err)

	deviceID, err := result.LastInsertId()
	require.NoError(t, err)

	// Create device pairing record with PAIRED status
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      deviceID,
		PairingStatus: "PAIRED",
	})
	require.NoError(t, err)

	// Generate unique IP based on device ID to avoid duplicate IP constraint violations
	// Use the last byte of the device ID for uniqueness
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

func createTestMinerCredentials(t *testing.T, db *sql.DB, deviceID int64) {
	t.Helper()

	if testEncryptService == nil {
		t.Fatal("Test encrypt service not available")
	}

	queries := sqlc.New(db)

	encryptedUsername, err := testEncryptService.Encrypt([]byte("testuser"))
	require.NoError(t, err)

	encryptedPassword, err := testEncryptService.Encrypt([]byte("testpass"))
	require.NoError(t, err)

	err = queries.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{
		DeviceID:    deviceID,
		UsernameEnc: encryptedUsername,
		PasswordEnc: encryptedPassword,
	})
	require.NoError(t, err)
}

func createTestDeviceWithCredentials(t *testing.T, db *sql.DB, deviceIdentifier string) {
	t.Helper()

	deviceID := createTestDevice(t, db, deviceIdentifier)
	createTestMinerCredentials(t, db, deviceID)
}

func createTestProtoMinerWithToken(t *testing.T, db *sql.DB, deviceIdentifier string) int64 {
	t.Helper()

	discoveredDeviceID := createDiscoveredDevice(t, db, "ProtoMiner", "ProtoCorp", "proto")

	queries := sqlc.New(db)

	result, err := queries.UpsertDevice(t.Context(), sqlc.UpsertDeviceParams{
		OrgID:              1,
		DiscoveredDeviceID: discoveredDeviceID,
		DeviceIdentifier:   deviceIdentifier,
		MacAddress:         fmt.Sprintf("00:11:22:33:44:%02x", len(deviceIdentifier)%256),
		SerialNumber:       sql.NullString{String: fmt.Sprintf("SN-%s", deviceIdentifier), Valid: true},
	})
	require.NoError(t, err)

	deviceID, err := result.LastInsertId()
	require.NoError(t, err)

	// Create device pairing record with PAIRED status and pairing token
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      deviceID,
		PairingStatus: "PAIRED",
	})
	require.NoError(t, err)

	err = queries.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{
		IpAddress: "192.168.1.200",
		Port:      "8080",
		UrlScheme: "https",
		ID:        deviceID,
	})
	require.NoError(t, err)

	return deviceID
}
