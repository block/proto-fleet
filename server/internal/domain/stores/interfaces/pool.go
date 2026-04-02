package interfaces

import (
	"context"

	pb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
)

type PoolStore interface {
	GetPool(ctx context.Context, orgID int64, poolID int64) (*pb.Pool, error)
	ListPools(ctx context.Context, orgID int64) ([]*pb.Pool, error)
	GetTotalPools(ctx context.Context, orgID int64) (int64, error)
	CreatePool(ctx context.Context, config *pb.PoolConfig, orgID int64) (int64, error)
	UpdatePool(ctx context.Context, request *pb.UpdatePoolRequest, orgID int64) error
	SoftDeletePool(ctx context.Context, orgID int64, poolID int64) error
}
