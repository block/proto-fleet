package fleetmanagement

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRefreshMinersRequestTimeoutScalesByConcurrencyWaves(t *testing.T) {
	perWaveTimeout := refreshMinersPerDeviceTimeout + refreshMinersSnapshotTimeout

	assert.Equal(t, perWaveTimeout, refreshMinersRequestTimeout(1))
	assert.Equal(t, perWaveTimeout, refreshMinersRequestTimeout(refreshMinersConcurrencyLimit))
	assert.Equal(t, 2*perWaveTimeout, refreshMinersRequestTimeout(refreshMinersConcurrencyLimit+1))
	assert.Equal(t, 5*perWaveTimeout, refreshMinersRequestTimeout(refreshMinersMaxDevices))
	assert.Equal(t, 35*time.Second, refreshMinersRequestTimeout(refreshMinersMaxDevices))
}
