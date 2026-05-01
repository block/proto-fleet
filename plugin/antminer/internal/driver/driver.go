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
	"strings"
	"sync"

	"github.com/block/proto-fleet/plugin/antminer/internal/device"
	"github.com/block/proto-fleet/plugin/antminer/internal/types"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	driverName        = "antminer"
	apiVersion        = "v1"
	requiredRPCPort   = 4028
	manufacturer      = "Bitmain"
	versionTypePrefix = "Antminer"

	// Firmware variant markers — substrings that appear in the BMMiner/Miner
	// version field for each aftermarket firmware. Checked case-insensitively.
	firmwareMarkerLuxOS   = "luxos"
	firmwareMarkerBraiins = "braiins"
	firmwareMarkerVNish   = "vnish"
	firmwareMarkerMaraFW  = "marafw"
	firmwareMarkerEpic    = "epic"
)

// nonStockFirmwareMarkers are case-insensitive substrings that indicate
// non-stock firmware that should be handled by a specialized plugin (e.g. asicrs).
var nonStockFirmwareMarkers = []string{
	firmwareMarkerLuxOS,
	firmwareMarkerBraiins,
	firmwareMarkerVNish,
	firmwareMarkerMaraFW,
	firmwareMarkerEpic,
}

// isNonStockFirmware reports whether the firmware string indicates a non-stock variant.
func isNonStockFirmware(firmware string) bool {
	lower := strings.ToLower(firmware)
	for _, marker := range nonStockFirmwareMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

var _ sdk.Driver = (*Driver)(nil)
var _ sdk.DefaultCredentialsProvider = (*Driver)(nil)
var _ sdk.ModelCapabilitiesProvider = (*Driver)(nil)
var _ sdk.DiscoveryPortsProvider = (*Driver)(nil)

// defaultCredentials contains well-known factory defaults for Bitmain Antminer devices.
// These are publicly documented and tried in order during auto-authentication.
var defaultCredentials = []sdk.UsernamePassword{
	{Username: "root", Password: "root"},
}

// efficiencyModeModels are model prefixes that support efficiency (low power) mode.
var efficiencyModeModels = []string{
	"Antminer S17",
	"Antminer S19",
	"Antminer T17",
	"Antminer T19",
}

// noEfficiencyModeModels are model prefixes that do NOT support efficiency mode.
// These take precedence over efficiencyModeModels.
var noEfficiencyModeModels = []string{
	"Antminer S21",
	"Antminer T21",
}

// Driver implements the SDK Driver interface for Antminer devices.
type Driver struct {
	devices map[string]sdk.Device
	mutex   sync.RWMutex

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

	capabilities := sdk.Capabilities{
		// Core capabilities
		sdk.CapabilityPollingHost: true,
		sdk.CapabilityDiscovery:   true,
		sdk.CapabilityPairing:     true,

		// Command capabilities
		sdk.CapabilityReboot:             true,
		sdk.CapabilityMiningStart:        true,
		sdk.CapabilityMiningStop:         true,
		sdk.CapabilityCurtailFull:        true, // FULL curtailment uses mining start/stop.
		sdk.CapabilityLEDBlink:           true,
		sdk.CapabilityFactoryReset:       false,
		sdk.CapabilityCoolingModeAir:     false,
		sdk.CapabilityCoolingModeImmerse: false,
		sdk.CapabilityPoolConfig:         true,
		sdk.CapabilityPoolPriority:       true,
		sdk.CapabilityLogsDownload:       true,

		// Power mode capabilities are model-specific; see GetCapabilitiesForModel.
		sdk.CapabilityPowerModeEfficiency: false,

		// Security capabilities
		sdk.CapabilityUpdateMinerPassword: true,

		// Telemetry capabilities
		sdk.CapabilityRealtimeTelemetry: true,
		sdk.CapabilityHistoricalData:    false,
		sdk.CapabilityHashrateReported:  true,
		sdk.CapabilityPowerUsage:        false,
		sdk.CapabilityTemperature:       true,
		sdk.CapabilityFanSpeed:          true,
		sdk.CapabilityEfficiency:        false,
		sdk.CapabilityUptime:            true,
		sdk.CapabilityErrorCount:        true,
		sdk.CapabilityMinerStatus:       true,
		sdk.CapabilityPoolStats:         true,
		sdk.CapabilityPerChipStats:      true,
		sdk.CapabilityPerBoardStats:     true,
		sdk.CapabilityPSUStats:          false,

		// Firmware capabilities
		sdk.CapabilityFirmware:     true,
		sdk.CapabilityOTAUpdate:    false,
		sdk.CapabilityManualUpload: true,

		// Authentication capabilities
		sdk.CapabilityBasicAuth: true,

		// Advanced capabilities
		sdk.CapabilityPollingPlugin: false,
		sdk.CapabilityBatchStatus:   false,
		sdk.CapabilityStreaming:     false,
	}

	return deviceInfo, capabilities, nil
}

// GetDiscoveryPorts returns the canonical RPC discovery port for Antminers.
func (d *Driver) GetDiscoveryPorts(_ context.Context) []string {
	return []string{fmt.Sprint(requiredRPCPort)}
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

	if port != fmt.Sprint(requiredRPCPort) {
		return sdk.DeviceInfo{}, sdk.NewErrorDeviceNotFound(ipAddress,
			fmt.Errorf("antminers use port %d for RPC, got %s", requiredRPCPort, port))
	}

	rpcPort, err := sdk.ParsePort(port)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("invalid RPC port number: %w", err)
	}

	webPort := types.WebPort()

	client, err := d.clientFactory(ipAddress, rpcPort, webPort, "http")
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	versionResp, err := client.GetVersion(ctx)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to get version info: %w", err)
	}

	if len(versionResp.Version) == 0 {
		return sdk.DeviceInfo{}, fmt.Errorf("empty version info from device")
	}

	versionInfo := versionResp.Version[0]

	if !strings.HasPrefix(versionInfo.Type, versionTypePrefix) {
		return sdk.DeviceInfo{}, fmt.Errorf("not an Antminer device: %s", versionInfo.Type)
	}

	model := versionInfo.Type
	if model == "" {
		model = "Unknown Antminer"
	}

	// Extract firmware version from version info
	firmwareVersion := versionInfo.BMMiner
	if firmwareVersion == "" {
		firmwareVersion = versionInfo.Miner
	}

	if versionInfo.LUXminer != "" {
		return sdk.DeviceInfo{}, sdk.NewErrorDeviceNotFound(ipAddress,
			fmt.Errorf("LuxOS firmware detected, skipping antminer plugin"))
	}

	if isNonStockFirmware(firmwareVersion) {
		return sdk.DeviceInfo{}, sdk.NewErrorDeviceNotFound(ipAddress,
			fmt.Errorf("non-stock firmware detected (%s), skipping antminer plugin", firmwareVersion))
	}

	return sdk.DeviceInfo{
		Host:            ipAddress,
		Port:            rpcPort,
		URLScheme:       "http",
		SerialNumber:    "",
		Model:           model,
		Manufacturer:    manufacturer,
		MacAddress:      "",
		FirmwareVersion: firmwareVersion,
	}, nil
}

