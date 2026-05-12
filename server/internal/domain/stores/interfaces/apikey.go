package interfaces

import (
	"context"
	"time"
)

type ApiKeySubjectKind string

const (
	ApiKeySubjectKindUser      ApiKeySubjectKind = "user"
	ApiKeySubjectKindFleetNode ApiKeySubjectKind = "fleet_node"
)

// ApiKey is the persisted form of an API key. Exactly one of UserID or
// FleetNodeID is populated, matching SubjectKind; consumers should branch via
// AsUser / AsFleetNode rather than nil-checking the pointer fields.
type ApiKey struct {
	ID                int64
	KeyID             string
	Name              string
	Prefix            string
	KeyHash           string
	SubjectKind       ApiKeySubjectKind
	UserID            *int64
	FleetNodeID       *int64
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

func (k *ApiKey) AsFleetNode() (int64, bool) {
	if k.SubjectKind == ApiKeySubjectKindFleetNode && k.FleetNodeID != nil {
		return *k.FleetNodeID, true
	}
	return 0, false
}

// ApiKeyStore handles API key persistence operations.
type ApiKeyStore interface {
	CreateApiKey(ctx context.Context, key *ApiKey) error
	CreateFleetNodeApiKey(ctx context.Context, key *ApiKey) error
	GetApiKeyByHash(ctx context.Context, keyHash string) (*ApiKey, error)
	ListApiKeysByOrganization(ctx context.Context, orgID int64) ([]ApiKey, error)
	RevokeApiKey(ctx context.Context, keyID string, orgID int64, revokedAt time.Time) (int64, error)
	// RevokeApiKeysByFleetNodeID returns the key_ids that were revoked so
	// callers can evict them from in-memory caches.
	RevokeApiKeysByFleetNodeID(ctx context.Context, fleetNodeID, orgID int64, revokedAt time.Time) ([]string, error)
	UpdateApiKeyLastUsed(ctx context.Context, id int64, lastUsedAt time.Time) error
}
