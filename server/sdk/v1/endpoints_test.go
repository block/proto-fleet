package sdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceEndpointKeyRejectsConflictingMetadata(t *testing.T) {
	valid := []DeviceEndpoint{
		{ID: "a", DeviceKey: "opaque", MemberIDs: []string{"a", "b"}, Host: "10.0.0.11", Port: 8080, URLScheme: "http", RPCPort: 9095},
		{ID: "b", DeviceKey: "opaque", MemberIDs: []string{"b", "a"}, Host: "10.0.0.12", Port: 8080, URLScheme: "http", RPCPort: 9095},
	}
	key, err := DeviceEndpointKey(valid)
	require.NoError(t, err)
	require.Equal(t, "opaque", key)
	require.Equal(t, []string{"b", "a"}, valid[1].MemberIDs, "validation must not reorder the caller's member IDs")
	for _, change := range []func([]DeviceEndpoint){
		func(e []DeviceEndpoint) { e[1].DeviceKey = "other" },
		func(e []DeviceEndpoint) { e[1].ID = "a" },
		func(e []DeviceEndpoint) { e[1].Host = e[0].Host },
		func(e []DeviceEndpoint) { e[1].RPCPort = -1 },
		func(e []DeviceEndpoint) { e[0].MemberIDs = []string{"a", "a", "b"} },
		func(e []DeviceEndpoint) { e[0].MemberIDs = []string{"b"} },
		func(e []DeviceEndpoint) { e[0].MemberIDs = []string{"a", ""} },
		func(e []DeviceEndpoint) { e[0].MemberIDs = []string{"a", "c"} },
		func(e []DeviceEndpoint) {
			e[0].MemberIDs = []string{"a", "b"}
			e[1].MemberIDs = []string{"a", "b", "c"}
		},
	} {
		changed := append([]DeviceEndpoint(nil), valid...)
		change(changed)
		_, err := DeviceEndpointKey(changed)
		require.Error(t, err)
	}
}
