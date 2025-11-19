package plugins

import (
	"context"
	"fmt"
	"log/slog"

	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

var _ pairing.Pairer = &Pairer{}

// Pairer implements the pairing.Pairer interface using plugins
type Pairer struct {
	manager               *Manager
	minerType             models.Type
	transactor            interfaces.Transactor
	discoveredDeviceStore interfaces.DiscoveredDeviceStore
	deviceStore           interfaces.DeviceStore
	userStore             interfaces.UserStore
	tokenService          *token.Service
	encryptService        *encrypt.Service
}

// NewPairer creates a new plugin-based pairer for a specific miner type
func NewPairer(manager *Manager, minerType models.Type, transactor interfaces.Transactor, discoveredDeviceStore interfaces.DiscoveredDeviceStore, deviceStore interfaces.DeviceStore, userStore interfaces.UserStore, tokenService *token.Service, encryptService *encrypt.Service) *Pairer {
	return &Pairer{
		manager:               manager,
		minerType:             minerType,
		transactor:            transactor,
		discoveredDeviceStore: discoveredDeviceStore,
		deviceStore:           deviceStore,
		userStore:             userStore,
		tokenService:          tokenService,
		encryptService:        encryptService,
	}
}

func (p *Pairer) GetDeviceInfo(ctx context.Context, device *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) (*pb.Device, error) {
	plugin, err := p.manager.GetPluginWithCapability(p.minerType, sdk.CapabilityPairing)
	if err != nil {
		return nil, err
	}

	deviceInfo := convertFleetDeviceToSDKDeviceInfo(&device.Device)

	secretBundle, err := p.getSecretBundleForDeviceInfo(ctx, device, credentials)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create secret bundle: %v", err)
	}

	result, err := plugin.Driver.NewDevice(ctx, device.DeviceIdentifier, deviceInfo, secretBundle)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create device: %v", err)
	}

	newDeviceInfo, _, err := result.Device.DescribeDevice(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to describe device: %v", err)
	}

	updatedDevice := convertSDKDeviceInfoToFleetDevice(newDeviceInfo, device.IpAddress, device.Port)

	return updatedDevice, nil
}

// PairDevice handles the entire pairing process using the plugin
// TODO(DASH-818): Refactor Pairing to use something other than pb.Credentials, this limits us to only username/password with out bespoke miner integrations.
func (p *Pairer) PairDevice(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error {
	plugin, err := p.manager.GetPluginWithCapability(p.minerType, sdk.CapabilityPairing)
	if err != nil {
		return err
	}

	deviceInfo := convertFleetDeviceToSDKDeviceInfo(&discoveredDevice.Device)

	secretBundle, err := p.createSecretBundle(ctx, discoveredDevice.OrgID, credentials)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to create secret bundle: %v", err)
	}

	updatedDeviceInfo, err := plugin.Driver.PairDevice(ctx, deviceInfo, secretBundle)
	if err != nil {
		return fleeterror.NewInternalErrorf("plugin pairing failed: %v", err)
	}

	discoveredDevice.SerialNumber = updatedDeviceInfo.SerialNumber
	discoveredDevice.MacAddress = updatedDeviceInfo.MacAddress
	discoveredDevice.Model = updatedDeviceInfo.Model
	discoveredDevice.Manufacturer = updatedDeviceInfo.Manufacturer

	if err := p.handlePairViaStore(ctx, discoveredDevice, credentials); err != nil {
		return fleeterror.NewInternalErrorf("error saving device to database: %v", err)
	}

	return nil
}

// handlePairViaStore saves the device to the database
func (p *Pairer) handlePairViaStore(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error {
	return p.transactor.RunInTx(ctx, func(ctx context.Context) error {
		// Check if device already exists (e.g., from AUTHENTICATION_NEEDED status)
		existingDevice, err := p.deviceStore.GetDeviceByDeviceIdentifier(ctx, discoveredDevice.DeviceIdentifier, discoveredDevice.OrgID)
		if err != nil && !fleeterror.IsNotFoundError(err) {
			return fleeterror.NewInternalErrorf("failed to check if device exists: %v", err)
		}

		if existingDevice == nil {
			if err := p.deviceStore.InsertDevice(ctx, &discoveredDevice.Device, discoveredDevice.OrgID, discoveredDevice.DeviceIdentifier); err != nil {
				return fleeterror.NewInternalErrorf("failed to insert device: %v", err)
			}
		} else {
			if err := p.deviceStore.UpdateDeviceInfo(ctx, &discoveredDevice.Device, discoveredDevice.OrgID); err != nil {
				return fleeterror.NewInternalErrorf("failed to update device info: %v", err)
			}
		}

		if err := p.saveCredentials(ctx, discoveredDevice, credentials); err != nil {
			return err
		}

		if err := p.deviceStore.UpsertDevicePairing(ctx, &discoveredDevice.Device, discoveredDevice.OrgID, pairing.StatusPaired); err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device pairing: %v", err)
		}

		return nil
	})
}

