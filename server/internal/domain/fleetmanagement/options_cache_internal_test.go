package fleetmanagement

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/fleetoptions"
	storesMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// newServiceWithCache builds a Service wired with a mock device store and an
// options cache. It is the smallest construction needed to exercise
// getCachedFleetOptions in isolation.
func newServiceWithCache(t *testing.T, ttl time.Duration) (*Service, *storesMocks.MockDeviceStore, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	store := storesMocks.NewMockDeviceStore(ctrl)
	svc := &Service{deviceStore: store}
	svc.WithOptionsCache(fleetoptions.NewCache(ttl, 16))
	return svc, store, ctrl
}

func TestGetCachedFleetOptions_HitAvoidsStoreCall(t *testing.T) {
	svc, store, _ := newServiceWithCache(t, time.Minute)

	store.EXPECT().GetAvailableModels(gomock.Any(), int64(1)).Return([]string{"S19"}, nil).Times(1)
	store.EXPECT().GetAvailableFirmwareVersions(gomock.Any(), int64(1)).Return([]string{"v1"}, nil).Times(1)

	for range 3 {
		opts, err := svc.getCachedFleetOptions(t.Context(), 1)
		require.NoError(t, err)
		assert.Equal(t, []string{"S19"}, opts.Models)
		assert.Equal(t, []string{"v1"}, opts.FirmwareVersions)
	}
}

func TestGetCachedFleetOptions_PerOrgIsolation(t *testing.T) {
	svc, store, _ := newServiceWithCache(t, time.Minute)

	store.EXPECT().GetAvailableModels(gomock.Any(), int64(1)).Return([]string{"S19"}, nil).Times(1)
	store.EXPECT().GetAvailableFirmwareVersions(gomock.Any(), int64(1)).Return([]string{"v1"}, nil).Times(1)
	store.EXPECT().GetAvailableModels(gomock.Any(), int64(2)).Return([]string{"M30"}, nil).Times(1)
	store.EXPECT().GetAvailableFirmwareVersions(gomock.Any(), int64(2)).Return([]string{"v2"}, nil).Times(1)

	a, err := svc.getCachedFleetOptions(t.Context(), 1)
	require.NoError(t, err)
	b, err := svc.getCachedFleetOptions(t.Context(), 2)
	require.NoError(t, err)

	assert.Equal(t, []string{"S19"}, a.Models)
	assert.Equal(t, []string{"M30"}, b.Models)
}

func TestGetCachedFleetOptions_ConcurrentCallsDedupe(t *testing.T) {
	svc, store, _ := newServiceWithCache(t, time.Minute)

	// Block the first call long enough that all goroutines pile up on the
	// singleflight slot before it returns. If singleflight collapses them,
	// the store sees exactly one call.
	release := make(chan struct{})
	store.EXPECT().GetAvailableModels(gomock.Any(), int64(42)).
		DoAndReturn(func(_, _ any) ([]string, error) {
			<-release
			return []string{"S19"}, nil
		}).Times(1)
	store.EXPECT().GetAvailableFirmwareVersions(gomock.Any(), int64(42)).
		Return([]string{"v1"}, nil).Times(1)

	const callers = 10
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			_, err := svc.getCachedFleetOptions(t.Context(), 42)
			assert.NoError(t, err)
		}()
	}

	// Give callers time to enqueue on the singleflight slot, then release.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
}

func TestGetCachedFleetOptions_StoreErrorDoesNotPoisonCache(t *testing.T) {
	svc, store, _ := newServiceWithCache(t, time.Minute)

	gomock.InOrder(
		store.EXPECT().GetAvailableModels(gomock.Any(), int64(1)).
			Return(nil, errors.New("db down")),
		store.EXPECT().GetAvailableModels(gomock.Any(), int64(1)).
			Return([]string{"S19"}, nil),
	)
	store.EXPECT().GetAvailableFirmwareVersions(gomock.Any(), int64(1)).
		Return([]string{"v1"}, nil).Times(1)

	_, err := svc.getCachedFleetOptions(t.Context(), 1)
	require.Error(t, err)

	opts, err := svc.getCachedFleetOptions(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"S19"}, opts.Models)
}

func TestGetCachedFleetOptions_NilCacheStillFetches(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := storesMocks.NewMockDeviceStore(ctrl)
	svc := &Service{deviceStore: store} // optionsCache left nil

	store.EXPECT().GetAvailableModels(gomock.Any(), int64(1)).Return([]string{"S19"}, nil).Times(2)
	store.EXPECT().GetAvailableFirmwareVersions(gomock.Any(), int64(1)).Return([]string{"v1"}, nil).Times(2)

	for range 2 {
		opts, err := svc.getCachedFleetOptions(t.Context(), 1)
		require.NoError(t, err)
		assert.Equal(t, []string{"S19"}, opts.Models)
	}
}
