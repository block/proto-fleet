package driver

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// simMinerContainer represents a running sim miner container for testing
type simMinerContainer struct {
	container  testcontainers.Container
	host       string
	mappedPort string
}

// startSimMiner starts a sim miner container and returns connection details
func startSimMiner(ctx context.Context, t *testing.T) *simMinerContainer {
	if testing.Short() {
		t.Skip("Skipping sim miner tests in short mode")
	}

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    "../../../..",
			Dockerfile: "server/fake-proto-rig/Dockerfile",
		},
		ExposedPorts: []string{"8080/tcp"},
		WaitingFor:   wait.ForHTTP("/health").WithPort("8080/tcp").WithStartupTimeout(2 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err, "Failed to start sim miner container")

	// Get container connection details
	host, err := container.Host(ctx)
	require.NoError(t, err, "Failed to get container host")

	mappedPort, err := container.MappedPort(ctx, "8080")
	require.NoError(t, err, "Failed to get mapped port 8080")

	simMiner := &simMinerContainer{
		container:  container,
		host:       host,
		mappedPort: mappedPort.Port(),
	}

	// Wait for miner to be ready
	waitForMinerReady(ctx, t, simMiner)

	return simMiner
}

// close terminates the sim miner container
func (s *simMinerContainer) close(ctx context.Context, t *testing.T) {
	if err := s.container.Terminate(ctx); err != nil {
		t.Logf("Failed to terminate sim miner container: %v", err)
	}
}

// waitForMinerReady waits for the sim miner to be ready for discovery
func waitForMinerReady(ctx context.Context, t *testing.T, simMiner *simMinerContainer) {
	timeout := time.After(2 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	portInt64, err := strconv.ParseInt(simMiner.mappedPort, 10, 32)
	require.NoError(t, err, "Invalid port number")
	portInt := int(portInt64)

	d, err := New(portInt)
	require.NoError(t, err, "Failed to create driver")

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for miner to be ready")
		case <-ticker.C:
			_, err := d.DiscoverDevice(ctx, simMiner.host, simMiner.mappedPort)
			if err == nil {
				t.Log("Sim miner is ready!")
				return
			}
			t.Logf("Sim miner not ready yet: %v", err)
		}
	}
}

// TestDiscoverDevice_WithSimMiner tests device discovery with a real sim miner
func TestDiscoverDevice_WithSimMiner(t *testing.T) {
	ctx := t.Context()
	simMiner := startSimMiner(ctx, t)
	defer simMiner.close(ctx, t)

	t.Run("successful discovery", func(t *testing.T) {
		portInt64, err := strconv.ParseInt(simMiner.mappedPort, 10, 32)
		require.NoError(t, err)
		portInt := int(portInt64)

		driver, err := New(portInt)
		require.NoError(t, err)

		deviceInfo, err := driver.DiscoverDevice(ctx, simMiner.host, simMiner.mappedPort)
		require.NoError(t, err, "Discovery should succeed with real sim miner")

		// Validate device info
		assert.Equal(t, simMiner.host, deviceInfo.Host)
		assert.Equal(t, int32(portInt64), deviceInfo.Port)
		assert.NotEmpty(t, deviceInfo.SerialNumber, "Serial number should not be empty")
		assert.NotEmpty(t, deviceInfo.MacAddress, "MAC address should not be empty")
		assert.Equal(t, "Proto", deviceInfo.Manufacturer)

		// URL scheme should be http or https
		assert.Contains(t, []string{"http", "https"}, deviceInfo.URLScheme)
	})

	t.Run("discovery with flexible port driver", func(t *testing.T) {
		// Test with driver that accepts any port (port 0)
		driver, err := New(0)
		require.NoError(t, err)

		deviceInfo, err := driver.DiscoverDevice(ctx, simMiner.host, simMiner.mappedPort)
		require.NoError(t, err, "Discovery should succeed with flexible port driver")

		assert.Equal(t, simMiner.host, deviceInfo.Host)
		assert.NotEmpty(t, deviceInfo.SerialNumber)
		assert.NotEmpty(t, deviceInfo.MacAddress)
	})

	t.Run("discovery with wrong port driver configuration", func(t *testing.T) {
		// Test with driver configured for a different port than the sim miner is using
		wrongPort := 9999

		driver, err := New(wrongPort)
		require.NoError(t, err)

		// This should fail because driver expects a specific port but we're trying a different one
		_, err = driver.DiscoverDevice(ctx, simMiner.host, simMiner.mappedPort)
		require.Error(t, err, "Discovery should fail when driver port doesn't match target port")
		assert.Contains(
			t,
			err.Error(),
			"proto miners are configured for port",
			"strict-port discovery should fail before any network call; the reported target port may be a Docker-mapped test port",
		)
	})

	t.Run("concurrent discovery", func(t *testing.T) {
		portInt64, err := strconv.ParseInt(simMiner.mappedPort, 10, 32)
		require.NoError(t, err)
		portInt := int(portInt64)

		driver, err := New(portInt)
		require.NoError(t, err)

		const numGoroutines = 5
		results := make(chan error, numGoroutines)

		// Launch concurrent discoveries
		for range numGoroutines {
			go func() {
				_, err := driver.DiscoverDevice(ctx, simMiner.host, simMiner.mappedPort)
				results <- err
			}()
		}

		// Collect results
		for range numGoroutines {
			err := <-results
			assert.NoError(t, err, "Concurrent discovery should succeed")
		}
	})
}

