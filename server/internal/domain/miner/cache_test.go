package miner_test

import (
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
)

// fakePluginManager implements miner.PluginManager for cache tests.
// It routes all driver lookups to a single pre-registered sdk.Driver.
type fakePluginManager struct {
	driver     sdk.Driver
	driverName string
}

func (f *fakePluginManager) HasPluginForDriverName(driverName string) bool {
	return driverName == f.driverName
}

func (f *fakePluginManager) GetCapabilitiesForDriverName(_ string) sdk.Capabilities {
	return sdk.Capabilities{}
}

func (f *fakePluginManager) GetDriverByDriverName(driverName string) (sdk.Driver, error) {
	if driverName == f.driverName {
		return f.driver, nil
	}
	return nil, fmt.Errorf("no driver registered for %s", driverName)
}

// TestMinerService_GetMinerFromDeviceIdentifier_CachesAfterFirstLookup verifies that
// repeated calls return the cached miner without hitting the DB or plugin driver again.
// The mock driver's NewDevice is set to Times(1) — a second uncached call would fail the test.
func TestMinerService_GetMinerFromDeviceIdentifier_CachesAfterFirstLookup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := models.DeviceIdentifier("cache-identifier-test")
	createTestDeviceWithCredentials(t, db, encryptService, string(deviceIdentifier))

	mockSDKDevice := sdkMocks.NewMockDevice(ctrl)
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	// NewDevice must be called exactly once; a second call (cache miss) would violate this.
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), string(deviceIdentifier), gomock.Any(), gomock.Any()).
		Return(sdk.NewDeviceResult{Device: mockSDKDevice}, nil).
		Times(1)

	pluginMgr := &fakePluginManager{driver: mockDriver, driverName: "antminer"}
	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService, pluginMgr)

	// Act — first call: DB lookup + plugin NewDevice
	miner1, err := service.GetMinerFromDeviceIdentifier(t.Context(), deviceIdentifier)
	require.NoError(t, err)
	require.NotNil(t, miner1)

	// Act — second call: must be served from cache (no DB or NewDevice)
	miner2, err := service.GetMinerFromDeviceIdentifier(t.Context(), deviceIdentifier)
	require.NoError(t, err)
	require.NotNil(t, miner2)

	// Assert — same instance returned; gomock Times(1) enforces no second NewDevice call
	assert.True(t, miner1 == miner2, "expected cache to return the same miner instance")
}

// TestMinerService_GetMiner_CachesAfterFirstLookup verifies that GetMiner (by numeric
// device ID) caches the miner handle so subsequent calls avoid DB and plugin calls.
func TestMinerService_GetMiner_CachesAfterFirstLookup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "cache-id-test"
	dbDeviceID := createTestDevice(t, db, deviceIdentifier)
	createTestMinerCredentials(t, db, encryptService, dbDeviceID)

	mockSDKDevice := sdkMocks.NewMockDevice(ctrl)
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	// NewDevice must be called exactly once; a second call (cache miss) would violate this.
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), deviceIdentifier, gomock.Any(), gomock.Any()).
		Return(sdk.NewDeviceResult{Device: mockSDKDevice}, nil).
		Times(1)

	pluginMgr := &fakePluginManager{driver: mockDriver, driverName: "antminer"}
	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService, pluginMgr)

	// Act — first call: DB lookup + plugin NewDevice
	miner1, err := service.GetMiner(t.Context(), dbDeviceID)
	require.NoError(t, err)
	require.NotNil(t, miner1)

	// Act — second call: must be served from cache (no DB or NewDevice)
	miner2, err := service.GetMiner(t.Context(), dbDeviceID)
	require.NoError(t, err)
	require.NotNil(t, miner2)

	// Assert — same instance returned; gomock Times(1) enforces no second NewDevice call
	assert.True(t, miner1 == miner2, "expected cache to return the same miner instance")
}

// TestMinerService_InvalidateMiner_ForcesRefreshOnNextLookup verifies that
// InvalidateMiner evicts both caches so the next call goes back to the DB.
func TestMinerService_InvalidateMiner_ForcesRefreshOnNextLookup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := models.DeviceIdentifier("invalidate-test-device")
	createTestDeviceWithCredentials(t, db, encryptService, string(deviceIdentifier))

	mockSDKDevice := sdkMocks.NewMockDevice(ctrl)
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	// NewDevice called once for the initial population; after InvalidateMiner the DB
	// is closed so the refresh attempt fails before reaching NewDevice again.
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), string(deviceIdentifier), gomock.Any(), gomock.Any()).
		Return(sdk.NewDeviceResult{Device: mockSDKDevice}, nil).
		Times(1)

	pluginMgr := &fakePluginManager{driver: mockDriver, driverName: "antminer"}
	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService, pluginMgr)

	// Populate the cache
	miner1, err := service.GetMinerFromDeviceIdentifier(t.Context(), deviceIdentifier)
	require.NoError(t, err)
	require.NotNil(t, miner1)

	// Act — evict the cache entry
	service.InvalidateMiner(deviceIdentifier)

	// Close DB to force any DB lookup to fail — proving the cache was evicted
	db.Close()

	// Act — post-invalidation lookup must attempt DB (not cache), and fail
	_, err = service.GetMinerFromDeviceIdentifier(t.Context(), deviceIdentifier)

	// Assert — error from DB proves the cache was not used
	assert.Error(t, err, "expected DB error after InvalidateMiner + DB close")
}

// TestMinerService_InvalidateMiner_ForcesRefreshForBothLookupPaths verifies that
// InvalidateMiner evicts the shared cache so both GetMiner and GetMinerFromDeviceIdentifier
// are forced back to the DB on the next call.
func TestMinerService_InvalidateMiner_ForcesRefreshForBothLookupPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "dual-cache-invalidate-device"
	dbDeviceID := createTestDevice(t, db, deviceIdentifier)
	createTestMinerCredentials(t, db, encryptService, dbDeviceID)

	mockSDKDevice := sdkMocks.NewMockDevice(ctrl)
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	// NewDevice must be called exactly once for the initial population.
	// After InvalidateMiner, DB is closed to prove both lookup paths miss the cache.
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), deviceIdentifier, gomock.Any(), gomock.Any()).
		Return(sdk.NewDeviceResult{Device: mockSDKDevice}, nil).
		Times(1)

	pluginMgr := &fakePluginManager{driver: mockDriver, driverName: "antminer"}
	service := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService, pluginMgr)

	// Populate the shared cache via GetMiner.
	_, err := service.GetMiner(t.Context(), dbDeviceID)
	require.NoError(t, err)

	// Act — evict the cache entry by identifier.
	service.InvalidateMiner(models.DeviceIdentifier(deviceIdentifier))

	// Close DB to force any DB lookup to fail.
	db.Close()

	// Both lookup paths must now miss the cache and attempt a DB query, which fails.
	_, errByID := service.GetMiner(t.Context(), dbDeviceID)
	_, errByIdent := service.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))

	// Assert — errors confirm the cache was evicted and DB was consulted.
	assert.Error(t, errByID, "expected DB error for GetMiner after InvalidateMiner + DB close")
	assert.Error(t, errByIdent, "expected DB error for GetMinerFromDeviceIdentifier after InvalidateMiner + DB close")
}
