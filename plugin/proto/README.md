# Proto Miner Plugin

A Fleet plugin for the Proto mining system, implementing SDK v1 with authentication and testing.

## Overview

This plugin provides Proto miner integration for the Fleet mining system. It demonstrates:

- Architecture with separation of concerns
- Patterns for error handling, logging, and testing
- Authentication with Ed25519 key pairs and JWT token management
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
│   │   └── driver.go         # SDK Driver interface
│   └── device/               # Device implementation
│       └── device.go         # SDK Device interface
│
├── pkg/                      # Reusable packages
│   └── proto/                # Proto-specific API client
│       └── client.go         # Miner communication with auth
│
├── docs/                     # Documentation
│   ├── getting-started.md    # Plugin development guide
│   ├── sdk-patterns.md       # SDK usage patterns
│   └── integration-testing.md # Testing guide
│
└── tests/                    # Test files
    ├── unit/                 # Unit tests
    │   ├── plugin_test.go    # Core functionality tests
    │   └── auth_persistence_test.go  # Authentication tests
    ├── integration_test.go   # Full integration tests with containers
    └── testutils/            # Testing utilities
        ├── jwt.go            # Ed25519 key pair and JWT generation
        └── jwt_test.go       # JWT utility tests
```

## Key Features

### ✅ **Features**
- Device discovery and pairing with Ed25519 public key authentication
- JWT-based device authentication for secure operations
- Mining control (start/stop) with real-time status monitoring
- Comprehensive telemetry collection (hashrate, power, temperature)
- Pool configuration with priority-based failover
- Device management (reboot, firmware updates, LED identification)
- Log retrieval with filtering and pagination
- Web view URL generation for device management interfaces
- TLS/HTTP2 support with configurable security settings

### 🔐 **Authentication & Security**
- Ed25519 key pair generation for cryptographic pairing
- JWT token management with proper signing and validation
- Bearer token authentication for API operations
- TLS certificate verification with configurable bypass for development
- Context-based auth injection for all API calls
- Secure credential handling through SDK SecretBundle interface

### 🧪 **Testing & Quality**
- Unit tests for authentication, client operations, and core functionality
- Integration tests with containerized Proto sim-miners using testcontainers
- Real miner testing support with environment variable configuration
- JWT test utilities for Ed25519 key generation and token validation
- Authentication persistence testing for credential management
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
2. **Configure environment**: Set `SKIP_TLS_VERIFY`, `LOG_LEVEL`, etc.
3. **Deploy with Fleet**: Reference the binary in Fleet configuration
4. **Monitor logs**: Check for authentication and connectivity issues

## Configuration

### Environment Variables

- `SKIP_TLS_VERIFY=true` - Skip TLS certificate verification
- `INSECURE_TLS=true` - Allow insecure TLS connections
- `LOG_LEVEL=debug` - Set logging level (debug, info, warn, error)
- `PLUGIN_TIMEOUT=30s` - Set operation timeout
- `PLUGIN_MAX_RETRIES=3` - Set retry attempts

### Device Discovery

The plugin discovers Proto miners on its configured port, which defaults to `443` and can be overridden with `PROTO_MINER_PORT`. Discovery tries HTTPS first, then HTTP:

```go
// Automatic discovery on the configured Proto port
deviceInfo, err := driver.DiscoverDevice(ctx, "192.168.1.100", "443")
```

### Authentication

The plugin supports multiple authentication methods with a focus on security:

#### Ed25519 Key Pair Authentication (Pairing)
```go
// Generate Ed25519 key pair for pairing
keyPair, err := testutils.GenerateEd25519KeyPair()
publicKeyBase64, err := keyPair.PublicKeyBase64()

// Use public key for device pairing
pairingSecret := sdk.SecretBundle{
    Version: "v1",
    Kind: sdk.APIKey{
        Key: publicKeyBase64,
    },
}
```

#### JWT Bearer Token Authentication (Operations)
```go
// Generate JWT token signed with Ed25519 private key
jwtToken, err := keyPair.GenerateJWT(deviceSerialNumber, 1*time.Hour)

// Use JWT token for device operations
operationSecret := sdk.SecretBundle{
    Version: "v1",
    Kind: sdk.BearerToken{
        Token: jwtToken,
    },
}
```

#### Alternative Authentication Methods
```go
// API key (for simple implementations)
secret := sdk.SecretBundle{
    Version: "v1", 
    Kind: sdk.APIKey{Key: "your-key"},
}

