package maintenance

import (
	"testing"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/stretchr/testify/require"
)

func TestTicketAssetSiteFilterHonorsUnassignedOnlyForOrgWideScope(t *testing.T) {
	sites, excluded, unassigned := ticketAssetSiteFilter(true, []int64{9}, []int64{1, 2}, true)
	require.Equal(t, []int64{1, 2}, sites)
	require.Equal(t, []int64{9}, excluded)
	require.True(t, unassigned, "org-wide callers may list site-less assets")

	sites, excluded, unassigned = ticketAssetSiteFilter(true, nil, []int64{1}, false)
	require.Equal(t, []int64{1}, sites)
	require.Empty(t, excluded)
	require.False(t, unassigned)

	sites, excluded, unassigned = ticketAssetSiteFilter(false, []int64{1, 2}, []int64{2, 3}, true)
	require.Equal(t, []int64{2}, sites, "requested sites are clamped to the caller's scope")
	require.Empty(t, excluded)
	require.False(t, unassigned, "site-scoped callers must never see site-less assets")

	sites, _, unassigned = ticketAssetSiteFilter(false, []int64{1, 2}, nil, true)
	require.Equal(t, []int64{1, 2}, sites)
	require.False(t, unassigned)

	sites, _, unassigned = ticketAssetSiteFilter(false, nil, nil, true)
	require.Empty(t, sites)
	require.False(t, unassigned)
}

func TestTicketHistoryRejectsUnassignedPickerFilter(t *testing.T) {
	ticket := int64(7)
	request := &pb.ListTicketAssetOptionsRequest{TicketId: &ticket}
	require.NoError(t, protovalidate.Validate(request))
	request.IncludeUnassigned = true
	require.ErrorContains(t, protovalidate.Validate(request), "ticket history uses its recorded site")
}
