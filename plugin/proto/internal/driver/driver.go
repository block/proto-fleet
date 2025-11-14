// Package driver implements the Fleet SDK Driver interface for Proto miners.
//
// The Driver is responsible for:
//   - Plugin lifecycle management
//   - Device discovery and pairing
//   - Device instance creation and management
//   - Driver-level capabilities reporting
//
// This implementation demonstrates best practices for:
//   - Clean SDK interface implementation
//   - Proper error handling and logging
//   - Resource management and cleanup
//   - Concurrent device management
package driver

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/btc-mining/proto-fleet/plugin/proto/internal/device"
	"github.com/btc-mining/proto-fleet/plugin/proto/pkg/proto"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

const (
	driverName         = "proto"
	apiVersion         = "v1"
	maxValidPortNumber = 65535 // Maximum valid TCP/UDP port number
)

var _ sdk.Driver = (*Driver)(nil) // Ensure Driver implements sdk.Driver

// Driver implements the SDK Driver interface for Proto miners.
type Driver struct {
	// devices tracks all active device instances
	devices      map[string]sdk.Device
	mutex        sync.RWMutex
	requiredPort int
}

// New creates a new Proto driver instance.
//
// This function demonstrates proper driver initialization:
//   - Sets up authentication services
//   - Initializes device tracking
//   - Handles initialization errors gracefully
func New(port int) (*Driver, error) {

	return &Driver{
		devices:      make(map[string]sdk.Device),
		requiredPort: port,
	}, nil
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

	// Define supported capabilities
	// These should match what your driver actually implements
	capabilities := sdk.Capabilities{
		// Core capabilities - required for basic operation
		sdk.CapabilityPollingHost: true, // We support host-side status polling
		sdk.CapabilityDiscovery:   true, // We can discover devices on the network
		sdk.CapabilityPairing:     true, // We can pair with discovered devices

		// Management capabilities - optional but recommended
		sdk.CapabilityReboot:     true, // We can reboot devices
		sdk.CapabilityFirmware:   true, // We can update device firmware
		sdk.CapabilityPoolConfig: true, // We can configure mining pools

		// Advanced capabilities - not yet implemented
		sdk.CapabilityPollingPlugin: false, // Plugin-side polling not supported
		sdk.CapabilityBatchStatus:   false, // Batch operations not supported
		sdk.CapabilityStreaming:     false, // Real-time streaming not supported

		// Additional capabilities from proto-rig configuration
		"factory_reset":         false, // factoryResetSupported: false
		"cooling_mode":          true,  // coolingModeSupported: true
		"logs_download":         true,  // logsDownloadSupported: true
		"realtime_telemetry":    true,  // realtimeTelemetrySupported: true
		"historical_data":       true,  // historicalDataSupported: true
		"hashrate_reported":     true,  // hashrateReported: true
		"power_usage_reported":  true,  // powerUsageReported: true
		"temperature_reported":  true,  // temperatureReported: true
		"fan_speed_reported":    true,  // fanSpeedReported: true
		"efficiency_reported":   true,  // efficiencyReported: true
		"uptime_reported":       true,  // uptimeReported: true
		"error_count_reported":  true,  // errorCountReported: true
		"miner_status_reported": true,  // minerStatusReported: true
		"pool_stats_reported":   true,  // poolStatsReported: true
		"per_chip_stats":        true,  // perChipStatsReported: true
		"per_board_stats":       true,  // perBoardStatsReported: true
		"psu_stats_reported":    true,  // psuStatsReported: true
		"ota_update":            true,  // otaUpdateSupported: true
		"manual_upload":         true,  // manualUploadSupported: true
		"asymmetric_auth":       true,  // authentication.supportedMethods: ["asymmetric"]
		"pool_priority":         true,  // poolPrioritySupported: true
	}

	return deviceInfo, capabilities, nil
}

// DiscoverDevice implements the SDK Driver interface.
//
// This method attempts to discover a Proto miner at the given network address.
// It demonstrates:
//   - Network connectivity testing
//   - Protocol negotiation (HTTPS vs HTTP)
//   - Device identification and validation
func (d *Driver) DiscoverDevice(ctx context.Context, ipAddress, port string) (sdk.DeviceInfo, error) {
	slog.Debug("Discovering device", "ip", ipAddress, "port", port)

	// Convert port to int32 for DeviceInfo
	portInt, err := strconv.Atoi(port)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("invalid port number: %s", port)
	}

	// Validate that this looks like a Proto miner port
	// Note: In integration tests, we may use different ports due to Docker port mapping
	if portInt != d.requiredPort && d.requiredPort != 0 {
		return sdk.DeviceInfo{}, fmt.Errorf("proto miners typically use port 2121, got %s", port)
	}

	// Check for port range overflow
	if portInt < 0 || portInt > maxValidPortNumber {
		return sdk.DeviceInfo{}, fmt.Errorf("port number out of range: %d", portInt)
	}

	// Validate host is not empty or whitespace-only
	if strings.TrimSpace(ipAddress) == "" {
		return sdk.DeviceInfo{}, fmt.Errorf("host address cannot be empty")
	}

	portInt32 := int32(portInt) // #nosec G109,G115 -- Range checked above

	// Try to connect and identify the device
	// We prefer HTTPS but fall back to HTTP if needed
	schemes := []string{"https", "http"}

	var lastValidationErr error

	for _, scheme := range schemes {
		deviceInfo, err := d.discoverWithScheme(ctx, ipAddress, portInt32, scheme)
		if err == nil {
			slog.Debug("Successfully discovered device",
				"ip", ipAddress,
				"port", port,
				"scheme", scheme,
				"serial", deviceInfo.SerialNumber)
			return deviceInfo, nil
		}

		// Check if this is a validation error (device responded but data was invalid)
		if strings.Contains(err.Error(), "device did not provide") {
			lastValidationErr = err
		}

		slog.Debug("Discovery failed with scheme", "scheme", scheme, "error", err)
	}

	// If we had a validation error, return that instead of generic connection error
	if lastValidationErr != nil {
		return sdk.DeviceInfo{}, lastValidationErr
	}

	return sdk.DeviceInfo{}, fmt.Errorf("failed to discover proto miner at %s:%s", ipAddress, port)
}

