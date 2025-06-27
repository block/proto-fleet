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
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
)

var _ pairing.Pairer = &Service{}

type Service struct {
	conn      *sql.DB
	encryptor *encrypt.Service
}

func NewService(
	conn *sql.DB,
	encryptor *encrypt.Service,
) *Service {
	return &Service{
		conn:      conn,
		encryptor: encryptor,
	}
}

func (s *Service) GetMinerType() models.Type {
	return models.TypeAntminer
}

func (s *Service) PairDevice(ctx context.Context, device *minerdiscovery.DiscoveredDevice, credentials *pb.Credentials) error {
	if credentials == nil || strings.TrimSpace(credentials.Username) == "" || credentials.Password == nil || strings.TrimSpace(*credentials.Password) == "" {
		return fleeterror.NewInvalidArgumentErrorf("credentials are required for Antminer pairing")
	}

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

		err = q.CreateMinerCredentials(ctx, sqlc.CreateMinerCredentialsParams{
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
