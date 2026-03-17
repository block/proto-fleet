package plugins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	discoverymodels "github.com/proto-at-block/proto-fleet/server/internal/domain/minerdiscovery/models"

	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/miner/models"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/pairing"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/token"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/secrets"
	sdk "github.com/proto-at-block/proto-fleet/server/sdk/v1"
)

var _ pairing.Pairer = &Pairer{}

// Pairer implements the pairing.Pairer interface using plugins
type Pairer struct {
	manager               *Manager
	transactor            interfaces.Transactor
	discoveredDeviceStore interfaces.DiscoveredDeviceStore
	deviceStore           interfaces.DeviceStore
	userStore             interfaces.UserStore
	tokenService          *token.Service
	encryptService        *encrypt.Service
}

// NewPairer creates a new plugin-based pairer
func NewPairer(manager *Manager, transactor interfaces.Transactor, discoveredDeviceStore interfaces.DiscoveredDeviceStore, deviceStore interfaces.DeviceStore, userStore interfaces.UserStore, tokenService *token.Service, encryptService *encrypt.Service) *Pairer {
	return &Pairer{
		manager:               manager,
		transactor:            transactor,
		discoveredDeviceStore: discoveredDeviceStore,
		deviceStore:           deviceStore,
		userStore:             userStore,
		tokenService:          tokenService,
		encryptService:        encryptService,
	}
}

// getPluginForDevice returns the plugin that should handle this device.
func (p *Pairer) getPluginForDevice(device *discoverymodels.DiscoveredDevice) (*LoadedPlugin, error) {
	if device.DriverName == "" {
		return nil, fleeterror.NewInternalErrorf("device %s has no driver_name — run backfill or re-discover", device.DeviceIdentifier)
	}
	return p.manager.GetPluginByDriverNameWithCapability(device.DriverName, sdk.CapabilityPairing)
}

func (p *Pairer) GetDeviceInfo(ctx context.Context, device *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) (*pb.Device, error) {
	plugin, err := p.getPluginForDevice(device)
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

	updatedDevice := convertSDKDeviceInfoToFleetDevice(newDeviceInfo, device.IpAddress, device.Port, plugin.Identifier.DriverName)

	return updatedDevice, nil
}

