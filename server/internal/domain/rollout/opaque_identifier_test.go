package rollout

import (
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rolloutv1 "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
)

func TestScopeDeviceIdentifierContractPreservesWhitespace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		ids     []string
		wantErr bool
	}{
		{name: "empty scope"},
		{name: "empty identifier", ids: []string{""}, wantErr: true},
		{name: "whitespace identifier", ids: []string{" \t\u00a0"}},
		{name: "padded identifier", ids: []string{" miner-0 "}},
		{name: "exact duplicates", ids: []string{" miner-0 ", " miner-0 "}},
		{name: "distinct spellings", ids: []string{"miner-0", " miner-0 ", "\tminer-0\t"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := protovalidate.Validate(&rolloutv1.ReleaseChannelScope{DeviceIdentifiers: tc.ids})
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestChannelScopeUsesExactDeviceIdentifiers(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	const paddedID = " miner-0 "
	const whitespaceID = " \t\u00a0"
	f.addMiner(t, paddedID, "Padded")
	f.addMiner(t, whitespaceID, "Whitespace")

	// Both the unpadded identifier and a distinct whitespace-bearing key
	// exist in storage. Preview must describe the requested miner only.
	preview, err := f.svc.PreviewScope(ctx, f.orgID, Scope{DeviceIdentifiers: []string{paddedID, paddedID}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(1), preview.MinerCount)
	assert.Equal(t, []ModelCount{{Manufacturer: "proto", Model: "Padded", MinerCount: 1}}, preview.Models)

	channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{
		Name: "Opaque identifiers", Scope: Scope{DeviceIdentifiers: []string{paddedID, whitespaceID, paddedID}},
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{paddedID, whitespaceID}, channel.Scope.DeviceIdentifiers)
	assert.Equal(t, int32(2), channel.MinerCount)
	miners, _, err := f.svc.ListChannelMiners(ctx, f.orgID, channel.ID, "", "", 0, "")
	require.NoError(t, err)
	var identifiers []string
	for _, miner := range miners {
		identifiers = append(identifiers, miner.DeviceIdentifier)
	}
	assert.ElementsMatch(t, []string{paddedID, whitespaceID}, identifiers)

	// The unpadded miner remains available to another channel: these keys
	// must not create a false overlap or merge separate miners.
	_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{
		Name: "Unpadded identifier", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}},
	})
	require.NoError(t, err)

	channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
		Name: "Opaque identifiers", Scope: Scope{DeviceIdentifiers: []string{whitespaceID, whitespaceID}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{whitespaceID}, channel.Scope.DeviceIdentifiers)
	assert.Equal(t, int32(1), channel.MinerCount)

	// A spelling that is absent must stay absent even when trimming it
	// would produce an existing identifier.
	missingScope := Scope{DeviceIdentifiers: []string{"miner-0 "}}
	preview, err = f.svc.PreviewScope(ctx, f.orgID, missingScope, 0)
	require.NoError(t, err)
	assert.Zero(t, preview.MinerCount)
	assert.Empty(t, preview.Conflicts)
	_, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{Name: "Opaque identifiers", Scope: missingScope})
	require.ErrorContains(t, err, `unknown miner "miner-0 "`)
	channel, err = f.svc.GetChannel(ctx, f.orgID, channel.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{whitespaceID}, channel.Scope.DeviceIdentifiers)
}
