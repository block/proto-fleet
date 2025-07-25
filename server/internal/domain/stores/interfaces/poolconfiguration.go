package interfaces

import (
	"context"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
)

type PoolConfigurationStore interface {
	ListPoolConfigurations(ctx context.Context, orgID int64) ([]*pb.PoolConfigurationWithPools, error)
	GetPoolConfiguration(ctx context.Context, orgID int64, configurationID int64) (*pb.PoolConfigurationWithPools, error)
	GetPoolConfigurationIDByOrg(ctx context.Context, orgID int64) (int64, error)

	DeletePoolConfiguration(ctx context.Context, orgID int64, configurationID int64) error
	DeletePoolConfigurationPools(ctx context.Context, configID int64) error

	UpsertPoolConfiguration(ctx context.Context, orgID int64, config *pb.PoolConfigurationBase) error
	AddPoolToConfiguration(ctx context.Context, poolConfigurationID int64, poolID int64, priority int32) error
}