// TestDiscoverDevice_PortValidation tests port validation logic without network calls
func TestDiscoverDevice_PortValidation(t *testing.T) {
	ctx := t.Context()

	testCases := []struct {
		name         string
		driverPort   int
		targetPort   string
		expectError  bool
		errorMessage string
	}{
		{
			name:         "invalid port - non-numeric",
			driverPort:   80,
			targetPort:   "abc",
			expectError:  true,
			errorMessage: "invalid port number",
		},
		{
			name:         "invalid port - negative",
			driverPort:   80,
			targetPort:   "-1",
			expectError:  true,
			errorMessage: "port number out of range",
		},
		{
			name:         "invalid port - too large",
			driverPort:   80,
			targetPort:   "65536",
			expectError:  true,
			errorMessage: "port number out of range",
		},
		{
			name:         "wrong port for strict driver",
			driverPort:   443,
			targetPort:   "8080",
			expectError:  true,
			errorMessage: "proto miners are configured for port 443",
		},
		{
			name:         "strict driver on 8080 rejects canonical https port before probing",
			driverPort:   8080,
			targetPort:   "443",
			expectError:  true,
			errorMessage: "proto miners are configured for port 8080",
		},
		{
			name:         "strict driver on 8080 accepts configured port before network call",
			driverPort:   8080,
			targetPort:   "8080",
			expectError:  true,
			errorMessage: "failed to discover proto miner",
		},
		{
			name:         "strict canonical driver rejects unsupported port",
			driverPort:   443,
			targetPort:   "80",
			expectError:  true,
			errorMessage: "proto miners are configured for port 443",
		},
		{
			name:         "invalid port with flexible driver - negative",
			driverPort:   0,
			targetPort:   "-1",
			expectError:  true,
			errorMessage: "port number out of range",
		},
		{
			name:         "invalid port with flexible driver - too large",
			driverPort:   0,
			targetPort:   "65536",
			expectError:  true,
			errorMessage: "port number out of range",
		},
		{
			name:         "empty port",
			driverPort:   0,
			targetPort:   "",
			expectError:  true,
			errorMessage: "invalid port number",
		},
		{
			name:         "whitespace in port",
			driverPort:   0,
			targetPort:   " 80 ",
			expectError:  true,
			errorMessage: "invalid port number",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			driver, err := New(tc.driverPort)
			require.NoError(t, err)

			// These tests should fail at port validation, before any network connection
			// Use a non-routable IP to ensure we don't accidentally connect
			_, err = driver.DiscoverDevice(ctx, "192.0.2.1", tc.targetPort)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorMessage != "" {
					assert.Contains(t, err.Error(), tc.errorMessage)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestDiscoverDevice_ConnectionErrors tests connection failure scenarios
func TestDiscoverDevice_ConnectionErrors(t *testing.T) {
	ctx := t.Context()

	testCases := []struct {
		name         string
		host         string
		port         string
		expectError  bool
		errorMessage string
	}{
		{
			name:         "unreachable host",
			host:         "192.0.2.1", // RFC 5737 test address - should not be routable
			port:         "80",
			expectError:  true,
			errorMessage: "failed to discover proto miner",
		},
		{
			name:         "unreachable port on localhost",
			host:         "localhost",
			port:         "9999", // Unlikely to be in use
			expectError:  true,
			errorMessage: "failed to discover proto miner",
		},
		{
			name:         "empty host",
			host:         "",
			port:         "80",
			expectError:  true,
			errorMessage: "", // Various possible error messages depending on system
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			driver, err := New(0) // Use flexible port driver
			require.NoError(t, err)

			// Use short timeout to avoid long waits
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			_, err = driver.DiscoverDevice(ctx, tc.host, tc.port)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorMessage != "" {
					assert.Contains(t, err.Error(), tc.errorMessage)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestDiscoverDevice_ContextCancellation tests context cancellation handling
func TestDiscoverDevice_ContextCancellation(t *testing.T) {
	driver, err := New(0)
	require.NoError(t, err)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Try to discover a device with cancelled context - should fail quickly
	_, err = driver.DiscoverDevice(ctx, "192.0.2.1", "80")
	require.Error(t, err)
	// The error might be context canceled or connection failure, both are acceptable
}

// TestDiscoverDevice_SchemeNegotiation tests HTTPS->HTTP fallback with sim miner
func TestDiscoverDevice_SchemeNegotiation(t *testing.T) {
	ctx := t.Context()
	simMiner := startSimMiner(ctx, t)
	defer simMiner.close(ctx, t)

	t.Run("scheme negotiation with real miner", func(t *testing.T) {
		portInt64, err := strconv.ParseInt(simMiner.mappedPort, 10, 32)
		require.NoError(t, err)
		portInt := int(portInt64)

		driver, err := New(portInt)
		require.NoError(t, err)

		// Discovery should succeed and negotiate the correct scheme
		deviceInfo, err := driver.DiscoverDevice(ctx, simMiner.host, simMiner.mappedPort)
		require.NoError(t, err, "Discovery should succeed with scheme negotiation")

		// The scheme should be either http or https depending on what the sim miner supports
		assert.Contains(t, []string{"http", "https"}, deviceInfo.URLScheme)
		assert.NotEmpty(t, deviceInfo.SerialNumber)
	})
}

// TestDiscoverDevice_MultipleSimMiners tests discovery with multiple sim miners
func TestDiscoverDevice_MultipleSimMiners(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multiple sim miner test in short mode")
	}

	ctx := t.Context()

	// Start multiple sim miners
	const numMiners = 2
	simMiners := make([]*simMinerContainer, numMiners)

	for i := range numMiners {
		simMiners[i] = startSimMiner(ctx, t)
		defer simMiners[i].close(ctx, t)
	}

	t.Run("discover multiple miners", func(t *testing.T) {
		driver, err := New(0) // Use flexible port driver
		require.NoError(t, err)

		deviceInfos := make([]sdk.DeviceInfo, numMiners)

		// Discover each miner
		for i, simMiner := range simMiners {
			deviceInfo, err := driver.DiscoverDevice(ctx, simMiner.host, simMiner.mappedPort)
			require.NoError(t, err, "Discovery should succeed for miner %d", i)
			deviceInfos[i] = deviceInfo
		}

		// Verify each miner has unique serial number
		serialNumbers := make(map[string]bool)
		for i, deviceInfo := range deviceInfos {
			assert.NotEmpty(t, deviceInfo.SerialNumber, "Miner %d should have serial number", i)
			assert.False(t, serialNumbers[deviceInfo.SerialNumber],
				"Serial number should be unique: %s", deviceInfo.SerialNumber)
			serialNumbers[deviceInfo.SerialNumber] = true
		}

		assert.Len(t, serialNumbers, numMiners, "Should discover all miners with unique serial numbers")
	})

	t.Run("concurrent discovery of multiple miners", func(t *testing.T) {
		driver, err := New(0) // Use flexible port driver
		require.NoError(t, err)

		results := make(chan struct {
			index      int
			deviceInfo sdk.DeviceInfo
			err        error
		}, numMiners)

		// Launch concurrent discoveries
		for i, simMiner := range simMiners {
			go func(index int, simMiner *simMinerContainer) {
				deviceInfo, err := driver.DiscoverDevice(ctx, simMiner.host, simMiner.mappedPort)
				results <- struct {
					index      int
					deviceInfo sdk.DeviceInfo
					err        error
				}{index, deviceInfo, err}
			}(i, simMiner)
		}

		// Collect results
		deviceInfos := make([]sdk.DeviceInfo, numMiners)
		for range numMiners {
			result := <-results
			require.NoError(t, result.err, "Concurrent discovery should succeed for miner %d", result.index)
			deviceInfos[result.index] = result.deviceInfo
		}

		// Verify unique serial numbers
		serialNumbers := make(map[string]bool)
		for i, deviceInfo := range deviceInfos {
			assert.NotEmpty(t, deviceInfo.SerialNumber, "Miner %d should have serial number", i)
			assert.False(t, serialNumbers[deviceInfo.SerialNumber],
				"Serial number should be unique: %s", deviceInfo.SerialNumber)
			serialNumbers[deviceInfo.SerialNumber] = true
		}
	})
}

// TestDiscoverDevice_EdgeCases tests edge cases and boundary conditions
func TestDiscoverDevice_EdgeCases(t *testing.T) {
	ctx := t.Context()

	// Get an unused non-routable IP to ensure test isolation
	// This prevents interference from local development containers
	unusedIP := getUnusedNonRoutableIP(t)

	testCases := []struct {
		name         string
		host         string
		port         string
		expectError  bool
		errorMessage string
	}{
		{
			name:         "non-routable IPv4 with valid port format",
			host:         unusedIP,
			port:         "80",
			expectError:  true, // Will fail because IP is non-routable
			errorMessage: "failed to discover proto miner",
		},
		{
			name:         "non-routable IPv4 with different valid port",
			host:         unusedIP,
			port:         "8080",
			expectError:  true, // Will fail because IP is non-routable
			errorMessage: "failed to discover proto miner",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			driver, err := New(0) // Use flexible port driver
			require.NoError(t, err)

			// Use short timeout to avoid long waits
			ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			_, err = driver.DiscoverDevice(ctx, tc.host, tc.port)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorMessage != "" {
					assert.Contains(t, err.Error(), tc.errorMessage)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Benchmark tests for performance validation
func BenchmarkDiscoverDevice(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	ctx := b.Context()
	simMiner := startSimMiner(ctx, &testing.T{}) // Convert to *testing.T for startSimMiner
	defer simMiner.close(ctx, &testing.T{})

	portInt64, err := strconv.ParseInt(simMiner.mappedPort, 10, 32)
	if err != nil {
		b.Fatal(err)
	}
	portInt := int(portInt64)

	driver, err := New(portInt)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		_, err := driver.DiscoverDevice(ctx, simMiner.host, simMiner.mappedPort)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// getUnusedNonRoutableIP returns an IP address from RFC 5737 TEST-NET-1 (192.0.2.0/24).
// These addresses are reserved for documentation and examples, and are guaranteed to be
// non-routable by design. They should never be assigned to real network interfaces.
//
// This function returns 192.0.2.1 from TEST-NET-1 for consistency across tests.
// If different test scenarios need different non-routable IPs to avoid conflicts,
// consider TEST-NET-2 (198.51.100.0/24) or TEST-NET-3 (203.0.113.0/24).
//
// See: https://www.rfc-editor.org/rfc/rfc5737
func getUnusedNonRoutableIP(t *testing.T) string {
	t.Helper()
	return "192.0.2.1"
}
