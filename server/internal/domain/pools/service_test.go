package pools

import (
	"context"
	"testing"

	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/proto-at-block/proto-fleet/server/internal/testutil"
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
	}, stubTransactor{}, Config{})

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
	}, stubTransactor{}, Config{})

	_, err := svc.UpdatePool(testCtx(t), &pb.UpdatePoolRequest{
		PoolId:   1,
		Username: "wallet.worker01",
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
	}, stubTransactor{}, Config{})

	updated, err := svc.UpdatePool(testCtx(t), &pb.UpdatePoolRequest{
		PoolId:   1,
		PoolName: "Renamed Pool",
		Username: "wallet.worker01",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "wallet.worker01", updated.GetUsername())
}
