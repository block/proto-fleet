package miner

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

	"github.com/btc-mining/proto-fleet/server/internal/domain/token"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/plugins"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

const (
	maxPort = 65535
)

var _ telemetry.MinerGetter = &MinerService{}

type MinerService struct {
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

func NewMinerService(db *sql.DB, userStore stores.UserStore, encryptService *encrypt.Service, filesService *files.Service, tokenService *token.Service, pluginManager PluginManager) *MinerService {
	if db == nil {
		panic("database cannot be nil")
	}
	if encryptService == nil {
		panic("encrypt service cannot be nil")
	}
	if filesService == nil {
		panic("files service cannot be nil")
	}

	return &MinerService{
		SQLConnectionManager: sqlstores.NewSQLConnectionManager(db),
		userStore:            userStore,
		encryptService:       encryptService,
		filesService:         filesService,
		tokenService:         tokenService,
		pluginManager:        pluginManager,
	}
}

func (s *MinerService) GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	deviceData, err := s.GetQueries(ctx).GetDeviceWithCredentialsAndIPByID(ctx, deviceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("device not found: %d", deviceID)
		}
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	return s.createMiner(
		ctx,
		deviceData.DeviceIdentifier,
		deviceData.OrgID,
		deviceData.Port,
		deviceData.Type,
		deviceData.UsernameEnc.String,
		deviceData.PasswordEnc.String,
		deviceData.IpAddress,
		deviceData.UrlScheme,
		deviceData.SerialNumber.String,
	)
}

func (s *MinerService) GetMinerFromDeviceIdentifier(ctx context.Context, deviceID models.DeviceIdentifier) (interfaces.Miner, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("device ID cannot be empty")
	}

	deviceData, err := s.GetQueries(ctx).GetDeviceWithCredentialsAndIPByDeviceIdentifier(ctx, string(deviceID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("device not found: %s", deviceID)
		}
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	return s.createMiner(
		ctx,
		deviceData.DeviceIdentifier,
		deviceData.OrgID,
		deviceData.Port,
		deviceData.Type,
		deviceData.UsernameEnc.String,
		deviceData.PasswordEnc.String,
		deviceData.IpAddress,
		deviceData.UrlScheme,
		deviceData.SerialNumber.String,
	)
}

func (s *MinerService) getProtoMinerAuthPrivateKey(ctx context.Context, orgID int64) ([]byte, error) {
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

func (s *MinerService) BuildMinerInfo(ctx context.Context, deviceIdentifier string, orgID int64, deviceIPAddress string, devicePort string, deviceScheme string, minerType models.Type, deviceSerialNumber string) (interfaces.MinerInfo, error) {
	portInt, err := strconv.Atoi(devicePort)
	if err != nil {
		return nil, fmt.Errorf("failed to parse port %s: %w", devicePort, err)
	}

	if portInt < 0 || portInt > maxPort {
		return nil, fmt.Errorf("port %d is out of valid range (0-%d)", portInt, maxPort)
	}

	port := uint16(portInt)

	scheme, err := networking.ProtocolFromString(deviceScheme)
	if err != nil {
		return nil, fmt.Errorf("failed to parse scheme: %w", err)
	}

	minerIdentifier := models.DeviceIdentifier(deviceIdentifier)
	switch minerType {
	case models.TypeAntminer:
		return antminer.NewAntminerInfo(minerIdentifier, deviceIPAddress, port, deviceSerialNumber), nil
	case models.TypeProto:
		minerAuthPrivateKey, err := s.getProtoMinerAuthPrivateKey(ctx, orgID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting auth private key: %v", err)
		}
		return proto.NewProtoMinerInfo(minerIdentifier, deviceIPAddress, port, scheme, minerAuthPrivateKey, deviceSerialNumber)
	case models.TypeWhatsminer, models.TypeAvalon, models.TypeUnknown:
		return nil, fmt.Errorf("unsupported miner type: %s", minerType)
	default:
		return nil, fmt.Errorf("unsupported miner type: %s", minerType)
	}
}

func (s *MinerService) createMiner(ctx context.Context, deviceIdentifier string, orgID int64, devicePort string, deviceType string, deviceUsername string, devicePassword string, deviceIPAddress string, deviceScheme string, deviceSerialNumber string) (interfaces.Miner, error) {
	// Parse device type string once at entry point
	minerType, err := models.TypeFromString(deviceType)
	if err != nil {
		return nil, fmt.Errorf("failed to parse device type: %w", err)
	}

	// Check if a plugin supports this miner type
	if s.pluginManager != nil && s.pluginManager.HasPluginForMinerType(minerType) {
		return s.createPluginMiner(ctx, deviceIdentifier, orgID, minerType, devicePort, deviceUsername, devicePassword, deviceIPAddress, deviceScheme, deviceSerialNumber)
	}

	// Fall back to built-in miner implementations
	minerInfo, err := s.BuildMinerInfo(ctx, deviceIdentifier, orgID, deviceIPAddress, devicePort, deviceScheme, minerType, deviceSerialNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get miner info: %w", err)
	}

	switch minerInfo.GetType() {
	case models.TypeAntminer:
		return s.createAntminer(minerInfo, deviceUsername, devicePassword)
	case models.TypeProto:
		protoMinerInfo, ok := minerInfo.(*proto.ProtoMinerInfo)
		if !ok {
			return nil, fmt.Errorf("expected *proto.ProtoMinerInfo but got %T", minerInfo)
		}
		return s.createProtoMiner(protoMinerInfo)
	case models.TypeWhatsminer, models.TypeAvalon, models.TypeUnknown:
		return nil, fmt.Errorf("unsupported miner type: %s", minerType)
	default:
		return nil, fmt.Errorf("unsupported miner type: %s", minerType)
	}
}

func (s *MinerService) createAntminer(minerInfo interfaces.MinerInfo, deviceUsername string, devicePassword string) (interfaces.Miner, error) {
	if deviceUsername == "" || devicePassword == "" {
		return nil, fmt.Errorf("antminer requires both username and password credentials")
	}

	decryptedUsername, err := s.encryptService.Decrypt(deviceUsername)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt username: %w", err)
	}

	decryptedPassword, err := s.encryptService.Decrypt(devicePassword)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password: %w", err)
	}

	webClient := web.NewService()
	rpcClient := rpc.NewService()
	password := *secrets.NewText(string(decryptedPassword))

	return antminer.NewAntminer(
		minerInfo,
		string(decryptedUsername),
		password,
		webClient,
		rpcClient,
	), nil
}

func (s *MinerService) createProtoMiner(minerInfo *proto.ProtoMinerInfo) (interfaces.Miner, error) {
	return proto.NewProtoMiner(
		minerInfo,
		s.filesService,
		s.tokenService,
		s.encryptService,
	)
}

func (s *MinerService) createPluginMiner(ctx context.Context, deviceIdentifier string, orgID int64, minerType models.Type, devicePort string, deviceUsername string, devicePassword string, deviceIPAddress string, deviceScheme string, deviceSerialNumber string) (interfaces.Miner, error) {
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
		GetOrgPrivateKey:   s.getProtoMinerAuthPrivateKey,
		DriverGetter:       s.pluginManager,
	})
}
