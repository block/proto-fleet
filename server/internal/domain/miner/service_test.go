package miner

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
)

func TestNewMinerService_WithValidDB_ShouldCreateService(t *testing.T) {
	db, encryptService := setupTestDB(t)

	service := NewMinerService(db, encryptService)

	assert.NotNil(t, service)
}

func TestNewMinerService_WithNilDB_ShouldPanic(t *testing.T) {
	_, encryptService := setupTestDB(t)

	assert.Panics(t, func() {
		NewMinerService(nil, encryptService)
	})
}

func TestNewMinerService_WithNilEncryptService_ShouldPanic(t *testing.T) {
	db, _ := setupTestDB(t)

	assert.Panics(t, func() {
		NewMinerService(db, nil)
	})
}

func TestMinerService_GetMinerFromDeviceID_WithValidDevice_ShouldReturnMiner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, encryptService := setupTestDB(t)

	deviceID := models.DeviceID("test-device-123")
	createTestDeviceWithCredentials(t, db, string(deviceID))

	service := NewMinerService(db, encryptService)

	miner, err := service.GetMinerFromDeviceID(t.Context(), deviceID)

	require.NoError(t, err)
	assert.NotNil(t, miner)
	assert.Equal(t, models.TypeAntminer, miner.GetType())
}

func TestMinerService_GetMinerFromDeviceID_WithNonexistentDevice_ShouldReturnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, encryptService := setupTestDB(t)

	service := NewMinerService(db, encryptService)

	miner, err := service.GetMinerFromDeviceID(t.Context(), models.DeviceID("nonexistent"))

	require.Error(t, err)
	assert.Nil(t, miner)
	assert.Contains(t, err.Error(), "device not found")
}

func TestMinerService_GetMinerFromDeviceID_WithEmptyDeviceID_ShouldReturnError(t *testing.T) {
	db, encryptService := setupTestDB(t)

	service := NewMinerService(db, encryptService)

	miner, err := service.GetMinerFromDeviceID(t.Context(), models.DeviceID(""))

	require.Error(t, err)
	assert.Nil(t, miner)
	assert.Contains(t, err.Error(), "device ID cannot be empty")
}

func TestMinerService_GetMinerFromDeviceID_WithDatabaseError_ShouldReturnError(t *testing.T) {
	db, encryptService := setupTestDB(t)
	db.Close() // Simulate database error

	service := NewMinerService(db, encryptService)

	miner, err := service.GetMinerFromDeviceID(t.Context(), models.DeviceID("device-123"))

	require.Error(t, err)
	assert.Nil(t, miner)
}

func TestMinerService_GetMinerFromDeviceID_WithMissingCredentials_ShouldReturnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, encryptService := setupTestDB(t)

	deviceID := models.DeviceID("test-device-no-creds")
	createTestDevice(t, db, string(deviceID))

	service := NewMinerService(db, encryptService)

	miner, err := service.GetMinerFromDeviceID(t.Context(), deviceID)

	require.Error(t, err)
	assert.Nil(t, miner)
}

func TestMinerService_ConcurrentAccess_ShouldBeThreadSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Parallel()

	db, encryptService := setupTestDB(t)

	const numDevices = 10
	deviceIDs := make([]models.DeviceID, numDevices)
	for i := range numDevices {
		deviceID := models.DeviceID(fmt.Sprintf("device-%d", i))
		deviceIDs[i] = deviceID
		createTestDeviceWithCredentials(t, db, string(deviceID))
	}

	service := NewMinerService(db, encryptService)

	const numGoroutines = 20
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			deviceID := deviceIDs[goroutineID%numDevices]
			_, err := service.GetMinerFromDeviceID(t.Context(), deviceID)
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

	db, encryptService := setupTestDB(t)

	service := NewMinerService(db, encryptService)

	tests := []struct {
		deviceType   string
		expectedType models.Type
	}{
		{"antminer", models.TypeAntminer},
		{"proto", models.TypeProto},
		{"whatsminer", models.TypeWhatsminer},
		{"avalon", models.TypeAvalon},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("type_%s", test.deviceType), func(t *testing.T) {
			deviceID := models.DeviceID(fmt.Sprintf("test-%s-device", test.deviceType))

			queries := sqlc.New(db)
			result, err := queries.UpsertDevice(t.Context(), sqlc.UpsertDeviceParams{
				OrgID:            0,
				DeviceIdentifier: string(deviceID),
				MacAddress:       "00:11:22:33:44:55",
				SerialNumber:     sql.NullString{String: "SN-123", Valid: true},
				Model:            sql.NullString{String: "TestMiner", Valid: true},
				Manufacturer:     sql.NullString{String: "TestCorp", Valid: true},
				Type:             test.deviceType,
				IsActive:         sql.NullBool{Bool: true, Valid: true},
			})
			require.NoError(t, err)

			dbDeviceID, err := result.LastInsertId()
			require.NoError(t, err)

			// Create device pairing record with PAIRED status
			_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
				DeviceID:      dbDeviceID,
				PairingToken:  sql.NullString{}, // No token for credential-based devices
				PairingStatus: "PAIRED",
			})
			require.NoError(t, err)

			err = queries.CreateInactiveDeviceIPAssignment(t.Context(), sqlc.CreateInactiveDeviceIPAssignmentParams{
				DeviceID:  dbDeviceID,
				IpAddress: "192.168.1.100",
				Port:      "4028",
			})
			require.NoError(t, err)

			err = queries.ActivateNewIPAssignment(t.Context(), sqlc.ActivateNewIPAssignmentParams{
				IpAddress: "192.168.1.100",
				Port:      "4028",
				DeviceID:  dbDeviceID,
			})
			require.NoError(t, err)

			createTestMinerCredentials(t, db, dbDeviceID)

			if test.expectedType == models.TypeAntminer || test.expectedType == models.TypeProto {
				miner, err := service.GetMinerFromDeviceID(t.Context(), deviceID)

				require.NoError(t, err)
				assert.NotNil(t, miner)
				assert.Equal(t, test.expectedType, miner.GetType())
			} else {
				miner, err := service.GetMinerFromDeviceID(t.Context(), deviceID)

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

	db, encryptService := setupTestDB(t)

	deviceID := models.DeviceID("test-proto-token-device")
	pairingToken := "test-pairing-token-123"
	createTestProtoMinerWithToken(t, db, string(deviceID), pairingToken)

	service := NewMinerService(db, encryptService)

	miner, err := service.GetMinerFromDeviceID(t.Context(), deviceID)

	require.NoError(t, err)
	assert.NotNil(t, miner)
	assert.Equal(t, models.TypeProto, miner.GetType())
}

