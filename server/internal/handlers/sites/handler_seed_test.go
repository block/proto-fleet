package sites

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
)

// A no-force create-and-seed needs only site:manage. Here: create + direct
// device seed (skip-level — no buildings or racks).
func TestHandler_CreateSite_happyDeviceSeed(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	const newSiteID = int64(1)
	identifiers := []string{"d1", "d2"}

	h.siteStore.EXPECT().ListSiteSlugs(gomock.Any(), int64(7)).Return(nil, nil)
	h.siteStore.EXPECT().CreateSite(gomock.Any(), gomock.Any()).
		Return(&models.Site{ID: newSiteID, Name: "alpha", Slug: "alpha"}, nil)
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), newSiteID).Return(nil)
	h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), identifiers).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), identifiers).Return(identifiers, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), identifiers).Return(map[string]int64{}, nil)
	h.siteStore.EXPECT().FindDevicesInSiteLessRacks(gomock.Any(), int64(7), identifiers).Return(nil, nil)
	h.siteStore.EXPECT().AssignDevicesToSite(gomock.Any(), int64(7), gomock.Any(), identifiers).Return(int64(2), nil)
	h.buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(gomock.Any(), int64(7), identifiers, gomock.Any()).Return(int64(0), nil)

	resp, err := h.handler.CreateSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateSiteRequest{
		Name:              "alpha",
		DeviceIdentifiers: identifiers,
	}))
	require.NoError(t, err)
	assert.Equal(t, newSiteID, resp.Msg.GetSite().GetId())
	assert.Equal(t, int64(2), resp.Msg.GetReassignedDeviceCount())
	assert.Empty(t, resp.Msg.GetConflicts())
}

// force_clear_conflicting_rack_membership deletes rack rows — a site:manage-only
// caller must be rejected before any work happens, mirroring AssignDevicesToSite.
func TestHandler_CreateSite_forceRequiresRackManage(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	// ctx carries site:manage but NOT rack:manage — no store calls should run.

	_, err := h.handler.CreateSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateSiteRequest{
		Name:                                "alpha",
		DeviceIdentifiers:                   []string{"d1"},
		ForceClearConflictingRackMembership: ptrBool(true),
	}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

// A cross-site device conflict (force off) surfaces as per-device conflicts
// with the site left unset — the handler must not report a created site.
func TestHandler_CreateSite_conflictPassthrough(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	const newSiteID = int64(1)
	identifiers := []string{"d1"}

	h.siteStore.EXPECT().ListSiteSlugs(gomock.Any(), int64(7)).Return(nil, nil)
	h.siteStore.EXPECT().CreateSite(gomock.Any(), gomock.Any()).
		Return(&models.Site{ID: newSiteID, Name: "alpha", Slug: "alpha"}, nil)
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), newSiteID).Return(nil)
	h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), identifiers).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), identifiers).Return(identifiers, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), identifiers).Return(map[string]int64{"d1": 42}, nil)
	h.siteStore.EXPECT().FindDevicesInSiteLessRacks(gomock.Any(), int64(7), identifiers).Return(nil, nil)

	resp, err := h.handler.CreateSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateSiteRequest{
		Name:              "alpha",
		DeviceIdentifiers: identifiers,
	}))
	require.NoError(t, err)
	assert.Nil(t, resp.Msg.GetSite())
	require.Len(t, resp.Msg.GetConflicts(), 1)
	assert.Equal(t, "d1", resp.Msg.GetConflicts()[0].GetDeviceIdentifier())
}
