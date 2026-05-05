package interfaces

import (
	"context"
	"time"
)

// ApiKeySubjectKind names the principal type an api_key is bound to.
type ApiKeySubjectKind string

const (
	ApiKeySubjectKindUser  ApiKeySubjectKind = "user"
	ApiKeySubjectKindAgent ApiKeySubjectKind = "agent"
)

// ApiKey represents an API key stored in the database.
// Exactly one of UserID or AgentID is populated, matching SubjectKind.
type ApiKey struct {
	ID                int64
	KeyID             string
	Name              string
	Prefix            string
	KeyHash           string
	SubjectKind       ApiKeySubjectKind
	UserID            *int64
	AgentID           *int64
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
	CreateAgentApiKey(ctx context.Context, key *ApiKey) error
	GetApiKeyByHash(ctx context.Context, keyHash string) (*ApiKey, error)
	ListApiKeysByOrganization(ctx context.Context, orgID int64) ([]ApiKey, error)
	RevokeApiKey(ctx context.Context, keyID string, orgID int64, revokedAt time.Time) (int64, error)
	UpdateApiKeyLastUsed(ctx context.Context, id int64, lastUsedAt time.Time) error
}
