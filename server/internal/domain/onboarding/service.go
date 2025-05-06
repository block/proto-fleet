package onboarding

import (
	"context"
	"database/sql"
	"errors"

	"connectrpc.com/authn"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
)

type Service struct {
	conn *sql.DB
}

func NewService(conn *sql.DB) *Service {
	return &Service{
		conn: conn,
	}
}

func (s *Service) GetFleetOnboardingStatus(ctx context.Context) (*pb.FleetOnboardingStatus, error) {
	claims, ok := authn.GetInfo(ctx).(tokenDomain.Claims)
	if !ok {
		return nil, ErrUnauthorized
	}
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (*pb.FleetOnboardingStatus, error) {
		totalPairedDevices, err := q.GetTotalPairedDevices(ctx)
		if err != nil {
			return nil, err
		}
		totalPools, err := q.GetTotalPools(ctx, claims.OrgID)
		if err != nil {
			return nil, err
		}

		return &pb.FleetOnboardingStatus{
			NetworkConfigured: false,
			PoolConfigured:    totalPools > 0,
			DevicePaired:      totalPairedDevices > 0,
		}, nil
	})
}
