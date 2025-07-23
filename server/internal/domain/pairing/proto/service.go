package proto

import (
	"context"
	"fmt"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"golang.org/x/crypto/bcrypt"
)

var _ pairing.Pairer = &Service{}

// pairingBcryptCost is lower than the default of 10 because we want it to be fast.
// We don't need to be too secure here because we're only using it to pair devices and not to authenticate users.
const pairingBcryptCost = 6

type Service struct {
	transactor  stores.Transactor
	deviceStore stores.DeviceStore
	cfg         pairing.Config
}

func NewService(
	transactor stores.Transactor,
	deviceStore stores.DeviceStore,
	cfg pairing.Config,
) *Service {
	return &Service{
		transactor:  transactor,
		deviceStore: deviceStore,
		cfg:         cfg,
	}
}

func (s *Service) GetMinerType() models.Type {
	return models.TypeProto
}

func (s *Service) PairDevice(ctx context.Context, device *minerdiscovery.DiscoveredDevice, _ *pb.Credentials) error {

	pairingToken, err := s.generatePairingToken(&device.Device)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to generate pairing token: %v", err)
	}

	return s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		err := s.deviceStore.UpsertDevice(ctx, &device.Device, device.OrgID, models.TypeProto.String())
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device: %v", err)
		}
		err = s.deviceStore.UpsertDeviceIPAssignment(ctx, &device.Device, device.OrgID)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device IP assignment: %v", err)
		}

		err = s.deviceStore.UpsertDevicePairing(ctx, &device.Device, device.OrgID, pairingToken, pairing.StatusPaired)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device pairing: %v", err)
		}
		return nil
	})
}

func (s *Service) generatePairingToken(device *pb.Device) (string, error) {
	deviceKey := device.SerialNumber
	bytes, err := bcrypt.GenerateFromPassword(fmt.Appendf(nil, "%s:%s", s.cfg.SecretKey, deviceKey), pairingBcryptCost)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("bcrypt failure: %v", err)
	}

	return string(bytes), nil
}