func TestMinerService_GetMinerFromDeviceID_WithUnpairedDevice_ShouldReturnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, encryptService := setupTestDB(t)

	queries := sqlc.New(db)

	// Create device without pairing record
	result, err := queries.UpsertDevice(t.Context(), sqlc.UpsertDeviceParams{
		OrgID:            0,
		DeviceIdentifier: "test-unpaired-device",
		MacAddress:       "00:11:22:33:44:99",
		SerialNumber:     sql.NullString{String: "SN-UNPAIRED", Valid: true},
		Model:            sql.NullString{String: "TestMiner", Valid: true},
		Manufacturer:     sql.NullString{String: "TestCorp", Valid: true},
		Type:             "antminer",
		IsActive:         sql.NullBool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	dbDeviceID, err := result.LastInsertId()
	require.NoError(t, err)

	// Create IP assignment and credentials but no pairing record
	err = queries.CreateInactiveDeviceIPAssignment(t.Context(), sqlc.CreateInactiveDeviceIPAssignmentParams{
		DeviceID:  dbDeviceID,
		IpAddress: "192.168.1.100",
		Port:      "4028",
	})
	require.NoError(t, err)

	err = queries.ActivateNewIPAssignment(t.Context(), sqlc.ActivateNewIPAssignmentParams{
		IpAddress: "192.168.1.100",
		Port:      "4028",
		DeviceID:  dbDeviceID,
	})
	require.NoError(t, err)

	createTestMinerCredentials(t, db, dbDeviceID)

	service := NewMinerService(db, encryptService)

	miner, err := service.GetMinerFromDeviceID(t.Context(), models.DeviceID("test-unpaired-device"))

	require.Error(t, err)
	assert.Nil(t, miner)
	assert.Contains(t, err.Error(), "device not found")
}

func TestMinerService_GetMinerFromDeviceID_WithDeviceNeitherTokenNorCredentials_ShouldReturnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, encryptService := setupTestDB(t)

	queries := sqlc.New(db)

	// Create device with pairing but no credentials or token
	result, err := queries.UpsertDevice(t.Context(), sqlc.UpsertDeviceParams{
		OrgID:            0,
		DeviceIdentifier: "test-no-auth-device",
		MacAddress:       "00:11:22:33:44:88",
		SerialNumber:     sql.NullString{String: "SN-NOAUTH", Valid: true},
		Model:            sql.NullString{String: "TestMiner", Valid: true},
		Manufacturer:     sql.NullString{String: "TestCorp", Valid: true},
		Type:             "antminer",
		IsActive:         sql.NullBool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	dbDeviceID, err := result.LastInsertId()
	require.NoError(t, err)

	// Create pairing record with PAIRED status but no token
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      dbDeviceID,
		PairingToken:  sql.NullString{}, // No token
		PairingStatus: "PAIRED",
	})
	require.NoError(t, err)

	// Create IP assignment but no credentials
	err = queries.CreateInactiveDeviceIPAssignment(t.Context(), sqlc.CreateInactiveDeviceIPAssignmentParams{
		DeviceID:  dbDeviceID,
		IpAddress: "192.168.1.100",
		Port:      "4028",
	})
	require.NoError(t, err)

	err = queries.ActivateNewIPAssignment(t.Context(), sqlc.ActivateNewIPAssignmentParams{
		IpAddress: "192.168.1.100",
		Port:      "4028",
		DeviceID:  dbDeviceID,
	})
	require.NoError(t, err)

	service := NewMinerService(db, encryptService)

	miner, err := service.GetMinerFromDeviceID(t.Context(), models.DeviceID("test-no-auth-device"))

	require.Error(t, err)
	assert.Nil(t, miner)
}
