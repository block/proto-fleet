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
	telemetry "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

var _ telemetry.MinerManager = &MinerService{}

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

func (s *MinerService) GetMinerFromDeviceID(ctx context.Context, deviceID models.DeviceID) (interfaces.Miner, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("device ID cannot be empty")
	}

	deviceData, err := s.queries.GetDeviceWithCredentialsAndIPByDeviceIdentifier(ctx, string(deviceID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("device not found or missing credentials/IP for device ID: %s", deviceID)
		}
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	return s.createMinerFromDeviceData(ctx, deviceData)
}

func (s *MinerService) createMinerFromDeviceData(
	ctx context.Context,
	deviceData sqlc.GetDeviceWithCredentialsAndIPByDeviceIdentifierRow,
) (interfaces.Miner, error) {
	portInt, err := strconv.Atoi(deviceData.Port)
	if err != nil {
		return nil, fmt.Errorf("failed to parse port %s: %w", deviceData.Port, err)
	}

	if portInt < 0 || portInt > 65535 {
		return nil, fmt.Errorf("port %d is out of valid range (0-65535)", portInt)
	}

	port := uint16(portInt)

	deviceType, err := models.TypeFromString(deviceData.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to parse device type: %w", err)
	}

	switch deviceType {
	case models.TypeAntminer:
		return s.createAntminer(deviceData, port)
	case models.TypeProto:
		return s.createProtoMiner(deviceData, port)
	case models.TypeWhatsminer, models.TypeAvalon, models.TypeUnknown:
		return nil, fmt.Errorf("unsupported miner type: %s", deviceType)
	default:
		return nil, fmt.Errorf("unsupported miner type: %s", deviceType)
	}
}

func (s *MinerService) createAntminer(deviceData sqlc.GetDeviceWithCredentialsAndIPByDeviceIdentifierRow, port uint16) (interfaces.Miner, error) {
	decryptedUsername, err := s.encryptService.Decrypt(deviceData.UsernameEnc)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt username: %w", err)
	}

	decryptedPassword, err := s.encryptService.Decrypt(deviceData.PasswordEnc)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password: %w", err)
	}

	webClient := web.NewService()
	rpcClient := rpc.NewService()
	password := *secrets.NewText(string(decryptedPassword))
	rpcPort := "4028" // Default RPC port for Antminer

	return antminer.NewAntminer(
		deviceData.ID,
		deviceData.IpAddress,
		port,
		rpcPort,
		string(decryptedUsername),
		password,
		webClient,
		rpcClient,
	), nil
}

func (s *MinerService) createProtoMiner(deviceData sqlc.GetDeviceWithCredentialsAndIPByDeviceIdentifierRow, port uint16) (interfaces.Miner, error) {
	decryptedAuthToken, err := s.encryptService.Decrypt(deviceData.PasswordEnc)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt auth token: %w", err)
	}

	authToken := *secrets.NewText(string(decryptedAuthToken)) // Proto miners use password as auth token

	return proto.NewProtoMiner(
		deviceData.ID,
		deviceData.IpAddress,
		port,
		authToken,
	)
}
