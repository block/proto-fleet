package plugins

import (
	"context"
	"fmt"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/plugins/mappers"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

// PluginDriverGetter defines the interface for getting SDK drivers for miner types
type PluginDriverGetter interface {
	GetDriverForMinerType(minerType models.Type) (sdk.Driver, error)
}

// PluginMinerConfig contains all parameters needed to create a plugin-based miner
type PluginMinerConfig struct {
	// Device information
	DeviceIdentifier   string
	MinerType          models.Type
	DeviceIPAddress    string
	DevicePort         string
	DeviceScheme       string
	DeviceSerialNumber string

	// Credentials (encrypted)
	DeviceUsername string // May be empty for Proto
	DevicePassword string // May be empty for Proto
	OrgID          int64  // Organization ID for retrieving Proto private key

	// Services and dependencies
	EncryptService   *encrypt.Service
	TokenService     *token.Service // Required for Proto miners to generate JWT tokens
	GetOrgPrivateKey func(ctx context.Context, orgID int64) ([]byte, error)
	DriverGetter     PluginDriverGetter
}

// NewPluginMinerWithCredentials creates a PluginMiner from the provided configuration.
// This factory encapsulates all SDK-specific logic for creating plugin-based miners,
// including credential decryption and SDK device initialization.
func NewPluginMinerWithCredentials(
	ctx context.Context,
	config PluginMinerConfig,
) (interfaces.Miner, error) {
	// Parse and validate port using SDK helper
	portInt32, err := sdk.ParsePort(config.DevicePort)
	if err != nil {
		return nil, err
	}

	// Parse URL scheme
	scheme, err := networking.ProtocolFromString(config.DeviceScheme)
	if err != nil {
		return nil, fmt.Errorf("failed to parse scheme: %w", err)
	}

	// Create connection info
	connectionInfo, err := networking.NewConnectionInfo(config.DeviceIPAddress, config.DevicePort, scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection info: %w", err)
	}

	// Get the plugin driver for this miner type
	driver, err := config.DriverGetter.GetDriverForMinerType(config.MinerType)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin driver: %w", err)
	}

	// Build SDK DeviceInfo from database info
	sdkDeviceInfo := sdk.DeviceInfo{
		Host:         config.DeviceIPAddress,
		Port:         portInt32,
		URLScheme:    config.DeviceScheme,
		SerialNumber: config.DeviceSerialNumber,
		Type:         mappers.FleetTypeToSDKType(config.MinerType),
	}

	// Build SDK SecretBundle from stored credentials
	var secretBundle sdk.SecretBundle

	// For Proto devices, generate JWT token for bearer authentication
	// Proto miners use Ed25519-signed JWT tokens for authentication after pairing
	if config.MinerType == models.TypeProto {
		// Step 1: Validate that TokenService is available (required for JWT generation)
		if config.TokenService == nil {
			return nil, fmt.Errorf("TokenService is required for Proto miners but was nil")
		}
		// Step 2: Validate that we have the device serial number (used as JWT subject)
		if config.DeviceSerialNumber == "" {
			return nil, fmt.Errorf("DeviceSerialNumber is required for Proto JWT generation")
		}

		// Step 3: Retrieve the organization's private key for signing the JWT
		// This key was generated during organization setup and pairs with the public key
		// that was provided to the miner during the pairing process
		privateKey, err := config.GetOrgPrivateKey(ctx, config.OrgID)
		if err != nil {
			return nil, fmt.Errorf("failed to get proto miner auth private key: %w", err)
		}

		// Step 4: Generate a JWT token signed with the org's private key
		// The miner will validate this JWT using the public key it received during pairing
		jwtToken, _, err := config.TokenService.GenerateMinerAuthJWT(config.DeviceSerialNumber, privateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to generate JWT for proto miner: %w", err)
		}

		// Step 5: Pass the JWT as BearerToken to the plugin
		// Note: This is a change from the previous challenge-response authentication approach.
		// Previously, we used TLSClientCert as a workaround due to SDK lacking PublicKeyAuth type.
		// Now, Proto miners use JWT bearer tokens, which the plugin expects in the BearerToken field.
		secretBundle.Kind = sdk.BearerToken{
			Token: jwtToken,
		}
	} else if config.DeviceUsername != "" && config.DevicePassword != "" {
		// For other devices with username/password credentials
		decryptedUsername, err := config.EncryptService.Decrypt(config.DeviceUsername)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt username: %w", err)
		}
		decryptedPassword, err := config.EncryptService.Decrypt(config.DevicePassword)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt password: %w", err)
		}

		secretBundle.Kind = sdk.UsernamePassword{
			Username: string(decryptedUsername),
			Password: string(decryptedPassword),
		}
	}

	// Create the SDK device using the plugin driver
	result, err := driver.NewDevice(ctx, config.DeviceIdentifier, sdkDeviceInfo, secretBundle)
	if err != nil {
		// Check if this is a network error and wrap it as ConnectionError
		if isNetworkError(err) {
			return nil, fleeterror.NewConnectionError(config.DeviceIdentifier, fmt.Errorf("failed to create SDK device: %w", err))
		}
		return nil, fleeterror.NewInternalErrorf("failed to create SDK device: %v", err)
	}

	// Wrap the SDK device in PluginMiner
	return NewPluginMiner(
		config.OrgID,
		models.DeviceIdentifier(config.DeviceIdentifier),
		config.MinerType,
		config.DeviceSerialNumber,
		*connectionInfo,
		result.Device,
		sdkDeviceInfo,
	), nil
}
