# Fleet Miner Driver SDK v1

A comprehensive SDK for building miner driver plugins that integrate with the Fleet mining management system. This SDK provides a standardized interface for discovering, managing, and monitoring various types of mining hardware.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Core Interfaces](#core-interfaces)
- [Implementation Guide](#implementation-guide)
- [Authentication](#authentication)
- [Testing](#testing)
- [Best Practices](#best-practices)
- [Examples](#examples)
- [Troubleshooting](#troubleshooting)

## Overview

The Fleet Miner Driver SDK enables you to create plugins that can:

- **Discover** mining devices on the network
- **Pair** and authenticate with devices
- **Monitor** device status and telemetry
- **Control** mining operations (start/stop)
- **Configure** device settings (pools, cooling modes)
- **Manage** device lifecycle (reboot, firmware updates)

### Key Features

- 🔌 **Plugin Architecture**: Uses HashiCorp's go-plugin for process isolation
- 📊 **Rich Telemetry**: Standardized metrics collection and reporting
- 🔐 **Flexible Authentication**: Support for multiple auth methods (JWT, TLS, API keys)
- 🌐 **Network Discovery**: Automated device discovery capabilities
- ⚡ **Real-time Control**: Direct device control operations
- 🔧 **Extensible Design**: Optional capabilities for advanced features

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Fleet Host Process                       │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐    ┌─────────────────────────────────┐ │
│  │   Fleet Server  │◄──►│        SDK Plugin Host          │ │
│  │                 │    │                                 │ │
│  └─────────────────┘    └─────────────────────────────────┘ │
└─────────────────────────────────┬───────────────────────────┘
                                  │ gRPC over go-plugin
┌─────────────────────────────────▼───────────────────────────┐
│                    Plugin Process                           │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐    ┌─────────────────────────────────┐ │
│  │   Your Driver   │◄──►│           SDK v1                │ │
│  │  Implementation │    │                                 │ │
│  └─────────────────┘    └─────────────────────────────────┘ │
└─────────────────────────────────┬───────────────────────────┘
                                  │ HTTP/gRPC/Custom Protocol
┌─────────────────────────────────▼───────────────────────────┐
│                   Mining Hardware                           │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────────────────┐ │
│  │   Antminer  │ │ Whatsminer  │ │    Custom Hardware      │ │
│  └─────────────┘ └─────────────┘ └─────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## Quick Start

### 1. Create a New Plugin Project

```bash
mkdir my-miner-plugin
cd my-miner-plugin
go mod init github.com/yourorg/my-miner-plugin
```

### 2. Add SDK Dependency

```go
// go.mod
module github.com/yourorg/my-miner-plugin

go 1.21

require (
    github.com/block/proto-fleet/server v0.0.0-latest
    github.com/hashicorp/go-plugin v1.7.0
)

// Use local development version if needed
replace github.com/block/proto-fleet/server => /path/to/local/server
```

### 3. Implement the Driver Interface

```go
// main.go
package main

import (
    "github.com/block/proto-fleet/server/sdk/v1"
    "github.com/hashicorp/go-plugin"
)

func main() {
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: sdk.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "driver": &sdk.DriverPlugin{Impl: NewMyDriver()},
        },
    })
}
```

### 4. Create Your Driver Implementation

```go
// driver.go
package main

import (
    "context"
    "fmt"
    sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type MyDriver struct {
    devices map[string]*MyDevice
}

func NewMyDriver() *MyDriver {
    return &MyDriver{
        devices: make(map[string]*MyDevice),
    }
}

func (d *MyDriver) Handshake(ctx context.Context) (sdk.Handshake, error) {
    return sdk.Handshake{
        DriverName: "my-miner",
        APIVersion: "v1",
    }, nil
}

// Implement other Driver interface methods...
```

## Core Interfaces

### Driver Interface

The `Driver` interface is the main entry point for your plugin:

```go
type Driver interface {
    // Core identification
    Handshake(ctx context.Context) (DriverIdentifier, error)
    DescribeDriver(ctx context.Context) (DriverIdentifier, Capabilities, error)

    // Device discovery and pairing
    DiscoverDevice(ctx context.Context, ipAddress, port string) (DeviceInfo, error)
    PairDevice(ctx context.Context, device DeviceInfo, access SecretBundle) (DeviceInfo, error)

    // Device lifecycle
    NewDevice(ctx context.Context, deviceID string, deviceInfo DeviceInfo, secret SecretBundle) (NewDeviceResult, error)
}
```

### Device Interface

The `Device` interface represents individual mining devices and is composed of several interface groups:

```go
// Device represents a single device instance managed by a driver
// It composes all the device interfaces to maintain backward compatibility
type Device interface {
    DeviceCore
    DeviceControl
    DeviceConfiguration
    DeviceMaintenance
    DeviceOptional
}

// DeviceCore represents the core functionality that all devices must implement
type DeviceCore interface {
    // ID returns the unique device instance identifier
    ID() string

    // DescribeDevice returns device info and capabilities
    DescribeDevice(ctx context.Context) (DeviceInfo, Capabilities, error)

    // Status returns current device status (CoreV1 - required)
    Status(ctx context.Context) (DeviceStatusResponse, error)

    // Close releases device resources
    Close(ctx context.Context) error
}

// DeviceControl represents mining control operations
type DeviceControl interface {
    // CoreV1 - Control methods (required)
    StartMining(ctx context.Context) error
    StopMining(ctx context.Context) error
    BlinkLED(ctx context.Context) error
    Reboot(ctx context.Context) error
}

// DeviceConfiguration represents device configuration operations
type DeviceConfiguration interface {
    // CoreV1 - Configuration methods (required)
    SetCoolingMode(ctx context.Context, mode CoolingMode) error
    UpdateMiningPools(ctx context.Context, pools []MiningPoolConfig) error
}

// DeviceMaintenance represents device maintenance operations
type DeviceMaintenance interface {
    DownloadLogs(ctx context.Context, since *time.Time, batchLogUUID string) (logData string, moreData bool, err error)
    FirmwareUpdate(ctx context.Context) error
}

// DeviceOptional represents optional device capabilities
type DeviceOptional interface {
    // Optional capabilities - return (result, false, nil) if unsupported
    TryBatchStatus(ctx context.Context, ids []string) (map[string]DeviceStatusResponse, bool, error)
    TrySubscribe(ctx context.Context, ids []string) (<-chan DeviceStatusResponse, bool, error)
    TryGetWebViewURL(ctx context.Context) (string, bool, error)
    TryGetTimeSeriesData(ctx context.Context, metricNames []string, startTime, endTime time.Time, granularity *time.Duration, maxPoints int32, pageToken string) (series []DeviceStatusResponse, nextPageToken string, supported bool, err error)
}
```

### Cooling Modes

The SDK supports different cooling modes for devices:

```go
type CoolingMode int

const (
    CoolingModeUnspecified      CoolingMode = iota // Unspecified cooling mode
    CoolingModeAirCooled                          // Air cooling (default for most miners)
    CoolingModeImmersionCooled                    // Immersion cooling (liquid cooling)
    CoolingModeManual                             // Manual cooling (user sets fan speed manually)
)
```

Example usage:

```go
// Set device to air cooling mode
err := device.SetCoolingMode(ctx, sdk.CoolingModeAirCooled)
if err != nil {
    return fmt.Errorf("failed to set cooling mode: %w", err)
}

// Set device to manual cooling mode (allows manual fan control)
err = device.SetCoolingMode(ctx, sdk.CoolingModeManual)
if err != nil {
    return fmt.Errorf("failed to set manual cooling mode: %w", err)
}
```

## Implementation Guide

### Step 1: Driver Implementation

```go
package main

import (
    "context"
    "fmt"
    "sync"
    "time"
    sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type MyDriver struct {
    devices map[string]*MyDevice
    mutex   sync.RWMutex
}

func NewMyDriver() *MyDriver {
    return &MyDriver{
        devices: make(map[string]*MyDevice),
    }
}

func (d *MyDriver) Handshake(ctx context.Context) (sdk.DriverIdentifier, error) {
    return sdk.DriverIdentifier{
        DriverName: "my-miner",
        APIVersion: "v1",
    }, nil
}

func (d *MyDriver) DescribeDriver(ctx context.Context) (sdk.DriverIdentifier, sdk.Capabilities, error) {
    handshake := sdk.DriverIdentifier{
        DriverName: "my-miner",
        APIVersion: "v1",
    }

    capabilities := sdk.Capabilities{
        sdk.CapabilityDiscovery:   true,  // Device discovery
        sdk.CapabilityPairing:     true,  // Device pairing
        sdk.CapabilityReboot:      true,  // Reboot support
        sdk.CapabilityFirmware:    false, // No firmware updates
        sdk.CapabilityPoolConfig:  true,  // Pool configuration
        // Advanced capabilities
        sdk.CapabilityPollingPlugin: false, // No plugin-side polling
        sdk.CapabilityBatchStatus:   false, // No batch operations
        sdk.CapabilityStreaming:     false, // No streaming
    }

    return handshake, capabilities, nil
}

func (d *MyDriver) DiscoverDevice(ctx context.Context, ipAddress, port string) (sdk.DeviceInfo, error) {
    // Implement device discovery logic
    // This should attempt to connect to the device and retrieve basic info
    
    // Example: Check if device responds on expected port
    if port != "4028" { // Your miner's API port
        return sdk.DeviceInfo{}, fmt.Errorf("expected port 4028, got %s", port)
    }

    // Try to connect and get device info
    deviceInfo, err := d.connectAndGetInfo(ipAddress, port)
    if err != nil {
        return sdk.DeviceInfo{}, fmt.Errorf("failed to discover device: %w", err)
    }

    return deviceInfo, nil
}

func (d *MyDriver) PairDevice(ctx context.Context, device sdk.DeviceInfo, access sdk.SecretBundle) (sdk.DeviceInfo, error) {
    // Implement device pairing logic
    // This should establish authentication with the device
    // and return updated device information

    switch kind := access.Kind.(type) {
    case sdk.UsernamePassword:
        // Handle username/password authentication
        if err := d.authenticateWithCredentials(device, kind.Username, kind.Password); err != nil {
            return sdk.DeviceInfo{}, err
        }
    case sdk.APIKey:
        // Handle API key authentication
        if err := d.authenticateWithAPIKey(device, kind.Key); err != nil {
            return sdk.DeviceInfo{}, err
        }
    default:
        return sdk.DeviceInfo{}, fmt.Errorf("unsupported authentication type: %T", access.Kind)
    }

    // Fetch additional device information during pairing (e.g., serial number, MAC address)
    updatedInfo, err := d.fetchDeviceDetails(ctx, device)
    if err != nil {
        return sdk.DeviceInfo{}, fmt.Errorf("failed to fetch device details: %w", err)
    }

    return updatedInfo, nil
}

func (d *MyDriver) NewDevice(ctx context.Context, deviceID string, deviceInfo sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
    // Validate device info
    if deviceInfo.Host == "" {
        return sdk.NewDeviceResult{}, fmt.Errorf("host is required")
    }

    // Validate device ID
    if deviceID == "" {
        return sdk.NewDeviceResult{}, fmt.Errorf("device_id is required")
    }
    
    device := &MyDevice{
        id:         deviceID,  // Use the provided device ID
        deviceInfo: deviceInfo,
        secret:     secret,
        // Initialize other fields...
    }

    // Store device
    d.mutex.Lock()
    d.devices[deviceID] = device
    d.mutex.Unlock()

    return sdk.NewDeviceResult{Device: device}, nil
}
```

### Step 2: Device Implementation

```go
type MyDevice struct {
    id         string
    deviceInfo sdk.DeviceInfo
    secret     sdk.SecretBundle
    // Add your device-specific fields
}

func (d *MyDevice) ID() string {
    return d.id
}

func (d *MyDevice) DescribeDevice(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
    return d.deviceInfo, sdk.Capabilities{
        sdk.CapabilityReboot:     true,
        sdk.CapabilityPoolConfig: true,
        // Set capabilities based on what your device supports
    }, nil
}

func (d *MyDevice) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
    // Implement status collection from your device
    now := time.Now()
    
    // Get metrics from your device API
    hashrate, power, temp := d.getMetrics(ctx)
    
    return sdk.DeviceStatusResponse{
        DeviceID:  d.id,
        Timestamp: now,
        Summary:   "Mining", // or "Stopped", "Error", etc.
        Health:    sdk.HealthHealthyActive, // or HealthyInactive, Warning, Critical, Unknown
        
        // Core metrics
        HashrateHS:              &hashrate,
        PowerWatts:              &power,
        TemperatureCelsius:      &temp,
        
        // Sampling information
        Sample: &sdk.SampleSemantics{
            Aggregation:     sdk.AggregationGauge,
            AveragingWindow: 30 * time.Second,
        },
        
        // Device metadata
        Metadata: map[string]string{
            "host": d.deviceInfo.Host,
            "port": fmt.Sprintf("%d", d.deviceInfo.Port),
            "model": d.deviceInfo.Model,
        },
    }, nil
}

func (d *MyDevice) StartMining(ctx context.Context) error {
    // Implement mining start logic
    return d.sendCommand(ctx, "start")
}

func (d *MyDevice) StopMining(ctx context.Context) error {
    // Implement mining stop logic
    return d.sendCommand(ctx, "stop")
}

func (d *MyDevice) UpdateMiningPools(ctx context.Context, pools []sdk.MiningPoolConfig) error {
    // Implement pool configuration logic
    for _, pool := range pools {
        err := d.configurePool(ctx, pool.Priority, pool.URL, pool.WorkerName)
        if err != nil {
            return fmt.Errorf("failed to configure pool %s: %w", pool.URL, err)
        }
    }
    return nil
}

func (d *MyDevice) Close(ctx context.Context) error {
    // Cleanup resources
    return nil
}

// Implement other required methods...
```

## Authentication

The SDK supports multiple authentication methods through the `SecretBundle` type:

### Username/Password Authentication

```go
secret := sdk.SecretBundle{
    Version: "v1",
    Kind: sdk.UsernamePassword{
        Username: "admin",
        Password: "password123",
    },
    TTL: &duration, // Optional expiration
}
```

### API Key Authentication

```go
secret := sdk.SecretBundle{
    Version: "v1",
    Kind: sdk.APIKey{
        Key: "your-api-key",
    },
}
```

### Bearer Token Authentication

```go
secret := sdk.SecretBundle{
    Version: "v1",
    Kind: sdk.BearerToken{
        Token: "jwt-token-or-bearer-token",
    },
}
```

### TLS Client Certificate Authentication

```go
secret := sdk.SecretBundle{
    Version: "v1",
    Kind: sdk.TLSClientCert{
        ClientCertPEM: clientCertBytes,
        KeyPEM:        privateKeyBytes,
        CACertPEM:     caCertBytes, // Optional
    },
}
```

## Metrics and Values

The SDK provides a type-safe metric value system for extensible telemetry data:

### MetricValue Interface

```go
type MetricValue interface {
    Type() ValueType
    AsInt() (int, bool)
    AsFloat64() (float64, bool)
    AsString() (string, bool)
    AsBool() (bool, bool)
}
```

### Creating Metric Values

```go
// Use the generic constructor for type safety
intValue := sdk.NewMetricValue(42)
floatValue := sdk.NewMetricValue(3.14)
stringValue := sdk.NewMetricValue("active")
boolValue := sdk.NewMetricValue(true)

// Use in ExtraMetrics
extraMetrics := []sdk.Metric{
    {
        Name:       "custom_temperature",
        Value:      sdk.NewMetricValue(75.5),
        Unit:       sdk.UnitCelsius,
        Kind:       sdk.MetricKindGauge,
        ObservedAt: time.Now(),
        Labels: map[string]string{
            "sensor": "intake",
        },
    },
}
```

## Testing

### Unit Testing Your Driver

```go
package main

import (
    "context"
    "testing"
    "time"
    sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestDriverHandshake(t *testing.T) {
    driver := NewMyDriver()
    
    handshake, err := driver.Handshake(context.Background())
    if err != nil {
        t.Fatalf("Handshake failed: %v", err)
    }
    
    if handshake.DriverName != "my-miner" {
        t.Errorf("Expected driver name 'my-miner', got %s", handshake.DriverName)
    }
}

func TestDeviceCreation(t *testing.T) {
    driver := NewMyDriver()
    
    deviceInfo := sdk.DeviceInfo{
        Host:      "192.168.1.100",
        Port:      4028,
        URLScheme: "http",
    }
    
    secret := sdk.SecretBundle{
        Version: "v1",
        Kind: sdk.UsernamePassword{
            Username: "admin",
            Password: "password",
        },
    }
    
    result, err := driver.NewDevice(context.Background(), "my-device-123", deviceInfo, secret)
    if err != nil {
        t.Fatalf("NewDevice failed: %v", err)
    }
    
    if result.Device.ID() == "" {
        t.Error("Expected non-empty device ID")
    }
    
    if result.Device.ID() != "my-device-123" {
        t.Errorf("Expected device ID 'my-device-123', got %s", result.Device.ID())
    }
}

func TestDeviceStatus(t *testing.T) {
    // Create device instance...
    
    status, err := device.Status(context.Background())
    if err != nil {
        t.Fatalf("Status failed: %v", err)
    }
    
    if status.Health == "" {
        t.Error("Expected health status to be set")
    }
}
```

### Integration Testing

```go
import "os"

func TestRealDeviceIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }
    
    // Test with real device if available
    deviceIP := os.Getenv("TEST_DEVICE_IP")
    if deviceIP == "" {
        t.Skip("No test device IP provided")
    }
    
    driver := NewMyDriver()
    
    // Test discovery
    device, err := driver.DiscoverDevice(context.Background(), deviceIP, "4028")
    if err != nil {
        t.Fatalf("Device discovery failed: %v", err)
    }
    
    // Test device operations...
}
```

## Error Handling

The SDK provides standardized error types for consistent error reporting across all drivers. These errors are automatically converted to appropriate gRPC status codes when crossing the plugin boundary.

### SDK Error Types

```go
type ErrorCode string

const (
    ErrCodeUnsupportedCapability ErrorCode = "UNSUPPORTED_CAPABILITY"
    ErrCodeDeviceNotFound        ErrorCode = "DEVICE_NOT_FOUND"
    ErrCodeInvalidConfig         ErrorCode = "INVALID_CONFIG"
    ErrCodeDeviceUnavailable     ErrorCode = "DEVICE_UNAVAILABLE"
    ErrCodeDriverShutdown        ErrorCode = "DRIVER_SHUTDOWN"
)

type SDKError struct {
    Code    ErrorCode
    Message string
    Err     error // Optional underlying error
}
```

### Error Constructors

The SDK provides convenient constructors for common error scenarios:

```go
// Device not found
err := NewErrorDeviceNotFound("device-123")

// Unsupported capability
err := NewErrUnsupportedCapability("streaming")

// Invalid configuration
err := NewErrorInvalidConfig("device-123")

// Device temporarily unavailable
err := NewErrorDeviceUnavailable("device-123")

// Driver shutting down
err := NewErrorDriverShutdown()
```

### Usage in Device Implementation

```go
func (d *MyDevice) StartMining(ctx context.Context) error {
    // Check if device is available
    if !d.isConnected() {
        return NewErrorDeviceUnavailable(d.ID())
    }
    
    // Check if mining is supported
    if !d.supportsMining() {
        return NewErrUnsupportedCapability("mining")
    }
    
    // Attempt to start mining
    if err := d.sendStartCommand(); err != nil {
        // Wrap underlying error
        return NewErrorDeviceUnavailable(d.ID(), err)
    }
    
    return nil
}

func (d *MyDevice) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
    // Return supported=false for unsupported capabilities
    if !d.hasWebInterface() {
        return "", false, nil
    }
    
    // Return error for actual failures
    url, err := d.getWebURL()
    if err != nil {
        return "", true, NewErrorDeviceUnavailable(d.ID(), err)
    }
    
    return url, true, nil
}
```

### Error Mapping to gRPC Status

The SDK automatically maps error codes to appropriate gRPC status codes:

| SDK Error Code | gRPC Status Code | Description |
|----------------|------------------|-------------|
| `ErrCodeDeviceNotFound` | `NotFound` | Device instance not found |
| `ErrCodeUnsupportedCapability` | `Unimplemented` | Feature not supported |
| `ErrCodeInvalidConfig` | `InvalidArgument` | Invalid configuration |
| `ErrCodeDeviceUnavailable` | `Unavailable` | Device temporarily unavailable |
| `ErrCodeDriverShutdown` | `Aborted` | Driver is shutting down |

## Best Practices

### 1. Capability Validation

The SDK provides utility functions for capability validation:

```go
// Check if a capability is supported
if sdk.IsCapabilitySupported(caps, sdk.CapabilityReboot) {
    // Reboot is supported
}

// Validate required capabilities
required := map[string]bool{
    sdk.CapabilityDiscovery: true,
    sdk.CapabilityPairing:   true,
}

if err := sdk.ValidateCapabilities(required, caps); err != nil {
    return fmt.Errorf("missing required capabilities: %w", err)
}
```

### 2. Error Handling

```go
// Use SDK error types for consistent error reporting
func (d *MyDevice) StartMining(ctx context.Context) error {
    if !d.isConnected() {
        return NewErrorDeviceUnavailable(d.ID())
    }
    
    if err := d.sendStartCommand(); err != nil {
        return NewErrorDeviceUnavailable(d.ID(), err)
    }
    
    return nil
}

// For unsupported capabilities, use the appropriate error type
func (d *MyDevice) TryBatchStatus(ctx context.Context, ids []string) (map[string]DeviceStatusResponse, bool, error) {
    // Return supported=false instead of error for unsupported features
    return nil, false, nil
}
```

### 3. Context Handling

```go
// Always respect context cancellation
func (d *MyDevice) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
    select {
    case <-ctx.Done():
        return sdk.DeviceStatusResponse{}, ctx.Err()
    default:
    }
    
    // Use context in HTTP requests
    req, err := http.NewRequestWithContext(ctx, "GET", d.statusURL(), nil)
    if err != nil {
        return sdk.DeviceStatusResponse{}, err
    }
    
    // ... make request
}
```

### 4. Resource Management

```go
func (d *MyDevice) Close(ctx context.Context) error {
    // Close connections, cleanup resources
    if d.httpClient != nil {
        d.httpClient.CloseIdleConnections()
    }
    
    if d.connection != nil {
        return d.connection.Close()
    }
    
    return nil
}
```

### 5. Capability Declaration

```go
func (d *MyDevice) DescribeDevice(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
    caps := sdk.Capabilities{}
    
    // Only declare capabilities you actually support
    if d.supportsReboot() {
        caps[sdk.CapabilityReboot] = true
    }
    
    if d.supportsFirmwareUpdate() {
        caps[sdk.CapabilityFirmware] = true
    }
    
    return d.deviceInfo, caps, nil
}
```

### 6. Optional Capability Implementation

```go
func (d *MyDevice) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
    // Return supported=false if not implemented
    if !d.hasWebInterface() {
        return "", false, nil
    }
    
    url := fmt.Sprintf("http://%s:%d", d.deviceInfo.Host, d.deviceInfo.Port)
    return url, true, nil
}
```

## Troubleshooting

### Common Issues

#### 1. Plugin Not Loading

```bash
# Check plugin binary
./my-miner-plugin
# Should output: "This binary is a plugin. These are not meant to be executed directly."

# Check plugin handshake
PLUGIN_DEBUG=1 ./host-binary
```

#### 2. Authentication Failures

```go
// Debug authentication in your device methods
func (d *MyDevice) authenticateRequest(req *http.Request) error {
    switch kind := d.secret.Kind.(type) {
    case sdk.UsernamePassword:
        req.SetBasicAuth(kind.Username, kind.Password)
    case sdk.BearerToken:
        req.Header.Set("Authorization", "Bearer "+kind.Token)
    default:
        return fmt.Errorf("unsupported auth type: %T", kind)
    }
    return nil
}
```

#### 3. Status Reporting Issues

```go
// Ensure all required fields are set
func (d *MyDevice) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
    status := sdk.DeviceStatusResponse{
        DeviceID:  d.id,           // Required
        Timestamp: time.Now(),     // Required
        Summary:   "Mining",       // Required
        Health:    sdk.HealthHealthyActive, // Required
    }
    
    // Optional metrics - use pointers for nil-ability
    if hashrate := d.getCurrentHashrate(); hashrate > 0 {
        status.HashrateHS = &hashrate
    }
    
    return status, nil
}
```

### Debug Logging

```go
import "log/slog"

// Add structured logging to your driver
func (d *MyDevice) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
    slog.Debug("Fetching device status", 
        "device_id", d.id, 
        "host", d.deviceInfo.Host)
    
    // ... implementation
    
    slog.Info("Status retrieved successfully", 
        "device_id", d.id, 
        "health", status.Health)
    
    return status, nil
}
```

### Performance Monitoring

```go
import "time"

func (d *MyDevice) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
    start := time.Now()
    defer func() {
        duration := time.Since(start)
        slog.Debug("Status call completed", 
            "device_id", d.id, 
            "duration", duration)
    }()
    
    // ... implementation
}
```

## API Reference

### Version Information

The SDK provides version constants and utility functions:

```go
// Current SDK API version
const APIVersion = "v1.0.0"

// Capability validation utilities
func IsCapabilitySupported(caps Capabilities, capability string) bool
func ValidateCapabilities(required map[string]bool, caps Capabilities) error
```

For complete API documentation, see:

- [Driver Interface](./interface.go) - Core interfaces and types
- [Error Handling](./errors.go) - Standardized error types and constructors
- [Metric Values](./metric_values.go) - Type-safe metric value system
- [Version Utils](./version.go) - Version constants and capability validation
- [Plugin Implementation](./plugin.go) - gRPC plugin implementation
- [Protocol Buffers](./pb/) - Generated protobuf definitions

## Contributing

When contributing to the SDK:

1. **Maintain backward compatibility** in interface changes
2. **Add comprehensive tests** for new features
3. **Update documentation** for interface changes
4. **Follow Go conventions** for naming and structure
5. **Add examples** for new capabilities

## License

This SDK is part of the Fleet mining management system and follows the same license terms as the parent project.
