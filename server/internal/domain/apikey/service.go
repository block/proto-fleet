package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	id "github.com/block/proto-fleet/server/internal/infrastructure/id"
)

const (
	prefixRandomBytes      = 4  // 4 bytes -> 8 hex chars
	secretRandomBytes      = 32 // 32 bytes -> 256-bit entropy
	keyPrefix              = "fleet"
	maxCreateRetries       = 3
	lastUsedUpdateInterval = 1 * time.Minute // debounce DB writes for last_used_at

	createAPIKeyClientError = "failed to create API key"
	listAPIKeysClientError  = "failed to list API keys"
	revokeAPIKeyClientError = "failed to revoke API key"
	//nolint:gosec // G101: client-facing error string, not a credential
	validateAPIKeyClientError = "API key service unavailable"
)

// Service provides API key management operations.
type Service struct {
	store       interfaces.ApiKeyStore
	activitySvc *activity.Service
	// lastUsedCache debounces last_used_at DB writes. Key: API key public ID string (keyID), Value: time.Time of last DB write.
	lastUsedCache sync.Map
}

// NewService creates a new API key service.
func NewService(store interfaces.ApiKeyStore, activitySvc *activity.Service) *Service {
	return &Service{store: store, activitySvc: activitySvc}
}

// Create generates a new API key and stores its hash. The full key is returned
// exactly once and cannot be retrieved again. Retries on prefix collision up to
// maxCreateRetries times.
func (s *Service) Create(ctx context.Context, userID, orgID int64, externalUserID, username, name string, expiresAt *time.Time) (string, *interfaces.ApiKey, error) {
	if expiresAt != nil && !expiresAt.After(time.Now().UTC()) {
		return "", nil, fleeterror.NewInvalidArgumentError("expiration date must be in the future")
	}

	var lastErr error
	for attempt := range maxCreateRetries {
		fullKey, apiKey, err := s.tryCreate(ctx, userID, orgID, name, expiresAt)
		if err == nil {
			apiKey.CreatedByUsername = username
			s.logActivity(ctx, activitymodels.Event{
				Category:       activitymodels.CategoryAuth,
				Type:           "create_api_key",
				Description:    fmt.Sprintf("Created API key '%s'", name),
				UserID:         &externalUserID,
				Username:       &username,
				OrganizationID: &orgID,
			})
			return fullKey, apiKey, nil
		}
		if !db.IsUniqueViolationError(err) {
			return "", nil, logInternalError("api key creation failed", createAPIKeyClientError, err)
		}
		lastErr = err
		slog.Debug("api key prefix collision, retrying", "attempt", attempt+1)
	}
	return "", nil, logInternalError(
		"api key creation failed after retries",
		createAPIKeyClientError,
		lastErr,
		"attempts", maxCreateRetries,
	)
}

func (s *Service) tryCreate(ctx context.Context, userID, orgID int64, name string, expiresAt *time.Time) (string, *interfaces.ApiKey, error) {
	prefixBytes := make([]byte, prefixRandomBytes)
	if _, err := rand.Read(prefixBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate api key prefix: %w", err)
	}
	prefix := hex.EncodeToString(prefixBytes)

	secretBytes := make([]byte, secretRandomBytes)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate api key secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)

	fullKey := fmt.Sprintf("%s_%s_%s", keyPrefix, prefix, secret)
	keyHash := hashKey(fullKey)

	now := time.Now().UTC()
	apiKey := &interfaces.ApiKey{
		KeyID:          id.GenerateID(),
		Name:           name,
		Prefix:         prefix,
		KeyHash:        keyHash,
		UserID:         userID,
		OrganizationID: orgID,
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
	}

	if err := s.store.CreateApiKey(ctx, apiKey); err != nil {
		return "", nil, err
	}

	return fullKey, apiKey, nil
}

// List returns all non-revoked API keys for the organization.
func (s *Service) List(ctx context.Context, orgID int64) ([]interfaces.ApiKey, error) {
	keys, err := s.store.ListApiKeysByOrganization(ctx, orgID)
	if err != nil {
		return nil, logInternalError("failed to list api keys", listAPIKeysClientError, err, "org_id", orgID)
	}

	return keys, nil
}

