package poolconfigurations

import (
	"context"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
)

type Service struct {
	poolConfigurationStore interfaces.PoolConfigurationStore
	transactor             interfaces.Transactor
}

func NewService(poolConfigurationStore interfaces.PoolConfigurationStore, transactor interfaces.Transactor) *Service {
	return &Service{
		poolConfigurationStore: poolConfigurationStore,
		transactor:             transactor,
	}
}

func (s *Service) ListPoolConfigurations(ctx context.Context) ([]*pb.PoolConfiguration, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	poolConfigurations, err := s.poolConfigurationStore.ListPoolConfigurations(ctx, claims.OrgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error listing pool configurations: %v", err)
	}

	return poolConfigurations, nil
}

func (s *Service) CreatePoolConfiguration(ctx context.Context, config *pb.PoolConfigurationConfig) (*pb.PoolConfiguration, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	poolConfigurationID, err := s.poolConfigurationStore.CreatePoolConfiguration(ctx, config, claims.OrgID)
	if err != nil {
		return nil, err
	}

	return s.poolConfigurationStore.GetPoolConfiguration(ctx, poolConfigurationID)
}

func (s *Service) DeletePoolConfiguration(ctx context.Context, poolConfigurationID int64) error {
	return s.poolConfigurationStore.DeletePoolConfiguration(ctx, poolConfigurationID)
}

func (s *Service) AddPoolToConfiguration(ctx context.Context, poolConfigurationID int64, poolID int64, priority int32) (*pb.PoolConfigurationPoolWithPriority, error) {
	poolConfigurationPoolID, err := s.poolConfigurationStore.AddPoolToConfiguration(ctx, poolConfigurationID, poolID, priority)
	if err != nil {
		return nil, err
	}

	return s.poolConfigurationStore.GetPoolConfigurationPoolWithPriority(ctx, poolConfigurationPoolID)
}

func (s *Service) RemovePoolFromConfiguration(ctx context.Context, poolConfigurationPoolID int64) error {
	return s.poolConfigurationStore.RemovePoolFromConfiguration(ctx, poolConfigurationPoolID)
}

func (s *Service) GetPoolConfigurationsWithPools(ctx context.Context) ([]*pb.PoolConfigurationWithPools, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	return s.poolConfigurationStore.GetPoolConfigurationsWithPools(ctx, claims.OrgID)
}
