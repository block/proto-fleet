package preflight

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/pools/rewriter"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// The scenarios below are the three "Step 16 E2E" cases called out in
// docs/stratum-v2-plan.md. They exercise the shared preflight at its
// RPC-shaped boundary so the commit path and the preview path agree on
// every axis: which slots are marked which RewriteReason, which warning
// enum values fire, and which slots carry the proxy URL.
//
// A true end-to-end test (virtual-miner Docker stack + real tProxy +
// live Fleet API roundtrip) lives outside this PR's scope — the
// preflight is the serialisation boundary that covers >80% of the risk
// since dispatch never re-evaluates the decision.

const (
	scenarioSV1URL   = "stratum+tcp://pool.example.com:3333"
	scenarioSV2URL   = "stratum2+tcp://pool.example.com:34254"
	scenarioProxyURL = "stratum+tcp://127.0.0.1:34255"
)

func TestScenario_MixedFleetSV2PoolAssignment_BothCohortsRoutedCorrectly(t *testing.T) {
	// Native-SV2 miner + SV1-only miner, proxy enabled, single SV2 pool.
	// Native miner should go direct (REWRITE_REASON_NATIVE); SV1 miner
	// should be rewritten to the proxy URL (REWRITE_REASON_PROXIED).
	// Neither device or slot warning fires.
	out, err := Run(Input{
		Slots: []SlotAssignment{{
			Slot: rewriter.SlotDefault,
			Pool: rewriter.Pool{URL: scenarioSV2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2},
		}},
		Devices: []Device{
			{Identifier: "native-sv2", Capabilities: fakeCaps{sdk.CapabilityStratumV2Native: true}},
			{Identifier: "sv1-only", Capabilities: fakeCaps{}},
		},
		Proxy: rewriter.ProxyConfig{ProxyEnabled: true, MinerURL: scenarioProxyURL},
	})

	require.NoError(t, err)
	require.False(t, out.HasMismatch, "mixed fleet with proxy should have no preflight mismatch")

	results := byIdentifier(out.Devices)

	native := results["native-sv2"]
	require.Len(t, native.Slots, 1)
	assert.Equal(t, commandpb.RewriteReason_REWRITE_REASON_NATIVE, native.Slots[0].ProtoReason)
	assert.Equal(t, scenarioSV2URL, native.Slots[0].EffectiveURL, "native miner receives the pool's own SV2 URL")
	assert.Equal(t, commandpb.SlotWarning_SLOT_WARNING_UNSPECIFIED, native.Slots[0].Warning)
	assert.Equal(t, commandpb.DeviceWarning_DEVICE_WARNING_UNSPECIFIED, native.DeviceWarning)

	sv1 := results["sv1-only"]
	require.Len(t, sv1.Slots, 1)
	assert.Equal(t, commandpb.RewriteReason_REWRITE_REASON_PROXIED, sv1.Slots[0].ProtoReason)
	assert.Equal(t, scenarioProxyURL, sv1.Slots[0].EffectiveURL, "SV1 miner receives the proxy's LAN-facing URL")
	assert.Equal(t, commandpb.SlotWarning_SLOT_WARNING_UNSPECIFIED, sv1.Slots[0].Warning)

	// No mismatches means the commit path would enqueue per-device
	// payloads and the preview path would return zero-warning previews
	// — by construction they agree.
	assert.Empty(t, out.Mismatches())
}

func TestScenario_SV2PoolAssignedWithProxyOff_SynchronousTypedRejection(t *testing.T) {
	// Same SV2 pool, same SV1 miner — but proxy disabled. Preflight
	// must surface SLOT_WARNING_SV2_NOT_SUPPORTED, and the commit path
	// the preflight feeds must reject the whole batch with
	// FAILED_PRECONDITION carrying the typed mismatch.
	out, err := Run(Input{
		Slots: []SlotAssignment{{
			Slot: rewriter.SlotDefault,
			Pool: rewriter.Pool{URL: scenarioSV2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2},
		}},
		Devices: []Device{
			{Identifier: "sv1-only", Capabilities: fakeCaps{}},
		},
		Proxy: rewriter.ProxyConfig{}, // proxy off
	})

	require.NoError(t, err)
	require.True(t, out.HasMismatch, "SV2-to-SV1 without proxy is the canonical mismatch")

	dev := out.Devices[0]
	require.Len(t, dev.Slots, 1)
	assert.Equal(t, commandpb.SlotWarning_SLOT_WARNING_SV2_NOT_SUPPORTED, dev.Slots[0].Warning)
	assert.Equal(t, commandpb.RewriteReason_REWRITE_REASON_UNSPECIFIED, dev.Slots[0].ProtoReason)
	assert.Equal(t, commandpb.DeviceWarning_DEVICE_WARNING_UNSPECIFIED, dev.DeviceWarning)

	mismatches := out.Mismatches()
	require.Len(t, mismatches, 1)
	assert.Equal(t, "sv1-only", mismatches[0].DeviceIdentifier)
	assert.Equal(t, commandpb.PoolSlot_POOL_SLOT_DEFAULT, mismatches[0].Slot)
	assert.Equal(t, commandpb.SlotWarning_SLOT_WARNING_SV2_NOT_SUPPORTED, mismatches[0].SlotWarning)
	// Device-level warning is UNSPECIFIED since the problem is slot-scoped.
	assert.Equal(t, commandpb.DeviceWarning_DEVICE_WARNING_UNSPECIFIED, mismatches[0].DeviceWarning)
}

