// Package driver implements the Fleet SDK Driver interface for Antminer devices.
//
// The Driver is responsible for:
//   - Plugin lifecycle management
//   - Device discovery via RPC API
//   - Device pairing with username/password authentication
//   - Device instance creation and management
//   - Driver-level capabilities reporting
//
// This implementation demonstrates best practices for:
//   - Clean SDK interface implementation
//   - Proper error handling and logging
//   - Resource management and cleanup
//   - Concurrent device management
//   - Antminer-specific RPC protocol handling
package driver

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/btc-mining/proto-fleet/plugin/antminer/internal/device"
	"github.com/btc-mining/proto-fleet/plugin/antminer/internal/types"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

const (
	driverName        = "antminer"
	apiVersion        = "v1"
	requiredRPCPort   = 4028
	defaultWebPort    = "80"
	manufacturer      = "Bitmain"
	versionTypePrefix = "Antminer"
)

var _ sdk.Driver = (*Driver)(nil) // Ensure Driver implements sdk.Driver

// Driver implements the SDK Driver interface for Antminer devices.
type Driver struct {
	// devices tracks all active device instances
	devices map[string]sdk.Device
	mutex   sync.RWMutex

	// clientFactory allows injection of mock clients for testing
	clientFactory types.ClientFactory
}

// New creates a new Antminer driver instance.
//
// The clientFactory parameter is required and allows for dependency injection.
// This enables easy testing with mock clients.
func New(clientFactory types.ClientFactory) (*Driver, error) {
	driver := &Driver{
		devices:       make(map[string]sdk.Device),
		clientFactory: clientFactory,
	}

	return driver, nil
}

// Handshake implements the SDK Driver interface.
//
// This method identifies the plugin to the Fleet server.
// It should return consistent values across plugin restarts.
func (d *Driver) Handshake(ctx context.Context) (sdk.DriverIdentifier, error) {
	return sdk.DriverIdentifier{
		DriverName: driverName,
		APIVersion: apiVersion,
	}, nil
}

// DescribeDriver implements the SDK Driver interface.
//
// This method reports the driver's capabilities to the Fleet server.
// Capabilities determine which SDK methods the server will call.
func (d *Driver) DescribeDriver(ctx context.Context) (sdk.DriverIdentifier, sdk.Capabilities, error) {
	deviceInfo := sdk.DriverIdentifier{
		DriverName: driverName,
		APIVersion: apiVersion,
	}

	// Define supported capabilities for Antminer devices
	// These should match what Antminer devices actually support
	capabilities := sdk.Capabilities{
		// Core capabilities - required for basic operation
		sdk.CapabilityPollingHost: true, // We support host-side status polling via RPC
		sdk.CapabilityDiscovery:   true, // We can discover devices via RPC version command
		sdk.CapabilityPairing:     true, // We can pair with username/password auth

		// Management capabilities - limited by Antminer API
		sdk.CapabilityReboot:     false, // Would require web API implementation
		sdk.CapabilityFirmware:   false, // Would require web API implementation
		sdk.CapabilityPoolConfig: false, // Would require web API implementation

		// Advanced capabilities - not supported by Antminer RPC
		sdk.CapabilityPollingPlugin: false, // Plugin-side polling not supported
		sdk.CapabilityBatchStatus:   false, // Batch operations not supported
		sdk.CapabilityStreaming:     false, // Real-time streaming not supported
	}

	return deviceInfo, capabilities, nil
}

// DiscoverDevice implements the SDK Driver interface.
//
// This method attempts to discover an Antminer at the given network address.
// It demonstrates:
//   - RPC connectivity testing
//   - Device identification via version command
//   - Antminer-specific validation
func (d *Driver) DiscoverDevice(ctx context.Context, ipAddress, port string) (sdk.DeviceInfo, error) {
	slog.Debug("Discovering Antminer device", "ip", ipAddress, "port", port)

	// Antminers use a specific port for RPC API
	if port != fmt.Sprint(requiredRPCPort) {
		return sdk.DeviceInfo{}, fmt.Errorf("antminers use port 4028 for RPC, got %s", port)
	}

	// Convert port to int32 for client
	rpcPortUint, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("invalid RPC port number: %s", port)
	}
	rpcPort := int32(rpcPortUint)

	webPortUint, err := strconv.ParseUint(defaultWebPort, 10, 16)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("invalid web port number: %s", defaultWebPort)
	}
	webPort := int32(webPortUint)

	// Use the injected client factory for RPC communication
	client, err := d.clientFactory(ipAddress, rpcPort, webPort, "http")
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	// Try to get version information via RPC
	versionResp, err := client.GetVersion(ctx)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to get version info: %w", err)
	}

	if len(versionResp.Version) == 0 {
		return sdk.DeviceInfo{}, fmt.Errorf("empty version info from device")
	}

	versionInfo := versionResp.Version[0]

	// Validate that this is an Antminer device
	if !strings.HasPrefix(versionInfo.Type, versionTypePrefix) {
		return sdk.DeviceInfo{}, fmt.Errorf("not an Antminer device: %s", versionInfo.Type)
	}

	model := versionInfo.Miner
	if model == "" {
		model = "Unknown Antminer"
	}

	return sdk.DeviceInfo{
		Host:         ipAddress,
		Port:         webPort, // Use web port for device operations
		URLScheme:    "http",
		SerialNumber: "", // Will be filled during pairing if web API is accessible
		Model:        model,
		Manufacturer: manufacturer,
		Type:         sdk.DeviceTypeASIC,
		MacAddress:   "", // Will be filled during pairing if web API is accessible
	}, nil
}

