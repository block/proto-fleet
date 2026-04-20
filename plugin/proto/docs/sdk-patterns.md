# Fleet SDK Patterns and Best Practices

This document describes common patterns and best practices when working with the Fleet SDK v1.

## Core Patterns

### 1. Plugin Lifecycle

```go
// Plugin startup sequence
func main() {
    // 1. Initialize your driver
    driver, err := NewMyDriver()
    if err != nil {
        log.Fatalf("Failed to create driver: %v", err)
    }
    
    // 2. Serve the plugin
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: sdk.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "driver": &sdk.DriverPlugin{Impl: driver},
        },
    })
}
```

### 2. Capability Declaration

Always declare capabilities accurately:

```go
func (d *Driver) DescribeDriver(ctx context.Context) (sdk.DriverIdentifier, sdk.Capabilities, error) {
    capabilities := sdk.Capabilities{
        // Core capabilities - implement these first
        sdk.CapabilityPollingHost: true,  // Status polling
        sdk.CapabilityDiscovery:   true,  // Device discovery
        sdk.CapabilityPairing:     true,  // Device pairing
        
        // Management capabilities
        sdk.CapabilityReboot:     true,   // Device reboot
        sdk.CapabilityFirmware:   false,  // Firmware updates (not implemented)
        sdk.CapabilityPoolConfig: true,   // Pool configuration
        
        // Advanced capabilities
        sdk.CapabilityPollingPlugin: false, // Plugin-side polling
        sdk.CapabilityBatchStatus:   false, // Batch operations
        sdk.CapabilityStreaming:     false, // Real-time streaming
    }
    
    handshake, _ := d.Handshake(ctx)
    return handshake, capabilities, nil
}
```

### 3. Device Discovery Pattern

```go
func (d *Driver) DiscoverDevice(ctx context.Context, ipAddress, port string) (sdk.DeviceInfo, error) {
    // 1. Validate input parameters
    if net.ParseIP(ipAddress) == nil {
        return sdk.DeviceInfo{}, fmt.Errorf("invalid IP address: %s", ipAddress)
    }
    
    portInt, err := strconv.Atoi(port)
    if err != nil || portInt < 1 || portInt > 65535 {
        return sdk.DeviceInfo{}, fmt.Errorf("invalid port: %s", port)
    }
    
    // 2. Try different connection methods
    schemes := []string{"https", "http"}
    for _, scheme := range schemes {
        if deviceInfo, err := d.tryDiscoverWithScheme(ctx, ipAddress, portInt, scheme); err == nil {
            return deviceInfo, nil
        }
    }
    
    return sdk.DeviceInfo{}, fmt.Errorf("failed to discover device at %s:%s", ipAddress, port)
}

func (d *Driver) tryDiscoverWithScheme(ctx context.Context, ip string, port int, scheme string) (sdk.DeviceInfo, error) {
    // Create client with timeout
    client, err := NewClientWithTimeout(ip, port, scheme, 10*time.Second)
    if err != nil {
        return sdk.DeviceInfo{}, err
    }
    defer client.Close()
    
    // Get device identification
    info, err := client.GetDeviceInfo(ctx)
    if err != nil {
        return sdk.DeviceInfo{}, err
    }
    
    // Validate required fields
    if info.SerialNumber == "" {
        return sdk.DeviceInfo{}, fmt.Errorf("device missing serial number")
    }
    
    return sdk.DeviceInfo{
        Host:         ip,
        Port:         int32(port),
        URLScheme:    scheme,
        SerialNumber: info.SerialNumber,
        Model:        info.Model,
        Manufacturer: info.Manufacturer,
        Type:         sdk.DeviceTypeASIC, // or appropriate type
        MacAddress:   info.MacAddress,
    }, nil
}
```

### 4. Device Status Pattern

