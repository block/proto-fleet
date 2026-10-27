package fleetmanagement

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

func TestWaitForPendingUnpairsReportsIncompleteDrain(t *testing.T) {
	service := &Service{}
	service.backgroundWg.Add(1)
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	require.ErrorIs(t, service.WaitForPendingUnpairs(ctx), context.DeadlineExceeded)
	service.backgroundWg.Done()
	require.NoError(t, service.WaitForPendingUnpairs(t.Context()))
}
