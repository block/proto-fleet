# Getting Started with Fleet Plugin Development

This guide will walk you through creating a Fleet plugin using the Proto plugin as a reference.

## Overview

Fleet plugins are Go programs that implement the Fleet SDK interface to manage mining devices. They communicate with the Fleet server using the go-plugin framework over gRPC.

## Quick Start

### 1. Understanding the Basics

Every Fleet plugin consists of two main components:

- **Driver**: Manages plugin lifecycle, device discovery, and device creation
- **Device**: Manages individual miner instances and operations

### 2. Plugin Structure

The Proto plugin demonstrates a production-ready structure:

```
plugin/proto/
├── main.go                    # Plugin entry point
├── internal/                  # Internal implementation
│   ├── driver/               # Driver implementation
│   └── device/               # Device implementation
├── pkg/                      # Reusable packages
│   └── proto/                # Proto-specific API client
└── docs/                     # Documentation
```

## Core Concepts

### Driver Interface

The Driver interface handles:

```go
type Driver interface {
    // Plugin identification
    Handshake(ctx context.Context) (DriverIdentifier, error)
    DescribeDriver(ctx context.Context) (DriverIdentifier, Capabilities, error)
    
    // Device management
    DiscoverDevice(ctx context.Context, ipAddress, port string) (DeviceInfo, error)
    PairDevice(ctx context.Context, device DeviceInfo, access SecretBundle) (string, error)
    NewDevice(ctx context.Context, deviceID string, deviceInfo DeviceInfo, secret SecretBundle) (NewDeviceResult, error)
}
```

### Device Interface

The Device interface handles:

```go
type Device interface {
    // Core operations
    ID() string
    DescribeDevice(ctx context.Context) (DeviceInfo, Capabilities, error)
    Status(ctx context.Context) (DeviceStatusResponse, error)
    Close(ctx context.Context) error
    
    // Control operations
    StartMining(ctx context.Context) error
    StopMining(ctx context.Context) error
    SetCoolingMode(ctx context.Context, mode CoolingMode) error
    UpdateMiningPools(ctx context.Context, pools []MiningPoolConfig) error
    BlinkLED(ctx context.Context) error
    
    // Maintenance operations
    DownloadLogs(ctx context.Context, since *time.Time, batchLogUUID string) (string, bool, error)
    Reboot(ctx context.Context) error
    FirmwareUpdate(ctx context.Context) error
    
    // Optional capabilities
    TryBatchStatus(ctx context.Context, ids []string) (map[string]DeviceStatusResponse, bool, error)
    TrySubscribe(ctx context.Context, ids []string) (<-chan DeviceStatusResponse, bool, error)
    TryGetWebViewURL(ctx context.Context) (string, bool, error)
    TryGetTimeSeriesData(ctx context.Context, ...) ([]DeviceStatusResponse, string, bool, error)
}
```

## Implementation Steps

### Step 1: Create the Main Entry Point

```go
package main

import (
    "github.com/block/proto-fleet/server/sdk/v1"
    "github.com/hashicorp/go-plugin"
)

func main() {
    driver := &MyDriver{} // Your driver implementation
    
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: sdk.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "driver": &sdk.DriverPlugin{Impl: driver},
        },
    })
}
```

### Step 2: Implement the Driver

```go
type MyDriver struct {
    devices map[string]sdk.Device
    mutex   sync.RWMutex
}

func (d *MyDriver) Handshake(ctx context.Context) (sdk.DriverIdentifier, error) {
    return sdk.DriverIdentifier{
        DriverName: "my-plugin",
        APIVersion: "v1",
    }, nil
}

func (d *MyDriver) DescribeDriver(ctx context.Context) (sdk.DriverIdentifier, sdk.Capabilities, error) {
    handshake, _ := d.Handshake(ctx)
    capabilities := sdk.Capabilities{
        sdk.CapabilityPollingHost: true, // We support status polling
        sdk.CapabilityDiscovery:   true, // We can discover devices
        // Add other capabilities as needed
    }
    return handshake, capabilities, nil
}

// Implement other Driver methods...
```

### Step 3: Implement the Device

