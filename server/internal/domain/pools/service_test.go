package pools

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubPoolStore struct {
	createPoolFn     func(ctx context.Context, config *pb.PoolConfig, orgID int64) (int64, error)
	updatePoolFn     func(ctx context.Context, request *pb.UpdatePoolRequest, orgID int64) error
	getPoolFn        func(ctx context.Context, orgID int64, poolID int64) (*pb.Pool, error)
	listPoolsFn      func(ctx context.Context, orgID int64) ([]*pb.Pool, error)
	getTotalPoolsFn  func(ctx context.Context, orgID int64) (int64, error)
	softDeletePoolFn func(ctx context.Context, orgID int64, poolID int64) error
}

func (s *stubPoolStore) GetPool(ctx context.Context, orgID int64, poolID int64) (*pb.Pool, error) {
	if s.getPoolFn != nil {
		return s.getPoolFn(ctx, orgID, poolID)
	}
	return nil, nil
}

func (s *stubPoolStore) ListPools(ctx context.Context, orgID int64) ([]*pb.Pool, error) {
	if s.listPoolsFn != nil {
		return s.listPoolsFn(ctx, orgID)
	}
	return nil, nil
}

func (s *stubPoolStore) GetTotalPools(ctx context.Context, orgID int64) (int64, error) {
	if s.getTotalPoolsFn != nil {
		return s.getTotalPoolsFn(ctx, orgID)
	}
	return 0, nil
}

func (s *stubPoolStore) CreatePool(ctx context.Context, config *pb.PoolConfig, orgID int64) (int64, error) {
	if s.createPoolFn != nil {
		return s.createPoolFn(ctx, config, orgID)
	}
	return 0, nil
}

func (s *stubPoolStore) UpdatePool(ctx context.Context, request *pb.UpdatePoolRequest, orgID int64) error {
	if s.updatePoolFn != nil {
		return s.updatePoolFn(ctx, request, orgID)
	}
	return nil
}

func (s *stubPoolStore) SoftDeletePool(ctx context.Context, orgID int64, poolID int64) error {
	if s.softDeletePoolFn != nil {
		return s.softDeletePoolFn(ctx, orgID, poolID)
	}
	return nil
}

type stubTransactor struct{}

func (stubTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (stubTransactor) RunInTxWithResult(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	return fn(ctx)
}

type noopActivityStore struct{}

func (noopActivityStore) Insert(_ context.Context, _ *activitymodels.Event) error {
	return nil
}

type spyActivityStore struct {
	noopActivityStore
	events []*activitymodels.Event
}

func (s *spyActivityStore) Insert(_ context.Context, event *activitymodels.Event) error {
	s.events = append(s.events, event)
	return nil
}

func (noopActivityStore) List(ctx context.Context, filter activitymodels.Filter) ([]activitymodels.Entry, error) {
	return nil, nil
}

func (noopActivityStore) Count(ctx context.Context, filter activitymodels.Filter) (int64, error) {
	return 0, nil
}

func (noopActivityStore) GetDistinctUsers(ctx context.Context, orgID int64) ([]activitymodels.UserInfo, error) {
	return nil, nil
}

func (noopActivityStore) GetDistinctEventTypes(ctx context.Context, orgID int64) ([]activitymodels.EventTypeInfo, error) {
	return nil, nil
}

func (noopActivityStore) GetDistinctScopeTypes(ctx context.Context, orgID int64) ([]string, error) {
	return nil, nil
}

func testActivitySvc() *activity.Service {
	return activity.NewService(noopActivityStore{})
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return testutil.MockAuthContextForTesting(t.Context(), 1, 1)
}

func TestService_CreatePool_RejectsUsernameWithSeparator(t *testing.T) {
	svc := NewService(&stubPoolStore{
		createPoolFn: func(context.Context, *pb.PoolConfig, int64) (int64, error) {
			t.Fatal("CreatePool should not be called for invalid usernames")
			return 0, nil
		},
	}, stubTransactor{}, Config{}, testActivitySvc())

	_, err := svc.CreatePool(testCtx(t), &pb.PoolConfig{
		PoolName: "Test Pool",
		Url:      "stratum+tcp://pool.example.com:3333",
		Username: "wallet.worker01",
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), invalidPoolUsernameSeparatorMessage)
}

func TestService_UpdatePool_RejectsUsernameWithSeparator(t *testing.T) {
	svc := NewService(&stubPoolStore{
		getPoolFn: func(context.Context, int64, int64) (*pb.Pool, error) {
			return &pb.Pool{
				PoolId:   1,
				Username: "wallet",
			}, nil
		},
		updatePoolFn: func(context.Context, *pb.UpdatePoolRequest, int64) error {
			t.Fatal("UpdatePool should not be called for invalid usernames")
			return nil
		},
	}, stubTransactor{}, Config{}, testActivitySvc())

	username := "wallet.worker01"
	_, err := svc.UpdatePool(testCtx(t), &pb.UpdatePoolRequest{
		PoolId:   1,
		Username: &username,
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), invalidPoolUsernameSeparatorMessage)
}

func TestService_UpdatePool_AllowsUnchangedLegacyUsernameWithSeparator(t *testing.T) {
	svc := NewService(&stubPoolStore{
		getPoolFn: func(context.Context, int64, int64) (*pb.Pool, error) {
			return &pb.Pool{
				PoolId:   1,
				PoolName: "Legacy Pool",
				Url:      "stratum+tcp://pool.example.com:3333",
				Username: "wallet.worker01",
			}, nil
		},
		updatePoolFn: func(context.Context, *pb.UpdatePoolRequest, int64) error {
			return nil
		},
	}, stubTransactor{}, Config{}, testActivitySvc())

	poolName := "Renamed Pool"
	username := "wallet.worker01"
	updated, err := svc.UpdatePool(testCtx(t), &pb.UpdatePoolRequest{
		PoolId:   1,
		PoolName: &poolName,
		Username: &username,
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "wallet.worker01", updated.GetUsername())
}

func TestService_UpdatePool_RejectsEmptyStringPatches(t *testing.T) {
	// Arrange
	svc := NewService(&stubPoolStore{
		updatePoolFn: func(context.Context, *pb.UpdatePoolRequest, int64) error {
			t.Fatal("UpdatePool should not be called for empty patches")
			return nil
		},
	}, stubTransactor{}, Config{}, testActivitySvc())

	tests := []struct {
		name     string
		mutate   func(req *pb.UpdatePoolRequest)
		wantSubs string
	}{
		{
			name:     "empty pool_name",
			mutate:   func(r *pb.UpdatePoolRequest) { empty := ""; r.PoolName = &empty },
			wantSubs: "pool_name",
		},
		{
			name:     "empty url",
			mutate:   func(r *pb.UpdatePoolRequest) { empty := ""; r.Url = &empty },
			wantSubs: "url",
		},
		{
			name:     "empty username",
			mutate:   func(r *pb.UpdatePoolRequest) { empty := ""; r.Username = &empty },
			wantSubs: "username",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			req := &pb.UpdatePoolRequest{PoolId: 1}
			tc.mutate(req)

			// Act
			_, err := svc.UpdatePool(testCtx(t), req)

			// Assert
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), tc.wantSubs)
		})
	}
}

