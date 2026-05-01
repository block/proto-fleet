package miner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"

	"github.com/block/proto-fleet/server/internal/domain/token"

	"github.com/block/proto-fleet/server/internal/infrastructure/files"

	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	// minerCacheTTL is the duration a cached miner handle is considered valid.
	// A short TTL self-heals stale connection coordinates (e.g., after a device
	// moves to a new IP) and credential rotations without requiring explicit
	// invalidation logic for every possible change path. Auth errors and
	// lifecycle events (unpair, delete, password change) still trigger immediate
	// eviction for faster recovery.
	minerCacheTTL = 1 * time.Minute

	// minerCacheSize is the maximum number of miner handles to cache.
	// Sized to cover very large fleets without meaningful memory overhead.
	minerCacheSize = 10_000
)

var _ telemetry.CachedMinerGetter = &Service{}

type Service struct {
	// TODO: Refactor this to use a store instead of SQLConnectionManager directly
	sqlstores.SQLConnectionManager
	userStore      stores.UserStore
	encryptService *encrypt.Service
	filesService   *files.Service
	tokenService   *token.Service
	pluginManager  PluginManager

	// cache stores miner handles keyed by DeviceIdentifier (string).
	// Both GetMiner and GetMinerFromDeviceIdentifier read from and write to
	// this single cache, keeping invalidation simple.
	cache *lru.LRU[string, interfaces.Miner]
}

// PluginManager defines the interface for plugin manager operations needed by MinerService
type PluginManager interface {
	HasPluginForDriverName(driverName string) bool
	GetCapabilitiesForDriverName(driverName string) sdk.Capabilities
	plugins.PluginDriverGetter
}

func NewMinerService(db *sql.DB, userStore stores.UserStore, encryptService *encrypt.Service, filesService *files.Service, tokenService *token.Service, pluginManager PluginManager) *Service {
	if db == nil {
		panic("database cannot be nil")
	}
	if encryptService == nil {
		panic("encrypt service cannot be nil")
	}
	if filesService == nil {
		panic("files service cannot be nil")
	}
	if pluginManager == nil {
		panic("plugin manager cannot be nil")
	}

	return &Service{
		SQLConnectionManager: sqlstores.NewSQLConnectionManager(db),
		userStore:            userStore,
		encryptService:       encryptService,
		filesService:         filesService,
		tokenService:         tokenService,
		pluginManager:        pluginManager,
		cache:                lru.NewLRU[string, interfaces.Miner](minerCacheSize, nil, minerCacheTTL),
	}
}

// GetMiner returns the miner handle for the given numeric device ID.
// It performs a lightweight identifier lookup then delegates to
// GetMinerFromDeviceIdentifier so both lookup paths share the same cache.
func (s *Service) GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	identifier, err := s.GetQueries(ctx).GetDeviceIdentifierByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %d", deviceID)
		}
		return nil, fmt.Errorf("failed to get device identifier: %w", err)
	}
	return s.GetMinerFromDeviceIdentifier(ctx, models.DeviceIdentifier(identifier))
}

func (s *Service) GetMinerFromDeviceIdentifier(ctx context.Context, deviceID models.DeviceIdentifier) (interfaces.Miner, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("device ID cannot be empty")
	}

	if m, ok := s.cache.Get(string(deviceID)); ok {
		return m, nil
	}

	deviceData, err := s.GetQueries(ctx).GetDeviceWithCredentialsAndIPByDeviceIdentifier(ctx, string(deviceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %s", deviceID)
		}
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	deviceModel := ""
	if deviceData.Model.Valid {
		deviceModel = deviceData.Model.String
	}
	deviceManufacturer := ""
	if deviceData.Manufacturer.Valid {
		deviceManufacturer = deviceData.Manufacturer.String
	}

	m, err := s.createMiner(
		ctx,
		deviceData.DeviceIdentifier,
		deviceData.OrgID,
		deviceData.Port,
		deviceData.DriverName,
		deviceManufacturer,
		deviceModel,
		deviceData.UsernameEnc.String,
		deviceData.PasswordEnc.String,
		deviceData.IpAddress,
		deviceData.UrlScheme,
		deviceData.SerialNumber.String,
		deviceData.MacAddress,
	)
	if err != nil {
		return nil, err
	}

	s.cache.Add(string(deviceID), m)
	return m, nil
}

// InvalidateMiner removes the cached miner handle for the given device identifier
// so the next lookup fetches fresh credentials and connection info from the DB.
// Call this on auth errors, credential changes, and device lifecycle events
// (unpair, delete).
func (s *Service) InvalidateMiner(deviceIdentifier models.DeviceIdentifier) {
	s.cache.Remove(string(deviceIdentifier))
}

func (s *Service) getProtoMinerAuthPrivateKey(ctx context.Context, orgID int64) ([]byte, error) {
	encryptedKey, err := s.userStore.GetOrganizationPrivateKey(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting org private key: %v", err)
	}

	privateKey, err := s.encryptService.Decrypt(encryptedKey)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error decrypting private key: %v", err)
	}

	return privateKey, nil
}

func (s *Service) createMiner(ctx context.Context, deviceIdentifier string, orgID int64, devicePort string, driverName string, deviceManufacturer string, deviceModel string, deviceUsername string, devicePassword string, deviceIPAddress string, deviceScheme string, deviceSerialNumber string, macAddress string) (interfaces.Miner, error) {
	if !s.pluginManager.HasPluginForDriverName(driverName) {
		return nil, fmt.Errorf("no plugin available (driver_name=%q) — ensure the device has been discovered and the appropriate plugin is loaded", driverName)
	}
	return plugins.NewPluginMinerWithCredentials(ctx, plugins.PluginMinerConfig{
		DeviceIdentifier:   deviceIdentifier,
		DriverName:         driverName,
		Caps:               s.effectiveCapabilitiesForDevice(ctx, driverName, deviceManufacturer, deviceModel),
		DeviceIPAddress:    deviceIPAddress,
		DevicePort:         devicePort,
		DeviceScheme:       deviceScheme,
		DeviceSerialNumber: deviceSerialNumber,
		DeviceUsername:     deviceUsername,
		DevicePassword:     devicePassword,
		MacAddress:         macAddress,
		OrgID:              orgID,
		EncryptService:     s.encryptService,
		TokenService:       s.tokenService,
		FilesService:       s.filesService,
		GetOrgPrivateKey:   s.getProtoMinerAuthPrivateKey,
		DriverGetter:       s.pluginManager,
	})
}

func (s *Service) effectiveCapabilitiesForDevice(ctx context.Context, driverName string, deviceManufacturer string, deviceModel string) sdk.Capabilities {
	caps := sdk.Capabilities{}
	for capability, enabled := range s.pluginManager.GetCapabilitiesForDriverName(driverName) {
		caps[capability] = enabled
	}

	if deviceModel == "" {
		return caps
	}

	driver, err := s.pluginManager.GetDriverByDriverName(driverName)
	if err != nil {
		return caps
	}

	modelProvider, ok := driver.(sdk.ModelCapabilitiesProvider)
	if !ok {
		return caps
	}

	modelCaps := modelProvider.GetCapabilitiesForModel(ctx, deviceManufacturer, deviceModel)
	if modelCaps == nil {
		return caps
	}

	for capability, enabled := range modelCaps {
		caps[capability] = enabled
	}
	return caps
}