// PairDevice implements the SDK Driver interface.
//
// This method establishes a trusted relationship with a discovered device.
// For Antminers, this involves username/password authentication.
func (d *Driver) PairDevice(ctx context.Context, deviceInfo sdk.DeviceInfo, access sdk.SecretBundle) (sdk.DeviceInfo, error) {
	slog.Debug("Pairing Antminer device", "host", deviceInfo.Host, "model", deviceInfo.Model)

	credentials, err := d.extractUsernamePassword(access)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to extract credentials: %w", err)
	}

	webPort := types.WebPort()
	client, err := d.clientFactory(deviceInfo.Host, requiredRPCPort, webPort, deviceInfo.URLScheme)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	err = client.SetCredentials(credentials)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to set credentials: %w", err)
	}

	if err := client.Pair(ctx, credentials); err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("pairing failed: %w", err)
	}

	deviceInfoResp, err := client.GetDeviceInfo(ctx)
	if err != nil {
		return sdk.DeviceInfo{}, fmt.Errorf("failed to get device info after pairing: %w", err)
	}

	deviceInfo.SerialNumber = deviceInfoResp.SerialNumber
	deviceInfo.MacAddress = deviceInfoResp.MacAddress

	// Get firmware version during pairing
	versionResp, err := client.GetVersion(ctx)
	if err != nil {
		slog.Debug("failed to get version during pairing", "error", err)
	} else if len(versionResp.Version) > 0 {
		deviceInfo.FirmwareVersion = versionResp.Version[0].BMMiner
		if deviceInfo.FirmwareVersion == "" {
			deviceInfo.FirmwareVersion = versionResp.Version[0].Miner
		}
	}

	slog.Debug("Device paired successfully, returning device info",
		"host", deviceInfo.Host,
		"model", deviceInfo.Model,
		"serial", deviceInfo.SerialNumber,
		"mac", deviceInfo.MacAddress,
		"firmware_version", deviceInfo.FirmwareVersion,
		"username", credentials.Username)

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
	slog.Debug("Creating new Antminer device instance", "deviceID", deviceID, "host", deviceInfo.Host)

	credentials, err := d.extractUsernamePassword(secret)
	if err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("failed to extract credentials: %w", err)
	}

	dev, err := device.New(deviceID, deviceInfo, credentials, d.clientFactory)
	if err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("failed to create device: %w", err)
	}

	err = dev.Connect(ctx)
	if err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("failed to connect device: %w", err)
	}

	d.mutex.Lock()
	d.devices[deviceID] = dev
	d.mutex.Unlock()

	slog.Info("Antminer device instance created", "deviceID", deviceID, "host", deviceInfo.Host, "username", credentials.Username)
	return sdk.NewDeviceResult{Device: dev}, nil
}