```go
func (d *Device) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
    // 1. Check cache first
    if d.isCacheValid() {
        return *d.cachedStatus, nil
    }
    
    // 2. Fetch fresh data
    status, err := d.fetchStatus(ctx)
    if err != nil {
        return sdk.DeviceStatusResponse{}, err
    }
    
    // 3. Cache the result
    d.cacheStatus(status)
    
    return status, nil
}

func (d *Device) fetchStatus(ctx context.Context) (sdk.DeviceStatusResponse, error) {
    // Get basic status
    minerStatus, err := d.client.GetStatus(ctx)
    if err != nil {
        return sdk.DeviceStatusResponse{}, fmt.Errorf("failed to get status: %w", err)
    }
    
    // Get telemetry (optional - don't fail if unavailable)
    telemetry, _ := d.client.GetTelemetry(ctx)
    
    // Build response
    response := sdk.DeviceStatusResponse{
        DeviceID:  d.id,
        Timestamp: time.Now(),
        Summary:   d.mapSummary(minerStatus.State),
        Health:    d.mapHealth(minerStatus.State),
        Metadata: map[string]string{
            "state": minerStatus.State,
        },
    }
    
    // Add telemetry if available
    if telemetry != nil {
        response.HashrateHS = &telemetry.HashrateHS
        response.PowerWatts = &telemetry.PowerWatts
        response.TemperatureCelsius = &telemetry.TemperatureCelsius
    }
    
    return response, nil
}
```

### 5. Error Handling Pattern

```go
// Define custom error types
type DeviceError struct {
    DeviceID string
    Op       string
    Err      error
}

func (e *DeviceError) Error() string {
    return fmt.Sprintf("device %s: %s failed: %v", e.DeviceID, e.Op, e.Err)
}

func (e *DeviceError) Unwrap() error {
    return e.Err
}

// Use in methods
func (d *Device) StartMining(ctx context.Context) error {
    if err := d.client.StartMining(ctx); err != nil {
        return &DeviceError{
            DeviceID: d.id,
            Op:       "start_mining",
            Err:      err,
        }
    }
    
    // Invalidate status cache
    d.invalidateCache()
    
    return nil
}
```

### 6. Authentication Pattern

```go
type AuthManager struct {
    credentials map[string]string
    mutex       sync.RWMutex
}

func (a *AuthManager) SetCredentials(deviceID string, secret sdk.SecretBundle) error {
    creds, err := a.extractCredentials(secret)
    if err != nil {
        return err
    }
    
    a.mutex.Lock()
    defer a.mutex.Unlock()
    a.credentials[deviceID] = creds
    
    return nil
}

func (a *AuthManager) extractCredentials(secret sdk.SecretBundle) (string, error) {
    switch kind := secret.Kind.(type) {
    case sdk.BearerToken:
        return kind.Token, nil
    case sdk.APIKey:
        return kind.Key, nil
    case sdk.TLSClientCert:
        return string(kind.ClientCertPEM), nil
    case sdk.UsernamePassword:
        return fmt.Sprintf("%s:%s", kind.Username, kind.Password), nil
    default:
        return "", fmt.Errorf("unsupported credential type: %T", secret.Kind)
    }
}

func (a *AuthManager) GetCredentials(deviceID string) string {
    a.mutex.RLock()
    defer a.mutex.RUnlock()
    return a.credentials[deviceID]
}
```

### 7. Configuration Pattern

```go
type Config struct {
    Timeout       time.Duration
    MaxRetries    int
    SkipTLSVerify bool
    LogLevel      string
}

func LoadConfig() Config {
    return Config{
        Timeout:       getEnvDuration("PLUGIN_TIMEOUT", 30*time.Second),
        MaxRetries:    getEnvInt("PLUGIN_MAX_RETRIES", 3),
        SkipTLSVerify: getEnvBool("SKIP_TLS_VERIFY", false),
        LogLevel:      getEnvString("LOG_LEVEL", "info"),
    }
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
    if value := os.Getenv(key); value != "" {
        if duration, err := time.ParseDuration(value); err == nil {
            return duration
        }
    }
    return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
    if value := os.Getenv(key); value != "" {
        if intValue, err := strconv.Atoi(value); err == nil {
            return intValue
        }
    }
    return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
    if value := os.Getenv(key); value != "" {
        if boolValue, err := strconv.ParseBool(value); err == nil {
            return boolValue
        }
    }
    return defaultValue
}
```

### 8. Telemetry Collection Pattern

