package plugins

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

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
	plugin, exists := p.manager.GetPluginForMinerType(p.minerType)
	if !exists {
		return nil, fleeterror.NewInternalErrorf("no plugin available for miner type %s", p.minerType)
	}

	if !plugin.Caps[sdk.CapabilityPairing] {
		return nil, fleeterror.NewInternalErrorf("plugin %s does not support pairing", plugin.Name)
	}

	deviceInfo := convertFleetDeviceToSDKDeviceInfo(&device.Device)

	secretBundle, err := p.createSecretBundle(ctx, device.OrgID, credentials)
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
	plugin, exists := p.manager.GetPluginForMinerType(p.minerType)
	if !exists {
		return fleeterror.NewInternalErrorf("no plugin available for miner type %s", p.minerType)
	}

	if !plugin.Caps[sdk.CapabilityPairing] {
		return fleeterror.NewInternalErrorf("plugin %s does not support pairing", plugin.Name)
	}

	slog.Debug("Using plugin for device pairing",
		"plugin", plugin.Name,
		"type", p.minerType,
		"device", discoveredDevice.DeviceIdentifier)

	deviceInfo := convertFleetDeviceToSDKDeviceInfo(&discoveredDevice.Device)

	secretBundle, err := p.createSecretBundle(ctx, discoveredDevice.OrgID, credentials)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to create secret bundle: %v", err)
	}

	message, err := plugin.Driver.PairDevice(ctx, deviceInfo, secretBundle)
	if err != nil {
		return fleeterror.NewInternalErrorf("plugin pairing failed: %v", err)
	}

	slog.Debug("Plugin pairing completed",
		"plugin", plugin.Name,
		"device", discoveredDevice.DeviceIdentifier,
		"message", message)

	// Save device to database
	if err := p.handlePairViaStore(ctx, discoveredDevice, credentials); err != nil {
		return fleeterror.NewInternalErrorf("error saving device to database: %v", err)
	}

	return nil
}

// handlePairViaStore saves the device to the database
func (p *Pairer) handlePairViaStore(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error {
	return p.transactor.RunInTx(ctx, func(ctx context.Context) error {
		if err := p.deviceStore.InsertDevice(ctx, &discoveredDevice.Device, discoveredDevice.OrgID, discoveredDevice.DeviceIdentifier); err != nil {
			return fleeterror.NewInternalErrorf("failed to insert device: %v", err)
		}

		// Save encrypted credentials based on SecretBundle type
		if err := p.saveCredentials(ctx, discoveredDevice, credentials); err != nil {
			return err
		}

		// Mark device as paired
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
	// Recreate the secret bundle to determine credential type
	bundle, err := p.createSecretBundle(ctx, discoveredDevice.OrgID, credentials)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to create secret bundle: %v", err)
	}

	switch kind := bundle.Kind.(type) {
	case sdk.UsernamePassword:
		// Store username/password authentication (e.g., Antminer)
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
		// Org-level API keys are derived on-demand from org private key, no storage needed (e.g., Proto miners)
		// Note: Device-specific API keys will require pb.Credentials extension (see TODO DASH-818)
		slog.Debug("Using org-level API key, no credential storage needed",
			"device", discoveredDevice.DeviceIdentifier)

	default:
		// Unsupported credential type - silent ignore for forward compatibility
		slog.Debug("No credentials stored for device",
			"device", discoveredDevice.DeviceIdentifier,
			"type", fmt.Sprintf("%T", bundle.Kind))
	}

	return nil
}

// GetMinerPublicKey retrieves the public key for the organization (same logic as proto pairing service)
func (p *Pairer) GetMinerPublicKey(ctx context.Context, orgID int64) (string, error) {
	encryptedKey, err := p.userStore.GetOrganizationPrivateKey(ctx, orgID)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error querying miner auth key: %v", err)
	}

	privateKey, err := p.encryptService.Decrypt(encryptedKey)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error decrypting miner auth key: %v", err)
	}

	key, err := p.tokenService.ExtractPublicKeyFromPrivateKey(privateKey)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error extracting public key from private key: %v", err)
	}

	return key, nil
}

// GetMinerType returns the miner type this pairer handles
func (p *Pairer) GetMinerType() models.Type {
	return p.minerType
}

// convertFleetDeviceToSDKDeviceInfo converts a Fleet pb.Device to SDK DeviceInfo format
func convertFleetDeviceToSDKDeviceInfo(device *pb.Device) sdk.DeviceInfo {
	portInt64, err := strconv.ParseInt(device.Port, 10, 32)
	if err != nil {
		slog.Warn("Invalid port number, using 0", "port", device.Port, "error", err)
		portInt64 = 0
	}
	if portInt64 < 0 || portInt64 > 65535 {
		slog.Warn("Port number out of valid range, using 0", "port", portInt64)
		portInt64 = 0
	}
	portInt32 := int32(portInt64)

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
		Port:         portInt32,
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

	if credentials != nil {
		// Use username/password authentication (e.g., for Antminer)
		bundle.Kind = sdk.UsernamePassword{
			Username: credentials.Username,
			Password: *credentials.Password,
		}
	} else {
		// No username or password - use fleet's public key
		fleetPublicKey, err := p.GetMinerPublicKey(ctx, orgID)
		if err != nil {
			return sdk.SecretBundle{}, fmt.Errorf("failed to get fleet public key: %w", err)
		}
		bundle.Kind = sdk.APIKey{
			Key: fleetPublicKey,
		}
	}

	return bundle, nil
}
