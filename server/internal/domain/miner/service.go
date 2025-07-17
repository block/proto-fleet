package miner

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

const (
	defaultAntminerRPCPort = "4028"
	maxPort                = 65535
)

var _ telemetry.MinerGetter = &MinerService{}

type MinerService struct {
	db             *sql.DB
	queries        *sqlc.Queries
	encryptService *encrypt.Service
}

func NewMinerService(db *sql.DB, encryptService *encrypt.Service) *MinerService {
	if db == nil {
		panic("database cannot be nil")
	}
	if encryptService == nil {
		panic("encrypt service cannot be nil")
	}

	return &MinerService{
		db:             db,
		queries:        sqlc.New(db),
		encryptService: encryptService,
	}
}

func (s *MinerService) GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	deviceData, err := s.queries.GetDeviceWithCredentialsAndIPByID(ctx, deviceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("device not found: %d", deviceID)
		}
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	return s.createMiner(deviceData.ID, deviceData.Port, deviceData.Type, deviceData.UsernameEnc.String, deviceData.PasswordEnc.String, deviceData.IpAddress, deviceData.PairingToken.String)
}

func (s *MinerService) GetMinerFromDeviceIdentifier(ctx context.Context, deviceID models.DeviceIdentifier) (interfaces.Miner, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("device ID cannot be empty")
	}

	deviceData, err := s.queries.GetDeviceWithCredentialsAndIPByDeviceIdentifier(ctx, string(deviceID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("device not found: %s", deviceID)
		}
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	return s.createMiner(deviceData.ID, deviceData.Port, deviceData.Type, deviceData.UsernameEnc.String, deviceData.PasswordEnc.String, deviceData.IpAddress, deviceData.PairingToken.String)
}

func (s *MinerService) createMiner(deviceID int64, devicePort string, deviceType string, deviceUsername string, devicePassword string, deviceIPAddress string, devicePairingToken string) (interfaces.Miner, error) {
	portInt, err := strconv.Atoi(devicePort)
	if err != nil {
		return nil, fmt.Errorf("failed to parse port %s: %w", devicePort, err)
	}

	if portInt < 0 || portInt > maxPort {
		return nil, fmt.Errorf("port %d is out of valid range (0-%d)", portInt, maxPort)
	}

	port := uint16(portInt)

	minerType, err := models.TypeFromString(deviceType)
	if err != nil {
		return nil, fmt.Errorf("failed to parse device type: %w", err)
	}

	switch minerType {
	case models.TypeAntminer:
		return s.createAntminer(deviceID, deviceUsername, devicePassword, deviceIPAddress, port)
	case models.TypeProto:
		return s.createProtoMiner(deviceID, devicePassword, devicePairingToken, deviceIPAddress, port)
	case models.TypeWhatsminer, models.TypeAvalon, models.TypeUnknown:
		return nil, fmt.Errorf("unsupported miner type: %s", deviceType)
	default:
		return nil, fmt.Errorf("unsupported miner type: %s", deviceType)
	}
}

func (s *MinerService) createAntminer(deviceID int64, deviceUsername string, devicePassword string, deviceIPAddress string, port uint16) (interfaces.Miner, error) {
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
		deviceID,
		deviceIPAddress,
		port,
		defaultAntminerRPCPort,
		string(decryptedUsername),
		password,
		webClient,
		rpcClient,
	), nil
}

func (s *MinerService) createProtoMiner(deviceID int64, devicePassword string, devicePairingToken string, deviceIPAddress string, port uint16) (interfaces.Miner, error) {
	var authToken secrets.Text

	if devicePairingToken != "" {
		authToken = *secrets.NewText(devicePairingToken)
	} else if devicePassword != "" {
		decryptedAuthToken, err := s.encryptService.Decrypt(devicePassword)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt auth token: %w", err)
		}
		authToken = *secrets.NewText(string(decryptedAuthToken))
	} else {
		return nil, fmt.Errorf("proto miner requires either pairing token or encrypted auth token")
	}

	return proto.NewProtoMiner(
		deviceID,
		deviceIPAddress,
		port,
		authToken,
	)
}
