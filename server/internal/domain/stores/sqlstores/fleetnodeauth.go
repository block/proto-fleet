package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
)

var _ auth.Store = &SQLFleetNodeAuthStore{}

type SQLFleetNodeAuthStore struct {
	SQLConnectionManager
}

func NewSQLFleetNodeAuthStore(conn *sql.DB) *SQLFleetNodeAuthStore {
	return &SQLFleetNodeAuthStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLFleetNodeAuthStore) q(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

func (s *SQLFleetNodeAuthStore) UpsertChallenge(ctx context.Context, challenge []byte, fleetNodeID int64, expiresAt time.Time) error {
	return s.q(ctx).UpsertFleetNodeAuthChallenge(ctx, sqlc.UpsertFleetNodeAuthChallengeParams{
		Challenge:   challenge,
		FleetNodeID: fleetNodeID,
		ExpiresAt:   expiresAt,
	})
}

func (s *SQLFleetNodeAuthStore) ConsumeChallenge(ctx context.Context, challenge []byte, now time.Time) (int64, error) {
	row, err := s.q(ctx).ConsumeFleetNodeAuthChallenge(ctx, sqlc.ConsumeFleetNodeAuthChallengeParams{
		Challenge: challenge,
		ExpiresAt: now,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fleeterror.NewNotFoundError("challenge not found or expired")
		}
		return 0, err
	}
	return row.FleetNodeID, nil
}

func (s *SQLFleetNodeAuthStore) SweepExpiredChallenges(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SweepExpiredFleetNodeAuthChallenges(ctx, now)
}

func (s *SQLFleetNodeAuthStore) UpsertSession(ctx context.Context, tokenHash string, fleetNodeID int64, expiresAt time.Time) error {
	return s.q(ctx).UpsertFleetNodeSession(ctx, sqlc.UpsertFleetNodeSessionParams{
		TokenHash:   tokenHash,
		FleetNodeID: fleetNodeID,
		ExpiresAt:   expiresAt,
	})
}

func (s *SQLFleetNodeAuthStore) GetSessionFleetNode(ctx context.Context, tokenHash string, now time.Time) (*auth.ResolvedFleetNode, error) {
	row, err := s.q(ctx).GetFleetNodeSessionByTokenHash(ctx, sqlc.GetFleetNodeSessionByTokenHashParams{
		TokenHash: tokenHash,
		ExpiresAt: now,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("session not found or expired")
		}
		return nil, err
	}
	return &auth.ResolvedFleetNode{
		FleetNodeID:    row.FleetNodeID,
		OrgID:          row.OrgID,
		Name:           row.Name,
		IdentityPubkey: row.IdentityPubkey,
	}, nil
}

func (s *SQLFleetNodeAuthStore) SweepExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SweepExpiredFleetNodeSessions(ctx, now)
}
