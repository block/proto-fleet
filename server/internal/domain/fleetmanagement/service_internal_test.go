package fleetmanagement

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRefreshMinersRequestTimeoutScalesByConcurrencyWaves(t *testing.T) {
	refreshDeviceTimeout := 30 * time.Second
	perWaveTimeout := 2*refreshDeviceTimeout + refreshMinersSnapshotTimeout

	assert.Equal(t, perWaveTimeout, refreshMinersRequestTimeout(1, refreshDeviceTimeout))
	assert.Equal(t, perWaveTimeout, refreshMinersRequestTimeout(refreshMinersConcurrencyLimit, refreshDeviceTimeout))
	assert.Equal(t, 2*perWaveTimeout, refreshMinersRequestTimeout(refreshMinersConcurrencyLimit+1, refreshDeviceTimeout))
	assert.Equal(t, 5*perWaveTimeout, refreshMinersRequestTimeout(refreshMinersMaxDevices, refreshDeviceTimeout))
	assert.Equal(t, 310*time.Second, refreshMinersRequestTimeout(refreshMinersMaxDevices, refreshDeviceTimeout))
	assert.Equal(t, 2*refreshMinersPerDeviceTimeout+refreshMinersSnapshotTimeout, refreshMinersRequestTimeout(1, 0))
}
