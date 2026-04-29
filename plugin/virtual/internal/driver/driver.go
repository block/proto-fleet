// Package driver implements the SDK Driver interface for virtual miners.
package driver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/block/proto-fleet/plugin/virtual/internal/config"
	"github.com/block/proto-fleet/plugin/virtual/internal/device"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	driverName      = "virtual"
	apiVersion      = "v1"
	virtualIPPrefix = "10.255."
	// virtualDiscoveryPort is the only port we respond to during discovery.
	// This prevents duplicate entries when scanning with multiple ports.
	// Using 4028 because it's the standard CGMiner API port used by most miners.
	virtualDiscoveryPort = "4028"
)

// Compile-time interface assertion for DefaultCredentialsProvider.
var _ sdk.DefaultCredentialsProvider = (*Driver)(nil)
var _ sdk.DiscoveryPortsProvider = (*Driver)(nil)

// defaultCredentials contains credentials for virtual miners.
// Virtual miners accept any credentials, but we provide defaults for consistency.
var defaultCredentials = []sdk.UsernamePassword{
	{Username: "virtual", Password: "virtual"},
	{Username: "root", Password: "root"},
}

// Driver implements sdk.Driver for virtual miners.
type Driver struct {
	config     *config.Config
	devices    map[string]sdk.Device
	minersByIP map[string]*config.VirtualMinerConfig
	mutex      sync.RWMutex
}

// New creates a new virtual miner driver from the given config file path.
func New(configPath string) (*Driver, error) {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Index miners by IP address only (not port), since discovery may use different ports
	minersByIP := make(map[string]*config.VirtualMinerConfig)
	for i := range cfg.Miners {
		miner := &cfg.Miners[i]
		minersByIP[miner.IPAddress] = miner
		slog.Info("Loaded virtual miner config", "serial", miner.SerialNumber, "ip", miner.IPAddress)
	}

	slog.Info("Virtual miner driver initialized", "miners", len(cfg.Miners))

	return &Driver{
		config:     cfg,
		devices:    make(map[string]sdk.Device),
		minersByIP: minersByIP,
	}, nil
}

// Handshake implements sdk.Driver.
func (d *Driver) Handshake(_ context.Context) (sdk.DriverIdentifier, error) {
	return sdk.DriverIdentifier{
		DriverName: driverName,
		APIVersion: apiVersion,
	}, nil
}

// DescribeDriver implements sdk.Driver.
func (d *Driver) DescribeDriver(_ context.Context) (sdk.DriverIdentifier, sdk.Capabilities, error) {
	return sdk.DriverIdentifier{
			DriverName: driverName,
			APIVersion: apiVersion,
		}, sdk.Capabilities{
			// Core
			sdk.CapabilityPollingHost: true,
			sdk.CapabilityDiscovery:   true,
			sdk.CapabilityPairing:     true,

			// Commands
			sdk.CapabilityReboot:             true,
			sdk.CapabilityMiningStart:        true,
			sdk.CapabilityMiningStop:         true,
			sdk.CapabilityCurtail:            true, // FULL curtailment wraps StopMining/StartMining
			sdk.CapabilityLEDBlink:           true,
			sdk.CapabilityCoolingModeAir:     true,
			sdk.CapabilityCoolingModeImmerse: true,
			sdk.CapabilityPoolConfig:         true,
			sdk.CapabilityPoolPriority:       true,

			// Telemetry
			sdk.CapabilityRealtimeTelemetry: true,
			sdk.CapabilityHashrateReported:  true,
			sdk.CapabilityPowerUsage:        true,
			sdk.CapabilityTemperature:       true,
			sdk.CapabilityFanSpeed:          true,
			sdk.CapabilityEfficiency:        true,
			sdk.CapabilityPerBoardStats:     true,
			sdk.CapabilityPSUStats:          true,
		}, nil
}

