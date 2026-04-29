package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/token"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// PluginDriverGetter defines the interface for getting SDK drivers
type PluginDriverGetter interface {
	GetDriverByDriverName(driverName string) (sdk.Driver, error)
}

// PluginMinerConfig contains all parameters needed to create a plugin-based miner
type PluginMinerConfig struct {
	// Device information
	DeviceIdentifier   string
	DriverName         string           // Plugin routing key (from discovered_device.driver_name)
	Caps               sdk.Capabilities // Plugin capabilities (for auth strategy, log format)
	DeviceIPAddress    string
	DevicePort         string
	DeviceScheme       string
	DeviceSerialNumber string
	MacAddress         string

	// Credentials (encrypted)
	DeviceUsername string // May be empty for Proto
	DevicePassword string // May be empty for Proto
	OrgID          int64  // Organization ID for retrieving Proto private key

	// Services and dependencies
	EncryptService   *encrypt.Service
	TokenService     *token.Service // Required for Proto miners to generate JWT tokens
	FilesService     *files.Service
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

	// Get the plugin driver for this device's driver name
	driver, err := config.DriverGetter.GetDriverByDriverName(config.DriverName)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin driver: %w", err)
	}

	// Build SDK DeviceInfo from database fields
	sdkDeviceInfo := sdk.DeviceInfo{
		Host:         config.DeviceIPAddress,
		Port:         portInt32,
		URLScheme:    config.DeviceScheme,
		SerialNumber: config.DeviceSerialNumber,
		MacAddress:   config.MacAddress,
	}

	// Build SDK SecretBundle from stored credentials.
	// Asymmetric auth devices (e.g., Proto) use Ed25519-signed JWT bearer tokens,
	// where the org's private key signs a JWT that the miner validates using the
	// public key it received during pairing.
	var secretBundle sdk.SecretBundle

	if config.Caps[sdk.CapabilityAsymmetricAuth] {
		if config.TokenService == nil {
			return nil, fmt.Errorf("TokenService is required for asymmetric auth but was nil")
		}
		if config.DeviceSerialNumber == "" {
			return nil, fmt.Errorf("DeviceSerialNumber is required for JWT generation")
		}

		privateKey, err := config.GetOrgPrivateKey(ctx, config.OrgID)
		if err != nil {
			return nil, fmt.Errorf("failed to get org private key: %w", err)
		}

		jwtToken, _, err := config.TokenService.GenerateMinerAuthJWT(config.DeviceSerialNumber, privateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to generate JWT: %w", err)
		}

		secretBundle.Kind = sdk.BearerToken{
			Token: jwtToken,
		}
	} else if config.DeviceUsername != "" && config.DevicePassword != "" {
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

	if config.FilesService == nil {
		return nil, fmt.Errorf("FilesService is required but was nil")
	}

	// Create the SDK device via the plugin driver, which establishes the connection
	result, err := driver.NewDevice(ctx, config.DeviceIdentifier, sdkDeviceInfo, secretBundle)
	if err != nil {
		// Check if this is a network error and wrap it as ConnectionError
		if isNetworkError(err) {
			return nil, fleeterror.NewConnectionError(config.DeviceIdentifier, fmt.Errorf("failed to create SDK device: %w", err))
		}

		return nil, classifyNewDeviceError(err, config.DeviceIdentifier)
	}

	return NewPluginMiner(
		config.OrgID,
		models.DeviceIdentifier(config.DeviceIdentifier),
		config.DriverName,
		config.Caps,
		config.DeviceSerialNumber,
		*connectionInfo,
		result.Device,
		sdkDeviceInfo,
		config.FilesService,
	), nil
}

// classifyNewDeviceError maps an error returned by Driver.NewDevice into the
// appropriate fleeterror category. Out-of-process drivers surface SDK errors
// as gRPC statuses (see sdkErrorToGRPCStatus), so credentials drift can arrive
// here as codes.Unauthenticated rather than sdk.ErrCodeAuthenticationFailed —
// without explicit handling it would fall through to InternalError and
// AUTHENTICATION_NEEDED remediation would never fire.
func classifyNewDeviceError(err error, deviceID string) error {
	var sdkErr sdk.SDKError
	if errors.As(err, &sdkErr) {
		switch sdkErr.Code {
		case sdk.ErrCodeAuthenticationFailed:
			return fleeterror.NewUnauthenticatedErrorf("device %s authentication failed: %v", deviceID, err)
		case sdk.ErrCodeUnsupportedCapability, sdk.ErrCodeDeviceNotFound,
			sdk.ErrCodeInvalidConfig, sdk.ErrCodeDeviceUnavailable,
			sdk.ErrCodeDriverShutdown,
			sdk.ErrCodeCurtailCapabilityNotSupported, sdk.ErrCodeCurtailTransient:
			// All other SDK error codes fall through to InternalError below.
		}
	}
	if isDefaultPasswordActiveError(err) {
		return fleeterror.NewForbiddenErrorf("device %s default password must be changed: %v", deviceID, err)
	}
	if st, ok := grpcstatus.FromError(err); ok {
		switch st.Code() {
		case codes.Unauthenticated:
			return fleeterror.NewUnauthenticatedErrorf("device %s authentication failed: %v", deviceID, err)
		case codes.PermissionDenied:
			return fleeterror.NewForbiddenErrorf("device %s access denied: %v", deviceID, err)
		case codes.OK, codes.Canceled, codes.Unknown, codes.InvalidArgument,
			codes.DeadlineExceeded, codes.NotFound, codes.AlreadyExists,
			codes.ResourceExhausted, codes.FailedPrecondition, codes.Aborted,
			codes.OutOfRange, codes.Unimplemented, codes.Internal,
			codes.Unavailable, codes.DataLoss:
			// All other gRPC status codes fall through to InternalError below.
		}
	}
	return fleeterror.NewInternalErrorf("failed to create SDK device: %v", err)
}
