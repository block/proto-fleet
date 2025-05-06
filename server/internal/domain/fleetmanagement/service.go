package fleetmanagement

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
)

var (
	ErrForbidden = errors.New("forbidden")
	ErrInternal  = errors.New("internal error")
)

type PoolStatus string

const (
	defaultPoolPriority int32 = 10
)

type Service struct {
	conn *sql.DB
}

func NewService(conn *sql.DB) *Service {
	return &Service{
		conn: conn,
	}
}

func (s *Service) ListPairedMiners(c context.Context, req *pb.ListPairedMinersRequest) (*pb.ListPairedMinersResponse, error) {
	// Validate and set page size
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50 // default page size
	}
	if pageSize > 1000 {
		pageSize = 1000 // maximum page size
	}

	// Decode cursor if provided
	cursor, err := decodeCursor(req.Cursor)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid page token: %w", err))
	}

	// Prepare query parameters
	params := sqlc.ListPairedDevicesParams{
		CursorID:       sql.NullInt64{Int64: cursor.ID, Valid: cursor.ID > 0},
		DeviceCursorID: sql.NullInt64{Int64: cursor.DeviceID, Valid: cursor.DeviceID > 0},
		Limit:          pageSize + 1, // request one extra to determine if there are more pages
	}

	return db.WithTransaction(c, s.conn, func(q *sqlc.Queries) (*pb.ListPairedMinersResponse, error) {

		// Query the database
		devices, err := q.ListPairedDevices(c, params)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list miners: %w", err))
		}

		// Prepare response
		resp := &pb.ListPairedMinersResponse{}

		// Handle pagination
		if len(devices) > int(pageSize) {
			// We got an extra record, so there are more pages
			resp.Miners = make([]*pb.PairedDevice, pageSize)
			for i, d := range devices[:pageSize] {
				resp.Miners[i] = &pb.PairedDevice{
					DeviceIdentifier: d.DeviceIdentifier,
					MacAddress:       d.MacAddress,
					SerialNumber:     d.SerialNumber.String,
				}
			}

			// Create next page token from last visible item
			lastDevice := devices[pageSize-1]
			cursor = Cursor{
				ID:       lastDevice.CursorID,
				DeviceID: lastDevice.DeviceID,
			}
			resp.Cursor = encodeCursor(cursor)
		} else {
			// This is the last page
			resp.Miners = make([]*pb.PairedDevice, len(devices))
			for i, d := range devices {
				resp.Miners[i] = &pb.PairedDevice{
					DeviceIdentifier: d.DeviceIdentifier,
					MacAddress:       d.MacAddress,
					SerialNumber:     d.SerialNumber.String,
				}
			}
		}

		// Get total count
		total, err := q.GetTotalPairedDevices(c)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get total count: %w", err))
		}
		resp.TotalMiners = int32(total) //nolint:gosec
		return resp, nil

	})
}

func (s *Service) UpdateDefaultPool(ctx context.Context, poolID int64) (*pb.Pool, error) {
	return s.UpdatePool(ctx, &pb.UpdatePoolRequest{
		Id:        poolID,
		IsDefault: true,
	})
}

func (s *Service) DeletePool(ctx context.Context, id int64) error {
	claims, ok := authn.GetInfo(ctx).(tokenDomain.Claims)
	if !ok {
		return ErrForbidden
	}
	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		return q.SoftDeletePool(ctx, sqlc.SoftDeletePoolParams{
			ID:    id,
			OrgID: claims.OrgID,
		})
	})
}

func (s *Service) UpdatePoolPriority(ctx context.Context, priorities []*pb.PoolPriority) ([]*pb.Pool, error) {
	claims, ok := authn.GetInfo(ctx).(tokenDomain.Claims)
	if !ok {
		return nil, ErrForbidden
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]*pb.Pool, error) {
		var pools []*pb.Pool
		for _, p := range priorities {
			err := q.UpdatePoolPriority(ctx, sqlc.UpdatePoolPriorityParams{
				OrgID:        claims.OrgID,
				ID:           p.Id,
				PoolPriority: p.Priority,
			})
			if err != nil {
				return nil, ErrInternal
			}
			pool, err := q.GetPool(ctx, sqlc.GetPoolParams{OrgID: claims.OrgID, ID: p.Id})
			if err != nil {
				return nil, ErrInternal
			}
			poolDto, err := toPoolDto(&pool)
			if err != nil {
				return nil, err
			}
			pools = append(pools, poolDto)
		}
		return pools, nil
	})
}

