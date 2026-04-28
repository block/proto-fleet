package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"

	pb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.PoolStore = &SQLPoolStore{}

type SQLPoolStore struct {
	SQLConnectionManager
	encryptor *encrypt.Service
}

func NewSQLPoolStore(conn *sql.DB, encryptor *encrypt.Service) *SQLPoolStore {
	return &SQLPoolStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
		encryptor:            encryptor,
	}
}

func (s *SQLPoolStore) GetPool(ctx context.Context, orgID int64, poolID int64) (*pb.Pool, error) {
	pool, err := s.GetQueries(ctx).GetPool(ctx, sqlc.GetPoolParams{ID: poolID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("pool not found: %d", poolID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get pool: %v", err)
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

func (s *SQLPoolStore) CreatePool(ctx context.Context, config *pb.PoolConfig, orgID int64) (int64, error) {
	password := ""
	if config.Password != nil {
		encryptedPassword, err := s.encryptor.Encrypt([]byte(config.Password.Value))
		if err != nil {
			return 0, fleeterror.NewInternalErrorf("error encrypting password: %v", err)
		}
		password = encryptedPassword
	}

	poolID, err := s.GetQueries(ctx).CreatePool(ctx, sqlc.CreatePoolParams{
		PoolName:    config.PoolName,
		Url:         config.Url,
		Username:    config.Username,
		PasswordEnc: password,
		CreatedAt:   time.Now(),
		OrgID:       orgID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("error creating pool: %v", err)
	}

	return poolID, nil
}

func (s *SQLPoolStore) UpdatePool(ctx context.Context, request *pb.UpdatePoolRequest, orgID int64) error {
	// First get the current pool to preserve values that aren't being updated
	pool, err := s.GetQueries(ctx).GetPool(ctx, sqlc.GetPoolParams{ID: request.PoolId, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewNotFoundErrorf("pool not found: %d", request.PoolId)
		}
		return fleeterror.NewInternalErrorf("error getting pool: %v", err)
	}

	if request.PoolName != nil {
		pool.PoolName = request.GetPoolName()
	}

	if request.Url != nil {
		pool.Url = request.GetUrl()
	}

	if request.Username != nil {
		pool.Username = request.GetUsername()
	}

	password := pool.PasswordEnc
	if request.Password != nil {
		encryptedPassword, err := s.encryptor.Encrypt([]byte(request.Password.Value))
		if err != nil {
			return fleeterror.NewInternalErrorf("error encrypting password: %v", err)
		}
		password = encryptedPassword
	}

	// Update the pool
	return s.GetQueries(ctx).UpdatePool(ctx, sqlc.UpdatePoolParams{
		PoolName:    pool.PoolName,
		Url:         pool.Url,
		Username:    pool.Username,
		PasswordEnc: password,
		UpdatedAt:   time.Now(),
		OrgID:       orgID,
		ID:          request.PoolId,
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
		PoolId:   pool.ID,
		PoolName: pool.PoolName,
		Url:      pool.Url,
		Username: pool.Username,
	}
}