```go
func (d *Device) collectTelemetry(ctx context.Context) (*Telemetry, error) {
    var wg sync.WaitGroup
    var mu sync.Mutex
    telemetry := &Telemetry{}
    errors := make([]error, 0)
    
    // Collect different metrics concurrently
    metrics := []struct {
        name string
        fn   func(context.Context) (float64, error)
        set  func(float64)
    }{
        {"hashrate", d.client.GetHashrate, func(v float64) { telemetry.HashrateHS = v }},
        {"power", d.client.GetPower, func(v float64) { telemetry.PowerWatts = v }},
        {"temperature", d.client.GetTemperature, func(v float64) { telemetry.TemperatureCelsius = v }},
    }
    
    for _, metric := range metrics {
        wg.Add(1)
        go func(m struct {
            name string
            fn   func(context.Context) (float64, error)
            set  func(float64)
        }) {
            defer wg.Done()
            
            if value, err := m.fn(ctx); err == nil {
                m.set(value)
            } else {
                mu.Lock()
                errors = append(errors, fmt.Errorf("%s: %w", m.name, err))
                mu.Unlock()
            }
        }(metric)
    }
    
    wg.Wait()
    
    // Log errors but don't fail - partial telemetry is better than none
    for _, err := range errors {
        slog.Warn("Telemetry collection failed", "error", err)
    }
    
    return telemetry, nil
}
```

### 9. Retry Pattern

```go
func (d *Device) withRetry(ctx context.Context, operation string, fn func() error) error {
    var lastErr error
    
    for attempt := 1; attempt <= d.config.MaxRetries; attempt++ {
        if err := fn(); err == nil {
            return nil
        } else {
            lastErr = err
            
            if attempt < d.config.MaxRetries {
                backoff := time.Duration(attempt) * time.Second
                slog.Warn("Operation failed, retrying",
                    "operation", operation,
                    "attempt", attempt,
                    "error", err,
                    "backoff", backoff)
                
                select {
                case <-time.After(backoff):
                    continue
                case <-ctx.Done():
                    return ctx.Err()
                }
            }
        }
    }
    
    return fmt.Errorf("operation %s failed after %d attempts: %w", 
        operation, d.config.MaxRetries, lastErr)
}

// Usage
func (d *Device) StartMining(ctx context.Context) error {
    return d.withRetry(ctx, "start_mining", func() error {
        return d.client.StartMining(ctx)
    })
}
```

### 10. Graceful Shutdown Pattern

```go
type Plugin struct {
    driver   *Driver
    shutdown chan struct{}
    wg       sync.WaitGroup
}

func (p *Plugin) Start() {
    // Start background tasks
    p.wg.Add(1)
    go p.healthChecker()
    
    // Serve the plugin
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: sdk.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "driver": &sdk.DriverPlugin{Impl: p.driver},
        },
    })
}

func (p *Plugin) Stop() {
    close(p.shutdown)
    p.wg.Wait()
}

func (p *Plugin) healthChecker() {
    defer p.wg.Done()
    
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            p.checkDeviceHealth()
        case <-p.shutdown:
            return
        }
    }
}
```

## Advanced Patterns

### Optional Capability Implementation

```go
func (d *Device) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
    // Check if device supports web interface
    if !d.hasWebInterface() {
        return "", false, nil // Not supported
    }
    
    url := fmt.Sprintf("%s://%s:%d", d.scheme, d.host, d.port)
    return url, true, nil // Supported and successful
}

func (d *Device) TryBatchStatus(ctx context.Context, ids []string) (map[string]sdk.DeviceStatusResponse, bool, error) {
    // This device doesn't support batch operations
    return nil, false, nil
}
```

### Metric Value Creation

```go
func createMetrics(telemetry *Telemetry) []sdk.Metric {
    now := time.Now()
    metrics := make([]sdk.Metric, 0)
    
    if telemetry.UptimeSeconds > 0 {
        metrics = append(metrics, sdk.Metric{
            Name:       "uptime_seconds",
            Value:      sdk.NewMetricValue(telemetry.UptimeSeconds),
            Unit:       sdk.UnitUnspecified,
            Kind:       sdk.MetricKindCounter,
            ObservedAt: now,
            Labels: map[string]string{
                "component": "system",
            },
        })
    }
    
    if telemetry.ErrorCount > 0 {
        metrics = append(metrics, sdk.Metric{
            Name:       "error_count",
            Value:      sdk.NewMetricValue(int(telemetry.ErrorCount)),
            Unit:       sdk.UnitUnspecified,
            Kind:       sdk.MetricKindCounter,
            ObservedAt: now,
            Labels: map[string]string{
                "component": "miner",
            },
        })
    }
    
    return metrics
}
```

These patterns provide a solid foundation for building robust, maintainable Fleet plugins. Always prioritize error handling, logging, and graceful degradation in your implementations.