func (s *Service) UpdatePool(ctx context.Context, r *pb.UpdatePoolRequest) (*pb.Pool, error) {
	claims, ok := authn.GetInfo(ctx).(tokenDomain.Claims)
	if !ok {
		return nil, ErrForbidden
	}
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (*pb.Pool, error) {
		pool, err := q.GetPool(ctx, sqlc.GetPoolParams{
			OrgID: claims.OrgID,
			ID:    r.Id,
		})
		if err != nil {
			return nil, ErrInternal
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
				return nil, ErrInternal
			}
		}
		if r.Url != "" {
			pool.Url = r.Url
		}
		if r.Username != "" {
			pool.Username = r.Username
		}
		if r.Password != "" {
			// TODO encrypt password
			pool.PasswordEnc = r.Password
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
			return nil, ErrInternal
		}
		return toPoolDto(&pool)
	})
}

func (s *Service) CreatePool(ctx context.Context, r *pb.PoolConfig) (*pb.Pool, error) {
	claims, ok := authn.GetInfo(ctx).(tokenDomain.Claims)
	if !ok {
		return nil, ErrForbidden
	}
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (*pb.Pool, error) {
		pools, err := q.ListPools(ctx, claims.OrgID)

		if err != nil {
			slog.Error("error getting list of pools", "org_id", claims.OrgID, "error", err)
			return nil, ErrInternal
		}
		result, err := q.CreatePool(ctx, sqlc.CreatePoolParams{
			PoolName: r.PoolName,
			Url:      r.Url,
			Username: r.Username,
			// TODO encrypt password
			PasswordEnc:  r.Password,
			PoolStatus:   sqlc.PoolPoolStatusUNKNOWN,
			PoolPriority: defaultPoolPriority,
			IsDefault:    sql.NullBool{Valid: true, Bool: len(pools) == 0},
			CreatedAt:    time.Now(),

			OrgID: claims.OrgID,
		})

		if err != nil {
			slog.Error("error saving pool", "org_id", claims.OrgID, "pool_name", r.PoolName, "error", err)
			return nil, ErrInternal
		}
		poolID, err := result.LastInsertId()

		if err != nil {
			slog.Error("error getting id of created pool", "org_id", claims.OrgID, "pool_name", r.PoolName, "error", err)
			return nil, ErrInternal
		}
		pool, err := q.GetPool(ctx, sqlc.GetPoolParams{
			OrgID: claims.OrgID,
			ID:    poolID,
		})
		if err != nil {
			slog.Error("error getting created pool", "org_id", claims.OrgID, "pool_id", poolID, "error", err)
			return nil, ErrInternal
		}
		return toPoolDto(&pool)
	})
}

func (s *Service) ListPools(ctx context.Context) ([]*pb.Pool, error) {
	claims, ok := authn.GetInfo(ctx).(tokenDomain.Claims)
	if !ok {
		return nil, ErrForbidden
	}
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]*pb.Pool, error) {
		result, err := q.ListPools(ctx, claims.OrgID)
		if err != nil {
			return nil, ErrInternal
		}
		var pools []*pb.Pool
		for _, p := range result {
			poolDto, err := toPoolDto(&p)
			if err != nil {
				return nil, err
			}
			pools = append(pools, poolDto)
		}
		return pools, nil
	})
}

func toPoolDto(pool *sqlc.Pool) (*pb.Pool, error) {
	return &pb.Pool{
		PoolId:       pool.ID,
		PoolName:     pool.PoolName,
		Url:          pool.Url,
		Username:     pool.Username,
		PoolPriority: pool.PoolPriority,
		PoolStatus:   convertToProtoStatus(pool.PoolStatus),
		IsDefault:    pool.IsDefault.Valid && pool.IsDefault.Bool,
	}, nil
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

type Cursor struct {
	ID       int64
	DeviceID int64
}

func encodeCursor(c Cursor) string {
	raw := fmt.Sprintf("%d:%d", c.ID, c.DeviceID)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(encoded string) (Cursor, error) {
	if encoded == "" {
		return Cursor{}, nil
	}

	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	var cursor Cursor
	_, err = fmt.Sscanf(string(b), "%d:%d", &cursor.ID, &cursor.DeviceID)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor values: %w", err)
	}

	return cursor, nil
}
