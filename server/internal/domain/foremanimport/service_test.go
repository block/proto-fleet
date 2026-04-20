package foremanimport

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
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

func (f *fakePoolCreator) CreatePool(_ context.Context, _ *poolspb.PoolConfig) (*poolspb.Pool, error) {
	f.created++
	return &poolspb.Pool{}, nil
}

// fakeCollectionManager is a test double for CollectionManager.
type fakeCollectionManager struct {
	mu                 sync.Mutex
	nextID             int64
	collectionsCreated int
	devicesAdded       int32
}

func (f *fakeCollectionManager) CreateCollection(_ context.Context, _ *collectionpb.CreateCollectionRequest) (*collectionpb.CreateCollectionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.collectionsCreated++
	return &collectionpb.CreateCollectionResponse{
		Collection: &collectionpb.DeviceCollection{Id: f.nextID},
		AddedCount: 0,
	}, nil
}

func (f *fakeCollectionManager) AddDevicesToCollection(_ context.Context, req *collectionpb.AddDevicesToCollectionRequest) (*collectionpb.AddDevicesToCollectionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int32
	if sel := req.GetDeviceSelector(); sel != nil {
		if dl := sel.GetDeviceList(); dl != nil {
			count = int32(len(dl.DeviceIdentifiers)) //nolint:gosec
		}
	}
	f.devicesAdded += count
	return &collectionpb.AddDevicesToCollectionResponse{
		CollectionId: req.CollectionId,
		AddedCount:   count,
	}, nil
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

func TestCompleteImport_CreatesPoolsFromMiners(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	poolCreator := &fakePoolCreator{}
	collectionMgr := &fakeCollectionManager{}
	deviceStore := mocks.NewMockDeviceStore(ctrl)

	mockClient := &mockForemanClient{
		miners: []foreman.Miner{
			{
				ID: 1, IP: "10.0.0.1", MAC: "AA:BB:CC:DD:EE:01",
				Type: foreman.MinerType{Name: "Antminer S21"},
				Pools: []foreman.Pool{
					{URL: "mine.ocean.xyz:3334", Worker: "wallet.worker1"},
				},
			},
			{
				ID: 2, IP: "10.0.0.2", MAC: "AA:BB:CC:DD:EE:02",
				Type: foreman.MinerType{Name: "Antminer S21"},
				Pools: []foreman.Pool{
					{URL: "mine.ocean.xyz:3334", Worker: "wallet.worker1"}, // duplicate
					{URL: "devfee", Worker: "devfee"},                      // should be skipped
				},
			},
		},
	}

	svc := NewService(poolCreator, collectionMgr, deviceStore)
	svc.newClient = func(_, _ string) ForemanClient { return mockClient }

	deviceStore.EXPECT().
		GetPairedDevicesByMACAddresses(gomock.Any(), gomock.Any(), int64(1)).
		Return(nil, nil)

	ctx := testutil.MockAuthContextForTesting(context.Background(), 1, 1)

	// Act
	resp, err := svc.CompleteImport(ctx, &pb.CompleteImportRequest{
		Credentials:  &pb.ForemanCredentials{ApiKey: "key", ClientId: "1"},
		ImportPools:  true,
		ImportGroups: true,
		ImportRacks:  true,
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.PoolsCreated)
	assert.Equal(t, 1, poolCreator.created)
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
		UpdateWorkerName(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	ctx := testutil.MockAuthContextForTesting(context.Background(), 1, 1)

	// Act
	resp, err := svc.CompleteImport(ctx, &pb.CompleteImportRequest{
		Credentials:  &pb.ForemanCredentials{ApiKey: "key", ClientId: "1"},
		ImportPools:  true,
		ImportGroups: true,
		ImportRacks:  true,
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.GroupsCreated)
	assert.Equal(t, int32(1), resp.RacksCreated)
	assert.Equal(t, 2, collectionMgr.collectionsCreated) // 1 group + 1 rack
}

func TestNormalizeForemanData_DeduplicatesPoolsAndSkipsDevfee(t *testing.T) {
	// Arrange
	miners := []foreman.Miner{
		{
			ID: 1, Pools: []foreman.Pool{
				{URL: "pool.example.com:3333", Worker: "worker1"},
				{URL: "devfee", Worker: "fee"},
			},
		},
		{
			ID: 2, Pools: []foreman.Pool{
				{URL: "pool.example.com:3333", Worker: "worker1"}, // duplicate
				{URL: "", Worker: ""},                             // empty, skip
			},
		},
	}

	// Act
	data := normalizeForemanData(miners, nil, nil)

	// Assert
	assert.Len(t, data.Pools, 1)
	assert.Equal(t, "pool.example.com:3333", data.Pools[0].URL)
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
