package interfaces

import (
	"context"
	"time"
)

type ApiKeySubjectKind string

const (
	ApiKeySubjectKindUser  ApiKeySubjectKind = "user"
	ApiKeySubjectKindAgent ApiKeySubjectKind = "agent"
)

// ApiKey is the persisted form of an API key. Exactly one of UserID or
// AgentID is populated, matching SubjectKind; consumers should branch via
// AsUser / AsAgent rather than nil-checking the pointer fields.
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

// AsUser returns the user_id if this key is bound to a user, with ok=false
// otherwise.
func (k *ApiKey) AsUser() (int64, bool) {
	if k.SubjectKind == ApiKeySubjectKindUser && k.UserID != nil {
		return *k.UserID, true
	}
	return 0, false
}

func (k *ApiKey) AsAgent() (int64, bool) {
	if k.SubjectKind == ApiKeySubjectKindAgent && k.AgentID != nil {
		return *k.AgentID, true
	}
	return 0, false
}

// ApiKeyStore handles API key persistence operations.
type ApiKeyStore interface {
	CreateApiKey(ctx context.Context, key *ApiKey) error
	CreateAgentApiKey(ctx context.Context, key *ApiKey) error
	GetApiKeyByHash(ctx context.Context, keyHash string) (*ApiKey, error)
	ListApiKeysByOrganization(ctx context.Context, orgID int64) ([]ApiKey, error)
	RevokeApiKey(ctx context.Context, keyID string, orgID int64, revokedAt time.Time) (int64, error)
	RevokeApiKeysByAgentID(ctx context.Context, agentID, orgID int64, revokedAt time.Time) (int64, error)
	UpdateApiKeyLastUsed(ctx context.Context, id int64, lastUsedAt time.Time) error
}
