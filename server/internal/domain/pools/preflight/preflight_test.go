package preflight

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/pools/rewriter"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	sv1URL   = "stratum+tcp://pool.example.com:3333"
	sv2URL   = "stratum2+tcp://pool.example.com:34254"
	proxyURL = "stratum+tcp://127.0.0.1:34255"
)

type fakeCaps map[string]bool

func (f fakeCaps) Has(c string) bool { return f[c] }

func sv1Pool() rewriter.Pool {
	return rewriter.Pool{URL: sv1URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV1}
}
func sv2Pool() rewriter.Pool {
	return rewriter.Pool{URL: sv2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}
}

func TestRun_HappyPathAllNative(t *testing.T) {
	out, err := Run(Input{
		Slots: []SlotAssignment{
			{Slot: rewriter.SlotDefault, Pool: sv2Pool()},
			{Slot: rewriter.SlotBackup1, Pool: sv2Pool()},
		},
		Devices: []Device{
			{Identifier: "dev-1", Capabilities: fakeCaps{sdk.CapabilityStratumV2Native: true}},
			{Identifier: "dev-2", Capabilities: fakeCaps{sdk.CapabilityStratumV2Native: true}},
		},
		Proxy: rewriter.ProxyConfig{},
	})

	require.NoError(t, err)
	assert.False(t, out.HasMismatch)
	require.Len(t, out.Devices, 2)
	for _, d := range out.Devices {
		require.Len(t, d.Slots, 2)
		for _, s := range d.Slots {
			assert.Equal(t, sv2URL, s.EffectiveURL)
			assert.Equal(t, commandpb.SlotWarning_SLOT_WARNING_UNSPECIFIED, s.Warning)
		}
		assert.Equal(t, commandpb.DeviceWarning_DEVICE_WARNING_UNSPECIFIED, d.DeviceWarning)
	}
}

func TestRun_MixedFleetSV2PoolProxiesSV1DevicesOnly(t *testing.T) {
	out, err := Run(Input{
		Slots: []SlotAssignment{
			{Slot: rewriter.SlotDefault, Pool: sv2Pool()},
		},
		Devices: []Device{
			{Identifier: "native", Capabilities: fakeCaps{sdk.CapabilityStratumV2Native: true}},
			{Identifier: "sv1-only", Capabilities: fakeCaps{}},
		},
		Proxy: rewriter.ProxyConfig{ProxyEnabled: true, MinerURL: proxyURL, UpstreamURL: sv2URL},
	})

	require.NoError(t, err)
	assert.False(t, out.HasMismatch)

	byID := byIdentifier(out.Devices)
	assert.Equal(t, sv2URL, byID["native"].Slots[0].EffectiveURL)
	assert.Equal(t, proxyURL, byID["sv1-only"].Slots[0].EffectiveURL)
}

func TestRun_SV2ToSV1WithoutProxySurfacesSlotWarning(t *testing.T) {
	out, err := Run(Input{
		Slots: []SlotAssignment{
			{Slot: rewriter.SlotDefault, Pool: sv1Pool()},
			{Slot: rewriter.SlotBackup1, Pool: sv2Pool()},
		},
		Devices: []Device{
			{Identifier: "sv1-only", Capabilities: fakeCaps{}},
		},
		Proxy: rewriter.ProxyConfig{},
	})

	require.NoError(t, err)
	assert.True(t, out.HasMismatch)

	require.Len(t, out.Devices, 1)
	d := out.Devices[0]
	assert.Equal(t, commandpb.DeviceWarning_DEVICE_WARNING_UNSPECIFIED, d.DeviceWarning)

	require.Len(t, d.Slots, 2)
	assert.Equal(t, commandpb.SlotWarning_SLOT_WARNING_UNSPECIFIED, d.Slots[0].Warning)
	assert.Equal(t, commandpb.SlotWarning_SLOT_WARNING_SV2_NOT_SUPPORTED, d.Slots[1].Warning)

	mismatches := out.Mismatches()
	require.Len(t, mismatches, 1)
	assert.Equal(t, "sv1-only", mismatches[0].DeviceIdentifier)
	assert.Equal(t, commandpb.PoolSlot_POOL_SLOT_BACKUP_1, mismatches[0].Slot)
	assert.Equal(t, commandpb.SlotWarning_SLOT_WARNING_SV2_NOT_SUPPORTED, mismatches[0].SlotWarning)
}

func TestRun_MultipleSV2SlotsProxiedSurfacesDeviceWarning(t *testing.T) {
	out, err := Run(Input{
		Slots: []SlotAssignment{
			{Slot: rewriter.SlotDefault, Pool: sv2Pool()},
			{Slot: rewriter.SlotBackup1, Pool: sv2Pool()},
		},
		Devices: []Device{
			{Identifier: "sv1-only", Capabilities: fakeCaps{}},
		},
		Proxy: rewriter.ProxyConfig{ProxyEnabled: true, MinerURL: proxyURL, UpstreamURL: sv2URL},
	})

	require.NoError(t, err)
	assert.True(t, out.HasMismatch)

	d := out.Devices[0]
	assert.Equal(t, commandpb.DeviceWarning_DEVICE_WARNING_MULTIPLE_SV2_SLOTS_PROXIED, d.DeviceWarning)
	// Per-slot info still populated so the UI can render it.
	require.Len(t, d.Slots, 2)
	for _, s := range d.Slots {
		assert.Equal(t, proxyURL, s.EffectiveURL)
	}

	mismatches := out.Mismatches()
	require.Len(t, mismatches, 1)
	assert.Equal(t, commandpb.PoolSlot_POOL_SLOT_UNSPECIFIED, mismatches[0].Slot)
	assert.Equal(t, commandpb.DeviceWarning_DEVICE_WARNING_MULTIPLE_SV2_SLOTS_PROXIED, mismatches[0].DeviceWarning)
}

func TestRun_ValidatesInput(t *testing.T) {
	_, err := Run(Input{
		Slots:   nil,
		Devices: []Device{{Identifier: "x"}},
	})
	require.Error(t, err)

	_, err = Run(Input{
		Slots: []SlotAssignment{
			{Slot: rewriter.SlotDefault, Pool: sv1Pool()},
			{Slot: rewriter.SlotDefault, Pool: sv1Pool()},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate slot")
}

func TestRun_EmptyDevicesProducesEmptyOutput(t *testing.T) {
	out, err := Run(Input{
		Slots:   []SlotAssignment{{Slot: rewriter.SlotDefault, Pool: sv1Pool()}},
		Devices: nil,
	})
	require.NoError(t, err)
	assert.Empty(t, out.Devices)
	assert.False(t, out.HasMismatch)
	assert.Empty(t, out.Mismatches())
}

func byIdentifier(devs []DeviceResult) map[string]DeviceResult {
	m := make(map[string]DeviceResult, len(devs))
	for _, d := range devs {
		m[d.DeviceIdentifier] = d
	}
	return m
}
