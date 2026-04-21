# Integration Testing Guide

This document explains how to run integration tests for the Proto plugin.

## Overview

The Proto plugin includes comprehensive integration tests that verify the plugin works correctly with real and simulated Proto miners. The tests use testcontainers to spin up containerized environments for testing.

## Test Types

### 1. Mock Integration Tests (`mock_integration_test.go`)

These tests use a lightweight mock HTTP server that simulates Proto miner API responses. They're faster and don't require the full miner firmware.

**Features tested:**
- Driver handshake and capabilities
- Device discovery with mock responses
- Device pairing and authentication
- Basic device operations (start/stop mining, LED control, reboot)
- Error handling and edge cases

### 2. Full Integration Tests (`integration_test.go`)

These tests use the actual Proto miner simulator container from the miner-firmware repository. They provide the most realistic testing environment and are completely self-contained.

**Test Functions:**
- `TestProtoPluginIntegration` - Comprehensive integration test covering the complete device lifecycle
- `TestProtoPluginWithRealSimMiner` - Additional integration test providing extra coverage for edge cases and scenarios

**Features tested:**
- Complete device lifecycle (discovery → pairing → management)
- Mining control operations
- Pool configuration
- Cooling mode settings
- Telemetry data collection
- Status caching and performance
- Concurrent operations
- Log retrieval

### 3. Benchmark Tests (`benchmark_test.go`)

Performance benchmarks for critical plugin operations.

**Benchmarks:**
- Driver operations (handshake, describe)
- Device creation and management
- Status caching performance
- Concurrent access patterns

## Prerequisites

### Docker Environment

All integration tests require Docker to be running:

```bash
# Verify Docker is available
docker --version
docker info
```

### Dependencies

Install test dependencies:

```bash
go mod tidy
```

## Running Tests

### Quick Tests (Mock Only)

Run lightweight mock tests:

```bash
cd tests/integration
go test -v -run TestMockProtoMinerIntegration .
```

### Full Integration Tests

Run tests with the real Proto simulator:

```bash
cd tests/integration
go test -v -run TestProtoPluginIntegration .
```

Run additional integration test coverage:

```bash
cd tests/integration
go test -v -run TestProtoPluginWithRealSimMiner .
```

### All Integration Tests

Run all integration tests:

```bash
cd tests/integration
go test -v .
```

### Benchmarks

Run performance benchmarks:

```bash
cd tests/integration
go test -bench=. .
```

### Skip Integration Tests

Use the `-short` flag to skip integration tests:

```bash
go test -short ./...
```

## Test Configuration

### Environment Variables

Configure test behavior with environment variables:

```bash
# Skip TLS verification for testing
export SKIP_TLS_VERIFY=true

# Set log level for debugging
export LOG_LEVEL=debug

# Adjust test timeouts
export TEST_TIMEOUT=5m
```

### Test Flags

Common test flags:

```bash
# Verbose output
go test -v

# Run specific test
go test -run TestMockDeviceDiscovery

# Run benchmarks
go test -bench=BenchmarkDriverOperations

# Generate coverage report
go test -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Test Structure

### Test Suites

Tests are organized using testify suites:

```go
type MockProtoMinerTestSuite struct {
    suite.Suite
    container testcontainers.Container
    driver    *driver.Driver
    // ... other fields
}

func (suite *MockProtoMinerTestSuite) SetupSuite() {
    // One-time setup before all tests
}

func (suite *MockProtoMinerTestSuite) TearDownSuite() {
    // One-time cleanup after all tests
}

func (suite *MockProtoMinerTestSuite) TestSomething() {
    // Individual test method
}
```

### Container Management

Tests automatically manage container lifecycle:

1. **Setup**: Start container with proper configuration
2. **Wait**: Wait for container to be ready
3. **Test**: Run test operations
4. **Cleanup**: Automatically terminate container

### Helper Methods

Common helper methods available in test suites:

```go
// Create a test device instance
device := suite.createTestDevice("test-name")
defer device.Close(suite.ctx)

// Wait for miner to be ready
suite.waitForMinerReady()

