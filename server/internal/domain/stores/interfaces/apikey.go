package interfaces

import (
	"context"
	"time"
)

// ApiKey represents an API key stored in the database.
type ApiKey struct {
	ID                int64
	KeyID             string
	Name              string
	Prefix            string
	KeyHash           string
	UserID            int64
	OrganizationID    int64
	CreatedAt         time.Time
	ExpiresAt         *time.Time
	RevokedAt         *time.Time
	LastUsedAt        *time.Time
	CreatedByUsername string
}

// ApiKeyStore handles API key persistence operations.
type ApiKeyStore interface {
	CreateApiKey(ctx context.Context, key *ApiKey) error
	GetApiKeyByHash(ctx context.Context, keyHash string) (*ApiKey, error)
	ListApiKeysByOrganization(ctx context.Context, orgID int64) ([]ApiKey, error)
	RevokeApiKey(ctx context.Context, keyID string, orgID int64, revokedAt time.Time) (int64, error)
	UpdateApiKeyLastUsed(ctx context.Context, id int64, lastUsedAt time.Time) error
}
