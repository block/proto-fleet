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
	"math"
	"strings"
	"sync"

	"github.com/block/proto-fleet/plugin/proto/internal/device"
	"github.com/block/proto-fleet/plugin/proto/pkg/proto"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	driverName         = "proto"
	apiVersion         = "v1"
	maxValidPortNumber = math.MaxUint16
)

var canonicalDiscoveryPorts = []int{443}

var _ sdk.Driver = (*Driver)(nil)
var _ sdk.DiscoveryPortsProvider = (*Driver)(nil)

// Driver implements the SDK Driver interface for Proto miners.
type Driver struct {
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

	capabilities := sdk.Capabilities{
		// Core capabilities - required for basic operation
		sdk.CapabilityPollingHost: true, // We support host-side status polling
		sdk.CapabilityDiscovery:   true, // We can discover devices on the network
		sdk.CapabilityPairing:     true, // We can pair with discovered devices

		// Command capabilities
		sdk.CapabilityReboot:             true,  // We can reboot devices
		sdk.CapabilityMiningStart:        true,  // We can start mining
		sdk.CapabilityMiningStop:         true,  // We can stop mining
		sdk.CapabilityCurtail:            true,  // FULL curtailment wraps StopMining/StartMining
		sdk.CapabilityLEDBlink:           true,  // We can blink LED for identification
		sdk.CapabilityFactoryReset:       false, // Factory reset not supported
		sdk.CapabilityCoolingModeAir:     true,  // We support air cooling mode
		sdk.CapabilityCoolingModeImmerse: true,  // We support immersion cooling mode
		sdk.CapabilityPoolConfig:         true,  // We can configure mining pools
		sdk.CapabilityPoolPriority:       true,  // We can set pool priority
		sdk.CapabilityLogsDownload:       true,  // We can download device logs

		// Power mode capabilities
		sdk.CapabilityPowerModeEfficiency: true, // We support efficiency/low power mode

		// Security capabilities
		sdk.CapabilityUpdateMinerPassword: true, // We can update miner web UI password

		// Telemetry capabilities
		sdk.CapabilityRealtimeTelemetry: true, // We support real-time telemetry
		sdk.CapabilityHistoricalData:    true, // We support historical data
		sdk.CapabilityHashrateReported:  true, // We report hashrate
		sdk.CapabilityPowerUsage:        true, // We report power usage
		sdk.CapabilityTemperature:       true, // We report temperature
		sdk.CapabilityFanSpeed:          true, // We report fan speed
		sdk.CapabilityEfficiency:        true, // We report efficiency
		sdk.CapabilityUptime:            true, // We report uptime
		sdk.CapabilityErrorCount:        true, // We report error count
		sdk.CapabilityMinerStatus:       true, // We report miner status
		sdk.CapabilityPoolStats:         true, // We report pool stats
		sdk.CapabilityPerChipStats:      true, // We report per-chip stats
		sdk.CapabilityPerBoardStats:     true, // We report per-board stats
		sdk.CapabilityPSUStats:          true, // We report PSU stats

		// Firmware capabilities
		sdk.CapabilityFirmware:     true, // We can update device firmware
		sdk.CapabilityOTAUpdate:    true, // We support OTA updates
		sdk.CapabilityManualUpload: true, // We support manual firmware upload

		// Authentication capabilities
		sdk.CapabilityAsymmetricAuth: true, // We use asymmetric key authentication

		// Advanced capabilities - not yet implemented
		sdk.CapabilityPollingPlugin: false, // Plugin-side polling not supported
		sdk.CapabilityBatchStatus:   false, // Batch operations not supported
		sdk.CapabilityStreaming:     false, // Real-time streaming not supported
	}

	return deviceInfo, capabilities, nil
}

// GetDiscoveryPorts returns discovery ports in the order they should be tried.
// When an explicit driver port override is configured, advertise that port first
// so default omitted-port discovery follows the configured environment.
func (d *Driver) GetDiscoveryPorts(_ context.Context) []string {
	if d.requiredPort > 0 && !isCanonicalDiscoveryPort(d.requiredPort) {
		return []string{fmt.Sprintf("%d", d.requiredPort)}
	}

	ports := make([]string, 0, len(canonicalDiscoveryPorts))
	seen := make(map[int]struct{}, len(canonicalDiscoveryPorts)+1)

	if d.requiredPort > 0 {
		ports = append(ports, fmt.Sprintf("%d", d.requiredPort))
		seen[d.requiredPort] = struct{}{}
	}

	for _, port := range canonicalDiscoveryPorts {
		if _, ok := seen[port]; ok {
			continue
		}
		ports = append(ports, fmt.Sprintf("%d", port))
	}
	return ports
}

