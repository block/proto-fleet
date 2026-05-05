package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/agentauth"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

var _ agentauth.Store = &SQLAgentAuthStore{}

type SQLAgentAuthStore struct {
	SQLConnectionManager
}

func NewSQLAgentAuthStore(conn *sql.DB) *SQLAgentAuthStore {
	return &SQLAgentAuthStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLAgentAuthStore) q(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

func (s *SQLAgentAuthStore) CreateChallenge(ctx context.Context, challenge []byte, agentID int64, expiresAt time.Time) error {
	return s.q(ctx).CreateAgentAuthChallenge(ctx, sqlc.CreateAgentAuthChallengeParams{
		Challenge: challenge,
		AgentID:   agentID,
		ExpiresAt: expiresAt,
	})
}

func (s *SQLAgentAuthStore) ConsumeChallenge(ctx context.Context, challenge []byte, now time.Time) (int64, error) {
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

func (s *SQLAgentAuthStore) SweepExpiredChallenges(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SweepExpiredAgentAuthChallenges(ctx, now)
}

func (s *SQLAgentAuthStore) DeleteChallengesForAgent(ctx context.Context, agentID int64) (int64, error) {
	return s.q(ctx).DeleteAgentAuthChallengesByAgentID(ctx, agentID)
}

func (s *SQLAgentAuthStore) CreateSession(ctx context.Context, tokenHash string, agentID int64, expiresAt time.Time) error {
	return s.q(ctx).CreateAgentSession(ctx, sqlc.CreateAgentSessionParams{
		TokenHash: tokenHash,
		AgentID:   agentID,
		ExpiresAt: expiresAt,
	})
}

func (s *SQLAgentAuthStore) GetSessionAgent(ctx context.Context, tokenHash string, now time.Time) (*agentauth.ResolvedAgent, error) {
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
	return &agentauth.ResolvedAgent{
		AgentID:        row.AgentID,
		OrgID:          row.OrgID,
		Name:           row.Name,
		IdentityPubkey: row.IdentityPubkey,
	}, nil
}

func (s *SQLAgentAuthStore) SweepExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SweepExpiredAgentSessions(ctx, now)
}
