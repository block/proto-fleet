# CLAUDE.md - E2E Testing Guide for AI Agents

This file provides guidance for Claude Code when working with end-to-end tests in this directory.

## Overview

The e2e tests validate the complete Proto Fleet system using the **real docker-compose infrastructure**. These are system-level tests, not unit or integration tests.

**Critical Philosophy**: These tests interact with the system exactly as a real client would - through the Fleet API at `http://localhost:4000`. Never bypass the API layer or use internal test infrastructure (testutil) for e2e tests.

## Test Architecture Principles

### 1. System Testing vs Integration Testing

**E2E Tests (this directory)**:
- Test against real docker-compose environment
- Trigger `just clean-build` for clean state
- Use production API clients (Connect RPC)
- Validate complete workflows through public APIs
- Run with `-tags=e2e` build constraint

**Integration Tests (elsewhere)**:
- Use `internal/testutil` for isolated test infrastructure
- Create httptest servers with in-memory databases
- Test individual handlers and services
- Run as part of standard `go test ./...`

### 2. Never Mix Approaches

❌ **Wrong** - Mixed approach:
```go
func TestCompleteWorkflow(t *testing.T) {
    // Creates isolated testutil infrastructure
    testCtx := testutil.InitializeDBServiceInfrastructure(t)

    // But also expects docker-compose to be running
    conn, err := grpc.Dial("localhost:4000")

    // This is confusing and fragile!
}
```

✅ **Correct** - Pure e2e approach:
```go
func TestCompleteWorkflow(t *testing.T) {
    // Triggers real docker-compose environment
    exec.Command("just", "clean-build").Run()

    // Uses real API clients
    client := authv1connect.NewAuthServiceClient(http.DefaultClient, "http://localhost:4000")

    // Everything goes through public APIs
}
```

## File Structure

```
e2e/
├── CLAUDE.md                    # This file - guidance for AI agents
├── README.md                    # User-facing documentation
└── plugin_integration_test.go   # E2E test implementations
```

## Test Categories

### TestPluginIntegration

**Purpose**: Infrastructure validation - ensures docker-compose environment is healthy

**When to run**:
- Before making changes to plugin system
- To validate docker-compose configuration
- As a smoke test after `just clean-build`

**Assumes**: Docker-compose is already running (does NOT trigger clean-build)

**Key validations**:
- Docker containers are running
- Plugin binaries are correct architecture (ELF ARM64, not Mach-O)
- Plugins loaded successfully (check logs for gRPC protocol)
- Database tables exist
- API endpoints are accessible

### TestCompletePluginWorkflow

**Purpose**: End-to-end workflow validation - discovery → pairing → telemetry

**When to run**:
- To validate plugin functionality end-to-end
- After changes to pairing or telemetry services
- To verify proto-sim integration

**Triggers**: `just clean-build` to ensure clean environment

**Workflow**:
1. Reset environment (`just clean-build`)
2. Wait for fleet-api health
3. Create admin user via API
4. Authenticate to get JWT token
5. Discover device via `/pairing.v1.PairingService/Discover`
6. Pair device via `/pairing.v1.PairingService/Pair`
7. Poll for telemetry via `/telemetry.v1.TelemetryService/GetSnapshot`

## Important Patterns

### 1. Always Use Build Tags

All e2e tests MUST have the build tag:

```go
//go:build e2e

package e2e
```

Without this tag, tests run in regular CI and slow down development. With it, developers can choose when to run expensive e2e tests.

Run with:
```bash
go test -tags=e2e ./e2e
```

### 2. Authentication Pattern

Every API call (except health and onboarding) requires authentication:

```go
// Step 1: Create admin user (only once per environment)
createAdminViaAPI(t, ctx, username, password)

// Step 2: Authenticate to get token
token := authenticateViaRealAPI(t, ctx, username, password)

// Step 3: Add token to all subsequent requests
req := connect.NewRequest(&pairingv1.DiscoverRequest{...})
req.Header().Set("Authorization", "Bearer "+token)
```

