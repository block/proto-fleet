package sdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceInfoPreservesIndependentRPCEndpoint(t *testing.T) {
	info := DeviceInfo{Host: "10.1.2.3", Port: 8080, URLScheme: "http", RPCPort: 9443, RPCTLS: true, DeviceClass: DeviceClassContainer}
	require.Equal(t, info, deviceInfoFromProto(deviceInfoToProto(info)))
}