func TestScenario_ThreeSV2PoolsOnSV1Miner_DeviceWarningFires(t *testing.T) {
	// The single bundled proxy has exactly one upstream pool. Pointing
	// three SV2 slots at the same proxy URL on one device would silently
	// collapse primary + both backups, so preflight rejects with the
	// device-scoped warning rather than proceeding. Per-slot REWRITE_REASON
	// stays PROXIED so the UI can show the operator what would have
	// happened and why we refused.
	out, err := Run(Input{
		Slots: []SlotAssignment{
			{Slot: rewriter.SlotDefault, Pool: rewriter.Pool{URL: scenarioSV2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
			{Slot: rewriter.SlotBackup1, Pool: rewriter.Pool{URL: scenarioSV2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
			{Slot: rewriter.SlotBackup2, Pool: rewriter.Pool{URL: scenarioSV2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
		},
		Devices: []Device{
			{Identifier: "sv1-only", Capabilities: fakeCaps{}},
		},
		Proxy: rewriter.ProxyConfig{ProxyEnabled: true, MinerURL: scenarioProxyURL},
	})

	require.NoError(t, err)
	require.True(t, out.HasMismatch)

	dev := out.Devices[0]
	assert.Equal(t, commandpb.DeviceWarning_DEVICE_WARNING_MULTIPLE_SV2_SLOTS_PROXIED, dev.DeviceWarning)
	require.Len(t, dev.Slots, 3)
	for _, s := range dev.Slots {
		assert.Equal(t, commandpb.RewriteReason_REWRITE_REASON_PROXIED, s.ProtoReason,
			"per-slot resolution still reports PROXIED so the UI can explain the rejection")
	}

	mismatches := out.Mismatches()
	require.Len(t, mismatches, 1)
	assert.Equal(t, "sv1-only", mismatches[0].DeviceIdentifier)
	// Device-level warning carries PoolSlot_POOL_SLOT_UNSPECIFIED because
	// the problem is the combination of slots, not any single one.
	assert.Equal(t, commandpb.PoolSlot_POOL_SLOT_UNSPECIFIED, mismatches[0].Slot)
	assert.Equal(t, commandpb.DeviceWarning_DEVICE_WARNING_MULTIPLE_SV2_SLOTS_PROXIED, mismatches[0].DeviceWarning)
}

func TestScenario_SV1PoolAssignment_AlwaysPassthrough(t *testing.T) {
	// Regression guard: SV1 pool to any device, regardless of SV2
	// capability or proxy config, must pass through untouched. The
	// worst-case outcome of SV2 wiring would be if an SV1 assignment
	// started getting rewritten somewhere.
	out, err := Run(Input{
		Slots: []SlotAssignment{{
			Slot: rewriter.SlotDefault,
			Pool: rewriter.Pool{URL: scenarioSV1URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV1},
		}},
		Devices: []Device{
			{Identifier: "sv2-native", Capabilities: fakeCaps{sdk.CapabilityStratumV2Native: true}},
			{Identifier: "sv1-only", Capabilities: fakeCaps{}},
		},
		Proxy: rewriter.ProxyConfig{ProxyEnabled: true, MinerURL: scenarioProxyURL},
	})

	require.NoError(t, err)
	require.False(t, out.HasMismatch)

	for _, dev := range out.Devices {
		require.Len(t, dev.Slots, 1)
		assert.Equal(t, commandpb.RewriteReason_REWRITE_REASON_PASSTHROUGH, dev.Slots[0].ProtoReason)
		assert.Equal(t, scenarioSV1URL, dev.Slots[0].EffectiveURL)
	}
}

func TestScenario_TelemetryWinsOverStaticForSV2Capability(t *testing.T) {
	// The capability merge is where a live fleet's "this firmware was
	// just upgraded to SV2-native" actually takes effect. Static caps
	// have no SV2 bit (same as the default static view for every plugin
	// today); telemetry reports Supported on one device and Unsupported
	// on the other. The preflight should route the Supported device
	// direct and the Unsupported device via the proxy.
	native := rewriter.MergeCapabilities(nil, nil, modelsV2.StratumV2SupportSupported)
	sv1 := rewriter.MergeCapabilities(nil, nil, modelsV2.StratumV2SupportUnsupported)

	out, err := Run(Input{
		Slots: []SlotAssignment{{
			Slot: rewriter.SlotDefault,
			Pool: rewriter.Pool{URL: scenarioSV2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2},
		}},
		Devices: []Device{
			{Identifier: "native", Capabilities: native},
			{Identifier: "sv1", Capabilities: sv1},
		},
		Proxy: rewriter.ProxyConfig{ProxyEnabled: true, MinerURL: scenarioProxyURL},
	})

	require.NoError(t, err)
	require.False(t, out.HasMismatch)

	results := byIdentifier(out.Devices)
	assert.Equal(t, commandpb.RewriteReason_REWRITE_REASON_NATIVE, results["native"].Slots[0].ProtoReason)
	assert.Equal(t, commandpb.RewriteReason_REWRITE_REASON_PROXIED, results["sv1"].Slots[0].ProtoReason)
}