### 3. Polling Pattern for Async Operations

Telemetry collection is asynchronous. Don't assume data is immediately available:

```go
// ❌ Wrong - assumes immediate availability
resp, err := client.GetSnapshot(ctx, req)
require.NotEmpty(t, resp.Msg.Telemetry) // May fail!

// ✅ Correct - polls until data available or timeout
deadline := time.Now().Add(30 * time.Second)
pollInterval := 2 * time.Second

for time.Now().Before(deadline) {
    resp, err := client.GetSnapshot(ctx, req)
    if err == nil && len(resp.Msg.Telemetry) > 0 {
        // Success!
        return resp.Msg
    }
    time.Sleep(pollInterval)
}
require.Fail(t, "timeout waiting for telemetry")
```

### 4. Subtest Organization

Use subtests to organize logical workflow steps:

```go
func TestCompleteWorkflow(t *testing.T) {
    // Shared setup
    ctx := context.Background()
    token := authenticate(...)

    var deviceID string

    // Step 1: Discovery
    t.Run("DiscoverDevice", func(t *testing.T) {
        devices := discover(...)
        deviceID = devices[0].DeviceIdentifier
        t.Logf("✓ Discovered: %s", deviceID)
    })

    // Step 2: Pairing (depends on discovery)
    t.Run("PairDevice", func(t *testing.T) {
        require.NotEmpty(t, deviceID, "must run after discovery")
        pair(deviceID)
        t.Logf("✓ Paired: %s", deviceID)
    })

    // Step 3: Telemetry (depends on pairing)
    t.Run("ValidateTelemetry", func(t *testing.T) {
        require.NotEmpty(t, deviceID, "must run after discovery")
        telemetry := pollForTelemetry(deviceID)
        t.Logf("✓ Telemetry: %d data points", len(telemetry))
    })
}
```

### 5. Helper Function Pattern

Extract API interactions into reusable helper functions:

```go
// waitForFleetAPIHealth waits for the Fleet API to be healthy
func waitForFleetAPIHealth(t *testing.T, ctx context.Context, timeout time.Duration) {
    // Implementation with polling logic
}

// createAdminViaAPI creates an admin user via the real API
func createAdminViaAPI(t *testing.T, ctx context.Context, username, password string) {
    client := onboardingv1connect.NewOnboardingServiceClient(http.DefaultClient, fleetAPIURL)
    req := connect.NewRequest(&onboardingv1.CreateAdminLoginRequest{
        Username: username,
        Password: password,
    })
    _, err := client.CreateAdminLogin(ctx, req)
    require.NoError(t, err, "admin user creation should succeed")
}

// authenticateViaRealAPI authenticates via the real API and returns JWT token
func authenticateViaRealAPI(t *testing.T, ctx context.Context, username, password string) string {
    client := authv1connect.NewAuthServiceClient(http.DefaultClient, fleetAPIURL)
    req := connect.NewRequest(&authv1.AuthenticateRequest{
        Username: username,
        Password: password,
    })
    resp, err := client.Authenticate(ctx, req)
    require.NoError(t, err, "authentication should succeed")
    require.NotEmpty(t, resp.Msg.Token, "token should not be empty")
    return resp.Msg.Token
}
```

**Benefits**:
- Tests read like documentation
- Helper functions can be reused across tests
- Easy to update if API changes
- Clear separation of concerns

## Common Pitfalls

### 1. Using testutil in E2E Tests

❌ **Don't**:
```go
import "github.com/btc-mining/proto-fleet/server/internal/testutil"

func TestE2E(t *testing.T) {
    // This creates an isolated test server, NOT e2e!
    testCtx := testutil.InitializeDBServiceInfrastructure(t)
}
```

✅ **Do**:
```go
func TestE2E(t *testing.T) {
    // Use real docker-compose environment
    exec.Command("just", "clean-build").Run()
    waitForFleetAPIHealth(t, ctx, 60*time.Second)
}
```

