# Proto Miner Plugin

A Fleet plugin for the Proto mining system, implementing SDK v1 with credentials-based authentication and testing.

## Overview

This plugin provides Proto miner integration for the Fleet mining system. It demonstrates:

- Architecture with separation of concerns
- Patterns for error handling, logging, and testing
- Credentials-based authentication and token refresh
- Testing with unit tests, integration tests, and containerized testing
- Compatibility with Proto sim-miners and production devices

## Quick Start

Build and run the plugin:

```bash
go build -o proto-plugin
./proto-plugin
```

## Project Structure

```
plugin/proto/                  # Plugin root
├── main.go                    # Plugin entry point
├── go.mod                     # Module definition
├── README.md                  # This file
├── justfile                   # Build automation
├── .golangci.yaml            # Linting configuration
│
├── internal/                  # Internal implementation
│   ├── driver/               # Driver implementation
│   │   └── driver.go         # SDK Driver interface, rig and CT1 discovery
│   ├── device/               # Proto rig device implementation
│   │   └── device.go         # SDK Device interface
│   └── ct1/                  # CT1 container implementation (MDK v2 gRPC)
│
├── pkg/                      # Reusable packages
│   └── proto/                # Proto rig API client (MDK v1 REST)
│       └── client.go         # Miner communication with auth
│
├── docs/                     # Documentation
│   ├── getting-started.md    # Plugin development guide
│   ├── sdk-patterns.md       # SDK usage patterns
│   └── integration-testing.md # Testing guide
│
└── tests/                    # Test files
    ├── unit/                 # Unit tests
    │   └── plugin_test.go    # Core functionality tests
    └── integration_test.go   # Rig lifecycle test with a simulator container
```

Each package in `internal/` and `pkg/` also has its own `*_test.go` files.

## Key Features

### ✅ **Features**
- Device discovery and pairing with username/password credentials
- Token-based session management with automatic re-login on expiry
- Mining control (start/stop) with real-time status monitoring
- Comprehensive telemetry collection (hashrate, power, temperature)
- Pool configuration with priority-based failover
- Device management (reboot, firmware updates, LED identification)
- Log retrieval with filtering and pagination
- Web view URL generation for device management interfaces
- HTTPS and HTTP/2 support for rigs; optional TLS for CT1 gRPC

R2 status polling reads firmware version and optional system uptime together on
the first poll, on the first successful poll after an unreachable one, and every
five minutes afterward. Firmware install-status polls read the same system
response and refresh that cache, so the next status reports the uptime sampled
with the install state. Uptime retains the original
request-start timestamp between probes so Fleet can safely recognize an external
reboot without treating cached uptime as a new observation. A missing, invalid,
or failed uptime read supplies no reboot evidence.

### 🔐 **Authentication & Security**
- Username/password login with factory-default auto-pairing (`admin`/`proto`)
- Cached access tokens with automatic re-login when the rig rejects a token
- Default-password lockout handling via the `UpdateMinerPassword` flow
- Encrypted rig HTTPS without certificate verification, because rigs commonly
  present self-signed certificates
- Secure credential handling through SDK SecretBundle interface

### 🧪 **Testing & Quality**
- Unit tests for authentication, client operations, and core functionality
- Integration tests against the `fake-proto-rig` and `fake-ct1` simulators
- Error handling and edge case coverage

### 📚 **Documentation**
- Progressive complexity: Start simple, add features gradually
- Documentation: Every pattern explained with real examples
- Architecture: Easy to understand and modify
- Patterns: Error handling, logging, testing, and security
- Reusable components: Auth utilities, client libraries, test helpers

### 🔧 **Developer Experience**
- Clear entry points: Obvious starting points for different needs
- Documentation: Multiple levels of guidance
- Working implementation: Production-ready code
- Test coverage: Unit tests and benchmarks

## Usage Patterns

### For Learning

1. **Read the getting started guide**: `docs/getting-started.md`
2. **Study SDK patterns**: `docs/sdk-patterns.md`
3. **Explore the implementation**: `internal/` and `pkg/`

### For Development

1. **Copy the structure**: Use as a template for new plugins
2. **Adapt the client**: Modify `pkg/proto/client.go` for your miner API
3. **Update discovery**: Change `internal/driver/driver.go` for your protocol
4. **Customize device logic**: Modify `internal/device/device.go` for your features

### For Production

