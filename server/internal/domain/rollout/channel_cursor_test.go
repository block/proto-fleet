package rollout

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/require"

	rolloutv1 "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
)

func TestChannelPaginationAcceptsLongDeviceIdentifiers(t *testing.T) {
	for _, test := range []struct {
		name        string
		identifiers []string
	}{
		{"ASCII", []string{strings.Repeat("a", 254) + "0", strings.Repeat("a", 254) + "1", strings.Repeat("a", 254) + "2"}},
		{"Unicode", []string{strings.Repeat("😀", 254) + "😀", strings.Repeat("😀", 254) + "😁", strings.Repeat("😀", 254) + "😂"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, 0)
			ctx := t.Context()
			groupA := f.addGroup(t, "A")
			groupB := f.addGroup(t, "B")
			for _, identifier := range test.identifiers {
				f.addMiner(t, identifier, "Rig")
				f.placeInSet(t, groupA, "group", identifier)
			}
			a, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "A", Scope: Scope{GroupIDs: []int64{groupA}}})
			require.NoError(t, err)
			_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "B", Scope: Scope{GroupIDs: []int64{groupB}}})
			require.NoError(t, err)

			wantMiners, _, err := f.svc.ListChannelMiners(ctx, f.orgID, a.ID, "", "", 100, "")
			require.NoError(t, err)
			require.Len(t, wantMiners, 3)
			var gotMiners []ChannelMiner
			cursor := ""
			for page := range wantMiners {
				require.NoError(t, protovalidate.Validate(&rolloutv1.ListReleaseChannelMinersRequest{ChannelId: a.ID, PageSize: 1, Cursor: cursor}))
				miners, next, err := f.svc.ListChannelMiners(ctx, f.orgID, a.ID, "", "", 1, cursor)
				require.NoError(t, err)
				require.Len(t, miners, 1)
				require.NoError(t, protovalidate.Validate(&rolloutv1.ListReleaseChannelMinersResponse{Cursor: next}))
				gotMiners = append(gotMiners, miners...)
				if page == 0 {
					require.Greater(t, len(next), 100)
					// A cursor retains its sort key when its anchor leaves the channel.
					f.removeFromSet(t, groupA, miners[0].DeviceIdentifier)
				}
				cursor = next
			}
			require.Empty(t, cursor)
			require.Equal(t, wantMiners, gotMiners)

			f.placeInSet(t, groupA, "group", wantMiners[0].DeviceIdentifier)
			for _, identifier := range test.identifiers {
				f.placeInSet(t, groupB, "group", identifier)
			}
			wantConflicts, _, err := f.svc.ListMembershipConflicts(ctx, f.orgID, 0, 100, "")
			require.NoError(t, err)
			require.Len(t, wantConflicts, 6)
			var gotConflicts []MembershipConflict
			for page := range wantConflicts {
				require.NoError(t, protovalidate.Validate(&rolloutv1.ListReleaseChannelMembershipConflictsRequest{PageSize: 1, Cursor: cursor}))
				conflicts, next, err := f.svc.ListMembershipConflicts(ctx, f.orgID, 0, 1, cursor)
				require.NoError(t, err)
				require.Len(t, conflicts, 1)
				require.NoError(t, protovalidate.Validate(&rolloutv1.ListReleaseChannelMembershipConflictsResponse{Cursor: next}))
				gotConflicts = append(gotConflicts, conflicts...)
				if page < len(wantConflicts)-1 {
					require.Greater(t, len(next), 100)
				}
				cursor = next
			}
			require.Empty(t, cursor)
			require.Equal(t, wantConflicts, gotConflicts, "pagination must retain both channel relations for each miner")
		})
	}
}

func TestChannelCursorContractCoversMaximumSortKeys(t *testing.T) {
	identifier := strings.Repeat("😀", 255)
	maxID := strconv.FormatInt(math.MaxInt64, 10)
	minerCursor := encodeCursor(identifier, maxID)
	conflictCursor := encodeCursor(identifier, maxID, maxID)
	// The cursor bound includes UTF-8 bytes, separators, int64 IDs and base64 overhead.
	require.Len(t, minerCursor, 1387)
	require.Len(t, conflictCursor, 1414)
	require.NoError(t, protovalidate.Validate(&rolloutv1.ListReleaseChannelMinersRequest{ChannelId: 1, Cursor: minerCursor}))
	require.NoError(t, protovalidate.Validate(&rolloutv1.ListReleaseChannelMinersResponse{Cursor: minerCursor}))
	require.NoError(t, protovalidate.Validate(&rolloutv1.ListReleaseChannelMembershipConflictsRequest{Cursor: conflictCursor}))
	require.NoError(t, protovalidate.Validate(&rolloutv1.ListReleaseChannelMembershipConflictsResponse{Cursor: conflictCursor}))
}
