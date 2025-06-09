package pools

import (
	"context"
	"database/sql"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
	stratumv1 "github.com/btc-mining/proto-fleet/server/internal/infrastructure/stratum/v1"
)

type PoolStatus string

const (
	defaultPoolPriority int32 = 10
)

type Service struct {
	conn *sql.DB
	cfg  Config
}

func NewService(db *sql.DB, cfg Config) *Service {
	return &Service{
		conn: db,
		cfg:  cfg,
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

	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		return q.SoftDeletePool(ctx, sqlc.SoftDeletePoolParams{
			ID:    id,
			OrgID: claims.OrgID,
		})
	})
}

func (s *Service) UpdatePoolPriority(ctx context.Context, priorities []*pb.PoolPriority) ([]*pb.Pool, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]*pb.Pool, error) {
		var pools []*pb.Pool
		for _, p := range priorities {
			err := q.UpdatePoolPriority(ctx, sqlc.UpdatePoolPriorityParams{
				OrgID:        claims.OrgID,
				ID:           p.PoolId,
				PoolPriority: p.Priority,
			})
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("error getting number of paired devices: %v", err)
			}
			pool, err := q.GetPool(ctx, sqlc.GetPoolParams{OrgID: claims.OrgID, ID: p.PoolId})
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to get pool: %v", err)
			}
			poolDto := toPoolDto(&pool)
			pools = append(pools, poolDto)
		}
		return pools, nil
	})
}

func (s *Service) UpdatePool(ctx context.Context, r *pb.UpdatePoolRequest) (*pb.Pool, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (*pb.Pool, error) {
		pool, err := q.GetPool(ctx, sqlc.GetPoolParams{
			OrgID: claims.OrgID,
			ID:    r.PoolId,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get pool: %v", err)
		}
		if r.PoolName != "" {
			pool.PoolName = r.PoolName
		}
		if r.IsDefault {
			pool.IsDefault = sql.NullBool{Bool: r.IsDefault, Valid: true}
			// unset any other default pool
			err := q.UnsetDefaultPool(ctx, sqlc.UnsetDefaultPoolParams{
				OrgID:     claims.OrgID,
				UpdatedAt: time.Now(),
			})
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to unser default pool: %v", err)
			}
		}
		if r.Url != "" {
			pool.Url = r.Url
		}
		if r.Username != "" {
			pool.Username = r.Username
		}
		if r.Password != nil {
			// TODO encrypt password
			pool.PasswordEnc = r.Password.Value
		}
		err = q.UpdatePool(ctx, sqlc.UpdatePoolParams{
			PoolName:     pool.PoolName,
			Url:          pool.Url,
			Username:     pool.Username,
			PoolPriority: pool.PoolPriority,
			PoolStatus:   pool.PoolStatus,
			IsDefault:    pool.IsDefault,
			UpdatedAt:    time.Now(),
			OrgID:        claims.OrgID,
			ID:           pool.ID,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to update pool: %v", err)
		}
		return toPoolDto(&pool), nil
	})
}

func (s *Service) CreatePool(ctx context.Context, r *pb.PoolConfig) (*pb.Pool, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (*pb.Pool, error) {
		pools, err := q.ListPools(ctx, claims.OrgID)

		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting list of pools for org_id: %d: %v", claims.OrgID, err)
		}
		password := ""
		if r.Password != nil {
			// TODO encrypt password
			password = r.Password.Value
		}
		result, err := q.CreatePool(ctx, sqlc.CreatePoolParams{
			PoolName:     r.PoolName,
			Url:          r.Url,
			Username:     r.Username,
			PasswordEnc:  password,
			PoolStatus:   sqlc.PoolPoolStatusUNKNOWN,
			PoolPriority: defaultPoolPriority,
			IsDefault:    sql.NullBool{Valid: true, Bool: len(pools) == 0},
			CreatedAt:    time.Now(),

			OrgID: claims.OrgID,
		})

		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error saving pool for org_id: %d, pool_name: %s: %v", claims.OrgID, r.PoolName, err)
		}
		poolID, err := result.LastInsertId()

		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting id of created pool for org_id: %d, pool_name: %s: %v", claims.OrgID, r.PoolName, err)
		}
		pool, err := q.GetPool(ctx, sqlc.GetPoolParams{
			OrgID: claims.OrgID,
			ID:    poolID,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting created pool for org_id: %d, pool_id: %d: %v", claims.OrgID, poolID, err)
		}
		return toPoolDto(&pool), nil
	})
}

func (s *Service) ListPools(ctx context.Context) ([]*pb.Pool, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]*pb.Pool, error) {
		result, err := q.ListPools(ctx, claims.OrgID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error listing pools : %v", err)
		}
		var pools []*pb.Pool
		for _, p := range result {
			poolDto := toPoolDto(&p)
			pools = append(pools, poolDto)
		}
		return pools, nil
	})
}

func toPoolDto(pool *sqlc.Pool) *pb.Pool {
	return &pb.Pool{
		PoolId:       pool.ID,
		PoolName:     pool.PoolName,
		Url:          pool.Url,
		Username:     pool.Username,
		PoolPriority: pool.PoolPriority,
		PoolStatus:   convertToProtoStatus(pool.PoolStatus),
		IsDefault:    pool.IsDefault.Valid && pool.IsDefault.Bool,
	}
}

// Convert internal status to proto status
func convertToProtoStatus(status sqlc.PoolPoolStatus) pb.PoolConnectionStatus {
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

// Convert proto status to internal status
func convertFromProtoStatus(status pb.PoolConnectionStatus) sqlc.PoolPoolStatus {
	switch status {
	case pb.PoolConnectionStatus_POOL_CONNECTION_STATUS_UNSPECIFIED:
		return sqlc.PoolPoolStatusUNKNOWN
	case pb.PoolConnectionStatus_POOL_CONNECTION_STATUS_IDLE:
		return sqlc.PoolPoolStatusIDLE
	case pb.PoolConnectionStatus_POOL_CONNECTION_STATUS_ACTIVE:
		return sqlc.PoolPoolStatusACTIVE
	case pb.PoolConnectionStatus_POOL_CONNECTION_STATUS_DEAD:
		return sqlc.PoolPoolStatusDEAD
	default:
		return sqlc.PoolPoolStatusUNKNOWN
	}
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
