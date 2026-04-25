package rewriter

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// fakeCaps is a minimal DeviceCapabilities for unit tests that does not
// exercise MergeCapabilities. Kept separate so the rewriter tests can
// focus on the resolution matrix without getting tangled up in merge logic.
type fakeCaps map[string]bool

func (f fakeCaps) Has(c string) bool { return f[c] }

var (
	sv1URL   = "stratum+tcp://pool.example.com:3333"
	sv1URL2  = "stratum+tcp://backup.example.com:3443"
	sv2URL   = "stratum2+tcp://pool.example.com:34254"
	proxyURL = "stratum+tcp://127.0.0.1:34255"
)

func TestPoolURLsForDevice_SingleSlotMatrix(t *testing.T) {
	cases := []struct {
		name         string
		poolProtocol poolspb.PoolProtocol
		poolURL      string
		sv2Native    bool
		proxyEnabled bool
		wantURL      string
		wantReason   RewriteReason
		wantErr      error
	}{
		{
			name:         "sv1 pool is passthrough regardless of capability",
			poolProtocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV1,
			poolURL:      sv1URL,
			wantURL:      sv1URL,
			wantReason:   ReasonPassthrough,
		},
		{
			name:         "unspecified protocol is treated as sv1",
			poolProtocol: poolspb.PoolProtocol_POOL_PROTOCOL_UNSPECIFIED,
			poolURL:      sv1URL,
			wantURL:      sv1URL,
			wantReason:   ReasonPassthrough,
		},
		{
			name:         "sv1 pool passthrough even when device is sv2-native",
			poolProtocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV1,
			poolURL:      sv1URL,
			sv2Native:    true,
			wantURL:      sv1URL,
			wantReason:   ReasonPassthrough,
		},
		{
			name:         "sv2 pool to sv2-native device goes direct",
			poolProtocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2,
			poolURL:      sv2URL,
			sv2Native:    true,
			wantURL:      sv2URL,
			wantReason:   ReasonNative,
		},
		{
			name:         "sv2 pool to sv2-native device goes direct even when proxy is on",
			poolProtocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2,
			poolURL:      sv2URL,
			sv2Native:    true,
			proxyEnabled: true,
			wantURL:      sv2URL,
			wantReason:   ReasonNative,
		},
		{
			name:         "sv2 pool to sv1 device via proxy when proxy enabled",
			poolProtocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2,
			poolURL:      sv2URL,
			proxyEnabled: true,
			wantURL:      proxyURL,
			wantReason:   ReasonProxied,
		},
		{
			name:         "sv2 pool to sv1 device rejected when proxy disabled",
			poolProtocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2,
			poolURL:      sv2URL,
			wantErr:      ErrSV2PoolNotSupportedByDevice,
		},
		{
			name:         "sv2 pool to sv1 device rejected when proxy enabled but miner URL blank",
			poolProtocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2,
			poolURL:      sv2URL,
			proxyEnabled: true,
			wantErr:      ErrSV2PoolNotSupportedByDevice,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caps := fakeCaps{}
			if tc.sv2Native {
				caps[sdk.CapabilityStratumV2Native] = true
			}
			proxy := ProxyConfig{}
			if tc.proxyEnabled {
				proxy.ProxyEnabled = true
				if tc.name != "sv2 pool to sv1 device rejected when proxy enabled but miner URL blank" {
					proxy.MinerURL = proxyURL
					// Match the proxy upstream to the test pool so the
					// rewriter doesn't reject with ErrProxyUpstreamMismatch
					// for cases that should succeed via the proxy.
					proxy.UpstreamURL = tc.poolURL
				}
			}

			resolved, err := PoolURLsForDevice(
				[]SlotAssignment{{
					Slot: SlotDefault,
					Pool: Pool{URL: tc.poolURL, Protocol: tc.poolProtocol},
				}},
				caps,
				proxy,
			)

			if tc.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tc.wantErr), "expected %v, got %v", tc.wantErr, err)
				return
			}

			require.NoError(t, err)
			require.Len(t, resolved, 1)
			assert.Equal(t, tc.wantURL, resolved[0].EffectiveURL)
			assert.Equal(t, tc.wantReason, resolved[0].RewriteReason)
		})
	}
}

