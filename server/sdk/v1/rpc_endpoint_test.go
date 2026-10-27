package sdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceInfoPreservesIndependentRPCEndpoint(t *testing.T) {
	info := DeviceInfo{
		Host: "10.1.2.3", Port: 8080, URLScheme: "http", RPCPort: 9443, RPCTLS: true, DeviceClass: DeviceClassContainer,
		Endpoints: []DeviceEndpoint{
			{ID: "a", DeviceKey: "opaque", Host: "10.0.0.11", Port: 8080, URLScheme: "http", RPCPort: 9095},
			{ID: "b", DeviceKey: "opaque", Host: "10.0.0.12", Port: 8443, URLScheme: "https", RPCPort: 9443, RPCTLS: true},
		},
	}
	require.Equal(t, info, deviceInfoFromProto(deviceInfoToProto(info)))
}
