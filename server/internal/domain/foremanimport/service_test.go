package foremanimport

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	models "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/foreman"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// mockForemanClient implements ForemanClient for testing.
type mockForemanClient struct {
	miners []foreman.Miner
	groups []foreman.SiteMapGroup
	racks  []foreman.SiteMapRack
}

func (m *mockForemanClient) ListMiners(_ context.Context) ([]foreman.Miner, error) {
	return m.miners, nil
}
func (m *mockForemanClient) ListSiteMapGroups(_ context.Context) ([]foreman.SiteMapGroup, error) {
	return m.groups, nil
}
func (m *mockForemanClient) ListSiteMapRacks(_ context.Context) ([]foreman.SiteMapRack, error) {
	return m.racks, nil
}

// fakePoolCreator is a simple test double for PoolCreator.
type fakePoolCreator struct {
	created int
}

func (f *fakePoolCreator) Create(_ context.Context, _ string, _ poolconfigurations.RevisionOptions) (poolconfigurations.Configuration, error) {
	f.created++
	return poolconfigurations.Configuration{}, nil
}

// fakeCollectionManager is a test double for CollectionManager.
type fakeCollectionManager struct {
	mu                 sync.Mutex
	nextID             int64
	collectionsCreated int
	devicesAdded       int32
	createdRequests    []*collectionpb.CreateCollectionRequest
}

func (f *fakeCollectionManager) CreateCollection(_ context.Context, req *collectionpb.CreateCollectionRequest) (*collectionpb.CreateCollectionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.collectionsCreated++
	f.createdRequests = append(f.createdRequests, req)
	return &collectionpb.CreateCollectionResponse{
		Collection: &collectionpb.DeviceCollection{Id: f.nextID},
		AddedCount: 0,
	}, nil
}

func (f *fakeCollectionManager) AddDevicesToGroup(_ context.Context, params collection.AddDevicesToGroupParams) (*collection.AddDevicesToGroupResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int32
	if sel := params.DeviceSelector; sel != nil {
		if dl := sel.GetDeviceList(); dl != nil {
			count = int32(len(dl.DeviceIdentifiers)) //nolint:gosec
		}
	}
	f.devicesAdded += count
	return &collection.AddDevicesToGroupResult{AddedCount: int64(count)}, nil
}

func (f *fakeCollectionManager) AssignDevicesToRack(_ context.Context, params collection.AssignDevicesToRackParams) (*collection.AssignDevicesToRackResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := int32(len(params.DeviceIdentifiers)) //nolint:gosec
	f.devicesAdded += count
	return &collection.AssignDevicesToRackResult{AssignedCount: int64(count)}, nil
}

func (f *fakeCollectionManager) ListCollections(_ context.Context, _ *collectionpb.ListCollectionsRequest) (*collectionpb.ListCollectionsResponse, error) {
	return &collectionpb.ListCollectionsResponse{}, nil
}

func TestImportFromForeman_ReturnsMinersForDiscovery(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	poolCreator := &fakePoolCreator{}
	collectionMgr := &fakeCollectionManager{}
	deviceStore := mocks.NewMockDeviceStore(ctrl)

	mockClient := &mockForemanClient{
		miners: []foreman.Miner{
			{ID: 1, Name: "miner1", IP: "10.0.0.1", MAC: "AA:BB:CC:DD:EE:01", Type: foreman.MinerType{Name: "Antminer S21"}},
			{ID: 2, Name: "miner2", IP: "10.0.0.2", MAC: "AA:BB:CC:DD:EE:02", Type: foreman.MinerType{Name: "Whatsminer M60S"}},
		},
	}

	svc := NewService(poolCreator, collectionMgr, deviceStore)
	svc.newClient = func(_, _ string) ForemanClient { return mockClient }

	// Act
	resp, err := svc.ImportFromForeman(context.Background(), &pb.ImportFromForemanRequest{
		Credentials: &pb.ForemanCredentials{ApiKey: "test-key", ClientId: "123"},
	})

	// Assert
	require.NoError(t, err)
	assert.Len(t, resp.Miners, 2)
	assert.Equal(t, "10.0.0.1", resp.Miners[0].IpAddress)
	assert.Equal(t, "10.0.0.2", resp.Miners[1].IpAddress)
	assert.Equal(t, "S21", resp.Miners[0].Model)
	assert.Equal(t, "M60S", resp.Miners[1].Model)
}

