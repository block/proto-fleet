package integration

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/btc-mining/proto-fleet/plugin/proto/internal/device"
	"github.com/btc-mining/proto-fleet/plugin/proto/internal/driver"
	"github.com/btc-mining/proto-fleet/plugin/proto/tests/testutils"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

func TestProtoPluginIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := t.Context()

	// Generate a key pair that will be used throughout the test
	// This simulates the real workflow where the same key pair is used for pairing and JWT generation
	keyPair, err := testutils.GenerateEd25519KeyPair()
	require.NoError(t, err, "Failed to generate Ed25519 key pair for test")

	// Start proto-sim container using the same config as server/docker-compose.yaml
	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    "../../../miner-firmware",
			Dockerfile: "docker/sim/Dockerfile",
			BuildArgs: map[string]*string{
				"TYPE": stringPtr("b4-sim"),
			},
			BuildOptionsModifier: func(opts *build.ImageBuildOptions) {
				opts.Version = build.BuilderBuildKit
			},
		},
		ExposedPorts: []string{"8080/tcp", "2121/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("8080/tcp"),
			wait.ForListeningPort("2121/tcp"),
		).WithDeadline(3 * time.Minute),
		Privileged: true,
		CapAdd:     []string{"NET_ADMIN", "NET_RAW"},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	// Get container connection details
	host, err := container.Host(ctx)
	require.NoError(t, err)

	port2121, err := container.MappedPort(ctx, "2121")
	require.NoError(t, err)

	// Wait for miner to be ready
	waitForMinerReady(ctx, t, host, port2121.Port())

	// Create driver
	d, err := driver.New(port2121.Int())
	require.NoError(t, err)

	t.Run("Driver Handshake", func(t *testing.T) {
		handshake, err := d.Handshake(ctx)
		require.NoError(t, err)
		assert.Equal(t, "proto", handshake.DriverName)
		assert.Equal(t, "v1", handshake.APIVersion)
	})

	t.Run("Driver Describe", func(t *testing.T) {
		driverInfo, capabilities, err := d.DescribeDriver(ctx)
		require.NoError(t, err)
		assert.Equal(t, "proto", driverInfo.DriverName)
		assert.True(t, capabilities[sdk.CapabilityDiscovery])
		assert.True(t, capabilities[sdk.CapabilityPairing])
	})

	t.Run("Device Discovery", func(t *testing.T) {
		deviceInfo, err := d.DiscoverDevice(ctx, host, port2121.Port())
		require.NoError(t, err)
		assert.Equal(t, host, deviceInfo.Host)
		assert.NotEmpty(t, deviceInfo.SerialNumber)
		assert.Equal(t, "Proto", deviceInfo.Manufacturer)
	})

	t.Run("Device Pairing", func(t *testing.T) {
		// Discover device first
		deviceInfo, err := d.DiscoverDevice(ctx, host, port2121.Port())
		require.NoError(t, err)

		// Get the public key in the format expected by the miner (base64 SPKI DER)
		publicKeyBase64, err := keyPair.PublicKeyBase64()
		require.NoError(t, err, "Failed to encode public key")

		// Test pairing with real Ed25519 public key
		pairingSecret := sdk.SecretBundle{
			Version: "v1",
			Kind: sdk.APIKey{
				Key: publicKeyBase64,
			},
		}

		// Attempt pairing with real Ed25519 key
		// This may still fail if the sim-miner doesn't support pairing, but the error
		// should be authentication-related, not a parsing error
		result, err := d.PairDevice(ctx, deviceInfo, pairingSecret)
		require.NoError(t, err, "Pairing failed with real Ed25519 key")
		assert.NotNil(t, result, "Pairing result should not be nil if no error occurred")
		assert.Contains(t, result, "Successfully paired Proto miner", "Expected success message on pairing")
	})

	t.Run("Real Miner Operations With JWT", func(t *testing.T) {
		// Discover device
		deviceInfo, err := d.DiscoverDevice(ctx, host, port2121.Port())
		require.NoError(t, err)

		// Generate a real JWT token signed with the same Ed25519 private key used for pairing
		// Use the device serial number as the subject
		jwtToken, err := keyPair.GenerateJWT(deviceInfo.SerialNumber, 1*time.Hour)
		require.NoError(t, err, "Failed to generate JWT token")

		// Create operation secret with real JWT token
		operationSecret := sdk.SecretBundle{
			Version: "v1",
			Kind: sdk.BearerToken{
				Token: jwtToken,
			},
		}

		// Create device instance using the real JWT token
		deviceID := "test-device"
		result, err := d.NewDevice(ctx, deviceID, deviceInfo, operationSecret)

		require.NoError(t, err, "Device creation failed with real JWT token")

		// If device creation succeeded, test basic operations
		require.NotNil(t, result, "Device creation result should not be nil if no error occurred")
		require.NotNil(t, result.Device, "Device instance should not be nil if creation succeeded")

		defer result.Device.Close(ctx)

		device, err := device.New(deviceID, deviceInfo, sdk.BearerToken{Token: jwtToken}, device.SetStatusTTL(0*time.Second))
		require.NoError(t, err)
		t.Run("Get Status", func(t *testing.T) {
			status, err := device.Status(ctx)
			require.NoError(t, err, "Device status check should not fail if device creation succeeded")

			assert.Equal(t, deviceID, status.DeviceID)
			assert.NotEmpty(t, status.Summary)
		})

		t.Run("Describe Device", func(t *testing.T) {
			deviceInfo2, capabilities, err := device.DescribeDevice(ctx)
			require.NoError(t, err, "Device describe should not fail")
			assert.Equal(t, deviceInfo.SerialNumber, deviceInfo2.SerialNumber)
			assert.NotEmpty(t, capabilities, "Device should report some capabilities")
		})

		// Test device capabilities
		// Test LED blinking
		t.Run("BlinkLED", func(t *testing.T) {
			err := device.BlinkLED(ctx)
			require.NoError(t, err, "BlinkLED should not fail")
		})

		// Test mining control operations
		t.Run("Mining Control", func(t *testing.T) {
			// Get initial status
			initialStatus, err := device.Status(ctx)
			require.NoError(t, err)

			assert.NotNil(t, initialStatus)

			// Test stop mining
			t.Run("StopMining", func(t *testing.T) {
				err := device.StopMining(ctx)
				require.NoError(t, err, "StopMining should not fail")
			})

			// Test start mining
			t.Run("StartMining", func(t *testing.T) {
				err := device.StartMining(ctx)
				require.NoError(t, err, "StartMining should not fail")
			})
		})

		// Test cooling mode configuration
		t.Run("Cooling Mode", func(t *testing.T) {
			// Test setting different cooling modes
			coolingModes := []sdk.CoolingMode{
				sdk.CoolingModeAirCooled,
				sdk.CoolingModeManual,
				sdk.CoolingModeAirCooled, // Reset to air cooled
			}

			for i := range coolingModes {
				mode := coolingModes[i]
				t.Run(fmt.Sprintf("SetCoolingMode_%v", mode), func(t *testing.T) {
					err := device.SetCoolingMode(ctx, mode)
					require.NoError(t, err, "SetCoolingMode should not fail for mode %v", mode)
				})
			}
		})

		// Test mining pool configuration
		t.Run("Mining Pools", func(t *testing.T) {
			pools := []sdk.MiningPoolConfig{
				{
					Priority:   1,
					URL:        "stratum+tcp://test-pool1.example.com:4444",
					WorkerName: "test-worker-1",
				},
				{
					Priority:   2,
					URL:        "stratum+tcp://test-pool2.example.com:4444",
					WorkerName: "test-worker-2",
				},
			}

			err := device.UpdateMiningPools(ctx, pools)
			require.NoError(t, err, "UpdateMiningPools should not fail")
		})

		// Test log download
		t.Run("Download Logs", func(t *testing.T) {
			since := time.Now().Add(-1 * time.Hour) // Last hour
			_, _, err := device.DownloadLogs(ctx, &since, "")
			require.NoError(t, err, "DownloadLogs should not fail")
			// There are likely to be no logs with the fresh sim-miner, so we don't check content
		})

		// Test web view URL
		t.Run("Web View URL", func(t *testing.T) {
			url, supported, err := device.TryGetWebViewURL(ctx)
			require.NoError(t, err, "TryGetWebViewURL should not fail")
			require.True(t, supported, "Web view URL should be supported")

			assert.NotEmpty(t, url)
			assert.Contains(t, url, host)
		})

		// Test reboot, sim miner should stay up but acknowledge the command
		t.Run("Reboot", func(t *testing.T) {
			err := device.Reboot(ctx)
			require.NoError(t, err, "Reboot should not fail")
		})

		// Test batch operations (should return not supported)
		t.Run("Batch Operations", func(t *testing.T) {
			deviceIDs := []string{deviceID}

			// Test batch status
			_, supported, err := device.TryBatchStatus(ctx, deviceIDs)
			require.NoError(t, err, "TryBatchStatus should not fail")
			require.False(t, supported, "Batch status should be supported")

		})
	})
}

func waitForMinerReady(ctx context.Context, t *testing.T, host, port string) {
	timeout := time.After(2 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	portInt, err := strconv.Atoi(port)
	require.NoError(t, err, "Invalid port number")

	d, err := driver.New(portInt)
	require.NoError(t, err)

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for miner to be ready")
		case <-ticker.C:
			_, err := d.DiscoverDevice(ctx, host, port)
			if err == nil {
				t.Log("Miner is ready!")
				return
			}
			t.Logf("Miner not ready yet: %v", err)
		}
	}
}

func stringPtr(s string) *string {
	return &s
}
