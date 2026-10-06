package buildings

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/buildings/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

// A no-force create-and-seed needs only site:manage — matching a plain
// CreateBuilding and the standalone Assign* surfaces. Here: create + direct
// device seed.
func TestHandler_CreateBuilding_happyDeviceSeed(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	const newBuildingID = int64(1)
	identifiers := []string{"d1", "d2"}

	h.buildingStore.EXPECT().CreateBuilding(gomock.Any(), gomock.Any()).
		Return(&models.Building{ID: newBuildingID, Name: "Aisle-1"}, nil)
	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), newBuildingID).Return(nil)
	h.buildingStore.EXPECT().GetBuildingSiteID(gomock.Any(), int64(7), newBuildingID).Return(nil, nil)
	h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), identifiers).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), identifiers).Return(identifiers, nil)
	h.buildingStore.EXPECT().FindDeviceBuildingConflicts(gomock.Any(), int64(7), identifiers).Return(map[string]int64{}, nil)
	h.buildingStore.EXPECT().FindDevicesInBuildingLessPlacedRacks(gomock.Any(), int64(7), identifiers).Return(nil, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), identifiers).Return(map[string]int64{}, nil)
	h.siteStore.EXPECT().FindDevicesInSiteLessRacks(gomock.Any(), int64(7), identifiers).Return(nil, nil)
	h.buildingStore.EXPECT().AssignDevicesToBuilding(gomock.Any(), int64(7), gomock.Any(), identifiers).Return(int64(2), nil)
	h.buildingStore.EXPECT().CascadeDevicesSiteForBuilding(gomock.Any(), int64(7), identifiers, gomock.Nil()).Return(int64(0), nil)

	resp, err := h.handler.CreateBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateBuildingRequest{
		Name:                  "Aisle-1",
		DefaultRackOrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
		DeviceIdentifiers:     identifiers,
	}))
	require.NoError(t, err)
	assert.Equal(t, newBuildingID, resp.Msg.GetBuilding().GetId())
	assert.Equal(t, int64(2), resp.Msg.GetReassignedDeviceCount())
	assert.Empty(t, resp.Msg.GetConflicts())
}

// force_clear_conflicting_rack_membership deletes rack rows — a site:manage-only
// caller must be rejected before any work happens, mirroring
// AssignDevicesToBuilding's gate.
func TestHandler_CreateBuilding_forceRequiresRackManage(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	// ctx carries site:manage but NOT rack:manage — no store calls should run.

	_, err := h.handler.CreateBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateBuildingRequest{
		Name:                                "Aisle-1",
		DefaultRackOrderIndex:               pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
		DeviceIdentifiers:                   []string{"d1"},
		ForceClearConflictingRackMembership: proto(true),
	}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

// A cross-building device conflict (force off) surfaces as per-device conflicts
// with the building left unset — the handler must not report a created building.
func TestHandler_CreateBuilding_conflictPassthrough(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	const newBuildingID = int64(1)
	identifiers := []string{"d1"}

	h.buildingStore.EXPECT().CreateBuilding(gomock.Any(), gomock.Any()).
		Return(&models.Building{ID: newBuildingID, Name: "Aisle-1"}, nil)
	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), newBuildingID).Return(nil)
	h.buildingStore.EXPECT().GetBuildingSiteID(gomock.Any(), int64(7), newBuildingID).Return(nil, nil)
	h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), identifiers).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), identifiers).Return(identifiers, nil)
	h.buildingStore.EXPECT().FindDeviceBuildingConflicts(gomock.Any(), int64(7), identifiers).Return(map[string]int64{"d1": 88}, nil)
	h.buildingStore.EXPECT().FindDevicesInBuildingLessPlacedRacks(gomock.Any(), int64(7), identifiers).Return(nil, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), identifiers).Return(map[string]int64{}, nil)
	h.siteStore.EXPECT().FindDevicesInSiteLessRacks(gomock.Any(), int64(7), identifiers).Return(nil, nil)

	resp, err := h.handler.CreateBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateBuildingRequest{
		Name:                  "Aisle-1",
		DefaultRackOrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
		DeviceIdentifiers:     identifiers,
	}))
	require.NoError(t, err)
	assert.Nil(t, resp.Msg.GetBuilding())
	require.Len(t, resp.Msg.GetConflicts(), 1)
	assert.Equal(t, "d1", resp.Msg.GetConflicts()[0].GetDeviceIdentifier())
}

// force + rack:manage is accepted — spot-check the gate lets the authorized
// caller through to the service (no conflict, no seed).
func TestHandler_CreateBuilding_forceWithRackManageAllowed(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	h.buildingStore.EXPECT().CreateBuilding(gomock.Any(), gomock.Any()).
		Return(&models.Building{ID: 3, Name: "Aisle-3"}, nil)

	ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermSiteRead, authz.PermSiteManage, authz.PermRackManage)
	resp, err := h.handler.CreateBuilding(ctx, connect.NewRequest(&pb.CreateBuildingRequest{
		Name:                                "Aisle-3",
		DefaultRackOrderIndex:               pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
		ForceClearConflictingRackMembership: proto(true),
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), resp.Msg.GetBuilding().GetId())
}

func proto(b bool) *bool { return &b }
