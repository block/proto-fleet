package device

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/plugin/virtual/internal/config"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func newTestDevice(t *testing.T) *Device {
	t.Helper()
	cfg := &config.VirtualMinerConfig{
		SerialNumber:        "test-device",
		Model:               "VirtualMiner-Test",
		Manufacturer:        "Test",
		BaselineHashrateTHS: 100,
		BaselinePowerW:      3000,
		BaselineTempC:       60,
		Hashboards:          3,
		ASICsPerBoard:       3,
		FanCount:            4,
	}
	d := New("test-device", sdk.DeviceInfo{
		Host:         "127.0.0.1",
		Port:         4028,
		Manufacturer: "Test",
		Model:        "VirtualMiner-Test",
	}, cfg)
	require.NotNil(t, d)
	return d
}

// CapabilityCurtail must be advertised so the curtailment selector pulls
// virtual miners into v1 events. CapabilityCurtailEfficiency / Partial
// remain off because they are reserved for v4.
func TestDevice_DescribeDevice_AdvertisesCurtailCapability(t *testing.T) {
	d := newTestDevice(t)

	_, caps, err := d.DescribeDevice(context.Background())
	require.NoError(t, err)
	assert.True(t, caps[sdk.CapabilityCurtail], "virtual plugin must advertise CapabilityCurtail")
	assert.False(t, caps[sdk.CapabilityCurtailEfficiency], "v4 efficiency curtailment is reserved")
	assert.False(t, caps[sdk.CapabilityCurtailPartial], "v4 partial curtailment is reserved")
}

// FULL curtailment is the only honored level in v1; the virtual plugin must
// stop mining and remember the level so subsequent telemetry reflects the
// curtailed state. Uncurtail returns the miner to mining.
func TestDevice_CurtailFull_ThenUncurtail(t *testing.T) {
	d := newTestDevice(t)

	require.NoError(t, d.Curtail(context.Background(), sdk.CurtailLevelFull))
	assert.False(t, d.isMining, "Curtail(FULL) must stop mining")
	assert.Equal(t, sdk.CurtailLevelFull, d.curtailLevel, "Curtail(FULL) must record the level")

	require.NoError(t, d.Uncurtail(context.Background()))
	assert.True(t, d.isMining, "Uncurtail must restart mining")
	assert.Equal(t, sdk.CurtailLevelUnspecified, d.curtailLevel, "Uncurtail must clear the level")
}

// Higher levels (efficiency, partial) are reserved for v4; the plugin must
// surface a permanent CurtailCapabilityNotSupported error so the curtailment
// reconciler can mark the target failed instead of retrying forever.
func TestDevice_CurtailUnsupportedLevel_ReturnsCapabilityNotSupported(t *testing.T) {
	d := newTestDevice(t)

	cases := []sdk.CurtailLevel{sdk.CurtailLevelEfficiency, sdk.CurtailLevelPartialPercent}
	for _, level := range cases {
		err := d.Curtail(context.Background(), level)
		require.Error(t, err)
		var sdkErr sdk.SDKError
		require.True(t, errors.As(err, &sdkErr), "expected sdk.SDKError, got %T", err)
		assert.Equal(t, sdk.ErrCodeCurtailCapabilityNotSupported, sdkErr.Code)
		assert.True(t, d.isMining, "unsupported-level Curtail must not change mining state")
	}
}

// Uncurtail when the miner is not curtailed is a safe no-op so duplicate
// restore dispatches from the reconciler do not error.
func TestDevice_UncurtailWhileNotCurtailed_IsNoop(t *testing.T) {
	d := newTestDevice(t)
	wasMining := d.isMining

	require.NoError(t, d.Uncurtail(context.Background()))
	assert.Equal(t, wasMining, d.isMining, "Uncurtail on a non-curtailed miner should not change state")
}
