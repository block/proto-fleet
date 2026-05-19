package fleetnodeadmin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestExpandIPv4Range_Inclusive(t *testing.T) {
	// Act
	got, err := expandIPv4Range("10.0.0.1", "10.0.0.3")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, got)
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
