package miner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

	"github.com/btc-mining/proto-fleet/server/internal/domain/token"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/plugins"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
)

var _ telemetry.MinerGetter = &Service{}

type Service struct {
	// TODO: DASH-579: Refactor this to use a store instead of SQLConnectionManager directly
	sqlstores.SQLConnectionManager
	userStore      stores.UserStore
	encryptService *encrypt.Service
	filesService   *files.Service
	tokenService   *token.Service
	pluginManager  PluginManager
}

// PluginManager defines the interface for plugin manager operations needed by MinerService
type PluginManager interface {
	HasPluginForMinerType(minerType models.Type) bool
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
		deviceData.Type,
		deviceData.Model.String,
		deviceData.UsernameEnc.String,
		deviceData.PasswordEnc.String,
		deviceData.IpAddress,
		deviceData.UrlScheme,
		deviceData.SerialNumber.String,
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
		deviceData.Type,
		deviceData.Model.String,
		deviceData.UsernameEnc.String,
		deviceData.PasswordEnc.String,
		deviceData.IpAddress,
		deviceData.UrlScheme,
		deviceData.SerialNumber.String,
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

func (s *Service) createMiner(ctx context.Context, deviceIdentifier string, orgID int64, devicePort string, deviceType string, deviceModel string, deviceUsername string, devicePassword string, deviceIPAddress string, deviceScheme string, deviceSerialNumber string) (interfaces.Miner, error) {
	// Parse device type using both type and model for disambiguation
	minerType, err := models.TypeFromDeviceInfo(deviceType, deviceModel)
	if err != nil {
		return nil, fmt.Errorf("failed to parse device type: %w", err)
	}

	// Check if a plugin supports this miner type
	if s.pluginManager != nil && s.pluginManager.HasPluginForMinerType(minerType) {
		return s.createPluginMiner(ctx, deviceIdentifier, orgID, minerType, devicePort, deviceUsername, devicePassword, deviceIPAddress, deviceScheme, deviceSerialNumber)
	}

	// No built-in implementations available for this miner type
	return nil, fmt.Errorf("no plugin available for miner type %s - please ensure the appropriate plugin is installed and loaded", minerType)
}

func (s *Service) createPluginMiner(ctx context.Context, deviceIdentifier string, orgID int64, minerType models.Type, devicePort string, deviceUsername string, devicePassword string, deviceIPAddress string, deviceScheme string, deviceSerialNumber string) (interfaces.Miner, error) {
	// Use the plugin factory to create the miner - this encapsulates all SDK logic
	return plugins.NewPluginMinerWithCredentials(ctx, plugins.PluginMinerConfig{
		DeviceIdentifier:   deviceIdentifier,
		MinerType:          minerType,
		DeviceIPAddress:    deviceIPAddress,
		DevicePort:         devicePort,
		DeviceScheme:       deviceScheme,
		DeviceSerialNumber: deviceSerialNumber,
		DeviceUsername:     deviceUsername,
		DevicePassword:     devicePassword,
		OrgID:              orgID,
		EncryptService:     s.encryptService,
		TokenService:       s.tokenService,
		GetOrgPrivateKey:   s.getProtoMinerAuthPrivateKey,
		DriverGetter:       s.pluginManager,
	})
}
