package control

import (
	"bytes"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestHAConnectionAndImportUpdatesAreBoundToRegistration(t *testing.T) {
	registry := NewRegistry()
	old := registry.Register(1)
	current := registry.Register(1)
	defer current.Unregister()
	require.True(t, current.SetHAConnection(1, &pb.ControlHello{HaCapable: true, InstanceId: "current"}, "current-session"))
	require.False(t, old.IsCurrent())
	require.False(t, old.SetHAConnection(1, &pb.ControlHello{HaCapable: true, InstanceId: "old"}, "old-session"))
	require.False(t, registry.UpdateHAFingerprint(1, "old-session", bytes.Repeat([]byte{1}, 32)))
	hello, session, ok := registry.HAConnection(1)
	require.True(t, ok)
	require.Equal(t, "current", hello.GetInstanceId())
	require.Equal(t, "current-session", session)
	require.Empty(t, hello.GetCredentialKeyFingerprint())
	fingerprint := bytes.Repeat([]byte{2}, 32)
	require.True(t, registry.UpdateHAFingerprint(1, "current-session", fingerprint))
	hello, session, ok = registry.HAConnection(1)
	require.True(t, ok)
	require.Equal(t, "current", hello.GetInstanceId())
	require.Equal(t, "current-session", session)
	require.Equal(t, fingerprint, hello.GetCredentialKeyFingerprint())
}
