package antminer

import (
	"context"
	"database/sql"
	"strings"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

var _ pairing.Pairer = &Service{}

type Service struct {
	conn      *sql.DB
	encryptor *encrypt.Service
	webClient web.WebAPIClient
}

func NewService(
	conn *sql.DB,
	encryptor *encrypt.Service,
	webClient web.WebAPIClient,
) *Service {
	return &Service{
		conn:      conn,
		encryptor: encryptor,
		webClient: webClient,
	}
}

func (s *Service) GetMinerType() models.Type {
	return models.TypeAntminer
}

func (s *Service) PairDevice(ctx context.Context, device *minerdiscovery.DiscoveredDevice, credentials *pb.Credentials) error {
	if credentials == nil || strings.TrimSpace(credentials.Username) == "" || credentials.Password == nil || strings.TrimSpace(*credentials.Password) == "" {
		return fleeterror.NewInvalidArgumentErrorf("credentials are required for Antminer pairing")
	}

	systemInfo, err := authAndGetSystemInfo(ctx, device, s, credentials)
	if err != nil {
		return err
	}

	// Update device with serial number and MAC address
	device.SerialNumber = systemInfo.SerialNumber
	device.MacAddress = systemInfo.MacAddr

	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		deviceID, err := pairing.SaveDiscoveredDevice(ctx, q, device)
		if err != nil {
			return err
		}

		// Store credentials
		encryptedUsername, err := s.encryptor.Encrypt([]byte(credentials.Username))
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to encrypt username: %v", err)
		}

		encryptedPassword, err := s.encryptor.Encrypt([]byte(*credentials.Password))
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to encrypt password: %v", err)
		}

		err = q.UpsertMinerCredentials(ctx, sqlc.UpsertMinerCredentialsParams{
			DeviceID:    deviceID,
			UsernameEnc: encryptedUsername,
			PasswordEnc: encryptedPassword,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to save antminer credentials: %v", err)
		}

		// Create pairing record
		_, err = q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
			DeviceID:      deviceID,
			PairingStatus: pairing.StatusPaired,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to create pairing for device device_identifier=%s: %v", device.DeviceIdentifier, err)
		}

		return nil
	})
}

func authAndGetSystemInfo(ctx context.Context, device *minerdiscovery.DiscoveredDevice, s *Service, credentials *pb.Credentials) (*web.SystemInfo, error) {
	connInfo, err := networking.NewConnectionInfo(device.IpAddress, web.DefaultPort, networking.ProtocolHTTP)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create connection info: %v", err)
	}

	systemInfo, err := s.webClient.GetSystemInfo(ctx, &web.AntminerConnectionInfo{
		ConnectionInfo: *connInfo,
		Username:       credentials.Username,
		Password:       *secrets.NewText(*credentials.Password),
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get system info: %v", err)
	}

	return systemInfo, nil
}
