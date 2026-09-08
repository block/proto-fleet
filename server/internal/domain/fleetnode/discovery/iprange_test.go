package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/netutil"
)

func TestValidatedIPv4Range_Inclusive(t *testing.T) {
	// Act
	start, end, err := validatedIPv4Range("10.0.0.5", "10.0.0.7")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.5", netutil.Uint32ToIPv4(start))
	assert.Equal(t, "10.0.0.7", netutil.Uint32ToIPv4(end))
}

func TestValidatedIPv4Range_SkipsNetworkAndGatewayStart(t *testing.T) {
	// Act: a range starting at .0 skips the network (.0) and gateway (.1)
	// addresses, matching the agent's own IPRange handling.
	start, end, err := validatedIPv4Range("10.0.0.0", "10.0.0.4")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", netutil.Uint32ToIPv4(start))
	assert.Equal(t, "10.0.0.4", netutil.Uint32ToIPv4(end))
}

func TestValidatedIPv4Range_RejectsNetworkGatewayOnlyRange(t *testing.T) {
	// Act: a range covering only .0/.1 has nothing left after the skip.
	_, _, err := validatedIPv4Range("10.0.0.0", "10.0.0.1")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "network/gateway")
}

func TestValidatedIPv4Range_SingleAddress(t *testing.T) {
	// Act
	start, end, err := validatedIPv4Range("192.168.1.5", "192.168.1.5")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, start, end)
	assert.Equal(t, "192.168.1.5", netutil.Uint32ToIPv4(start))
}

func TestValidatedIPv4Range_RejectsInvalidStartIP(t *testing.T) {
	// Act
	_, _, err := validatedIPv4Range("not-an-ip", "10.0.0.5")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "start_ip")
}

func TestValidatedIPv4Range_RejectsIPv6(t *testing.T) {
	// Act
	_, _, err := validatedIPv4Range("2001:db8::1", "2001:db8::5")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestValidatedIPv4Range_RejectsEndBeforeStart(t *testing.T) {
	// Act
	_, _, err := validatedIPv4Range("10.0.0.10", "10.0.0.5")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), ">=")
}

func TestValidatedIPv4Range_RejectsOverflow(t *testing.T) {
	// Act
	_, _, err := validatedIPv4Range("10.0.0.0", "10.0.16.0")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "exceeds")
}

func TestValidatedIPv4Range_RejectsPublicRange(t *testing.T) {
	// Act
	_, _, err := validatedIPv4Range("8.8.8.8", "8.8.8.10")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "private")
}
