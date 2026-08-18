package sqlstores

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/block/proto-fleet/server/internal/domain/channel"
)

func TestFirmwareTransitionStateMapsChannelEnforcementStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		expected channel.FirmwareTransitionState
	}{
		{"pending", channel.FirmwareTransitionPending},
		{"held", channel.FirmwareTransitionPending},
		{"dispatching", channel.FirmwareTransitionUpdating},
		{"dispatched", channel.FirmwareTransitionUpdating},
		{"verifying", channel.FirmwareTransitionVerifying},
		{"confirmed", channel.FirmwareTransitionConfirmed},
		{"attention_required", channel.FirmwareTransitionNeedsAttention},
		{"cancelled", channel.FirmwareTransitionNeedsAttention},
	}

	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.expected, firmwareTransitionState(channel.EnforcementState(test.raw)))
		})
	}
}