func getAndValidateDeviceInfo(ctx context.Context, client *proto.Client) (*proto.DeviceInfo, error) {
	info, err := client.GetDeviceInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get device info: %w", err)
	}

	if info.SerialNumber == "" {
		return nil, fmt.Errorf("device did not provide serial number")
	}
	if info.MacAddress == "" {
		return nil, fmt.Errorf("device did not provide MAC address")
	}

	return info, nil
}

// discoverWithScheme attempts device discovery using a specific URL scheme.
func (d *Driver) discoverWithScheme(ctx context.Context, ipAddress string, port int32, scheme string) (sdk.DeviceInfo, error) {
	// Create a client to communicate with the miner
	client, err := proto.NewClient(ipAddress, port, scheme)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	info, err := getAndValidateDeviceInfo(ctx, client)
	if err != nil {
		return sdk.DeviceInfo{}, err
	}

	return sdk.DeviceInfo{
		Host:         ipAddress,
		Port:         port,
		URLScheme:    scheme,
		SerialNumber: info.SerialNumber,
		Model:        info.Model,
		Manufacturer: info.Manufacturer,
		Type:         sdk.DeviceTypeASIC,
		MacAddress:   info.MacAddress,
	}, nil
}

// PairDevice implements the SDK Driver interface.
//
// This method establishes a trusted relationship with a discovered device.
// It demonstrates:
//   - Authentication credential exchange
//   - Secure communication setup
//   - Pairing verification
func (d *Driver) PairDevice(ctx context.Context, deviceInfo sdk.DeviceInfo, access sdk.SecretBundle) (string, error) {
	slog.Debug("Pairing device", "serial", deviceInfo.SerialNumber, "host", deviceInfo.Host)

	// Create client for the device
	client, err := proto.NewClient(deviceInfo.Host, deviceInfo.Port, deviceInfo.URLScheme)
	if err != nil {
		return "", fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	info, err := getAndValidateDeviceInfo(ctx, client)
	if err != nil {
		return "", err
	}
	deviceInfo.SerialNumber = info.SerialNumber
	deviceInfo.MacAddress = info.MacAddress

	publicKey, ok := access.Kind.(sdk.APIKey)
	if !ok {
		return "", fmt.Errorf("expected APIKey in secret bundle for pairing, got %T", access.Kind)
	}

	// For Ed25519 authentication, the credentials should be the base64-encoded public key
	// The miner expects this format for pairing
	if err := client.Pair(ctx, publicKey); err != nil {
		return "", fmt.Errorf("pairing failed: %w", err)
	}

	message := fmt.Sprintf("Successfully paired Proto miner %s at %s:%d",
		deviceInfo.SerialNumber, deviceInfo.Host, deviceInfo.Port)

	// TODO (DASH-857) Return device info to fleet so this data can be persisted
	message += fmt.Sprintf(" (S/N: %s)", deviceInfo.SerialNumber)
	message += fmt.Sprintf(" (MAC: %s)", deviceInfo.MacAddress)

	slog.Debug("Device paired successfully", "serial", deviceInfo.SerialNumber)
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
	slog.Debug("Creating new device instance", "deviceID", deviceID, "serial", deviceInfo.SerialNumber)

	// For device operations, we need to generate JWT tokens, not use the pairing token
	// The pairing token (base64 public key) is only used for the pairing process
	// For actual API calls, we need to generate JWT tokens signed with our private key

	token, ok := secret.Kind.(sdk.BearerToken)
	if !ok {
		return sdk.NewDeviceResult{}, fmt.Errorf("expected BearerToken in secret bundle, got %T", secret.Kind)
	}
	// Create the device instance
	dev, err := device.New(deviceID, deviceInfo, token)
	if err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("failed to create device: %w", err)
	}

	// Track the device instance
	d.mutex.Lock()
	d.devices[deviceID] = dev
	d.mutex.Unlock()

	slog.Info("Device instance created", "deviceID", deviceID, "serial", deviceInfo.SerialNumber)
	return sdk.NewDeviceResult{Device: dev}, nil
}
