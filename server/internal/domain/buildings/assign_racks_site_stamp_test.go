package buildings

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// A building-only unassign records the source site only when the whole batch
// shares one: single-rack or same-site multi-rack stamp it; a batch straddling
// sites — or including a site-less rack — stays site-less so the activity row
// never misattributes one rack's site to the rest.
func TestAssignRacksToBuilding_unassignStampsSingleSourceSite(t *testing.T) {
	siteA := int64(3)
	siteB := int64(8)

	cases := []struct {
		name      string
		rackSites map[int64]*int64 // rackID -> source site (nil = site-less rack)
		wantSite  *int64
	}{
		{
			name:      "same-site multi-rack unassign stamps the shared site",
			rackSites: map[int64]*int64{99: &siteA, 100: &siteA},
			wantSite:  &siteA,
		},
		{
			name:      "cross-site multi-rack unassign stays site-less",
			rackSites: map[int64]*int64{99: &siteA, 100: &siteB},
			wantSite:  nil,
		},
		{
			name:      "site-less rack in the batch stays site-less",
			rackSites: map[int64]*int64{99: &siteA, 100: nil},
			wantSite:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := mocks.NewMockBuildingStore(ctrl)
			siteStore := mocks.NewMockSiteStore(ctrl)
			collectionStore := mocks.NewMockCollectionStore(ctrl)
			activityStore := mocks.NewMockActivityStore(ctrl)
			tx := &fakeTransactor{}
			svc := NewService(store, siteStore, collectionStore, nil, nil, tx, activity.NewService(activityStore))

			rackIDs := make([]int64, 0, len(tc.rackSites))
			for id := range tc.rackSites {
				rackIDs = append(rackIDs, id)
			}
			sort.Slice(rackIDs, func(i, j int) bool { return rackIDs[i] < rackIDs[j] })

			// Each rack was assigned to building 11 (so building_id 11 → nil
			// drives the building cascade); site is preserved on unassign.
			for id, site := range tc.rackSites {
				collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, id, testOrgID).
					Return(interfaces.RackPlacement{SiteID: site, BuildingID: ptrInt64(11)}, nil)
			}
			collectionStore.EXPECT().
				UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, rackIDs, (*int64)(nil), (*int64)(nil)).
				Return(int64(len(rackIDs)), nil)
			collectionStore.EXPECT().
				CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, rackIDs, gomock.Nil()).
				Return(int64(0), nil)

			var got *activitymodels.Event
			activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, e *activitymodels.Event) error {
					got = e
					return nil
				})

			params := models.AssignRacksToBuildingParams{OrgID: testOrgID, TargetBuildingID: nil}
			for _, id := range rackIDs {
				params.Racks = append(params.Racks, models.RackPlacementParam{RackID: id})
			}
			_, err := svc.AssignRacksToBuilding(context.Background(), params)
			require.NoError(t, err)

			require.NotNil(t, got, "expected a rack-assigned-building activity row")
			if tc.wantSite == nil {
				assert.Nil(t, got.SiteID)
			} else {
				require.NotNil(t, got.SiteID)
				assert.Equal(t, *tc.wantSite, *got.SiteID)
			}
		})
	}
}

// TestAssignDevicesToBuilding_unassignStampsDeviceSetSiteScope pins #538
// for the device building-unassign path: with no target building site to
// stamp, the activity row's scope comes from the touched devices' own
// sites — a single shared site is stamped; a set spanning sites is marked
// multi_site so it stays out of the /unassigned bucket.
func TestAssignDevicesToBuilding_unassignStampsDeviceSetSiteScope(t *testing.T) {
	siteA := int64(3)
	siteB := int64(8)
	identifiers := []string{"d1", "d2"}

	cases := []struct {
		name              string
		deviceSites       []*int64
		wantSite          *int64
		wantMultiSite     bool
		wantMembers       []int64
		wantTouchesUnassd bool
	}{
		{
			name:        "single shared site stamps that site (scalar, no membership)",
			deviceSites: []*int64{&siteA, &siteA},
			wantSite:    &siteA,
		},
		{
			name:          "cross-site set records membership for both sites",
			deviceSites:   []*int64{&siteA, &siteB},
			wantSite:      nil,
			wantMultiSite: true,
			wantMembers:   []int64{siteA, siteB},
		},
		{
			name:              "mix of sited + site-less records membership + touches unassigned",
			deviceSites:       []*int64{&siteA, nil},
			wantSite:          nil,
			wantMultiSite:     true,
			wantMembers:       []int64{siteA},
			wantTouchesUnassd: true,
		},
		{
			name:        "all site-less stays unassigned (no site, not multi_site)",
			deviceSites: []*int64{nil, nil},
			wantSite:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := mocks.NewMockBuildingStore(ctrl)
			siteStore := mocks.NewMockSiteStore(ctrl)
			activityStore := mocks.NewMockActivityStore(ctrl)
			tx := &fakeTransactor{}
			svc := NewService(store, siteStore, nil, nil, nil, tx, activity.NewService(activityStore))

			// Building-unassign (target nil): no building lock, no site
			// cascade. The conflict probes that gate on a non-nil target are
			// skipped; only existence + building-conflict checks run.
			siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
			siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
			store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
			store.EXPECT().AssignDevicesToBuilding(inTxCtx, testOrgID, gomock.Nil(), identifiers).Return(int64(len(identifiers)), nil)
			// The new device-set scope resolution (#538).
			siteStore.EXPECT().GetDistinctDeviceSiteIDs(inTxCtx, testOrgID, identifiers).Return(tc.deviceSites, nil)

			var got *activitymodels.Event
			activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, e *activitymodels.Event) error {
					got = e
					return nil
				})

			_, _, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
				OrgID:             testOrgID,
				TargetBuildingID:  nil,
				DeviceIdentifiers: identifiers,
			})
			require.NoError(t, err)

			require.NotNil(t, got, "expected a devices-reassigned-to-building activity row")
			assert.Equal(t, tc.wantMultiSite, got.MultiSite)
			assert.Equal(t, tc.wantTouchesUnassd, got.TouchesUnassigned)
			assert.ElementsMatch(t, tc.wantMembers, got.MemberSiteIDs)
			if tc.wantSite == nil {
				assert.Nil(t, got.SiteID)
			} else {
				require.NotNil(t, got.SiteID)
				assert.Equal(t, *tc.wantSite, *got.SiteID)
			}
		})
	}
}