// DiscoverDevice implements the SDK Driver interface.
//
// This method attempts to discover a Proto miner at the given network address.
// It demonstrates:
//   - Network connectivity testing
//   - Protocol negotiation (HTTPS vs HTTP)
//   - Device identification and validation
func (d *Driver) DiscoverDevice(ctx context.Context, ipAddress, port string) (sdk.DeviceInfo, error) {
	slog.Debug("Plugin DiscoverDevice called",
		"ip", ipAddress,
		"port", port,
		"required_port", d.requiredPort)

	portInt32, err := sdk.ParsePort(port)
	if err != nil {
		return sdk.DeviceInfo{}, err
	}

	portInt := int(portInt32)

	// Note: In integration tests, we may use different ports due to Docker port mapping
	if !d.isAllowedDiscoveryPort(portInt) {
		return sdk.DeviceInfo{}, fmt.Errorf("proto miners are configured for %s, got %s", d.expectedDiscoveryPorts(), port)
	}

	if strings.TrimSpace(ipAddress) == "" {
		return sdk.DeviceInfo{}, fmt.Errorf("host address cannot be empty")
	}

	schemes := []string{"https", "http"}

	var lastValidationErr error

	for _, scheme := range schemes {
		deviceInfo, err := d.discoverWithScheme(ctx, ipAddress, portInt32, scheme)
		if err == nil {
			slog.Debug("Plugin successfully discovered device",
				"ip", ipAddress,
				"port", port,
				"scheme", scheme,
				"serial", deviceInfo.SerialNumber,
				"model", deviceInfo.Model,
				"manufacturer", deviceInfo.Manufacturer)
			return deviceInfo, nil
		}

		if strings.Contains(err.Error(), "device did not provide") {
			lastValidationErr = err
		}
	}

	if lastValidationErr != nil {
		return sdk.DeviceInfo{}, lastValidationErr
	}

	return sdk.DeviceInfo{}, fmt.Errorf("failed to discover proto miner at %s:%s", ipAddress, port)
}

func (d *Driver) isAllowedDiscoveryPort(port int) bool {
	if d.requiredPort == 0 {
		return true
	}
	if isCanonicalDiscoveryPort(d.requiredPort) {
		return isCanonicalDiscoveryPort(port)
	}
	return port == d.requiredPort
}

func (d *Driver) expectedDiscoveryPorts() string {
	if !isCanonicalDiscoveryPort(d.requiredPort) {
		return fmt.Sprintf("port %d", d.requiredPort)
	}
	return "port 443"
}

func isCanonicalDiscoveryPort(port int) bool {
	for _, canonicalPort := range canonicalDiscoveryPorts {
		if port == canonicalPort {
			return true
		}
	}
	return false
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
	client, err := proto.NewClient(ipAddress, port, scheme)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	info, err := getAndValidateDeviceInfo(ctx, client)
	if err != nil {
		return sdk.DeviceInfo{}, err
	}

	// Get firmware version during discovery
	firmwareVersion := ""
	fwVersion, err := client.GetFirmwareVersion(ctx)
	if err != nil {
		slog.Debug("failed to get firmware version during discovery", "error", err)
	} else {
		firmwareVersion = fwVersion
	}

	return sdk.DeviceInfo{
		Host:            ipAddress,
		Port:            port,
		URLScheme:       scheme,
		SerialNumber:    info.SerialNumber,
		Model:           info.Model,
		Manufacturer:    info.Manufacturer,
		MacAddress:      info.MacAddress,
		FirmwareVersion: firmwareVersion,
	}, nil
}

// PairDevice implements the SDK Driver interface.
//
// This method establishes a trusted relationship with a discovered device.
// It demonstrates:
//   - Authentication credential exchange
//   - Secure communication setup
//   - Pairing verification
func (d *Driver) PairDevice(ctx context.Context, deviceInfo sdk.DeviceInfo, access sdk.SecretBundle) (sdk.DeviceInfo, error) {
	slog.Debug("Plugin PairDevice called",
		"serial", deviceInfo.SerialNumber,
		"host", deviceInfo.Host,
		"port", deviceInfo.Port,
		"url_scheme", deviceInfo.URLScheme)

	client, err := proto.NewClient(deviceInfo.Host, deviceInfo.Port, deviceInfo.URLScheme)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	info, err := getAndValidateDeviceInfo(ctx, client)
	if err != nil {
		return sdk.DeviceInfo{}, err
	}
	deviceInfo.SerialNumber = info.SerialNumber
	deviceInfo.MacAddress = info.MacAddress

	publicKey, ok := access.Kind.(sdk.APIKey)
	if !ok {
		return sdk.DeviceInfo{}, fmt.Errorf("expected APIKey in secret bundle for pairing, got %T", access.Kind)
	}

	if err := client.Pair(ctx, publicKey); err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("pairing failed: %w", err)
	}

	return deviceInfo, nil
}

// NewDevice implements the SDK Driver interface.
//
// This method creates a new device instance for management.
// It demonstrates:
//   - Device instance lifecycle management
//   - Credential handling and storage
//   - Concurrent device tracking
func (d *Driver) NewDevice(ctx context.Context, deviceID string, deviceInfo sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	slog.Debug("Plugin NewDevice called",
		"device_id", deviceID,
		"serial", deviceInfo.SerialNumber,
		"host", deviceInfo.Host,
		"port", deviceInfo.Port)

	token, ok := secret.Kind.(sdk.BearerToken)
	if !ok {
		return sdk.NewDeviceResult{}, fmt.Errorf("expected BearerToken in secret bundle, got %T", secret.Kind)
	}

	dev, err := device.New(deviceID, deviceInfo, token)
	if err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("failed to create device: %w", err)
	}

	d.mutex.Lock()
	d.devices[deviceID] = dev
	d.mutex.Unlock()

	slog.Info("Plugin device instance created successfully",
		"device_id", deviceID,
		"serial", deviceInfo.SerialNumber,
		"total_devices", len(d.devices))
	return sdk.NewDeviceResult{Device: dev}, nil
}
