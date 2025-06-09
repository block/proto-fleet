package onboarding

import (
	"context"
	"database/sql"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
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
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (*pb.FleetOnboardingStatus, error) {
		totalPairedDevices, err := q.GetTotalPairedDevices(ctx)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting number of paired devices: %v", err)
		}

		totalPools, err := q.GetTotalPools(ctx, claims.OrgID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting number of configured pools: %v", err)
		}

		return &pb.FleetOnboardingStatus{
			NetworkConfigured: false,
			PoolConfigured:    totalPools > 0,
			DevicePaired:      totalPairedDevices > 0,
		}, nil
	})
}