### 2. Bypassing API Layer

❌ **Don't**:
```go
func TestTelemetry(t *testing.T) {
    // Calling internal services directly bypasses API layer
    telemetry, err := testCtx.ServiceProvider.TelemetryService.GetSnapshot(...)
}
```

✅ **Do**:
```go
func TestTelemetry(t *testing.T) {
    // Use public API exactly as clients would
    client := telemetryv1connect.NewTelemetryServiceClient(http.DefaultClient, "http://localhost:4000")
    req := connect.NewRequest(&telemetryv1.GetSnapshotRequest{...})
    req.Header().Set("Authorization", "Bearer "+token)
    resp, err := client.GetSnapshot(ctx, req)
}
```

### 3. Forgetting Build Tags

❌ **Don't**:
```go
package e2e

import "testing"

func TestE2E(t *testing.T) {
    // Missing build tag! This will run in regular CI and slow down tests
}
```

✅ **Do**:
```go
//go:build e2e

package e2e

import "testing"

func TestE2E(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping e2e test in short mode")
    }
    // Test implementation
}
```

### 4. Not Handling Async Operations

❌ **Don't**:
```go
func TestTelemetry(t *testing.T) {
    // Pairing triggers async telemetry collection
    pair(deviceID)

    // Immediately checking - will likely fail!
    resp, err := client.GetSnapshot(ctx, req)
    require.NotEmpty(t, resp.Msg.Telemetry) // ❌ Race condition
}
```

✅ **Do**:
```go
func TestTelemetry(t *testing.T) {
    pair(deviceID)

    // Poll with timeout
    telemetry := pollForTelemetryViaRealAPI(t, ctx, token, deviceID, 30*time.Second)
    require.NotEmpty(t, telemetry.Telemetry) // ✅ Handles async
}
```

### 5. Hardcoding Proto Structure Assumptions

❌ **Don't**:
```go
// Assuming structure without checking proto definitions
req := &telemetryv1.GetSnapshotRequest{
    DeviceSelector: &telemetryv1.DeviceSelector{ // Wrong field!
        DeviceList: &telemetryv1.DeviceList{
            DeviceIds: []string{deviceID},
        },
    },
}
```

✅ **Do**:
```go
// Check proto definition first: proto/telemetry/v1/telemetry.proto
// GetSnapshotRequest has "repeated string device_ids", not DeviceSelector
req := &telemetryv1.GetSnapshotRequest{
    DeviceIds: []string{deviceID},
    MeasurementTypes: []telemetryv1.MeasurementType{
        telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
    },
}
```

**Always verify proto definitions** before constructing requests. Generated Go code matches proto exactly.

## Debugging E2E Test Failures

### Step 1: Check Docker Containers

```bash
# Are all containers running?
docker ps | grep server-

# Expected containers:
# - server-fleet-api-1
# - server-proto-sim-1
# - server-timescaledb-1
```

### Step 2: Check Fleet API Logs

```bash
# View recent logs
docker logs server-fleet-api-1 --tail 100

# Follow logs in real-time
docker logs -f server-fleet-api-1

# Look for:
# - "plugin started: path=/app/plugins/proto-plugin" (plugin loaded)
# - "plugin.proto-plugin: plugin address:" (gRPC connection)
# - "network=unix" (correct protocol)
# - "Migrating database" (database initialized)
```

### Step 3: Check Plugin Binary Architecture

```bash
# Plugins MUST be Linux ARM64 ELF binaries (for Docker)
file plugins/proto-plugin

# ✅ Correct output:
# plugins/proto-plugin: ELF 64-bit LSB executable, ARM aarch64

# ❌ Wrong output (will fail):
# plugins/proto-plugin: Mach-O 64-bit executable arm64
```

If wrong architecture, rebuild:
```bash
just build-plugins
```

### Step 4: Check Database State

