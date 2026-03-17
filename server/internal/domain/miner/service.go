package miner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"

	"github.com/proto-at-block/proto-fleet/server/internal/domain/token"

	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/files"

	"github.com/proto-at-block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/miner/models"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/plugins"
	stores "github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/telemetry"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/encrypt"
	sdk "github.com/proto-at-block/proto-fleet/server/sdk/v1"
)

var _ telemetry.MinerGetter = &Service{}

type Service struct {
	// TODO: Refactor this to use a store instead of SQLConnectionManager directly
	sqlstores.SQLConnectionManager
	userStore      stores.UserStore
	encryptService *encrypt.Service
	filesService   *files.Service
	tokenService   *token.Service
	pluginManager  PluginManager
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
	}
}

func (s *Service) GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	deviceData, err := s.GetQueries(ctx).GetDeviceWithCredentialsAndIPByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %d", deviceID)
		}
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	return s.createMiner(
		ctx,
		deviceData.DeviceIdentifier,
		deviceData.OrgID,
		deviceData.Port,
		deviceData.DriverName,
		deviceData.UsernameEnc.String,
		deviceData.PasswordEnc.String,
		deviceData.IpAddress,
		deviceData.UrlScheme,
		deviceData.SerialNumber.String,
		deviceData.MacAddress,
	)
}

func (s *Service) GetMinerFromDeviceIdentifier(ctx context.Context, deviceID models.DeviceIdentifier) (interfaces.Miner, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("device ID cannot be empty")
	}

	deviceData, err := s.GetQueries(ctx).GetDeviceWithCredentialsAndIPByDeviceIdentifier(ctx, string(deviceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("device not found: %s", deviceID)
		}
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	return s.createMiner(
		ctx,
		deviceData.DeviceIdentifier,
		deviceData.OrgID,
		deviceData.Port,
		deviceData.DriverName,
		deviceData.UsernameEnc.String,
		deviceData.PasswordEnc.String,
		deviceData.IpAddress,
		deviceData.UrlScheme,
		deviceData.SerialNumber.String,
		deviceData.MacAddress,
	)
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

func (s *Service) createMiner(ctx context.Context, deviceIdentifier string, orgID int64, devicePort string, driverName string, deviceUsername string, devicePassword string, deviceIPAddress string, deviceScheme string, deviceSerialNumber string, macAddress string) (interfaces.Miner, error) {
	if !s.pluginManager.HasPluginForDriverName(driverName) {
		return nil, fmt.Errorf("no plugin available (driver_name=%q) — ensure the device has been discovered and the appropriate plugin is loaded", driverName)
	}
	return plugins.NewPluginMinerWithCredentials(ctx, plugins.PluginMinerConfig{
		DeviceIdentifier:   deviceIdentifier,
		DriverName:         driverName,
		Caps:               s.pluginManager.GetCapabilitiesForDriverName(driverName),
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
