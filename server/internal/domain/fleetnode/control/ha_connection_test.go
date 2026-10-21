package control

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestHAConnectionIsBoundToRegistration(t *testing.T) {
	registry := NewRegistry()
	old, err := registry.RegisterAuthenticated(1, "bearer", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1)
	require.NoError(t, err)
	current, err := registry.RegisterAuthenticated(1, "bearer", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1)
	require.NoError(t, err)
	defer current.Unregister()
	require.True(t, current.SetHAConnection(1, &pb.ControlHello{HaCapable: true, InstanceId: "current"}, "current-session"))
	require.False(t, old.IsCurrent())
	require.False(t, old.SetHAConnection(1, &pb.ControlHello{HaCapable: true, InstanceId: "old"}, "old-session"))
	hello, session, ok := registry.HAConnection(1)
	require.True(t, ok)
	require.Equal(t, "current", hello.GetInstanceId())
	require.Equal(t, "current-session", session)
	require.Empty(t, hello.GetCredentialKeyFingerprint())
}