```bash
# Connect to database
just db-shell

# Check discovered devices
SELECT * FROM discovered_device;

# Check paired devices
SELECT * FROM device;

# Check users
SELECT * FROM user;
```

### Step 5: Check Network Connectivity

```bash
# Can fleet-api reach proto-sim?
docker exec server-fleet-api-1 ping server-proto-sim-1

# Can host reach fleet-api?
curl http://localhost:4000/health

# Can host reach proto-sim?
curl http://localhost:2121/health
```

### Step 6: Test API Manually

Use the test credentials to manually verify APIs:

```bash
# Health check (no auth)
curl http://localhost:4000/health

# Create admin (if needed)
curl -X POST http://localhost:4000/onboarding.v1.OnboardingService/CreateAdminLogin \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"proto"}'

# Authenticate
curl -X POST http://localhost:4000/auth.v1.AuthService/Authenticate \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"proto"}'
```

## Adding New E2E Tests

### Checklist

When adding a new e2e test:

1. ✅ Add `//go:build e2e` tag
2. ✅ Add `testing.Short()` skip guard
3. ✅ Use real API clients (not testutil)
4. ✅ Handle authentication (create admin, get token)
5. ✅ Use polling for async operations
6. ✅ Add descriptive log messages (`t.Logf()`)
7. ✅ Use subtests for logical grouping (`t.Run()`)
8. ✅ Extract reusable logic into helper functions
9. ✅ Add timeout contexts to prevent hanging
10. ✅ Update README.md with new test documentation

### Template for New E2E Test

```go
//go:build e2e

package e2e

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    // Import generated API clients
)

// TestMyNewFeature validates [describe what it tests]
//
// Prerequisites: Docker-compose environment running (or trigger clean-build)
//
// The test validates:
// 1. [First thing]
// 2. [Second thing]
// 3. [Third thing]
func TestMyNewFeature(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping e2e test in short mode")
    }

    ctx := context.Background()

    // Optional: Trigger clean build if you need fresh state
    // exec.Command("just", "clean-build").Run()
    // waitForFleetAPIHealth(t, ctx, 60*time.Second)

    // Authenticate
    createAdminViaAPI(t, ctx, testUsername, testPassword)
    token := authenticateViaRealAPI(t, ctx, testUsername, testPassword)

    // Test workflow
    t.Run("FirstStep", func(t *testing.T) {
        // Test implementation
        // Use real API clients with token
        t.Logf("✓ First step completed")
    })

    t.Run("SecondStep", func(t *testing.T) {
        // Test implementation
        // May depend on first step results
        t.Logf("✓ Second step completed")
    })
}

// Helper functions follow...
```

## API Client Patterns

### Creating Clients

```go
import (
    "net/http"
    authv1connect "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
    pairingv1connect "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
    telemetryv1connect "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1/telemetryv1connect"
)

const fleetAPIURL = "http://localhost:4000"

// Create clients (lightweight, can create per-request or reuse)
authClient := authv1connect.NewAuthServiceClient(http.DefaultClient, fleetAPIURL)
pairingClient := pairingv1connect.NewPairingServiceClient(http.DefaultClient, fleetAPIURL)
telemetryClient := telemetryv1connect.NewTelemetryServiceClient(http.DefaultClient, fleetAPIURL)
```

### Making Requests (Unary RPC)

```go
import (
    "connectrpc.com/connect"
    authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
)

// Create request
req := connect.NewRequest(&authv1.AuthenticateRequest{
    Username: "admin",
    Password: "proto",
})

// Add auth header (if needed)
req.Header().Set("Authorization", "Bearer "+token)

// Make request
resp, err := authClient.Authenticate(ctx, req)
require.NoError(t, err)

// Access response
token := resp.Msg.Token
```

### Making Requests (Server Streaming RPC)

