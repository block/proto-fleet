package poolconfigurations

import (
	"context"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
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

func (s *Service) ListPoolConfigurations(ctx context.Context) (*pb.ListPoolConfigurationsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	poolConfigurations, err := s.poolConfigurationStore.ListPoolConfigurations(ctx, info.OrganizationID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error listing pool configurations: %v", err)
	}

	return &pb.ListPoolConfigurationsResponse{Configurations: poolConfigurations}, nil
}

func (s *Service) GetPoolConfiguration(ctx context.Context, id int64) (*pb.GetPoolConfigurationResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	poolConfiguration, err := s.poolConfigurationStore.GetPoolConfiguration(ctx, info.OrganizationID, id)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting pool configuration: %v", err)
	}

	return &pb.GetPoolConfigurationResponse{Configuration: poolConfiguration}, nil
}

func (s *Service) UpsertPoolConfiguration(
	ctx context.Context,
	poolConfiguration *pb.PoolConfigurationBase,
	poolEntries []*pb.PoolConfigurationEntry,
) (*pb.UpsertPoolConfigurationResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		err := s.poolConfigurationStore.UpsertPoolConfiguration(ctx, info.OrganizationID, poolConfiguration)
		if err != nil {
			return nil, err
		}

		configID, err := s.poolConfigurationStore.GetPoolConfigurationIDByOrg(ctx, info.OrganizationID)
		if err != nil {
			return nil, err
		}

		err = s.poolConfigurationStore.DeletePoolConfigurationPools(ctx, configID)
		if err != nil {
			return nil, err
		}

		for _, entry := range poolEntries {
			err = s.poolConfigurationStore.AddPoolToConfiguration(ctx, configID, entry.Id, entry.Priority)
			if err != nil {
				return nil, err
			}
		}

		updatedConfig, err := s.poolConfigurationStore.GetPoolConfiguration(ctx, info.OrganizationID, configID)
		if err != nil {
			return nil, err
		}

		return updatedConfig, nil
	})

	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to upsert pool configuration: %v", err)
	}

	configuration, ok := result.(*pb.PoolConfigurationWithPools)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	return &pb.UpsertPoolConfigurationResponse{Configuration: configuration}, nil
}

func (s *Service) DeletePoolConfiguration(ctx context.Context, id int64) (*pb.DeletePoolConfigurationResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	err = s.poolConfigurationStore.DeletePoolConfiguration(ctx, info.OrganizationID, id)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error deleting pool configuration: %v", err)
	}

	return &pb.DeletePoolConfigurationResponse{}, nil
}