// Revoke permanently revokes an API key. Returns NotFound if the key does not
// exist, is already revoked, or belongs to a different organization.
func (s *Service) Revoke(ctx context.Context, keyID string, orgID int64, externalUserID, username string) error {
	rowsAffected, err := s.store.RevokeApiKey(ctx, keyID, orgID, time.Now().UTC())
	if err != nil {
		return logInternalError("failed to revoke api key", revokeAPIKeyClientError, err, "key_id", keyID, "org_id", orgID)
	}
	if rowsAffected == 0 {
		return fleeterror.NewNotFoundErrorf("api key '%s' not found or already revoked", keyID)
	}

	// Evict from debounce cache so revoked keys don't accumulate
	s.lastUsedCache.Delete(keyID)

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "revoke_api_key",
		Description:    fmt.Sprintf("Revoked API key '%s'", keyID),
		UserID:         &externalUserID,
		Username:       &username,
		OrganizationID: &orgID,
	})

	return nil
}

func (s *Service) logActivity(ctx context.Context, event activitymodels.Event) {
	if s.activitySvc == nil {
		return
	}

	s.activitySvc.Log(ctx, event)
}

// Validate checks a raw API key and returns the stored key record if valid.
// Returns a generic unauthenticated error for any rejection reason (not found,
// revoked, expired) to avoid leaking key validity to callers.
// Revoked keys are already excluded at the SQL layer (WHERE revoked_at IS NULL).
func (s *Service) Validate(ctx context.Context, rawKey string) (*interfaces.ApiKey, error) {
	if !strings.HasPrefix(rawKey, keyPrefix+"_") {
		return nil, fleeterror.NewUnauthenticatedError("invalid api key")
	}

	keyHash := hashKey(rawKey)
	apiKey, err := s.store.GetApiKeyByHash(ctx, keyHash)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, fleeterror.NewUnauthenticatedError("invalid api key")
		}
		// Transient store failure — propagate as internal error rather than
		// masking it as an authentication rejection.
		return nil, logInternalError("api key validation failed", validateAPIKeyClientError, err)
	}

	if apiKey.ExpiresAt != nil && time.Now().UTC().After(*apiKey.ExpiresAt) {
		slog.Debug("api key rejected: expired", "key_id", apiKey.KeyID)
		return nil, fleeterror.NewUnauthenticatedError("invalid api key")
	}

	return apiKey, nil
}

// RecordSuccessfulUse updates last_used_at after the full API key authentication
// flow has succeeded, including user and role lookups.
func (s *Service) RecordSuccessfulUse(ctx context.Context, apiKey *interfaces.ApiKey) {
	if apiKey == nil {
		return
	}

	// Update last_used_at inline but debounced: skip the DB write if we already
	// wrote for this key within lastUsedUpdateInterval. This avoids spawning an
	// unbounded goroutine per request while keeping the write off the hot path
	// for frequently-used keys.
	s.updateLastUsedDebounced(ctx, apiKey.KeyID, apiKey.ID)
}

func (s *Service) updateLastUsedDebounced(ctx context.Context, keyID string, dbID int64) {
	now := time.Now().UTC()
	if lastWrite, ok := s.lastUsedCache.Load(keyID); ok {
		if t, isTime := lastWrite.(time.Time); isTime && now.Sub(t) < lastUsedUpdateInterval {
			return
		}
	}
	if err := s.store.UpdateApiKeyLastUsed(ctx, dbID, now); err != nil {
		slog.Debug("failed to update api key last_used_at", "error", err)
		return
	}
	s.lastUsedCache.Store(keyID, now)
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func logInternalError(logMessage, clientMessage string, err error, attrs ...any) error {
	if err == nil {
		return fleeterror.NewInternalError(clientMessage)
	}

	attrs = append(attrs, "error", err)
	slog.Error(logMessage, attrs...)
	return fleeterror.NewInternalError(clientMessage)
}