func TestCompleteImport_RejectsPoolImportBeforeFetching(t *testing.T) {
	ctrl := gomock.NewController(t)
	poolCreator := &fakePoolCreator{}
	collectionMgr := &fakeCollectionManager{}
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	svc := NewService(poolCreator, collectionMgr, deviceStore)
	fetched := false
	svc.newClient = func(_, _ string) ForemanClient {
		fetched = true
		return &mockForemanClient{}
	}
	ctx := testutil.MockAuthContextForTesting(t.Context(), 1, 1)
	resp, err := svc.CompleteImport(ctx, &pb.CompleteImportRequest{
		Credentials: &pb.ForemanCredentials{ApiKey: "key", ClientId: "1"},
		ImportPools: true, ImportGroups: true, ImportRacks: true,
	})
	require.ErrorContains(t, err, "password")
	require.Nil(t, resp)
	assert.False(t, fetched, "unsupported pool import must be rejected before fetching or changing metadata")
	assert.Zero(t, poolCreator.created)
	assert.Zero(t, collectionMgr.collectionsCreated)
}

func TestCompleteImport_CreatesGroupsAndRacksWithDeviceAssignment(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	poolCreator := &fakePoolCreator{}
	collectionMgr := &fakeCollectionManager{}
	deviceStore := mocks.NewMockDeviceStore(ctrl)

	groupID := 100
	mockClient := &mockForemanClient{
		miners: []foreman.Miner{
			{
				ID: 1, IP: "10.0.0.1", MAC: "AA:BB:CC:DD:EE:01",
				Type:     foreman.MinerType{Name: "Antminer S21"},
				Location: &foreman.MinerLocation{RackID: 200, Row: 0, Index: 0},
			},
		},
		groups: []foreman.SiteMapGroup{
			{ID: 100, Name: "Building A"},
		},
		racks: []foreman.SiteMapRack{
			{ID: 200, Name: "Rack 1", GroupID: &groupID},
		},
	}

	svc := NewService(poolCreator, collectionMgr, deviceStore)
	svc.newClient = func(_, _ string) ForemanClient { return mockClient }

	deviceStore.EXPECT().
		GetPairedDevicesByMACAddresses(gomock.Any(), gomock.Any(), int64(1)).
		Return(map[string]*interfaces.PairedDeviceInfo{
			"AA:BB:CC:DD:EE:01": {DeviceIdentifier: "fleet-device-1", MacAddress: "AA:BB:CC:DD:EE:01"},
		}, nil)

	// Worker name set to normalized MAC (no pools on this miner)
	deviceStore.EXPECT().
		InitializeWorkerName(gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(true, nil)

	ctx := testutil.MockAuthContextForTesting(context.Background(), 1, 1)

	// Act
	resp, err := svc.CompleteImport(ctx, &pb.CompleteImportRequest{
		Credentials:  &pb.ForemanCredentials{ApiKey: "key", ClientId: "1"},
		ImportPools:  false,
		ImportGroups: true,
		ImportRacks:  true,
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.GroupsCreated)
	assert.Equal(t, int32(1), resp.RacksCreated)
	assert.Equal(t, 2, collectionMgr.collectionsCreated) // 1 group + 1 rack
}

func TestCompleteImport_WithoutPoolsPreservesCollectionsAndNames(t *testing.T) {
	ctrl := gomock.NewController(t)
	poolCreator := &fakePoolCreator{}
	collectionMgr := &fakeCollectionManager{}
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	groupID := 100
	mockClient := &mockForemanClient{
		miners: []foreman.Miner{{
			ID: 1, Name: "North miner", IP: "10.0.0.1", MAC: "AA:BB:CC:DD:EE:01",
			Type:     foreman.MinerType{Name: "Antminer S21"},
			Location: &foreman.MinerLocation{RackID: 200, Row: 0, Index: 0},
			Pools:    []foreman.Pool{{URL: "pool.example.com:3333", Worker: "account.worker1"}},
		}},
		groups: []foreman.SiteMapGroup{{ID: groupID, Name: "Building A"}},
		racks:  []foreman.SiteMapRack{{ID: 200, Name: "Rack 1", GroupID: &groupID}},
	}
	svc := NewService(poolCreator, collectionMgr, deviceStore)
	svc.newClient = func(_, _ string) ForemanClient { return mockClient }

	// Resolve the allowlist and then the importer assignments for the paired miner.
	deviceStore.EXPECT().
		GetPairedDevicesByMACAddresses(gomock.Any(), []string{"AA:BB:CC:DD:EE:01"}, int64(1)).
		Return(map[string]*interfaces.PairedDeviceInfo{
			"AA:BB:CC:DD:EE:01": {DeviceIdentifier: "fleet-device-1", MacAddress: "AA:BB:CC:DD:EE:01"},
		}, nil).Times(2)
	deviceStore.EXPECT().
		InitializeWorkerName(gomock.Any(), int64(1), models.DeviceIdentifier("fleet-device-1"), "AA:BB:CC:DD:EE:01").Return(false, nil)
	deviceStore.EXPECT().
		UpdateDeviceCustomNames(gomock.Any(), int64(1), map[string]string{"fleet-device-1": "North miner"}).
		Return(nil)

	ctx := testutil.MockAuthContextForTesting(context.Background(), 1, 1)
	resp, err := svc.CompleteImport(ctx, &pb.CompleteImportRequest{
		Credentials:             &pb.ForemanCredentials{ApiKey: "key", ClientId: "1"},
		ImportPools:             false,
		ImportGroups:            true,
		ImportRacks:             true,
		PairedDeviceIdentifiers: []string{"fleet-device-1"},
	})

	require.NoError(t, err)
	assert.Zero(t, resp.PoolsCreated)
	assert.Zero(t, poolCreator.created)
	assert.Equal(t, int32(1), resp.GroupsCreated)
	assert.Equal(t, int32(1), resp.RacksCreated)
	assert.Zero(t, resp.WorkerNamesSet, "the worker name already initialized during pairing is preserved")
	assert.Equal(t, int32(1), resp.MinerNamesSet)
	require.Len(t, collectionMgr.createdRequests, 2)
	assert.Equal(t, "Building A", collectionMgr.createdRequests[0].Label)
	assert.Equal(t, "Rack 1", collectionMgr.createdRequests[1].Label)
	for _, req := range collectionMgr.createdRequests {
		assert.Equal(t, []string{"fleet-device-1"}, req.DeviceSelector.GetDeviceList().DeviceIdentifiers)
	}
}

func TestNormalizeForemanData_SetsRackLocationToGroupName(t *testing.T) {
	// Arrange
	groupID := 10
	groups := []foreman.SiteMapGroup{{ID: 10, Name: "Data Center 1"}}
	racks := []foreman.SiteMapRack{{ID: 20, Name: "R1", GroupID: &groupID}}

	// Act
	data := normalizeForemanData(nil, groups, racks)

	// Assert
	require.Len(t, data.Racks, 1)
	assert.Equal(t, "Data Center 1", data.Racks[0].Location)
	assert.Equal(t, "10", data.Racks[0].GroupID)
}

func TestCompleteImport_DistinctPoolUsernamesImportOnlyMetadata(t *testing.T) {
	const count = 1000
	ctrl := gomock.NewController(t)
	poolCreator := &fakePoolCreator{}
	collections := &fakeCollectionManager{}
	devices := mocks.NewMockDeviceStore(ctrl)
	groupID := 100
	client := &mockForemanClient{
		groups: []foreman.SiteMapGroup{{ID: groupID, Name: "Building A"}},
		racks:  []foreman.SiteMapRack{{ID: 200, Name: "Rack 1", GroupID: &groupID}},
	}
	macs := make([]string, 0, count)
	paired := map[string]*interfaces.PairedDeviceInfo{}
	names := map[string]string{}
	macByID := map[models.DeviceIdentifier]string{}
	for i := range count {
		mac := fmt.Sprintf("AA:BB:CC:DD:%02X:%02X", i>>8, i&255)
		id := fmt.Sprintf("fleet-device-%d", i)
		name := fmt.Sprintf("Miner %d", i)
		client.miners = append(client.miners, foreman.Miner{
			ID: i + 1, MAC: mac, Name: name,
			Location: &foreman.MinerLocation{RackID: 200},
			Pools:    []foreman.Pool{{URL: "pool.example.com:3333", Worker: fmt.Sprintf("wallet.main.worker%d", i)}},
		})
		macs = append(macs, mac)
		paired[mac] = &interfaces.PairedDeviceInfo{DeviceIdentifier: id, MacAddress: mac}
		names[id] = name
		macByID[models.DeviceIdentifier(id)] = mac
	}
	devices.EXPECT().GetPairedDevicesByMACAddresses(gomock.Any(), macs, int64(1)).Return(paired, nil)
	devices.EXPECT().InitializeWorkerName(gomock.Any(), int64(1), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, id models.DeviceIdentifier, name string) (bool, error) {
			require.Equal(t, macByID[id], name, "do not derive Fleet worker names from pool usernames")
			return false, nil // Preserve the worker name already initialized during pairing.
		}).Times(count)
	devices.EXPECT().UpdateDeviceCustomNames(gomock.Any(), int64(1), names).Return(nil)
	svc := NewService(poolCreator, collections, devices)
	svc.newClient = func(_, _ string) ForemanClient { return client }
	ctx := testutil.MockAuthContextForTesting(t.Context(), 1, 1)
	response, err := svc.CompleteImport(ctx, &pb.CompleteImportRequest{
		Credentials:  &pb.ForemanCredentials{ApiKey: "key", ClientId: "1"},
		ImportGroups: true, ImportRacks: true,
	})
	require.NoError(t, err)
	assert.Zero(t, response.PoolsCreated)
	assert.Zero(t, poolCreator.created)
	assert.Equal(t, int32(1), response.GroupsCreated)
	assert.Equal(t, int32(1), response.RacksCreated)
	assert.Equal(t, int32(count), response.MinerNamesSet)
	assert.Zero(t, response.WorkerNamesSet)
	require.Len(t, collections.createdRequests, 2)
	for _, request := range collections.createdRequests {
		assert.Len(t, request.DeviceSelector.GetDeviceList().DeviceIdentifiers, count)
	}
}