// GetDiscoveryPorts returns the canonical discovery port for virtual miners.
func (d *Driver) GetDiscoveryPorts(_ context.Context) []string {
	return []string{virtualDiscoveryPort}
}

// DiscoverDevice implements sdk.Driver.
func (d *Driver) DiscoverDevice(_ context.Context, ipAddress, port string) (sdk.DeviceInfo, error) {
	// Only handle IPs in the virtual range
	if !strings.HasPrefix(ipAddress, virtualIPPrefix) {
		return sdk.DeviceInfo{}, fmt.Errorf("not a virtual miner IP: %s", ipAddress)
	}

	// Only respond on the designated port to prevent duplicates when scanning multiple ports
	if port != virtualDiscoveryPort {
		return sdk.DeviceInfo{}, fmt.Errorf("virtual miner only responds on port %s, got %s", virtualDiscoveryPort, port)
	}

	// Look up by IP only (ignoring port) since virtual miners don't use real network ports
	d.mutex.RLock()
	minerCfg, exists := d.minersByIP[ipAddress]
	d.mutex.RUnlock()

	if !exists {
		return sdk.DeviceInfo{}, fmt.Errorf("no virtual miner configured at %s", ipAddress)
	}

	slog.Info("Discovered virtual miner", "serial", minerCfg.SerialNumber, "ip", ipAddress)

	return sdk.DeviceInfo{
		Host:            ipAddress,
		Port:            int32(minerCfg.Port),
		URLScheme:       "virtual",
		SerialNumber:    minerCfg.SerialNumber,
		Model:           minerCfg.Model,
		Manufacturer:    minerCfg.Manufacturer,
		MacAddress:      minerCfg.MacAddress,
		FirmwareVersion: "1.0.0-virtual",
	}, nil
}

// PairDevice implements sdk.Driver.
func (d *Driver) PairDevice(_ context.Context, deviceInfo sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
	// Look up miner config to get full device info (MAC, serial, etc.)
	d.mutex.RLock()
	minerCfg, exists := d.minersByIP[deviceInfo.Host]
	d.mutex.RUnlock()

	if !exists {
		return sdk.DeviceInfo{}, fmt.Errorf("no virtual miner configured at %s", deviceInfo.Host)
	}

	slog.Info("Paired virtual miner", "serial", minerCfg.SerialNumber, "mac", minerCfg.MacAddress)

	// Return full device info from config
	return sdk.DeviceInfo{
		Host:            deviceInfo.Host,
		Port:            int32(minerCfg.Port),
		URLScheme:       "virtual",
		SerialNumber:    minerCfg.SerialNumber,
		Model:           minerCfg.Model,
		Manufacturer:    minerCfg.Manufacturer,
		MacAddress:      minerCfg.MacAddress,
		FirmwareVersion: "1.0.0-virtual",
	}, nil
}

// NewDevice implements sdk.Driver.
func (d *Driver) NewDevice(_ context.Context, deviceID string, deviceInfo sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	// Find the miner config by IP (ignoring port)
	d.mutex.RLock()
	minerCfg, exists := d.minersByIP[deviceInfo.Host]
	d.mutex.RUnlock()

	if !exists {
		return sdk.NewDeviceResult{}, fmt.Errorf("no virtual miner config for %s", deviceInfo.Host)
	}

	// Create the device instance
	dev := device.New(deviceID, deviceInfo, minerCfg)

	d.mutex.Lock()
	d.devices[deviceID] = dev
	d.mutex.Unlock()

	slog.Info("Created virtual device instance", "device_id", deviceID, "serial", deviceInfo.SerialNumber)

	return sdk.NewDeviceResult{Device: dev}, nil
}

// GetDefaultCredentials implements sdk.DefaultCredentialsProvider.
// Returns default credentials for virtual miners to enable auto-authentication during pairing.
func (d *Driver) GetDefaultCredentials(_ context.Context, _, _ string) []sdk.UsernamePassword {
	return defaultCredentials
}
