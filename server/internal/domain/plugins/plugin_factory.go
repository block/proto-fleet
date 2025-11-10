package plugins

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/plugins/mappers"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

const (
	maxPort = 65535 // Maximum valid TCP/UDP port number
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
	// Parse and validate port
	portInt, err := strconv.Atoi(config.DevicePort)
	if err != nil {
		return nil, fmt.Errorf("failed to parse port %s: %w", config.DevicePort, err)
	}
	if portInt < 0 || portInt > maxPort {
		return nil, fmt.Errorf("port %d is out of valid range (0-%d)", portInt, maxPort)
	}
	// Validate port fits in int32 for SDK
	if portInt > math.MaxInt32 {
		return nil, fmt.Errorf("port %d exceeds int32 maximum", portInt)
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
	// Port is validated above to fit in int32 range (0-65535 < math.MaxInt32)
	sdkDeviceInfo := sdk.DeviceInfo{
		Host:         config.DeviceIPAddress,
		Port:         int32(portInt), //nolint:gosec // G109: Port validated above to fit in int32
		URLScheme:    config.DeviceScheme,
		SerialNumber: config.DeviceSerialNumber,
		Type:         mappers.FleetTypeToSDKType(config.MinerType),
	}

	// Build SDK SecretBundle from stored credentials
	var secretBundle sdk.SecretBundle

	// For Proto devices, use TLS client certificate authentication
	// Proto miners use SSH-style public key auth, which maps to SDK's TLSClientCert type
	if config.MinerType == models.TypeProto {
		privateKey, err := config.GetOrgPrivateKey(ctx, config.OrgID)
		if err != nil {
			return nil, fmt.Errorf("failed to get proto miner auth private key: %w", err)
		}
		// Note: Proto uses the private key for challenge-response auth during pairing.
		// For SDK plugins, we pass the key in TLSClientCert.KeyPEM since the SDK doesn't
		// have a dedicated PublicKeyAuth type. The proto plugin will handle the actual
		// challenge-response protocol.
		secretBundle.Kind = sdk.TLSClientCert{
			KeyPEM: privateKey,
			// ClientCertPEM and CACertPEM are not used for Proto's auth protocol
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
		return nil, fleeterror.NewInternalErrorf("failed to create SDK device: %v", err)
	}

	// Wrap the SDK device in PluginMiner
	return NewPluginMiner(
		models.DeviceIdentifier(config.DeviceIdentifier),
		config.MinerType,
		config.DeviceSerialNumber,
		*connectionInfo,
		result.Device,
		sdkDeviceInfo,
	), nil
}
