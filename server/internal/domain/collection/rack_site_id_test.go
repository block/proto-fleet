package collection

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
)

// rackSiteID must read the site from Placement (what GetCollection populates),
// NOT from TypeDetails.RackInfo (which GetCollection leaves nil). A regression
// here silently routes rack slot-position activity into /unassigned/activity.
func TestRackSiteID(t *testing.T) {
	t.Parallel()

	siteID := int64(7)

	cases := []struct {
		name string
		coll *pb.DeviceCollection
		want *int64
	}{
		{
			name: "site from placement (the GetCollection-populated field)",
			coll: &pb.DeviceCollection{
				Placement: &commonpb.PlacementRefs{Site: &commonpb.ResourceRef{Id: siteID}},
			},
			want: &siteID,
		},
		{
			name: "no placement site → unassigned (nil)",
			coll: &pb.DeviceCollection{Placement: &commonpb.PlacementRefs{}},
			want: nil,
		},
		{
			name: "nil placement → unassigned (nil)",
			coll: &pb.DeviceCollection{},
			want: nil,
		},
		{
			name: "rack_info site is ignored (GetCollection never fills it)",
			coll: &pb.DeviceCollection{
				TypeDetails: &pb.DeviceCollection_RackInfo{RackInfo: &pb.RackInfo{SiteId: &siteID}},
			},
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := rackSiteID(tc.coll)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
		})
	}
}