// Parse port string to int32
port := mustParsePort("443")
```

## Troubleshooting

### Docker Issues

**Problem**: Docker daemon not running
```
Error: Cannot connect to the Docker daemon
```

**Solution**: Start Docker Desktop or Docker daemon

**Problem**: Permission denied
```
Error: permission denied while trying to connect to Docker daemon
```

**Solution**: Add user to docker group or use sudo

### Container Issues

**Problem**: Container fails to start
```
Error: failed to start container
```

**Solutions**:
- Check Docker has enough resources (memory, disk)
- Verify no port conflicts (2121, 8080)
- Check container logs: `docker logs <container-id>`

### Network Issues

**Problem**: Cannot connect to container
```
Error: connection refused
```

**Solutions**:
- Verify container is running: `docker ps`
- Check port mapping is correct
- Ensure firewall allows connections
- Wait longer for container to be ready

### Test Timeouts

**Problem**: Tests timeout waiting for container
```
Error: timeout waiting for container to be ready
```

**Solutions**:
- Increase timeout values in test code
- Check container startup logs
- Verify container health endpoints work
- Use faster container images

## Continuous Integration

### GitHub Actions

Example workflow for running integration tests:

```yaml
name: Integration Tests

on: [push, pull_request]

jobs:
  integration:
    runs-on: ubuntu-latest
    
    services:
      docker:
        image: docker:dind
        options: --privileged
    
    steps:
    - uses: actions/checkout@v3
    
    - name: Set up Go
      uses: actions/setup-go@v3
      with:
        go-version: 1.24
    
    - name: Run integration tests
      run: |
        cd plugin/proto
        go test ./tests/integration -v
```

### Local Development

For local development, consider:

1. **Use mock tests** for rapid iteration
2. **Run full tests** before commits
3. **Use benchmarks** to catch performance regressions
4. **Check coverage** to ensure comprehensive testing

## Writing New Tests

### Adding Mock Tests

1. Add new test methods to `MockProtoMinerTestSuite`
2. Use helper methods for common operations
3. Follow naming convention: `TestMock*`
4. Add appropriate assertions and logging

### Adding Full Integration Tests

1. Add new test methods to `ProtoPluginIntegrationTestSuite`
2. Use real container for testing
3. Follow naming convention: `Test*`
4. Test realistic scenarios and edge cases

### Adding Benchmarks

1. Create benchmark functions: `BenchmarkSomething`
2. Use `b.ResetTimer()` before measured operations
3. Use `b.RunParallel()` for concurrent benchmarks
4. Include meaningful operations in benchmarks

## Best Practices

1. **Isolation**: Each test should be independent
2. **Cleanup**: Always clean up resources (containers, devices)
3. **Timeouts**: Use reasonable timeouts for operations
4. **Logging**: Include helpful log messages for debugging
5. **Assertions**: Use descriptive assertion messages
6. **Performance**: Consider test execution time
7. **Reliability**: Handle flaky network conditions gracefully

## Example Test Run

```bash
$ cd plugin/proto/tests/integration
$ go test -v .

=== RUN   TestMockProtoMinerIntegration
=== RUN   TestMockProtoMinerIntegration/TestMockDriverHandshake
=== RUN   TestMockProtoMinerIntegration/TestMockDeviceDiscovery
=== RUN   TestMockProtoMinerIntegration/TestMockDevicePairing
=== RUN   TestMockProtoMinerIntegration/TestMockDeviceOperations
--- PASS: TestMockProtoMinerIntegration (15.23s)

=== RUN   TestProtoPluginIntegration
=== RUN   TestProtoPluginIntegration/TestDriverHandshake
=== RUN   TestProtoPluginIntegration/TestDeviceDiscovery
=== RUN   TestProtoPluginIntegration/TestDeviceMiningControl
--- PASS: TestProtoPluginIntegration (45.67s)

PASS
ok  	github.com/block/proto-fleet/plugin/proto/tests/integration	60.90s
```

This comprehensive integration testing setup ensures the Proto plugin works correctly in real-world scenarios and provides confidence for production deployments.
