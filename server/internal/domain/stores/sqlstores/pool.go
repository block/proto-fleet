package sqlstores

import (
	"context"
	"database/sql"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.PoolStore = &SQLPoolStore{}

type SQLPoolStore struct {
	SQLConnectionManager
}

func NewSQLPoolStore(conn *sql.DB) *SQLPoolStore {
	return &SQLPoolStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLPoolStore) GetPool(ctx context.Context, orgID int64, poolID int64) (*pb.Pool, error) {
	pool, err := s.GetQueries(ctx).GetPool(ctx, sqlc.GetPoolParams{
		OrgID: orgID,
		ID:    poolID,
	})
	if err != nil {
		return nil, err
	}

	return convertToProtoPool(pool), nil
}

func (s *SQLPoolStore) ListPools(ctx context.Context, orgID int64) ([]*pb.Pool, error) {
	pools, err := s.GetQueries(ctx).ListPools(ctx, orgID)
	if err != nil {
		return nil, err
	}

	result := make([]*pb.Pool, len(pools))
	for i, pool := range pools {
		result[i] = convertToProtoPool(pool)
	}

	return result, nil
}

func (s *SQLPoolStore) GetTotalPools(ctx context.Context, orgID int64) (int64, error) {
	return s.GetQueries(ctx).GetTotalPools(ctx, orgID)
}

func (s *SQLPoolStore) CreatePool(ctx context.Context, config *pb.PoolConfig, orgID int64, poolPriority int32, isDefault bool) (int64, error) {
	password := ""
	if config.Password != nil {
		password = config.Password.Value
	}

	result, err := s.GetQueries(ctx).CreatePool(ctx, sqlc.CreatePoolParams{
		PoolName:     config.PoolName,
		Url:          config.Url,
		Username:     config.Username,
		PasswordEnc:  password,
		PoolStatus:   sqlc.PoolPoolStatusUNKNOWN,
		PoolPriority: poolPriority,
		IsDefault:    sql.NullBool{Bool: isDefault, Valid: true},
		CreatedAt:    time.Now(),
		OrgID:        orgID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("error creating pool: %v", err)
	}

	poolID, err := result.LastInsertId()
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("error getting pool id: %v", err)
	}

	return poolID, nil
}

func (s *SQLPoolStore) UpdatePool(ctx context.Context, request *pb.UpdatePoolRequest, orgID int64) error {
	// First get the current pool to preserve values that aren't being updated
	pool, err := s.GetQueries(ctx).GetPool(ctx, sqlc.GetPoolParams{
		OrgID: orgID,
		ID:    request.PoolId,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting pool: %v", err)
	}

	// Apply updates from the request
	if request.PoolName != "" {
		pool.PoolName = request.PoolName
	}

	if request.Url != "" {
		pool.Url = request.Url
	}

	if request.Username != "" {
		pool.Username = request.Username
	}
	password := ""
	if request.Password != nil {
		// TODO encrypt password
		password = request.Password.Value
	}

	if request.IsDefault {
		pool.IsDefault = sql.NullBool{Bool: true, Valid: true}
	}

	// Update the pool
	return s.GetQueries(ctx).UpdatePool(ctx, sqlc.UpdatePoolParams{
		PoolName:     pool.PoolName,
		Url:          pool.Url,
		Username:     pool.Username,
		PasswordEnc:  password,
		PoolPriority: pool.PoolPriority,
		PoolStatus:   pool.PoolStatus,
		IsDefault:    pool.IsDefault,
		UpdatedAt:    time.Now(),
		OrgID:        orgID,
		ID:           request.PoolId,
	})
}

func (s *SQLPoolStore) UpdatePoolPriority(ctx context.Context, orgID int64, poolID int64, priority int32) error {
	return s.GetQueries(ctx).UpdatePoolPriority(ctx, sqlc.UpdatePoolPriorityParams{
		OrgID:        orgID,
		ID:           poolID,
		PoolPriority: priority,
	})
}

func (s *SQLPoolStore) UnsetDefaultPool(ctx context.Context, orgID int64) error {
	return s.GetQueries(ctx).UnsetDefaultPool(ctx, sqlc.UnsetDefaultPoolParams{
		OrgID:     orgID,
		UpdatedAt: time.Now(),
	})
}

func (s *SQLPoolStore) SoftDeletePool(ctx context.Context, orgID int64, poolID int64) error {
	return s.GetQueries(ctx).SoftDeletePool(ctx, sqlc.SoftDeletePoolParams{
		OrgID: orgID,
		ID:    poolID,
	})
}

func convertToProtoPool(pool sqlc.Pool) *pb.Pool {
	return &pb.Pool{
		PoolId:       pool.ID,
		PoolName:     pool.PoolName,
		Url:          pool.Url,
		Username:     pool.Username,
		PoolPriority: pool.PoolPriority,
		PoolStatus:   convertSQLStatusToProtoStatus(pool.PoolStatus),
		IsDefault:    pool.IsDefault.Valid && pool.IsDefault.Bool,
	}
}

// Convert SQL status to proto status
func convertSQLStatusToProtoStatus(status sqlc.PoolPoolStatus) pb.PoolConnectionStatus {
	switch status {
	case sqlc.PoolPoolStatusUNKNOWN:
		return pb.PoolConnectionStatus_POOL_CONNECTION_STATUS_UNSPECIFIED
	case sqlc.PoolPoolStatusIDLE:
		return pb.PoolConnectionStatus_POOL_CONNECTION_STATUS_IDLE
	case sqlc.PoolPoolStatusACTIVE:
		return pb.PoolConnectionStatus_POOL_CONNECTION_STATUS_ACTIVE
	case sqlc.PoolPoolStatusDEAD:
		return pb.PoolConnectionStatus_POOL_CONNECTION_STATUS_DEAD
	default:
		return pb.PoolConnectionStatus_POOL_CONNECTION_STATUS_UNSPECIFIED
	}
}