1. **Build the plugin**: `go build -o proto-plugin`
2. **Configure environment**: Set the [environment variables](#environment-variables) that the deployment needs
3. **Deploy with Fleet**: Reference the binary in Fleet configuration
4. **Monitor logs**: Check for authentication and connectivity issues

## Configuration

### Environment Variables

All variables are optional.

- `PROTO_MINER_PORT` - Restrict discovery to one port (see [Device Discovery](#device-discovery))
- `PROTO_STATUS_CACHE_TTL` - Rig status cache lifetime as a Go duration; default `5s`
- `CT1_WEB_PORT` - Web port for newly discovered CT1s; default `8080`
- `CT1_WEB_SCHEME` - `http` or `https` for newly discovered CT1s; default `http`
- `CT1_RPC_TLS` - Boolean; use TLS for CT1 gRPC discovery

A nonempty `CT1_RPC_TOKEN_FILE` is rejected. Enter each controller's token in Fleet.

### Device Discovery

The plugin discovers Proto rigs over HTTP(S) on ports `443`, `80`, and `8080`, and Frigate CT1 controllers over native gRPC on port `9095`. A positive `PROTO_MINER_PORT` restricts discovery to that single port; leave it unset or set it to `0` to use all defaults. The development stack uses these defaults. R2 discovery tries HTTPS first, then HTTP. CT1 configuration and credentials are described in [the gRPC transport guide](../../docs/development/ct1-grpc.md).

```go
// Automatic discovery on the configured Proto port
deviceInfo, err := driver.DiscoverDevice(ctx, "192.168.1.100", "443")
```

### Authentication

The plugin authenticates to Proto rigs with username/password credentials. The
factory defaults are `admin` / `proto`; the server auto-pairs with these when the
operator does not supply credentials (see `GetDefaultCredentials`). The client
logs in to the rig's `/api/v1/auth/login` endpoint, caches the returned access
token, and re-logs in automatically when the rig rejects the token.

#### Credentials Authentication (Pairing and Operations)
```go
// Pair and operate with username/password credentials.
secret := sdk.SecretBundle{
    Version: "v1",
    Kind: sdk.UsernamePassword{
        Username: "admin",
        Password: "proto",
    },
}
```

Proto devices without stored credentials are treated as needing authentication
and are repaired by the normal failed-poll remediation flow. Fleet keeps
factory-password rigs command-eligible; Proto firmware or the driver may reject
specific operations until `UpdateMinerPassword` clears the default password.

Managed Proto handles bind to the expected miner's serial number and MAC address.
Before each live API request, the driver reads the public pairing-info endpoint;
at least one stable identifier must match and shared identifiers must not conflict.
Handles created without an expected identity learn and retain the first usable
identity. A changed or unreadable identity reports the original miner unavailable
so Fleet can recover its address, without treating the failure as a changed password.
The same check covers reauthentication, command retries, password changes, and
firmware uploads. Bound handles reject redirects, and identity reads never include
credentials. This adds one small identity request per live API request; cached
status reads remain local. These checks assume the operator-controlled miner LAN
and do not provide cryptographic endpoint authentication.

## Testing

The tests run against the `fake-proto-rig` and `fake-ct1` simulators in this
repository. The rig integration tests need Docker. See the
[integration testing guide](docs/integration-testing.md) for the requirements
of each test.

```bash
# Unit tests in tests/unit
go test ./tests/unit/... -v

# All tests, without the tests that need Docker
go test -short ./...

# All tests
go test -timeout=30m ./...

# The rig lifecycle test with a simulator container
go test ./tests -run TestProtoPluginIntegration -v

# With coverage
go test -short -cover ./...
```

## Development

### Building

```bash
# Development build
go build -o proto-plugin

# Production build with optimizations
go build -ldflags="-s -w" -o proto-plugin

# Cross-compilation
GOOS=linux GOARCH=amd64 go build -o proto-plugin-linux
```

### Using the Justfile

The `justfile` has recipes for common development tasks. The repository
provides `just` through Hermit.

```bash
just --list   # show all recipes
just build
just lint
just test     # all tests
just check    # lint and all tests with the CI timeout
```

### Adding Features

1. **Update capabilities** in `internal/driver/driver.go`
2. **Implement methods** in `internal/device/device.go`
3. **Add API calls** in `pkg/proto/client.go`
4. **Write tests** next to the changed package or in `tests/unit/`, and update integration tests
5. **Update authentication** if needed in `pkg/proto/client.go` (rigs) or `internal/ct1/auth.go` (CT1)
6. **Update documentation** including this README

## API Compatibility

- **Fleet SDK v1**: Full compatibility with current interface
- **Proto rigs**: MDK v1 REST API over HTTP or HTTPS
- **CT1 containers**: MDK v2 over native gRPC
- **Go**: The version in `go.mod`
- **Credential Authentication**: Username/password pairing with token refresh for miner sessions

The vendored MDK contracts and their sources are recorded in
[`proto-rig-api`](../../proto-rig-api/README.md) and
[`sdk/mdk`](../../sdk/mdk/README.md).

## Dependencies

Core dependencies for the plugin:

- `github.com/block/proto-fleet/server` - Fleet server SDK
- `github.com/block/proto-fleet/sdk/mdk` - Generated MDK v2 bindings
- `github.com/hashicorp/go-plugin` - Go plugin framework
- `google.golang.org/grpc` - gRPC client for CT1 containers

Testing dependencies:

- `github.com/stretchr/testify` - Testing assertions and utilities
- `github.com/testcontainers/testcontainers-go` - Container-based integration testing
- `github.com/moby/moby/client` - Docker client for test image cleanup

## Contributing

This plugin serves as a reference implementation for the community. When making changes:

1. Maintain clarity: Keep code readable and well-documented
2. Add tests: Cover new functionality with both unit and integration tests
3. Document patterns: Update docs for new authentication or API patterns
4. Consider beginners: Think about the learning experience for new developers
5. Test thoroughly: Verify changes against the simulators and, where possible, real miners
6. Security first: Ensure authentication and cryptographic implementations follow best practices

## Security Considerations

When working with this plugin:

- Access tokens: The client caches the rig access token in memory and logs in again when the rig rejects it
- TLS verification: Rig HTTPS is encrypted but the rig certificate is not verified, so it does not protect against an active attacker on the miner LAN
- Credential storage: Use secure methods for storing authentication materials
- Network security: Prefer HTTPS over HTTP in production deployments

## License

This plugin is part of the Proto Fleet project and follows the same licensing terms.
