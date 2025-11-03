package miner_test

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/testutil"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
)

func TestNewMinerService_WithValidDB_ShouldCreateService(t *testing.T) {
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)

	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService)

	assert.NotNil(t, service)
}

func TestNewMinerService_WithNilDB_ShouldPanic(t *testing.T) {
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)

	assert.Panics(t, func() {
		miner.NewMinerService(nil, userStore, encryptService, filesService, tokenService)
	})
}

func TestNewMinerService_WithNilEncryptService_ShouldPanic(t *testing.T) {
	db, _, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)

	assert.Panics(t, func() {
		miner.NewMinerService(db, userStore, nil, filesService, tokenService)
	})
}

func TestMinerService_GetMinerFromDeviceID_WithValidDevice_ShouldReturnMiner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)

	deviceID := models.DeviceIdentifier("test-device-123")
	createTestDeviceWithCredentials(t, db, string(deviceID))

	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService)

	miner, err := service.GetMinerFromDeviceIdentifier(t.Context(), deviceID)

	require.NoError(t, err)
	assert.NotNil(t, miner)
	assert.Equal(t, models.TypeAntminer, miner.GetType())
}

func TestMinerService_GetMinerFromDeviceID_WithNonexistentDevice_ShouldReturnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)

	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService)

	miner, err := service.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier("nonexistent"))

	require.Error(t, err)
	assert.Nil(t, miner)
	assert.Contains(t, err.Error(), "device not found")
}

func TestMinerService_GetMinerFromDeviceID_WithEmptyDeviceID_ShouldReturnError(t *testing.T) {
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)

	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService)

	miner, err := service.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(""))

	require.Error(t, err)
	assert.Nil(t, miner)
	assert.Contains(t, err.Error(), "device ID cannot be empty")
}

func TestMinerService_GetMinerFromDeviceID_WithDatabaseError_ShouldReturnError(t *testing.T) {
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)

	db.Close() // Simulate database error

	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService)

	miner, err := service.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier("device-123"))

	require.Error(t, err)
	assert.Nil(t, miner)
}

func TestMinerService_GetMinerFromDeviceID_WithMissingCredentials_ShouldReturnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceID := models.DeviceIdentifier("test-device-no-creds")
	createTestDevice(t, db, string(deviceID))

	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService)

	miner, err := service.GetMinerFromDeviceIdentifier(t.Context(), deviceID)

	require.Error(t, err)
	assert.Nil(t, miner)
}

func TestMinerService_ConcurrentAccess_ShouldBeThreadSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Parallel()

	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	const numDevices = 10
	deviceIDs := make([]models.DeviceIdentifier, numDevices)
	for i := range numDevices {
		deviceID := models.DeviceIdentifier(fmt.Sprintf("device-%d", i))
		deviceIDs[i] = deviceID
		createTestDeviceWithCredentials(t, db, string(deviceID))
	}

	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService)

	const numGoroutines = 20
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			deviceID := deviceIDs[goroutineID%numDevices]
			_, err := service.GetMinerFromDeviceIdentifier(t.Context(), deviceID)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d failed: %w", goroutineID, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent operation failed: %v", err)
	}
}

func TestMinerService_GetMinerFromDeviceID_WithDifferentMinerTypes_ShouldReturnCorrectType(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testContext.DatabaseService.CreateSuperAdminUser()

	tests := []struct {
		deviceType   string
		expectedType models.Type
	}{
		{"antminer", models.TypeAntminer},
		{"proto", models.TypeProto},
		{"whatsminer", models.TypeWhatsminer},
		{"avalon", models.TypeAvalon},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("type_%s", test.deviceType), func(t *testing.T) {
			deviceID := models.DeviceIdentifier(fmt.Sprintf("test-%s-device", test.deviceType))
			// Use unique IP addresses for each subtest to avoid conflicts
			testIPAddress := fmt.Sprintf("192.168.1.%d", 100+i)

			queries := sqlc.New(testContext.ServiceProvider.DB)

			discoveredDeviceID := createDiscoveredDevice(t, testContext.ServiceProvider.DB, "TestMiner", "TestCorp", test.deviceType)

			result, err := queries.UpsertDevice(t.Context(), sqlc.UpsertDeviceParams{
				OrgID:              1,
				DiscoveredDeviceID: discoveredDeviceID,
				DeviceIdentifier:   string(deviceID),
				MacAddress:         fmt.Sprintf("00:11:22:33:44:%02x", 50+i),
				SerialNumber:       sql.NullString{String: fmt.Sprintf("SN-%d", 100+i), Valid: true},
			})
			require.NoError(t, err)

			dbDeviceID, err := result.LastInsertId()
			require.NoError(t, err)

			// Create device pairing record with PAIRED status
			_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
				DeviceID:      dbDeviceID,
				PairingStatus: "PAIRED",
			})
			require.NoError(t, err)

			err = queries.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{
				IpAddress: testIPAddress,
				Port:      "4028",
				UrlScheme: "https",
				ID:        dbDeviceID,
			})
			require.NoError(t, err)

			err = queries.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{
				DeviceID:    dbDeviceID,
				UsernameEnc: testContext.Config.GetAntminerUsernameEnc(t),
				PasswordEnc: testContext.Config.GetAntminerPasswordEnc(t),
			})
			require.NoError(t, err)

			if test.expectedType == models.TypeAntminer || test.expectedType == models.TypeProto {
				miner, err := testContext.ServiceProvider.MinerService.GetMinerFromDeviceIdentifier(t.Context(), deviceID)

				require.NoError(t, err)
				assert.NotNil(t, miner)
				assert.Equal(t, test.expectedType, miner.GetType())
			} else {
				miner, err := testContext.ServiceProvider.MinerService.GetMinerFromDeviceIdentifier(t.Context(), deviceID)

				require.Error(t, err)
				assert.Nil(t, miner)
				assert.Contains(t, err.Error(), "unsupported miner type")
			}
		})
	}
}

