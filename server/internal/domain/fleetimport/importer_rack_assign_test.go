package fleetimport

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
)

// fakeCollectionManager is a minimal fake that records the last call to
// AssignDevicesToRack and returns a configurable result. The other
// CollectionManager methods are stubbed to satisfy the interface — the
// re-import path exercised here only touches ListCollections (used to
// build the existing-collection map) and AssignDevicesToRack.
type fakeCollectionManager struct {
	listResp   []*collectionpb.DeviceCollection
	assignResp *collection.AssignDevicesToRackResult

	assignCalls []collection.AssignDevicesToRackParams
}

func (f *fakeCollectionManager) ListCollections(_ context.Context, req *collectionpb.ListCollectionsRequest) (*collectionpb.ListCollectionsResponse, error) {
	var out []*collectionpb.DeviceCollection
	for _, c := range f.listResp {
		if c.Type == req.Type {
			out = append(out, c)
		}
	}
	return &collectionpb.ListCollectionsResponse{Collections: out}, nil
}

func (f *fakeCollectionManager) CreateCollection(_ context.Context, _ *collectionpb.CreateCollectionRequest) (*collectionpb.CreateCollectionResponse, error) {
	return &collectionpb.CreateCollectionResponse{}, nil
}

func (f *fakeCollectionManager) AddDevicesToGroup(_ context.Context, _ collection.AddDevicesToGroupParams) (*collection.AddDevicesToGroupResult, error) {
	return &collection.AddDevicesToGroupResult{}, nil
}

func (f *fakeCollectionManager) AssignDevicesToRack(_ context.Context, params collection.AssignDevicesToRackParams) (*collection.AssignDevicesToRackResult, error) {
	f.assignCalls = append(f.assignCalls, params)
	return f.assignResp, nil
}

// TestImporter_RackReimport_UsesNewlyAssignedCount confirms the importer
// reports devices_assigned based on AssignDevicesToRackResult's
// NewlyAssignedCount (rows the store actually inserted via ON CONFLICT
// DO NOTHING), not AssignedCount (final-membership count, includes
// devices already in the target). A re-import that re-targets devices
// already assigned to the same rack would otherwise overstate the
// count.
func TestImporter_RackReimport_UsesNewlyAssignedCount(t *testing.T) {
	ctrl := gomock.NewController(t)

	mac1 := "aa:bb:cc:dd:ee:01"
	mac2 := "aa:bb:cc:dd:ee:02"
	mac3 := "aa:bb:cc:dd:ee:03"
	norm := func(s string) string { return networking.NormalizeMAC(s) }

	deviceStore := mocks.NewMockDeviceStore(ctrl)
	deviceStore.EXPECT().
		GetPairedDevicesByMACAddresses(gomock.Any(), gomock.Any(), int64(1)).
		Return(map[string]*interfaces.PairedDeviceInfo{
			norm(mac1): {DeviceIdentifier: "device-1", MacAddress: norm(mac1)},
			norm(mac2): {DeviceIdentifier: "device-2", MacAddress: norm(mac2)},
			norm(mac3): {DeviceIdentifier: "device-3", MacAddress: norm(mac3)},
		}, nil)
	// setWorkerNames falls back to the normalized MAC when no
	// per-miner worker name is provided. AnyTimes — we don't care how
	// many it makes; this test only asserts the rack-assignment count.
	deviceStore.EXPECT().
		UpdateWorkerName(gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes().
		Return(nil)
	// setMinerNames is skipped when ImportMiner.Name is empty (all
	// miners here lack Name), so UpdateDeviceCustomNames is never
	// called — no mock expectation needed.

	// Existing rack labeled "rack-A" with ID 42 — flagged by
	// buildExistingCollectionMap so the rack path takes the
	// AssignDevicesToRack branch (not CreateCollection).
	cm := &fakeCollectionManager{
		listResp: []*collectionpb.DeviceCollection{
			{Id: 42, Label: "rack-A", Type: collectionpb.CollectionType_COLLECTION_TYPE_RACK},
		},
		// Three devices requested, but only one is newly inserted —
		// the other two were already members of rack-A (the re-import
		// scenario). AssignedCount counts all three; the importer must
		// use NewlyAssignedCount.
		assignResp: &collection.AssignDevicesToRackResult{
			AssignedCount:      3,
			NewlyAssignedCount: 1,
		},
	}

	imp := &Importer{
		collectionManager: cm,
		deviceStore:       deviceStore,
	}

	result := imp.Import(t.Context(), 1, &ImportData{
		Miners: []ImportMiner{
			{SourceID: "m1", MAC: mac1, RackID: "r1"},
			{SourceID: "m2", MAC: mac2, RackID: "r1"},
			{SourceID: "m3", MAC: mac3, RackID: "r1"},
		},
		Racks: []ImportRack{
			{SourceID: "r1", Name: "rack-A", Rows: 1, Columns: 3},
		},
	})

	require.Len(t, cm.assignCalls, 1, "expected one AssignDevicesToRack call")
	require.NotNil(t, cm.assignCalls[0].TargetRackID)
	assert.Equal(t, int64(42), *cm.assignCalls[0].TargetRackID)
	// devices_assigned must reflect NewlyAssignedCount (1), not
	// AssignedCount (3). The old behavior would have reported 3.
	assert.Equal(t, int32(1), result.DevicesAssigned)
}
