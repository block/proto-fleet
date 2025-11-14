package proto_test

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/integrationtesting"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/proto"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
)

func TestDiscoverer_Discover(t *testing.T) {
	testCases := []struct {
		name           string
		port           string
		useTLS         bool
		expectedScheme string
		expectError    bool
		errorMessage   string
	}{
		{
			name:           "should discover proto miner over HTTP",
			port:           "2121",
			useTLS:         false,
			expectedScheme: "http",
			expectError:    false,
		},
		{
			name:           "should discover proto miner over HTTPS",
			port:           "2121",
			useTLS:         true,
			expectedScheme: "https",
			expectError:    false,
		},
		{
			name:         "should fail for wrong port",
			port:         "8080",
			useTLS:       false,
			expectError:  true,
			errorMessage: "miner not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			var mockMinerServer *httptest.Server
			var serverURL *url.URL
			var ipAddress string
			var minerCallCounter *integrationtesting.MockMinerCallCounter

			if tc.port == "2121" {
				minerCallCounter = integrationtesting.NewMockMinerCallCounter()
				// Use port 2121 to match the discoverer's requirement
				mockMinerServer = testutil.SetupMockMinerServer(t, minerCallCounter, tc.useTLS, 2121)
				defer mockMinerServer.Close()

				var err error
				serverURL, err = url.Parse(mockMinerServer.URL)
				require.NoError(t, err)
				ipAddress = serverURL.Hostname()
			} else {
				ipAddress = "localhost"
			}

			// Act
			discoverer := proto.NewDiscoverer()
			device, err := discoverer.Discover(t.Context(), ipAddress, tc.port)

			// Assert
			if tc.expectError {
				require.Error(t, err)
				if tc.errorMessage != "" {
					assert.Contains(t, err.Error(), tc.errorMessage)
				}
				assert.Nil(t, device)
			} else {
				require.NoError(t, err)
				require.NotNil(t, device)

				// Verify device properties
				assert.Equal(t, ipAddress, device.Device.IpAddress)
				assert.Equal(t, tc.port, device.Device.Port)
				assert.Equal(t, tc.expectedScheme, device.Device.UrlScheme)
				assert.Equal(t, "00:00:00:00:00:00", device.Device.MacAddress)
				assert.Equal(t, "1234567890", device.Device.SerialNumber)
				assert.Equal(t, "Rig", device.Device.Model)
				assert.Equal(t, "Proto", device.Device.Manufacturer)
				assert.Equal(t, models.TypeProto.String(), device.Type)

				// Verify the miner was called
				minerCallCounter.AssertCalls(t, integrationtesting.MethodGetPairingInfo, 1)
			}
		})
	}
}
