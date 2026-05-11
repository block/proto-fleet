package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeauth"
)

var _ fleetnodeauth.Store = &SQLFleetNodeAuthStore{}

type SQLFleetNodeAuthStore struct {
	SQLConnectionManager
}

func NewSQLFleetNodeAuthStore(conn *sql.DB) *SQLFleetNodeAuthStore {
	return &SQLFleetNodeAuthStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLFleetNodeAuthStore) q(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

func (s *SQLFleetNodeAuthStore) UpsertChallenge(ctx context.Context, challenge []byte, agentID int64, expiresAt time.Time) error {
	return s.q(ctx).UpsertAgentAuthChallenge(ctx, sqlc.UpsertAgentAuthChallengeParams{
		Challenge: challenge,
		AgentID:   agentID,
		ExpiresAt: expiresAt,
	})
}

func (s *SQLFleetNodeAuthStore) ConsumeChallenge(ctx context.Context, challenge []byte, now time.Time) (int64, error) {
	row, err := s.q(ctx).ConsumeAgentAuthChallenge(ctx, sqlc.ConsumeAgentAuthChallengeParams{
		Challenge: challenge,
		ExpiresAt: now,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fleeterror.NewNotFoundError("challenge not found or expired")
		}
		return 0, err
	}
	return row.AgentID, nil
}

func (s *SQLFleetNodeAuthStore) SweepExpiredChallenges(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SweepExpiredAgentAuthChallenges(ctx, now)
}

func (s *SQLFleetNodeAuthStore) UpsertSession(ctx context.Context, tokenHash string, agentID int64, expiresAt time.Time) error {
	return s.q(ctx).UpsertAgentSession(ctx, sqlc.UpsertAgentSessionParams{
		TokenHash: tokenHash,
		AgentID:   agentID,
		ExpiresAt: expiresAt,
	})
}

func (s *SQLFleetNodeAuthStore) GetSessionAgent(ctx context.Context, tokenHash string, now time.Time) (*fleetnodeauth.ResolvedFleetNode, error) {
	row, err := s.q(ctx).GetAgentSessionByTokenHash(ctx, sqlc.GetAgentSessionByTokenHashParams{
		TokenHash: tokenHash,
		ExpiresAt: now,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("session not found or expired")
		}
		return nil, err
	}
	return &fleetnodeauth.ResolvedFleetNode{
		FleetNodeID:    row.AgentID,
		OrgID:          row.OrgID,
		Name:           row.Name,
		IdentityPubkey: row.IdentityPubkey,
	}, nil
}

func (s *SQLFleetNodeAuthStore) SweepExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SweepExpiredAgentSessions(ctx, now)
}
