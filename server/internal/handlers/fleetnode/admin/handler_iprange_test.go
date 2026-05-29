package admin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestExpandIPv4Range_Inclusive(t *testing.T) {
	// Act
	got, err := expandIPv4Range("10.0.0.5", "10.0.0.7")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.5", "10.0.0.6", "10.0.0.7"}, got)
}

func TestExpandIPv4Range_SkipsNetworkAndGatewayStart(t *testing.T) {
	// Act: a range starting at .0 skips the network (.0) and gateway (.1)
	// addresses, matching the agent's own IPRange handling.
	got, err := expandIPv4Range("10.0.0.0", "10.0.0.4")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"}, got)
}

func TestExpandIPv4Range_RejectsNetworkGatewayOnlyRange(t *testing.T) {
	// Act: a range covering only .0/.1 has nothing left after the skip.
	_, err := expandIPv4Range("10.0.0.0", "10.0.0.1")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "network/gateway")
}

func TestExpandIPv4Range_SingleAddress(t *testing.T) {
	// Act
	got, err := expandIPv4Range("192.168.1.5", "192.168.1.5")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"192.168.1.5"}, got)
}

func TestExpandIPv4Range_RejectsInvalidStartIP(t *testing.T) {
	// Act
	_, err := expandIPv4Range("not-an-ip", "10.0.0.5")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "start_ip")
}

func TestExpandIPv4Range_RejectsIPv6(t *testing.T) {
	// Act
	_, err := expandIPv4Range("2001:db8::1", "2001:db8::5")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestExpandIPv4Range_RejectsEndBeforeStart(t *testing.T) {
	// Act
	_, err := expandIPv4Range("10.0.0.10", "10.0.0.5")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), ">=")
}

func TestExpandIPv4Range_RejectsOverflow(t *testing.T) {
	// Act
	_, err := expandIPv4Range("10.0.0.0", "10.0.16.0")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "exceeds")
}

func TestExpandIPv4Range_RejectsPublicRange(t *testing.T) {
	// Act
	_, err := expandIPv4Range("8.8.8.8", "8.8.8.10")

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "private")
}
