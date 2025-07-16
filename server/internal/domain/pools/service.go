package pools

import (
	"context"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
	stratumv1 "github.com/btc-mining/proto-fleet/server/internal/infrastructure/stratum/v1"
)

type PoolStatus string

type Service struct {
	poolStore  interfaces.PoolStore
	transactor interfaces.Transactor
	cfg        Config
}

func NewService(poolStore interfaces.PoolStore, transactor interfaces.Transactor, cfg Config) *Service {
	return &Service{
		poolStore:  poolStore,
		transactor: transactor,
		cfg:        cfg,
	}
}

func (s *Service) UpdateDefaultPool(ctx context.Context, poolID int64) (*pb.Pool, error) {
	return s.UpdatePool(ctx, &pb.UpdatePoolRequest{
		PoolId:    poolID,
		IsDefault: true,
	})
}

func (s *Service) DeletePool(ctx context.Context, id int64) error {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return err
	}

	return s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		return s.poolStore.SoftDeletePool(ctx, claims.OrgID, id)
	})
}

func (s *Service) UpdatePool(ctx context.Context, r *pb.UpdatePoolRequest) (*pb.Pool, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		// If setting as default, unset any other default pool first
		if r.IsDefault {
			err := s.poolStore.UnsetDefaultPool(ctx, claims.OrgID)
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to unset default pool: %v", err)
			}
		}

		// Update the pool
		err := s.poolStore.UpdatePool(ctx, r, claims.OrgID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to update pool: %v", err)
		}

		// Get the updated pool
		updatedPool, err := s.poolStore.GetPool(ctx, claims.OrgID, r.PoolId)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get updated pool: %v", err)
		}

		return updatedPool, nil
	})

	if err != nil {
		return nil, err
	}

	updatedPool, ok := result.(*pb.Pool)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}
	return updatedPool, nil
}

func (s *Service) CreatePool(ctx context.Context, poolConfig *pb.PoolConfig) (*pb.Pool, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		totalPools, err := s.poolStore.GetTotalPools(ctx, claims.OrgID)
		if err != nil {
			return nil, err
		}

		isDefault := totalPools == 0

		poolID, err := s.poolStore.CreatePool(ctx, poolConfig, claims.OrgID, isDefault)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error saving pool for org_id: %d, pool_name: %s: %v", claims.OrgID, poolConfig.PoolName, err)
		}

		pool, err := s.poolStore.GetPool(ctx, claims.OrgID, poolID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting created pool for org_id: %d, pool_id: %d: %v", claims.OrgID, poolID, err)
		}

		return pool, nil
	})

	if err != nil {
		return nil, err
	}

	pool, ok := result.(*pb.Pool)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}
	return pool, nil
}

func (s *Service) ListPools(ctx context.Context) ([]*pb.Pool, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	pools, err := s.poolStore.ListPools(ctx, claims.OrgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error listing pools: %v", err)
	}

	return pools, nil
}

// ValidateConnection the connection to a pool server.
// It returns true if the connection is successful, otherwise false.
// We currently only support Stratum V1 connection pools, if you need V2
// support please use a proxy v1->v2 as described https://stratumprotocol.org/docs/#proxies
func (s *Service) ValidateConnection(ctx context.Context, url string, username string, password *secrets.Text, timeout *time.Duration) (bool, error) {
	to := s.cfg.Timeout
	if timeout != nil {
		to = *timeout
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	ok, err := stratumv1.Authenticate(ctx, url, username, password)

	if err != nil {
		return false, err
	}

	return ok, nil
}
