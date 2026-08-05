package ha

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEndpointHealthRequiresLocalAddressAndFreshHeartbeat(t *testing.T) {
	// Arrange
	heartbeat := filepath.Join(t.TempDir(), "endpoint-heartbeat")
	require.NoError(t, os.WriteFile(heartbeat, nil, 0o600))
	healthy, err := newEndpointHealth("127.0.0.1", heartbeat, time.Second)
	require.NoError(t, err)

	// Act and assert
	require.True(t, healthy())
	stale := time.Now().Add(-2 * time.Second)
	require.NoError(t, os.Chtimes(heartbeat, stale, stale))
	require.False(t, healthy())

	future := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(heartbeat, future, future))
	require.False(t, healthy())

	unassigned, err := newEndpointHealth("192.0.2.1", heartbeat, time.Second)
	require.NoError(t, err)
	require.False(t, unassigned())
}
