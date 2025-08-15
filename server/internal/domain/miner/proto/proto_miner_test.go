package proto_test

import (
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtoMinerInfo_GetWebViewURL(t *testing.T) {
	testCases := []struct {
		name        string
		protocol    networking.Protocol
		ipAddress   string
		expectedURL string
	}{
		{
			name:        "HTTP Protocol",
			protocol:    networking.ProtocolHTTP,
			ipAddress:   "192.168.1.100",
			expectedURL: "http://192.168.1.100",
		},
		{
			name:        "HTTPS Protocol",
			protocol:    networking.ProtocolHTTPS,
			ipAddress:   "192.168.1.101",
			expectedURL: "https://192.168.1.101",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			deviceID := models.DeviceIdentifier("123")
			minerInfo, err := proto.NewProtoMinerInfo(deviceID, tc.ipAddress, 2121, tc.protocol, []byte("test_private_key"), "test_serial_number")
			require.NoError(t, err)

			// Act
			actualURL := minerInfo.GetWebViewURL()

			// Assert
			assert.Equal(t, tc.expectedURL, actualURL.String())
		})
	}
}