func TestService_UpdatePool_AbsentFieldsLeaveValuesUnchanged(t *testing.T) {
	// Arrange
	var captured *pb.UpdatePoolRequest
	svc := NewService(&stubPoolStore{
		getPoolFn: func(context.Context, int64, int64) (*pb.Pool, error) {
			return &pb.Pool{
				PoolId:   7,
				PoolName: "Old Name",
				Url:      "stratum+tcp://pool.example.com:3333",
				Username: "wallet",
			}, nil
		},
		updatePoolFn: func(_ context.Context, req *pb.UpdatePoolRequest, _ int64) error {
			captured = req
			return nil
		},
	}, stubTransactor{}, Config{}, testActivitySvc())

	newName := "New Name"

	// Act
	_, err := svc.UpdatePool(testCtx(t), &pb.UpdatePoolRequest{
		PoolId:   7,
		PoolName: &newName,
	})

	// Assert
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "New Name", captured.GetPoolName())
	assert.Nil(t, captured.Url)
	assert.Nil(t, captured.Username)
}

func TestActivityLogging_CreatePoolLogsEvent(t *testing.T) {
	spy := &spyActivityStore{}
	svc := NewService(&stubPoolStore{
		createPoolFn: func(_ context.Context, _ *pb.PoolConfig, _ int64) (int64, error) {
			return 1, nil
		},
		getPoolFn: func(_ context.Context, _ int64, _ int64) (*pb.Pool, error) {
			return &pb.Pool{PoolId: 1, PoolName: "Test Pool"}, nil
		},
	}, stubTransactor{}, Config{}, activity.NewService(spy))

	pool, err := svc.CreatePool(testCtx(t), &pb.PoolConfig{
		PoolName: "Test Pool",
		Url:      "stratum+tcp://pool.example.com:3333",
		Username: "wallet",
	})

	require.NoError(t, err)
	assert.Equal(t, "Test Pool", pool.GetPoolName())
	require.Len(t, spy.events, 1)
	assert.Equal(t, activitymodels.CategoryPool, spy.events[0].Category)
	assert.Equal(t, "create_pool", spy.events[0].Type)
	assert.Equal(t, "Test Pool", spy.events[0].Metadata["pool_name"])
}

func TestActivityLogging_DeletePoolBestEffortPreFetch(t *testing.T) {
	spy := &spyActivityStore{}
	svc := NewService(&stubPoolStore{
		getPoolFn: func(_ context.Context, _ int64, _ int64) (*pb.Pool, error) {
			return nil, fleeterror.NewNotFoundErrorf("pool not found")
		},
	}, stubTransactor{}, Config{}, activity.NewService(spy))

	err := svc.DeletePool(testCtx(t), 999)

	require.NoError(t, err)
	assert.Empty(t, spy.events, "activity log should be skipped when pre-fetch fails")
}