func TestPoolURLsForDevice_RejectsMultipleProxiedSlots(t *testing.T) {
	caps := fakeCaps{} // sv1 device
	proxy := ProxyConfig{ProxyEnabled: true, MinerURL: proxyURL, UpstreamURL: sv2URL}

	_, err := PoolURLsForDevice(
		[]SlotAssignment{
			{Slot: SlotDefault, Pool: Pool{URL: sv2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
			{Slot: SlotBackup1, Pool: Pool{URL: sv2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
		},
		caps,
		proxy,
	)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMultipleSV2SlotsRequireProxy))
}

func TestPoolURLsForDevice_AllowsMultipleSV2SlotsWhenNative(t *testing.T) {
	// Multiple SV2 slots are fine when the device is native — no proxy
	// involvement, no primary/backup collapse risk.
	caps := fakeCaps{sdk.CapabilityStratumV2Native: true}
	proxy := ProxyConfig{}

	resolved, err := PoolURLsForDevice(
		[]SlotAssignment{
			{Slot: SlotDefault, Pool: Pool{URL: sv2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
			{Slot: SlotBackup1, Pool: Pool{URL: sv2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
			{Slot: SlotBackup2, Pool: Pool{URL: sv2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
		},
		caps,
		proxy,
	)

	require.NoError(t, err)
	require.Len(t, resolved, 3)
	for _, r := range resolved {
		assert.Equal(t, ReasonNative, r.RewriteReason)
		assert.Equal(t, sv2URL, r.EffectiveURL)
	}
}

func TestPoolURLsForDevice_MixedSV1AndProxiedSV2IsAllowed(t *testing.T) {
	// Primary SV1 + one SV2 backup through the proxy — exactly one proxied
	// slot, so the device-level rejection doesn't trip.
	caps := fakeCaps{} // sv1 device
	proxy := ProxyConfig{ProxyEnabled: true, MinerURL: proxyURL, UpstreamURL: sv2URL}

	resolved, err := PoolURLsForDevice(
		[]SlotAssignment{
			{Slot: SlotDefault, Pool: Pool{URL: sv1URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV1}},
			{Slot: SlotBackup1, Pool: Pool{URL: sv2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
			{Slot: SlotBackup2, Pool: Pool{URL: sv1URL2, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV1}},
		},
		caps,
		proxy,
	)

	require.NoError(t, err)
	require.Len(t, resolved, 3)
	assert.Equal(t, ReasonPassthrough, resolved[0].RewriteReason)
	assert.Equal(t, ReasonProxied, resolved[1].RewriteReason)
	assert.Equal(t, ReasonPassthrough, resolved[2].RewriteReason)
	assert.Equal(t, sv1URL, resolved[0].EffectiveURL)
	assert.Equal(t, proxyURL, resolved[1].EffectiveURL)
	assert.Equal(t, sv1URL2, resolved[2].EffectiveURL)
}

func TestPoolURLsForDevice_ErrorWrapsWithSlotLabel(t *testing.T) {
	caps := fakeCaps{} // sv1 device
	proxy := ProxyConfig{}

	_, err := PoolURLsForDevice(
		[]SlotAssignment{
			{Slot: SlotBackup1, Pool: Pool{URL: sv2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
		},
		caps,
		proxy,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "slot BACKUP_1")
	assert.True(t, errors.Is(err, ErrSV2PoolNotSupportedByDevice))
}

func TestPoolURLsForDevice_NilCapabilitiesFallbackIsSV1Only(t *testing.T) {
	_, err := PoolURLsForDevice(
		[]SlotAssignment{
			{Slot: SlotDefault, Pool: Pool{URL: sv2URL, Protocol: poolspb.PoolProtocol_POOL_PROTOCOL_SV2}},
		},
		nil,
		ProxyConfig{},
	)
	// No caps + no proxy + SV2 pool = rejection.
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSV2PoolNotSupportedByDevice))
}

// --- MergeCapabilities ---------------------------------------------------

func TestMergeCapabilities_Precedence(t *testing.T) {
	static := map[string]bool{
		"polling_host":                true,
		sdk.CapabilityStratumV2Native: false,
	}
	model := map[string]bool{
		"polling_host": false, // model overrides static
	}

	cases := []struct {
		name          string
		telemetry     modelsV2.StratumV2SupportStatus
		wantPolling   bool
		wantSV2Native bool
	}{
		{
			name:          "telemetry supported wins over static false",
			telemetry:     modelsV2.StratumV2SupportSupported,
			wantPolling:   false,
			wantSV2Native: true,
		},
		{
			name:          "telemetry unsupported wins over static false",
			telemetry:     modelsV2.StratumV2SupportUnsupported,
			wantPolling:   false,
			wantSV2Native: false,
		},
		{
			name:          "telemetry unknown leaves static value alone",
			telemetry:     modelsV2.StratumV2SupportUnknown,
			wantPolling:   false,
			wantSV2Native: false,
		},
		{
			name:          "telemetry unspecified leaves static value alone",
			telemetry:     modelsV2.StratumV2SupportUnspecified,
			wantPolling:   false,
			wantSV2Native: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged := MergeCapabilities(static, model, tc.telemetry)
			assert.Equal(t, tc.wantPolling, merged.Has("polling_host"))
			assert.Equal(t, tc.wantSV2Native, merged.Has(sdk.CapabilityStratumV2Native))
		})
	}
}

func TestMergeCapabilities_TelemetryPromotesMissingStatic(t *testing.T) {
	// Static has no opinion on SV2 (common case for untouched drivers);
	// telemetry SUPPORTED should introduce the bit as true.
	static := map[string]bool{}
	model := map[string]bool{}

	merged := MergeCapabilities(static, model, modelsV2.StratumV2SupportSupported)
	assert.True(t, merged.Has(sdk.CapabilityStratumV2Native))
}

func TestMergeCapabilities_TelemetryUnknownDoesNotDeleteStatic(t *testing.T) {
	static := map[string]bool{sdk.CapabilityStratumV2Native: true}

	merged := MergeCapabilities(static, nil, modelsV2.StratumV2SupportUnknown)
	assert.True(t, merged.Has(sdk.CapabilityStratumV2Native))
}

func TestMergeCapabilities_InputsLeftUnchanged(t *testing.T) {
	static := map[string]bool{"a": true}
	model := map[string]bool{"b": true}

	_ = MergeCapabilities(static, model, modelsV2.StratumV2SupportSupported)
	assert.Len(t, static, 1)
	assert.Len(t, model, 1)
	_, ok := static[sdk.CapabilityStratumV2Native]
	assert.False(t, ok, "static should not be mutated")
}
