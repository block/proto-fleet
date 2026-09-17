package rollout

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
)

func TestScopeOverlapCarriesDomainReason(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	f.channel(t, allAtOnce, "miner-0")
	empty, err := f.svc.CreateChannel(ctx, f.orgID, testActor.ID, ChannelSpec{Name: "Empty channel"})
	require.NoError(t, err)
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			spec := ChannelSpec{Name: "Conflicting channel", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}}}
			var err error
			if operation == "create" {
				_, err = f.svc.CreateChannel(ctx, f.orgID, testActor.ID, spec)
			} else {
				_, err = f.svc.UpdateChannel(ctx, f.orgID, empty.ID, spec)
			}
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			require.ErrorContains(t, err, "scope overlaps release channel Test channel (1 miners)")
			info, ok := ReasonOf(err)
			require.True(t, ok, "a real scope conflict must carry its machine-readable domain reason")
			require.Equal(t, ReasonScopeOverlap, info.Reason)
		})
	}
}