```go
import (
    pairingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
)

// Create request
req := connect.NewRequest(&pairingv1.DiscoverRequest{
    Mode: &pairingv1.DiscoverRequest_IpList{
        IpList: &pairingv1.IPListModeRequest{
            IpAddresses: []string{"127.0.0.1"},
            Ports:       []string{"2121"},
        },
    },
})
req.Header().Set("Authorization", "Bearer "+token)

// Get stream
stream, err := pairingClient.Discover(ctx, req)
require.NoError(t, err)

// Receive messages
var devices []*pairingv1.Device
for stream.Receive() {
    msg := stream.Msg()
    devices = append(devices, msg.Devices...)
}

// Check for errors
require.NoError(t, stream.Err())
```

## Constants Reference

```go
const (
    // Fleet API endpoint
    fleetAPIURL = "http://localhost:4000"

    // Proto-sim connection details
    protoSimIP       = "127.0.0.1"  // localhost from host
    protoSimPort     = "2121"       // gRPC port
    protoSimHTTPPort = "8080"       // HTTP API port

    // Default test credentials
    testUsername = "admin"
    testPassword = "proto"

    // Timeouts
    requestTimeout = 10 * time.Second
    healthTimeout  = 60 * time.Second
    telemetryPollTimeout = 30 * time.Second

    // Docker container prefix
    containerPrefix = "server-"
)
```

## Proto File References

When working with API requests/responses, always reference proto definitions:

- **Auth**: `../../proto/auth/v1/auth.proto`
- **Onboarding**: `../../proto/onboarding/v1/onboarding.proto`
- **Pairing**: `../../proto/pairing/v1/pairing.proto`
- **Telemetry**: `../../proto/telemetry/v1/telemetry.proto`
- **Command**: `../../proto/minercommand/v1/minercommand.proto`

Generated Go code is in:
- `../generated/grpc/{service}/v1/` - message types
- `../generated/grpc/{service}/v1/{service}v1connect/` - client interfaces

## Performance Considerations

### Test Duration

- `TestPluginIntegration`: ~10-20 seconds (assumes docker-compose running)
- `TestCompletePluginWorkflow`: ~2-5 minutes (includes clean-build)

### Optimizations

**Don't trigger clean-build unnecessarily**:
```go
// ❌ Slow - rebuilds everything
func TestQuickValidation(t *testing.T) {
    exec.Command("just", "clean-build").Run()
    // ... quick test ...
}

// ✅ Fast - assumes environment is healthy
func TestQuickValidation(t *testing.T) {
    // Just check if it's already running
    waitForFleetAPIHealth(t, ctx, 10*time.Second)
    // ... quick test ...
}
```

**Use Go test caching**:
```bash
# First run: Full execution
go test -tags=e2e ./e2e

# Second run: Cached (if no changes)
go test -tags=e2e ./e2e
# PASS (cached)
```

**Reduce polling timeouts for faster feedback**:
```go
// Development: Faster feedback
telemetry := pollForTelemetryViaRealAPI(t, ctx, token, deviceID, 10*time.Second)

// CI/Production: More patient
telemetry := pollForTelemetryViaRealAPI(t, ctx, token, deviceID, 60*time.Second)
```

## Related Documentation

- **User-facing docs**: `README.md` (this directory)
- **Server development**: `../CLAUDE.md`
- **Project overview**: `../../CLAUDE.md`
- **API definitions**: `../../proto/`
- **Plugin guide**: `../../plugin/CLAUDE.md` (if exists)

## Summary for Claude Code

When working with e2e tests:

1. **Always use real docker-compose environment** - never testutil
2. **Always use public APIs** - never internal services
3. **Always include `//go:build e2e`** - keep tests optional
4. **Always authenticate** - most APIs require JWT tokens
5. **Always poll async operations** - don't assume immediate availability
6. **Always check proto definitions** - don't guess request structure
7. **Always add descriptive logs** - help debug failures
8. **Always use helper functions** - keep tests readable

The goal is to test the system **exactly as users would experience it** - through the Fleet API at `http://localhost:4000`.
