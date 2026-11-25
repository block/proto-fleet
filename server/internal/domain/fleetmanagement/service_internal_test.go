package fleetmanagement

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

func TestValidatePageSize(t *testing.T) {
	tests := []struct {
		name      string
		requested int32
		want      int32
	}{
		{name: "zero uses the default", requested: 0, want: 50},
		{name: "negative uses the default", requested: -1, want: 50},
		{name: "one stays", requested: 1, want: 1},
		{name: "default stays", requested: 50, want: 50},
		{name: "maximum stays", requested: 1000, want: 1000},
		{name: "one above the maximum is capped", requested: 1001, want: 1000},
		{name: "large value is capped", requested: 5000, want: 1000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, validatePageSize(tc.requested))
		})
	}
}

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
