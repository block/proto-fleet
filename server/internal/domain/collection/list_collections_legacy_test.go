package collection

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// TestListCollections_LegacyZonesTranslate covers PR #249 review G1:
// the deprecated collection.v1.ListCollections RPC must keep working
// during the wire-contract sunset (#255). req.Zones is shimmed into
// wildcard ZoneKeys so the store sees an equivalent filter to the
// pre-#229 behavior.
func TestListCollections_LegacyZonesTranslate(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	mockStore.EXPECT().
		ListCollections(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_RACK,
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_, _, _, _, _, _ any, filter *interfaces.DeviceSetFilter) ([]*pb.DeviceCollection, string, int32, error) {
			require.NotNil(t, filter)
			require.Len(t, filter.ZoneKeys, 2)
			for _, zk := range filter.ZoneKeys {
				assert.Equal(t, int64(0), zk.BuildingID, "legacy zones must shim to wildcard ZoneKey")
			}
			return nil, "", 0, nil
		})

	_, err := svc.ListCollections(ctx, &pb.ListCollectionsRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_RACK,
		Zones: []string{"Room 2", "Cold Aisle"}, //nolint:staticcheck // SA1019 — exercising the deprecated path
	})
	require.NoError(t, err)
}

// TestListCollections_LegacyZonesRejectsNonRack restores the pre-#229
// contract: zone filter on a non-rack collection type must surface
// InvalidArgument rather than silently returning broader results.
func TestListCollections_LegacyZonesRejectsNonRack(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := testCtx(t)

	_, err := svc.ListCollections(ctx, &pb.ListCollectionsRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_GROUP,
		Zones: []string{"Room 2"}, //nolint:staticcheck // SA1019 — exercising the deprecated path
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "rack")
}

// TestListCollections_LegacyZonesRejectsEmpty mirrors the convert.go
// and parseFilter empty-zone rules — silently dropping empty entries
// turns "" into "no filter", which broadens results.
func TestListCollections_LegacyZonesRejectsEmpty(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := testCtx(t)

	_, err := svc.ListCollections(ctx, &pb.ListCollectionsRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_RACK,
		Zones: []string{""}, //nolint:staticcheck // SA1019 — exercising the deprecated path
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zones[0]")
}

// TestListCollections_LegacyZonesOversized guards the request-shape cap
// so the cross-org bulk lookup can't be DoS'd via a huge zones array.
func TestListCollections_LegacyZonesOversized(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := testCtx(t)

	tooMany := make([]string, maxDeviceSetFilterValues+1)
	for i := range tooMany {
		tooMany[i] = "z"
	}
	_, err := svc.ListCollections(ctx, &pb.ListCollectionsRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_RACK,
		Zones: tooMany, //nolint:staticcheck // SA1019 — exercising the deprecated path
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zones")
}