func TestMinerService_GetMinerFromDeviceID_WithProtoMinerToken_ShouldReturnProtoMiner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testContext.DatabaseService.CreateSuperAdminUser()

	deviceID := models.DeviceIdentifier("test-proto-token-device")
	createTestProtoMinerWithToken(t, testContext.ServiceProvider.DB, string(deviceID))
	userStore := sqlstores.NewSQLUserStore(testContext.ServiceProvider.DB)

	service := miner.NewMinerService(testContext.ServiceProvider.DB, userStore, testContext.ServiceProvider.EncryptService, testContext.ServiceProvider.FilesService, testContext.ServiceProvider.TokenService)

	miner, err := service.GetMinerFromDeviceIdentifier(t.Context(), deviceID)

	require.NoError(t, err)
	assert.NotNil(t, miner)
	assert.Equal(t, models.TypeProto, miner.GetType())
}

func TestMinerService_GetMinerFromDeviceID_WithUnpairedDevice_ShouldReturnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testContext.DatabaseService.CreateSuperAdminUser()

	queries := sqlc.New(testContext.DatabaseService.DB)

	discoveredDeviceID := createDiscoveredDevice(t, testContext.DatabaseService.DB, "TestMiner", "TestCorp", "antminer")

	// Create device without pairing record
	result, err := queries.UpsertDevice(t.Context(), sqlc.UpsertDeviceParams{
		OrgID:              1,
		DiscoveredDeviceID: discoveredDeviceID,
		DeviceIdentifier:   "test-unpaired-device",
		MacAddress:         "00:11:22:33:44:99",
		SerialNumber:       sql.NullString{String: "SN-UNPAIRED", Valid: true},
	})
	require.NoError(t, err)

	dbDeviceID, err := result.LastInsertId()
	require.NoError(t, err)

	// Create IP assignment and credentials but no pairing record
	err = queries.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{
		IpAddress: "192.168.1.100",
		Port:      "4028",
		UrlScheme: "https",
		ID:        dbDeviceID,
	})
	require.NoError(t, err)

	err = queries.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{
		DeviceID:    dbDeviceID,
		UsernameEnc: testContext.Config.GetAntminerUsernameEnc(t),
		PasswordEnc: testContext.Config.GetAntminerPasswordEnc(t),
	})
	require.NoError(t, err)

	userStore := sqlstores.NewSQLUserStore(testContext.DatabaseService.DB)
	service := miner.NewMinerService(testContext.DatabaseService.DB, userStore, testContext.ServiceProvider.EncryptService, testContext.ServiceProvider.FilesService, testContext.ServiceProvider.TokenService)

	miner, err := service.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier("test-unpaired-device"))

	require.Error(t, err)
	assert.Nil(t, miner)
	assert.Contains(t, err.Error(), "device not found")
}

func TestMinerService_GetMinerFromDeviceID_WithDeviceNeitherTokenNorCredentials_ShouldReturnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testContext.DatabaseService.CreateSuperAdminUser()

	queries := sqlc.New(testContext.DatabaseService.DB)

	discoveredDeviceID := createDiscoveredDevice(t, testContext.DatabaseService.DB, "TestMiner", "TestCorp", "antminer")

	// Create device with pairing but no credentials or token
	result, err := queries.UpsertDevice(t.Context(), sqlc.UpsertDeviceParams{
		OrgID:              1,
		DiscoveredDeviceID: discoveredDeviceID,
		DeviceIdentifier:   "test-no-auth-device",
		MacAddress:         "00:11:22:33:44:88",
		SerialNumber:       sql.NullString{String: "SN-NOAUTH", Valid: true},
	})
	require.NoError(t, err)

	dbDeviceID, err := result.LastInsertId()
	require.NoError(t, err)

	// Create pairing record with PAIRED status but no token
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      dbDeviceID,
		PairingStatus: "PAIRED",
	})
	require.NoError(t, err)

	// Create IP assignment but no credentials
	err = queries.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{
		IpAddress: "192.168.1.100",
		Port:      "4028",
		UrlScheme: "https",
		ID:        dbDeviceID,
	})
	require.NoError(t, err)

	miner, err := testContext.ServiceProvider.MinerService.GetMinerFromDeviceIdentifier(t.Context(), "test-no-auth-device")

	require.Error(t, err)
	assert.Nil(t, miner)
}