```go
type MyDevice struct {
    id         string
    deviceInfo sdk.DeviceInfo
    // Add your device-specific fields
}

func (d *MyDevice) ID() string {
    return d.id
}

func (d *MyDevice) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
    return sdk.DeviceStatusResponse{
        DeviceID:  d.id,
        Timestamp: time.Now(),
        Summary:   "Running",
        Health:    sdk.HealthHealthyActive,
        // Add telemetry data
    }, nil
}

// Implement other Device methods...
```

## Best Practices

### 1. Error Handling

Always provide meaningful error messages:

```go
if err != nil {
    return fmt.Errorf("failed to connect to miner at %s:%d: %w", host, port, err)
}
```

### 2. Logging

Use structured logging:

```go
slog.Info("Device discovered", "serial", device.SerialNumber, "host", device.Host)
slog.Error("Failed to start mining", "deviceID", deviceID, "error", err)
```

### 3. Resource Management

Always clean up resources:

```go
func (d *MyDevice) Close(ctx context.Context) error {
    if d.client != nil {
        d.client.Close()
    }
    return nil
}
```

### 4. Concurrent Safety

Use mutexes for shared state:

```go
type Driver struct {
    devices map[string]sdk.Device
    mutex   sync.RWMutex
}

func (d *Driver) addDevice(id string, device sdk.Device) {
    d.mutex.Lock()
    defer d.mutex.Unlock()
    d.devices[id] = device
}
```

### 5. Status Caching

Cache expensive operations:

```go
type Device struct {
    lastStatus   *sdk.DeviceStatusResponse
    lastStatusAt time.Time
    statusTTL    time.Duration
}

func (d *Device) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
    if d.lastStatus != nil && time.Since(d.lastStatusAt) < d.statusTTL {
        return *d.lastStatus, nil
    }
    // Fetch fresh status...
}
```

## Testing Your Plugin

### Unit Tests

Create unit tests for your components:

```go
func TestDriverHandshake(t *testing.T) {
    driver := NewMyDriver()
    handshake, err := driver.Handshake(context.Background())
    assert.NoError(t, err)
    assert.Equal(t, "my-plugin", handshake.DriverName)
}
```

### Integration Tests

Test with real devices when possible:

```go
func TestDeviceDiscovery(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }
    
    driver := NewMyDriver()
    device, err := driver.DiscoverDevice(context.Background(), "192.168.1.100", "443")
    assert.NoError(t, err)
    assert.NotEmpty(t, device.SerialNumber)
}
```

### Manual Testing

Build and test your plugin:

```bash
go build -o my-plugin
./my-plugin &
# Test with Fleet server or plugin test tools
```

## Common Patterns

### Configuration

Use environment variables for configuration:

```go
func getConfig() Config {
    return Config{
        Timeout:     getEnvDuration("PLUGIN_TIMEOUT", 30*time.Second),
        MaxRetries:  getEnvInt("PLUGIN_MAX_RETRIES", 3),
        SkipTLSVerify: getEnvBool("SKIP_TLS_VERIFY", false),
    }
}
```

### Authentication

Handle different authentication types:

```go
func extractCredentials(secret sdk.SecretBundle) (string, error) {
    switch kind := secret.Kind.(type) {
    case sdk.BearerToken:
        return kind.Token, nil
    case sdk.APIKey:
        return kind.Key, nil
    case sdk.UsernamePassword:
        return kind.Password, nil
    default:
        return "", fmt.Errorf("unsupported auth type: %T", secret.Kind)
    }
}
```

### Health Status Mapping

Map device states to SDK health status:

```go
func mapHealthStatus(deviceState string) sdk.HealthStatus {
    switch deviceState {
    case "mining":
        return sdk.HealthHealthyActive
    case "idle":
        return sdk.HealthyInactive
    case "error":
        return sdk.Critical
    case "warning":
        return sdk.Warning
    default:
        return sdk.Unknown
    }
}
```

## Next Steps

1. Study the full Proto plugin implementation
2. Adapt the patterns to your specific miner type
3. Implement device discovery for your protocol
4. Add comprehensive error handling and logging
5. Create thorough tests
6. Document your plugin's specific requirements

For more detailed patterns and implementation details, see the full implementation in the main plugin files.