// TLS client certificate (for certificate-based auth)
secret := sdk.SecretBundle{
    Version: "v1",
    Kind: sdk.TLSClientCert{
        ClientCertPEM: certPEM,
        KeyPEM: keyPEM,
        CACertPEM: caPEM,
    },
}
```

## Testing

### Unit Tests

Run unit tests for core functionality and authentication:

```bash
# Run all unit tests
go test ./tests/unit -v

# Run specific authentication tests
go test ./tests/unit -run TestSecretBundleExtraction -v
go test ./tests/unit -run TestClientAuthInterceptor -v

# Run JWT utility tests
go test ./tests/testutils -v
```

### Integration Tests

The plugin includes comprehensive integration tests using testcontainers:

```bash
# Run full integration tests with containerized sim-miner
go test ./tests -run TestProtoPluginIntegration -v

# Run additional integration test coverage
go test ./tests -run TestProtoPluginWithRealSimMiner -v

# Run all integration tests
go test ./tests -v

# Skip integration tests (run only unit tests)
go test -short ./...
```

## Development

### Running Tests

```bash
# Unit tests only
go test ./tests/unit/...

# All tests including integration
go test ./...

# With coverage
go test -cover ./...

# Benchmarks
go test -bench=. ./tests/unit/

# Specific test patterns
go test ./tests -run TestProtoPluginIntegration -v
```

### Building

```bash
# Development build
go build -o proto-plugin

# Production build with optimizations
go build -ldflags="-s -w" -o proto-plugin

# Cross-compilation
GOOS=linux GOARCH=amd64 go build -o proto-plugin-linux
```

### Using Justfile (Optional)

The project includes a `justfile` for common development tasks:

```bash
# Install just if not already installed
# brew install just  # macOS
# cargo install just  # Rust/Cargo

# View available commands
just --list

# Example usage (if justfile commands are defined)
just test
just build
just lint
```

### Adding Features

1. **Update capabilities** in `internal/driver/driver.go`
2. **Implement methods** in `internal/device/device.go`
3. **Add API calls** in `pkg/proto/client.go`
4. **Write tests** in `tests/unit/` and update integration tests
5. **Update authentication** if needed in client auth interceptors
6. **Update documentation** including this README

## API Compatibility

- **Fleet SDK v1**: Full compatibility with current interface
- **Proto Miner API**: Connect-RPC over HTTP/2 with gRPC compatibility
- **Go 1.24.2+**: Modern Go features and performance optimizations
- **Ed25519 Cryptography**: Industry-standard elliptic curve for secure authentication
- **JWT Authentication**: RFC 7519 compliant tokens with EdDSA signing

## Dependencies

Core dependencies for the plugin:

- `github.com/block/proto-fleet/server` - Fleet server SDK
- `connectrpc.com/connect` - Connect-RPC client library for API communication
- `github.com/hashicorp/go-plugin` - Go plugin framework
- `github.com/golang-jwt/jwt/v5` - JWT authentication and token management
- `golang.org/x/net` - HTTP/2 support and network utilities

Testing dependencies:

- `github.com/stretchr/testify` - Testing assertions and utilities
- `github.com/testcontainers/testcontainers-go` - Container-based integration testing
- `github.com/docker/docker` - Docker integration for containerized tests

## Contributing

This plugin serves as a reference implementation for the community. When making changes:

1. Maintain clarity: Keep code readable and well-documented
2. Add tests: Cover new functionality with both unit and integration tests
3. Document patterns: Update docs for new authentication or API patterns
4. Consider beginners: Think about the learning experience for new developers
5. Test thoroughly: Verify changes work with both containerized and real miners
6. Security first: Ensure authentication and cryptographic implementations follow best practices

## Security Considerations

When working with this plugin:

- Ed25519 keys: Private keys should never be logged or exposed
- JWT tokens: Tokens should have appropriate expiration times
- TLS verification: Only disable for development/testing environments
- Credential storage: Use secure methods for storing authentication materials
- Network security: Prefer HTTPS over HTTP in production deployments

## License

This plugin is part of the Proto Fleet project and follows the same licensing terms.
