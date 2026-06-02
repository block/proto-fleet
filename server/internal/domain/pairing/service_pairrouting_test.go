package pairing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

type assignCall struct {
	fleetNodeID int64
	deviceID    int64
	orgID       int64
	assignedBy  *int64
}

type fakeAssigner struct {
	calls         []assignCall
	failDeviceIDs map[int64]bool
}

func (f *fakeAssigner) PairDevice(_ context.Context, fleetNodeID, deviceID, orgID int64, assignedBy *int64) error {
	f.calls = append(f.calls, assignCall{fleetNodeID, deviceID, orgID, assignedBy})
	if f.failDeviceIDs[deviceID] {
		return fleeterror.NewFailedPreconditionError("device already paired; unpair first")
	}
	return nil
}

func includeReq(ids ...string) *pb.PairRequest {
	return &pb.PairRequest{DeviceSelector: &commandpb.DeviceSelector{
		SelectionType: &commandpb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonv1.DeviceIdentifierList{DeviceIdentifiers: ids},
		},
	}}
}

func fleetNodeDiscoveredDevice(identifier string, orgID, nodeID int64) *discoverymodels.DiscoveredDevice {
	return &discoverymodels.DiscoveredDevice{
		Device:                  pb.Device{DeviceIdentifier: identifier, IpAddress: "10.0.0.5", Port: "80"},
		OrgID:                   orgID,
		DiscoveredByFleetNodeID: &nodeID,
	}
}

func TestPairDevices_FleetNodeDeviceRoutesToAssigner(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	const (
		orgID  = int64(7)
		userID = int64(3)
		nodeID = int64(55)
		dbID   = int64(900)
	)
	doi := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: "dev-1", OrgID: orgID}
	mockDD := mocks.NewMockDiscoveredDeviceStore(ctrl)
	mockDD.EXPECT().GetDevice(gomock.Any(), doi).Return(fleetNodeDiscoveredDevice("dev-1", orgID, nodeID), nil)
	mockDD.EXPECT().GetDatabaseID(gomock.Any(), doi).Return(dbID, nil)
	assigner := &fakeAssigner{}
	svc := &Service{discoveredDeviceStore: mockDD, fleetNodeAssigner: assigner}
	ctx := mockSessionContext(t.Context(), userID, orgID)

	// Act
	resp, err := svc.PairDevices(ctx, includeReq("dev-1"))

	// Assert: routed to the assigner with the resolved DB id + caller, not dialed.
	require.NoError(t, err)
	assert.Empty(t, resp.GetFailedDeviceIds())
	require.Len(t, assigner.calls, 1)
	assert.Equal(t, nodeID, assigner.calls[0].fleetNodeID)
	assert.Equal(t, dbID, assigner.calls[0].deviceID)
	assert.Equal(t, orgID, assigner.calls[0].orgID)
	require.NotNil(t, assigner.calls[0].assignedBy)
	assert.Equal(t, userID, *assigner.calls[0].assignedBy)
}

func TestPairDevices_FleetNodeDeviceRefusedWithoutAssigner(t *testing.T) {
	// Arrange: no assigner wired keeps the pre-fan-out refusal behavior.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	const orgID = int64(7)
	doi := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: "dev-1", OrgID: orgID}
	mockDD := mocks.NewMockDiscoveredDeviceStore(ctrl)
	mockDD.EXPECT().GetDevice(gomock.Any(), doi).Return(fleetNodeDiscoveredDevice("dev-1", orgID, 55), nil)
	svc := &Service{discoveredDeviceStore: mockDD} // fleetNodeAssigner nil
	ctx := mockSessionContext(t.Context(), 3, orgID)

	// Act
	_, err := svc.PairDevices(ctx, includeReq("dev-1"))

	// Assert: the only device was refused, so nothing paired.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to pair any devices")
}

func TestPairDevices_FleetNodeAssignPartialSuccess(t *testing.T) {
	// Arrange: two fleet-node devices; the assigner fails the second only.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	const orgID = int64(7)
	doi1 := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: "dev-1", OrgID: orgID}
	doi2 := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: "dev-2", OrgID: orgID}
	mockDD := mocks.NewMockDiscoveredDeviceStore(ctrl)
	mockDD.EXPECT().GetDevice(gomock.Any(), doi1).Return(fleetNodeDiscoveredDevice("dev-1", orgID, 55), nil)
	mockDD.EXPECT().GetDatabaseID(gomock.Any(), doi1).Return(int64(900), nil)
	mockDD.EXPECT().GetDevice(gomock.Any(), doi2).Return(fleetNodeDiscoveredDevice("dev-2", orgID, 55), nil)
	mockDD.EXPECT().GetDatabaseID(gomock.Any(), doi2).Return(int64(901), nil)
	assigner := &fakeAssigner{failDeviceIDs: map[int64]bool{901: true}}
	svc := &Service{discoveredDeviceStore: mockDD, fleetNodeAssigner: assigner}
	ctx := mockSessionContext(t.Context(), 3, orgID)

	// Act
	resp, err := svc.PairDevices(ctx, includeReq("dev-1", "dev-2"))

	// Assert: dev-1 paired, dev-2 failed; partial success returns no top-level error.
	require.NoError(t, err)
	assert.Equal(t, []string{"dev-2"}, resp.GetFailedDeviceIds())
}
