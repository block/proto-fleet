package proto

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"golang.org/x/crypto/bcrypt"
)

var _ pairing.Pairer = &Service{}

const DefaultBcryptCost = 14

type Service struct {
	conn *sql.DB
	cfg  pairing.Config
}

func NewService(
	conn *sql.DB,
	cfg pairing.Config,
) *Service {
	return &Service{
		conn: conn,
		cfg:  cfg,
	}
}

func (s *Service) GetMinerType() models.Type {
	return models.TypeProto
}

func (s *Service) PairDevice(ctx context.Context, device *minerdiscovery.DiscoveredDevice, _ *pb.Credentials) error {
	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		deviceID, err := pairing.SaveDiscoveredDevice(ctx, q, device)
		if err != nil {
			return err
		}

		dbDevice, err := q.GetDeviceByID(ctx, sqlc.GetDeviceByIDParams{ID: deviceID, OrgID: device.OrgID})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to fetch device: id=%d %v", deviceID, err)
		}

		// Generate pairing token
		pairingToken, err := s.generatePairingToken(&dbDevice)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed generate pairing token for device device_identifier=%s: %v", device.DeviceIdentifier, err)
		}

		// Create pairing record
		_, err = q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
			DeviceID:      deviceID,
			PairingToken:  sql.NullString{Valid: true, String: pairingToken},
			PairingStatus: pairing.StatusPaired,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to create pairing for device device_identifier=%s: %v", device.DeviceIdentifier, err)
		}
		return nil
	})
}

func (s *Service) generatePairingToken(device *sqlc.Device) (string, error) {
	deviceKey := device.SerialNumber.String
	bytes, err := bcrypt.GenerateFromPassword(fmt.Appendf(nil, "%s:%s", s.cfg.SecretKey, deviceKey), DefaultBcryptCost)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("bcrypt failure: %v", err)
	}

	return string(bytes), nil
}
