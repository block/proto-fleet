package pairing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldScheduleTelemetryForPairingStatus(t *testing.T) {
	assert.True(t, shouldScheduleTelemetryForPairingStatus(StatusPaired))
	assert.True(t, shouldScheduleTelemetryForPairingStatus(StatusDefaultPassword))
	assert.False(t, shouldScheduleTelemetryForPairingStatus(StatusAuthenticationNeeded))
	assert.False(t, shouldScheduleTelemetryForPairingStatus(StatusUnpaired))
	assert.False(t, shouldScheduleTelemetryForPairingStatus(StatusPending))
	assert.False(t, shouldScheduleTelemetryForPairingStatus(StatusFailed))
}