func (d *Driver) extractUsernamePassword(secret sdk.SecretBundle) (sdk.UsernamePassword, error) {
	switch kind := secret.Kind.(type) {
	case sdk.UsernamePassword:
		return kind, nil
	default:
		return sdk.UsernamePassword{}, fmt.Errorf("unsupported secret bundle type for Antminer: %T (expected UsernamePassword)", secret.Kind)
	}
}

// GetDefaultCredentials implements sdk.DefaultCredentialsProvider.
// Returns known default credentials for Antminer devices to enable auto-authentication during pairing.
func (d *Driver) GetDefaultCredentials(_ context.Context, _, _ string) []sdk.UsernamePassword {
	return defaultCredentials
}

// GetCapabilitiesForModel implements sdk.ModelCapabilitiesProvider.
// Stock Bitmain firmware is SV1-only and the plugin rejects non-stock
// firmware at discovery, so CapabilityNativeStratumV2 is never set.
func (d *Driver) GetCapabilitiesForModel(_ context.Context, _, model string) sdk.Capabilities {
	caps := sdk.Capabilities{}
	if d.modelSupportsEfficiencyMode(model) {
		caps[sdk.CapabilityPowerModeEfficiency] = true
	}
	return caps
}

// modelSupportsEfficiencyMode checks if the given model supports efficiency/low power mode.
func (d *Driver) modelSupportsEfficiencyMode(model string) bool {
	// Check exclusion list first (takes priority)
	for _, prefix := range noEfficiencyModeModels {
		if strings.HasPrefix(model, prefix) {
			return false
		}
	}

	// Check inclusion list
	for _, prefix := range efficiencyModeModels {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}

	// Unknown models: default to not supported (safe default)
	return false
}
