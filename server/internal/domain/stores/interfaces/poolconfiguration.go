package interfaces

import (
	"context"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
)

type PoolConfigurationStore interface {
	GetPoolConfiguration(ctx context.Context, poolConfigurationID int64) (*pb.PoolConfiguration, error)
	ListPoolConfigurations(ctx context.Context, orgID int64) ([]*pb.PoolConfiguration, error)
	CreatePoolConfiguration(ctx context.Context, config *pb.PoolConfigurationConfig, orgID int64) (int64, error)
	DeletePoolConfiguration(ctx context.Context, poolConfigurationID int64) error
	AddPoolToConfiguration(ctx context.Context, poolConfigurationID int64, poolID int64, priority int32) (int64, error)
	RemovePoolFromConfiguration(ctx context.Context, poolConfigurationPoolID int64) error
	GetPoolConfigurationsWithPools(ctx context.Context, orgID int64) ([]*pb.PoolConfigurationWithPools, error)
	GetPoolConfigurationPoolWithPriority(ctx context.Context, poolConfigurationPoolID int64) (*pb.PoolConfigurationPoolWithPriority, error)
}