// PairDevice implements the SDK Driver interface.
//
// This method establishes a trusted relationship with a discovered device.
// For Antminers, this involves username/password authentication.
func (d *Driver) PairDevice(ctx context.Context, deviceInfo sdk.DeviceInfo, access sdk.SecretBundle) (string, error) {
	slog.Debug("Pairing Antminer device", "host", deviceInfo.Host, "model", deviceInfo.Model)

	// Extract credentials from the secret bundle
	credentials, err := d.extractUsernamePassword(access)
	if err != nil {
		return "", fmt.Errorf("failed to extract credentials: %w", err)
	}

	// Use the injected client factory for communication
	client, err := d.clientFactory(deviceInfo.Host, requiredRPCPort, deviceInfo.Port, deviceInfo.URLScheme)
	if err != nil {
		return "", fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	err = client.SetCredentials(credentials)
	if err != nil {
		return "", fmt.Errorf("failed to set credentials: %w", err)
	}

	// Perform the pairing operation
	if err := client.Pair(ctx, credentials); err != nil {
		return "", fmt.Errorf("pairing failed: %w", err)
	}

	// Try to get additional device information if possible
	deviceInfoResp, err := client.GetDeviceInfo(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get device info after pairing: %w", err)
	}
	serialNumber := deviceInfoResp.SerialNumber
	macAddress := deviceInfoResp.MacAddress

	message := fmt.Sprintf("Successfully paired Antminer %s at %s:%d",
		deviceInfo.Model, deviceInfo.Host, deviceInfo.Port)

	// TODO (DASH-857) Return device info to fleet so this data can be persisted
	message += fmt.Sprintf(" (S/N: %s)", serialNumber)
	message += fmt.Sprintf(" (MAC: %s)", macAddress)

	slog.Debug("Device paired successfully", "host", deviceInfo.Host, "model", deviceInfo.Model)
	return message, nil
}

// NewDevice implements the SDK Driver interface.
//
// This method creates a new device instance for management.
// It demonstrates:
//   - Device instance lifecycle management
//   - Credential handling and storage
//   - Concurrent device tracking
func (d *Driver) NewDevice(ctx context.Context, deviceID string, deviceInfo sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	slog.Debug("Creating new Antminer device instance", "deviceID", deviceID, "host", deviceInfo.Host)

	// Extract credentials for the device - ensure we have UsernamePassword
	credentials, err := d.extractUsernamePassword(secret)
	if err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("failed to extract credentials: %w", err)
	}

	// Create the device instance with the same client factory used by the driver
	dev, err := device.New(deviceID, deviceInfo, credentials, d.clientFactory)
	if err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("failed to create device: %w", err)
	}

	err = dev.Connect(ctx)
	if err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("failed to connect device: %w", err)
	}

	// Track the device instance
	d.mutex.Lock()
	d.devices[deviceID] = dev
	d.mutex.Unlock()

	slog.Info("Antminer device instance created", "deviceID", deviceID, "host", deviceInfo.Host, "username", credentials.Username)
	return sdk.NewDeviceResult{Device: dev}, nil
}

// extractUsernamePassword extracts UsernamePassword credentials from a secret bundle.
//
// This method ensures type safety and prevents credential leakage in logs.
func (d *Driver) extractUsernamePassword(secret sdk.SecretBundle) (sdk.UsernamePassword, error) {
	switch kind := secret.Kind.(type) {
	case sdk.UsernamePassword:
		return kind, nil
	default:
		return sdk.UsernamePassword{}, fmt.Errorf("unsupported secret bundle type for Antminer: %T (expected UsernamePassword)", secret.Kind)
	}
}