// saveCredentials stores device-specific credentials based on the SecretBundle type.
// - UsernamePassword: Stores encrypted username/password (e.g., Antminer devices)
// - APIKey: No storage (org-level keys derived on-demand, device-specific keys not yet supported)
// Note: pb.Credentials currently only supports username/password. Device-specific API keys
// will require extending pb.Credentials (see TODO DASH-818).
func (p *Pairer) saveCredentials(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error {
	bundle, err := p.createSecretBundle(ctx, discoveredDevice.OrgID, credentials)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to create secret bundle: %v", err)
	}

	switch kind := bundle.Kind.(type) {
	case sdk.UsernamePassword:
		encryptedUsername, err := p.encryptService.Encrypt([]byte(kind.Username))
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to encrypt username: %v", err)
		}

		encryptedPassword, err := p.encryptService.Encrypt([]byte(kind.Password))
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to encrypt password: %v", err)
		}

		if err := p.deviceStore.UpsertMinerCredentials(ctx, &discoveredDevice.Device, discoveredDevice.OrgID, encryptedUsername, secrets.NewText(encryptedPassword)); err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert miner credentials: %v", err)
		}

	case sdk.APIKey:
		slog.Debug("Using org-level API key, no credential storage needed",
			"device", discoveredDevice.DeviceIdentifier)

	default:
		slog.Debug("No credentials stored for device",
			"device", discoveredDevice.DeviceIdentifier,
			"type", fmt.Sprintf("%T", bundle.Kind))
	}

	return nil
}

// GetMinerPublicKey retrieves the public key for the organization (same logic as proto pairing service)
func (p *Pairer) GetMinerPublicKey(ctx context.Context, orgID int64) (string, error) {
	privateKey, err := p.getOrgPrivateKey(ctx, orgID)
	if err != nil {
		return "", err
	}

	key, err := p.tokenService.ExtractPublicKeyFromPrivateKey(privateKey)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error extracting public key from private key: %v", err)
	}

	return key, nil
}

// getOrgPrivateKey fetches and decrypts the organization's miner auth private key
func (p *Pairer) getOrgPrivateKey(ctx context.Context, orgID int64) ([]byte, error) {
	encryptedKey, err := p.userStore.GetOrganizationPrivateKey(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error querying miner auth key: %v", err)
	}

	privateKey, err := p.encryptService.Decrypt(encryptedKey)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error decrypting miner auth key: %v", err)
	}

	return privateKey, nil
}

// GetMinerType returns the miner type this pairer handles
func (p *Pairer) GetMinerType() models.Type {
	return p.minerType
}

// convertFleetDeviceToSDKDeviceInfo converts a Fleet pb.Device to SDK DeviceInfo format
func convertFleetDeviceToSDKDeviceInfo(device *pb.Device) sdk.DeviceInfo {
	port, err := sdk.ParsePort(device.Port)
	if err != nil {
		slog.Warn("Invalid port number, using 0", "port", device.Port, "error", err)
		port = 0
	}

	var deviceType sdk.DeviceType
	switch device.Type {
	case "asic":
		deviceType = sdk.DeviceTypeASIC
	case "gpu":
		deviceType = sdk.DeviceTypeGPU
	case "fpga":
		deviceType = sdk.DeviceTypeFPGA
	default:
		deviceType = sdk.DeviceTypeUnspecified
	}

	return sdk.DeviceInfo{
		Host:         device.IpAddress,
		Port:         port,
		URLScheme:    device.UrlScheme,
		SerialNumber: device.SerialNumber,
		Model:        device.Model,
		Manufacturer: device.Manufacturer,
		Type:         deviceType,
		MacAddress:   device.MacAddress,
	}
}

// createSecretBundle creates an SDK SecretBundle for device pairing.
// If credentials are provided with a username, it creates a username/password bundle.
// Otherwise, it fetches the organization's public key from the database and creates an API key bundle.
func (p *Pairer) createSecretBundle(ctx context.Context, orgID int64, credentials *pb.Credentials) (sdk.SecretBundle, error) {
	bundle := sdk.SecretBundle{
		Version: "v1",
	}

	// TODO(ASH-899): Make adaptive based on device info not hardcoded miner type
	if p.minerType == models.TypeProto {
		fleetPublicKey, err := p.GetMinerPublicKey(ctx, orgID)
		if err != nil {
			return sdk.SecretBundle{}, fmt.Errorf("failed to get fleet public key: %w", err)
		}
		bundle.Kind = sdk.APIKey{
			Key: fleetPublicKey,
		}
	} else {
		bundle.Kind = sdk.UsernamePassword{
			Username: credentials.Username,
			Password: *credentials.Password,
		}
	}

	return bundle, nil
}

// getSecretBundleForDeviceInfo builds the SecretBundle used when describing a device via plugins.
// Proto miners expect JWT bearer tokens for runtime authentication, while other miners reuse
// the standard credential handling implemented in createSecretBundle.
func (p *Pairer) getSecretBundleForDeviceInfo(ctx context.Context, device *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) (sdk.SecretBundle, error) {
	if p.minerType != models.TypeProto {
		return p.createSecretBundle(ctx, device.OrgID, credentials)
	}

	return p.createProtoBearerSecretBundle(ctx, device)
}

// createProtoBearerSecretBundle issues a JWT bearer token for proto devices so that runtime
// plugin calls (e.g., NewDevice/DescribeDevice) authenticate correctly.
func (p *Pairer) createProtoBearerSecretBundle(ctx context.Context, device *discoverymodels.DiscoveredDevice) (sdk.SecretBundle, error) {
	if device.SerialNumber == "" {
		return sdk.SecretBundle{}, fleeterror.NewInternalError("proto devices require serial number for bearer authentication")
	}

	privateKey, err := p.getOrgPrivateKey(ctx, device.OrgID)
	if err != nil {
		return sdk.SecretBundle{}, err
	}

	jwtToken, _, err := p.tokenService.GenerateMinerAuthJWT(device.SerialNumber, privateKey)
	if err != nil {
		return sdk.SecretBundle{}, fleeterror.NewInternalErrorf("failed to generate proto bearer token: %v", err)
	}

	return sdk.SecretBundle{
		Version: "v1",
		Kind: sdk.BearerToken{
			Token: jwtToken,
		},
	}, nil
}
