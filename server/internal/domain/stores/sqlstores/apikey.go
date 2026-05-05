package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.ApiKeyStore = &SQLApiKeyStore{}

// SQLApiKeyStore implements interfaces.ApiKeyStore using SQL database.
type SQLApiKeyStore struct {
	SQLConnectionManager
}

// NewSQLApiKeyStore creates a new SQL-backed API key store.
func NewSQLApiKeyStore(conn *sql.DB) *SQLApiKeyStore {
	return &SQLApiKeyStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLApiKeyStore) getQueries(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

func (s *SQLApiKeyStore) CreateApiKey(ctx context.Context, key *interfaces.ApiKey) error {
	if key.UserID == nil {
		return fleeterror.NewInvalidArgumentError("user api key requires user_id")
	}
	return s.getQueries(ctx).CreateApiKey(ctx, sqlc.CreateApiKeyParams{
		KeyID:          key.KeyID,
		Name:           key.Name,
		Prefix:         key.Prefix,
		KeyHash:        key.KeyHash,
		UserID:         sql.NullInt64{Int64: *key.UserID, Valid: true},
		OrganizationID: key.OrganizationID,
		CreatedAt:      key.CreatedAt,
		ExpiresAt:      timePtrToNullTime(key.ExpiresAt),
	})
}

func (s *SQLApiKeyStore) CreateAgentApiKey(ctx context.Context, key *interfaces.ApiKey) error {
	if key.AgentID == nil {
		return fleeterror.NewInvalidArgumentError("agent api key requires agent_id")
	}
	return s.getQueries(ctx).CreateAgentApiKey(ctx, sqlc.CreateAgentApiKeyParams{
		KeyID:          key.KeyID,
		Name:           key.Name,
		Prefix:         key.Prefix,
		KeyHash:        key.KeyHash,
		AgentID:        sql.NullInt64{Int64: *key.AgentID, Valid: true},
		OrganizationID: key.OrganizationID,
		CreatedAt:      key.CreatedAt,
		ExpiresAt:      timePtrToNullTime(key.ExpiresAt),
	})
}

func (s *SQLApiKeyStore) GetApiKeyByHash(ctx context.Context, keyHash string) (*interfaces.ApiKey, error) {
	row, err := s.getQueries(ctx).GetApiKeyByHash(ctx, keyHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("api key not found")
		}
		return nil, err
	}

	return &interfaces.ApiKey{
		ID:                row.ID,
		KeyID:             row.KeyID,
		Name:              row.Name,
		Prefix:            row.Prefix,
		KeyHash:           row.KeyHash,
		SubjectKind:       interfaces.ApiKeySubjectKind(row.SubjectKind),
		UserID:            nullInt64ToPtr(row.UserID),
		AgentID:           nullInt64ToPtr(row.AgentID),
		OrganizationID:    row.OrganizationID,
		CreatedAt:         row.CreatedAt,
		ExpiresAt:         nullTimeToPtr(row.ExpiresAt),
		RevokedAt:         nullTimeToPtr(row.RevokedAt),
		LastUsedAt:        nullTimeToPtr(row.LastUsedAt),
		CreatedByUsername: row.CreatedByUsername,
	}, nil
}

// ListApiKeysByOrganization returns non-revoked user-owned keys for the org.
// Agent-owned keys are intentionally excluded; agents are listed via the
// agentadmin service.
func (s *SQLApiKeyStore) ListApiKeysByOrganization(ctx context.Context, orgID int64) ([]interfaces.ApiKey, error) {
	rows, err := s.getQueries(ctx).ListApiKeysByOrganization(ctx, orgID)
	if err != nil {
		return nil, err
	}

	keys := make([]interfaces.ApiKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, interfaces.ApiKey{
			ID:                row.ID,
			KeyID:             row.KeyID,
			Name:              row.Name,
			Prefix:            row.Prefix,
			SubjectKind:       interfaces.ApiKeySubjectKindUser,
			UserID:            nullInt64ToPtr(row.UserID),
			OrganizationID:    row.OrganizationID,
			CreatedAt:         row.CreatedAt,
			ExpiresAt:         nullTimeToPtr(row.ExpiresAt),
			RevokedAt:         nullTimeToPtr(row.RevokedAt),
			LastUsedAt:        nullTimeToPtr(row.LastUsedAt),
			CreatedByUsername: row.CreatedByUsername,
		})
	}
	return keys, nil
}

func (s *SQLApiKeyStore) RevokeApiKey(ctx context.Context, keyID string, orgID int64, revokedAt time.Time) (int64, error) {
	return s.getQueries(ctx).RevokeApiKey(ctx, sqlc.RevokeApiKeyParams{
		RevokedAt:      sql.NullTime{Time: revokedAt, Valid: true},
		KeyID:          keyID,
		OrganizationID: orgID,
	})
}

func (s *SQLApiKeyStore) UpdateApiKeyLastUsed(ctx context.Context, id int64, lastUsedAt time.Time) error {
	return s.getQueries(ctx).UpdateApiKeyLastUsed(ctx, sqlc.UpdateApiKeyLastUsedParams{
		LastUsedAt: sql.NullTime{Time: lastUsedAt, Valid: true},
		ID:         id,
	})
}

func timePtrToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func nullInt64ToPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
