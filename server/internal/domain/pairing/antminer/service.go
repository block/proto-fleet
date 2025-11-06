package antminer

import (
	"context"
	"strings"

	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

var _ pairing.Pairer = &Service{}

type Service struct {
	transactor            stores.Transactor
	discoveredDeviceStore stores.DiscoveredDeviceStore
	deviceStore           stores.DeviceStore
	encryptor             *encrypt.Service
	webClient             web.WebAPIClient
}

func NewService(
	transactor stores.Transactor,
	discoveredDeviceStore stores.DiscoveredDeviceStore,
	deviceStore stores.DeviceStore,
	encryptor *encrypt.Service,
	webClient web.WebAPIClient,
) *Service {
	return &Service{
		transactor:            transactor,
		discoveredDeviceStore: discoveredDeviceStore,
		deviceStore:           deviceStore,
		encryptor:             encryptor,
		webClient:             webClient,
	}
}

func (s *Service) GetMinerType() models.Type {
	return models.TypeAntminer
}

func (s *Service) PairDevice(ctx context.Context, device *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error {
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

	encryptedUsername, err := s.encryptor.Encrypt([]byte(credentials.Username))
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to encrypt username: %v", err)
	}

	encryptedPassword, err := s.encryptor.Encrypt([]byte(*credentials.Password))
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to encrypt password: %v", err)
	}

	return s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		if err := s.deviceStore.InsertDevice(ctx, &device.Device, device.OrgID, device.DeviceIdentifier); err != nil {
			return fleeterror.NewInternalErrorf("failed to insert device: %v", err)
		}

		err = s.deviceStore.UpsertMinerCredentials(ctx, &device.Device, device.OrgID, encryptedUsername, secrets.NewText(encryptedPassword))
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert miner credentials: %v", err)
		}
		err = s.deviceStore.UpsertDevicePairing(ctx, &device.Device, device.OrgID, pairing.StatusPaired)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device pairing: %v", err)
		}
		return nil
	})
}

func (s *Service) GetDeviceInfo(ctx context.Context, device *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) (*pb.Device, error) {
	if credentials == nil || strings.TrimSpace(credentials.Username) == "" || credentials.Password == nil || strings.TrimSpace(*credentials.Password) == "" {
		return nil, fleeterror.NewInvalidArgumentErrorf("credentials are required to get device info")
	}

	systemInfo, err := authAndGetSystemInfo(ctx, device, s, credentials)
	if err != nil {
		return nil, err
	}

	device.SerialNumber = systemInfo.SerialNumber
	device.MacAddress = systemInfo.MacAddr

	return &device.Device, nil
}

func authAndGetSystemInfo(ctx context.Context, device *discoverymodels.DiscoveredDevice, s *Service, credentials *pb.Credentials) (*web.SystemInfo, error) {
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
