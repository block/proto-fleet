package proto

import (
	"context"

	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
)

var _ pairing.Pairer = &Service{}

type Service struct {
	transactor     stores.Transactor
	deviceStore    stores.DeviceStore
	userStore      stores.UserStore
	minerService   *miner.MinerService
	tokenService   *token.Service
	encryptService *encrypt.Service
}

func NewService(
	transactor stores.Transactor,
	deviceStore stores.DeviceStore,
	userStore stores.UserStore,
	minerService *miner.MinerService,
	tokenService *token.Service,
	encryptService *encrypt.Service,
) *Service {
	return &Service{
		transactor:     transactor,
		deviceStore:    deviceStore,
		userStore:      userStore,
		minerService:   minerService,
		tokenService:   tokenService,
		encryptService: encryptService,
	}
}

func (s *Service) GetMinerType() models.Type {
	return models.TypeProto
}

func (s *Service) GetMinerPublicKey(ctx context.Context, orgID int64) (string, error) {
	encryptedKey, err := s.userStore.GetOrganizationPrivateKey(ctx, orgID)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error querying miner auth key: %v", err)
	}

	privateKey, err := s.encryptService.Decrypt(encryptedKey)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error decrypting miner auth key: %v", err)
	}

	key, err := s.tokenService.ExtractPublicKeyFromPrivateKey(privateKey)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error extracting public key from private key: %v", err)
	}

	return key, nil
}

func (s *Service) handlePairViaStore(ctx context.Context, device *discoverymodels.DiscoveredDevice) error {
	return s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.deviceStore.UpsertDevice(txCtx, &device.Device, device.OrgID, models.TypeProto.String()); err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device: %v", err)
		}

		if err := s.deviceStore.UpsertDevicePairing(txCtx, &device.Device, device.OrgID, pairing.StatusPaired); err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device pairing: %v", err)
		}

		return nil
	})
}

func (s *Service) PairDevice(ctx context.Context, device *discoverymodels.DiscoveredDevice, _ *pb.Credentials) error {
	protocol, err := networking.ProtocolFromString(device.Device.UrlScheme)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to parse protocol: %v", err)
	}

	pairingInfo, err := client.GetPairingInfo(ctx, device.IpAddress, device.Port, protocol)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting pairing info: %v", err)
	}

	if len(pairingInfo.Msg.CbSn) == 0 {
		return fleeterror.NewInternalErrorf("miner at '%s' does not have a serial number which is required for pairing", device.IpAddress)
	}

	if len(pairingInfo.Msg.Mac) == 0 {
		return fleeterror.NewInternalErrorf("miner at '%s' does not have a mac address which is required for pairing", device.IpAddress)
	}

	device.SerialNumber = pairingInfo.Msg.CbSn
	device.MacAddress = pairingInfo.Msg.Mac

	err = s.handlePairViaStore(ctx, device)
	if err != nil {
		return fleeterror.NewInternalErrorf("error pairing in the DB: %v", err)
	}

	deviceIdentifier := models.DeviceIdentifier(device.DeviceIdentifier)

	minerImpl, err := s.minerService.GetMinerFromDeviceIdentifier(ctx, deviceIdentifier)
	if err != nil {
		return err
	}

	protoMinerImpl, ok := minerImpl.(*proto.ProtoMiner)
	if !ok {
		return fleeterror.NewInternalErrorf("expected ProtoMiner but got %T", minerImpl)
	}

	publicKey, err := s.GetMinerPublicKey(ctx, device.OrgID)
	if err != nil {
		return err
	}

	err = protoMinerImpl.SetAuthKey(ctx, publicKey)
	if err != nil {
		return fleeterror.NewInternalErrorf("error setting auth key: %v", err)
	}

	return nil
}