// PairDevice handles the entire pairing process using the plugin
// TODO: Refactor Pairing to use something other than pb.Credentials, this limits us to only username/password without bespoke miner integrations.
func (p *Pairer) PairDevice(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error {
	plugin, err := p.getPluginForDevice(discoveredDevice)
	if err != nil {
		return err
	}

	// If no credentials provided, try default credentials if plugin provides them
	if credentials == nil {
		if provider, ok := plugin.Driver.(sdk.DefaultCredentialsProvider); ok {
			defaultCreds := provider.GetDefaultCredentials(ctx)
			if len(defaultCreds) > 0 {
				return p.pairWithDefaultCredentials(ctx, plugin, discoveredDevice, defaultCreds)
			}
		}
		// Devices that advertise CapabilityAsymmetricAuth (e.g. Proto devices) use public key
		// based authentication managed by the plugin/SDK instead of username/password.
		if !plugin.Caps[sdk.CapabilityAsymmetricAuth] {
			return fleeterror.NewInvalidArgumentErrorf("invalid_argument: credentials are required for pairing")
		}
	}

	return p.executePairing(ctx, plugin, discoveredDevice, credentials)
}

// pairWithDefaultCredentials attempts pairing with plugin-provided default credentials.
// It tries each credential combination in order, returning on first success.
// If all attempts fail, it returns a "credentials required" error to trigger AUTHENTICATION_NEEDED.
func (p *Pairer) pairWithDefaultCredentials(ctx context.Context, plugin *LoadedPlugin, discoveredDevice *discoverymodels.DiscoveredDevice, defaultCreds []sdk.UsernamePassword) error {
	for _, cred := range defaultCreds {
		password := cred.Password
		credentials := &pb.Credentials{
			Username: cred.Username,
			Password: &password,
		}

		err := p.callPluginPairDevice(ctx, plugin, discoveredDevice, credentials)
		if err != nil {
			if isAuthenticationFailure(err) {
				continue
			}
			return fleeterror.NewInternalErrorf("plugin pairing failed: %v", err)
		}

		if err := p.handlePairViaStore(ctx, discoveredDevice, credentials); err != nil {
			return fleeterror.NewInternalErrorf("error saving device to database: %v", err)
		}

		// Fetch additional device info (firmware version, etc.) using the successful credentials
		// This ensures auto-auth has the same effect as bulk authentication
		if deviceInfo, err := p.GetDeviceInfo(ctx, discoveredDevice, credentials); err != nil {
			slog.Warn("Failed to get device info after auto-auth pairing",
				"device_identifier", discoveredDevice.DeviceIdentifier,
				"error", err)
		} else if deviceInfo.FirmwareVersion != "" {
			discoveredDevice.FirmwareVersion = deviceInfo.FirmwareVersion
		}

		slog.Info("Device paired successfully with default credentials",
			"device_identifier", discoveredDevice.DeviceIdentifier)
		return nil
	}

	// All credential attempts failed - signal that user credentials are needed
	slog.Debug("Default credentials not accepted, manual authentication required",
		"device_identifier", discoveredDevice.DeviceIdentifier)
	return fleeterror.NewInvalidArgumentErrorf("invalid_argument: credentials are required for pairing")
}

// callPluginPairDevice calls the plugin's PairDevice and updates discoveredDevice with the response.
// Returns raw errors (not wrapped) so callers can inspect error types before wrapping.
func (p *Pairer) callPluginPairDevice(ctx context.Context, plugin *LoadedPlugin, discoveredDevice *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error {
	deviceInfo := convertFleetDeviceToSDKDeviceInfo(&discoveredDevice.Device)

	secretBundle, err := p.createSecretBundle(ctx, discoveredDevice.OrgID, plugin.Caps, credentials)
	if err != nil {
		return fmt.Errorf("failed to create secret bundle: %w", err)
	}

	updatedDeviceInfo, err := plugin.Driver.PairDevice(ctx, deviceInfo, secretBundle)
	if err != nil {
		return err
	}

	discoveredDevice.SerialNumber = updatedDeviceInfo.SerialNumber
	discoveredDevice.MacAddress = updatedDeviceInfo.MacAddress
	discoveredDevice.Model = updatedDeviceInfo.Model
	discoveredDevice.Manufacturer = updatedDeviceInfo.Manufacturer
	discoveredDevice.FirmwareVersion = updatedDeviceInfo.FirmwareVersion

	return nil
}

// executePairing performs the actual pairing operation with given credentials.
func (p *Pairer) executePairing(ctx context.Context, plugin *LoadedPlugin, discoveredDevice *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error {
	if err := p.callPluginPairDevice(ctx, plugin, discoveredDevice, credentials); err != nil {
		return fleeterror.NewInternalErrorf("plugin pairing failed: %v", err)
	}

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

		// Set initial device status to ACTIVE since the miner was reachable during pairing
		// This ensures the dashboard shows correct status immediately after pairing
		if err := p.deviceStore.UpsertDeviceStatus(ctx, models.DeviceIdentifier(discoveredDevice.DeviceIdentifier), models.MinerStatusActive, ""); err != nil {
			return fleeterror.NewInternalErrorf("failed to set initial device status: %v", err)
		}

		return nil
	})
}

// saveCredentials stores device-specific credentials based on the SecretBundle type.
// - UsernamePassword: Stores encrypted username/password (e.g., Antminer devices)
// - APIKey: No storage (org-level keys derived on-demand, device-specific keys not yet supported)
// Note: pb.Credentials currently only supports username/password. Device-specific API keys
// will require extending pb.Credentials.
func (p *Pairer) saveCredentials(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error {
	plugin, pluginErr := p.getPluginForDevice(discoveredDevice)
	if pluginErr != nil {
		return pluginErr
	}
	bundle, err := p.createSecretBundle(ctx, discoveredDevice.OrgID, plugin.Caps, credentials)
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

// convertFleetDeviceToSDKDeviceInfo converts a Fleet pb.Device to SDK DeviceInfo format
func convertFleetDeviceToSDKDeviceInfo(device *pb.Device) sdk.DeviceInfo {
	port, err := sdk.ParsePort(device.Port)
	if err != nil {
		slog.Warn("Invalid port number, using 0", "port", device.Port, "error", err)
		port = 0
	}

	return sdk.DeviceInfo{
		Host:            device.IpAddress,
		Port:            port,
		URLScheme:       device.UrlScheme,
		SerialNumber:    device.SerialNumber,
		Model:           device.Model,
		Manufacturer:    device.Manufacturer,
		MacAddress:      device.MacAddress,
		FirmwareVersion: device.FirmwareVersion,
	}
}

func (p *Pairer) createSecretBundle(ctx context.Context, orgID int64, caps sdk.Capabilities, credentials *pb.Credentials) (sdk.SecretBundle, error) {
	bundle := sdk.SecretBundle{
		Version: "v1",
	}

	if caps[sdk.CapabilityAsymmetricAuth] {
		fleetPublicKey, err := p.GetMinerPublicKey(ctx, orgID)
		if err != nil {
			return sdk.SecretBundle{}, fmt.Errorf("failed to get fleet public key: %w", err)
		}
		bundle.Kind = sdk.APIKey{
			Key: fleetPublicKey,
		}
	} else {
		if credentials == nil {
			return sdk.SecretBundle{}, fmt.Errorf("credentials required for secret bundle")
		}
		if credentials.Password == nil {
			return sdk.SecretBundle{}, fmt.Errorf("password is required for secret bundle")
		}
		bundle.Kind = sdk.UsernamePassword{
			Username: credentials.Username,
			Password: *credentials.Password,
		}
	}

	return bundle, nil
}

// getSecretBundleForDeviceInfo builds the SecretBundle used when describing a device via plugins.
// Devices with asymmetric auth use JWT bearer tokens, others use credential-based bundles.
func (p *Pairer) getSecretBundleForDeviceInfo(ctx context.Context, device *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) (sdk.SecretBundle, error) {
	plugin, err := p.getPluginForDevice(device)
	if err != nil {
		return sdk.SecretBundle{}, err
	}

	if !plugin.Caps[sdk.CapabilityAsymmetricAuth] {
		return p.createSecretBundle(ctx, device.OrgID, plugin.Caps, credentials)
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

// isAuthenticationFailure checks if an error indicates authentication failed.
// This is distinct from "credentials required" - authentication failed means
// credentials were provided but were rejected by the device.
func isAuthenticationFailure(err error) bool {
	if err == nil {
		return false
	}

	// Check for gRPC Unauthenticated status code (set by sdkErrorToGRPCStatus in plugin RPC layer)
	if status.Code(err) == codes.Unauthenticated {
		return true
	}

	// Check for fleet authentication error (Connect protocol)
	if fleeterror.IsAuthenticationError(err) {
		return true
	}

	// Check for SDK authentication error (in-process plugins, no RPC boundary)
	var sdkErr sdk.SDKError
	return errors.As(err, &sdkErr) && sdkErr.Code == sdk.ErrCodeAuthenticationFailed
}
