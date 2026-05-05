// Package agentenrollment manages the operator-issued enrollment-code lifecycle.
//
// Codes are 32 random bytes shown to the operator once at creation; only the
// SHA-256 hash is persisted. State machine: PENDING (created) -> AWAITING_CONFIRMATION
// (agent registered, fingerprint visible to operator) -> CONFIRMED (operator
// confirmed; api_key issued). EXPIRED and CANCELLED are terminal failure states.
package agentenrollment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	codeRandomBytes  = 32
	defaultCodeTTL   = 1 * time.Hour
	agentApiKeyLabel = "Agent enrollment"

	clientErrCreateCode    = "failed to create enrollment code"
	clientErrResolveCode   = "enrollment lookup failed"
	clientErrRegisterAgent = "agent registration failed"
	clientErrConfirmAgent  = "agent confirmation failed"
	clientErrCancel        = "enrollment cancellation failed"
	clientErrListAgents    = "failed to list agents"
)

type PendingEnrollmentStore interface {
	CreatePendingEnrollment(ctx context.Context, codeHash string, orgID, createdBy int64, expiresAt time.Time) (*PendingEnrollment, error)
	GetPendingEnrollmentByCodeHash(ctx context.Context, codeHash string) (*PendingEnrollment, error)
	GetPendingEnrollmentByAgent(ctx context.Context, agentID, orgID int64) (*PendingEnrollment, error)
	BindEnrollmentToAgent(ctx context.Context, enrollmentID, agentID int64) (int64, error)
	ConfirmEnrollment(ctx context.Context, enrollmentID int64, consumedAt time.Time) (int64, error)
	CancelPendingEnrollment(ctx context.Context, enrollmentID, orgID int64, consumedAt time.Time) (int64, error)
	SweepExpiredEnrollments(ctx context.Context, now time.Time) (int64, error)
}

type AgentStore interface {
	CreateAgent(ctx context.Context, orgID int64, name string, identityPubkey, minerSigningPubkey []byte) (*Agent, error)
	GetAgentByID(ctx context.Context, agentID, orgID int64) (*Agent, error)
	GetAgentByIDUnscoped(ctx context.Context, agentID int64) (*Agent, error)
	ListAgentsForOrganization(ctx context.Context, orgID int64) ([]Agent, error)
	SetAgentEnrollmentStatus(ctx context.Context, status string, agentID, orgID int64) (int64, error)
}

// Store is the union of pending-enrollment and agent-row operations the
// service needs. Implementations typically satisfy both via one SQL store.
type Store interface {
	PendingEnrollmentStore
	AgentStore
}

type Service struct {
	store      Store
	apiKeySvc  *apikey.Service
	transactor stores.Transactor
}

func NewService(store Store, apiKeySvc *apikey.Service, transactor stores.Transactor) *Service {
	return &Service{store: store, apiKeySvc: apiKeySvc, transactor: transactor}
}

// CreateCode generates a new enrollment code and stores its hash. The plaintext
// is returned exactly once; the operator hands it to the agent operator
// out-of-band.
func (s *Service) CreateCode(ctx context.Context, userID, orgID int64, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 {
		ttl = defaultCodeTTL
	}
	codeBytes := make([]byte, codeRandomBytes)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", time.Time{}, logInternal("generate enrollment code", clientErrCreateCode, err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(codeBytes)
	expiresAt := time.Now().UTC().Add(ttl)
	if _, err := s.store.CreatePendingEnrollment(ctx, hashCode(plaintext), orgID, userID, expiresAt); err != nil {
		return "", time.Time{}, logInternal("create pending enrollment", clientErrCreateCode, err)
	}
	return plaintext, expiresAt, nil
}

// resolveCode validates an incoming plaintext code from a Register call.
// Returns the matching pending enrollment if PENDING and unexpired; otherwise
// a fixed Unauthenticated error.
func (s *Service) resolveCode(ctx context.Context, plaintextCode string) (*PendingEnrollment, error) {
	row, err := s.store.GetPendingEnrollmentByCodeHash(ctx, hashCode(plaintextCode))
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, fleeterror.NewUnauthenticatedError("invalid enrollment code")
		}
		return nil, logInternal("resolve enrollment code", clientErrResolveCode, err)
	}
	if row.Status != StatusPending {
		return nil, fleeterror.NewUnauthenticatedError("invalid enrollment code")
	}
	if !row.ExpiresAt.After(time.Now().UTC()) {
		return nil, fleeterror.NewUnauthenticatedError("invalid enrollment code")
	}
	return row, nil
}

// RegisterAgent atomically creates the agent row and binds the pending
// enrollment to it. The pending enrollment must be in PENDING state.
// Wrapped in a transaction so a partial failure cannot leave an orphan agent
// row behind a still-PENDING code.
func (s *Service) RegisterAgent(ctx context.Context, plaintextCode, name string, identityPubkey, minerSigningPubkey []byte) (*Agent, *PendingEnrollment, error) {
	var (
		agent *Agent
		pe    *PendingEnrollment
	)
	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		var err error
		pe, err = s.resolveCode(ctx, plaintextCode)
		if err != nil {
			return err
		}
		agent, err = s.store.CreateAgent(ctx, pe.OrgID, name, identityPubkey, minerSigningPubkey)
		if err != nil {
			return logInternal("create agent", clientErrRegisterAgent, err)
		}
		bound, err := s.store.BindEnrollmentToAgent(ctx, pe.ID, agent.ID)
		if err != nil {
			return logInternal("bind enrollment", clientErrRegisterAgent, err)
		}
		if bound == 0 {
			return fleeterror.NewFailedPreconditionError("enrollment code already consumed")
		}
		pe.Status = StatusAwaitingConfirmation
		pe.AgentID = &agent.ID
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return agent, pe, nil
}

// Confirm flips an AWAITING_CONFIRMATION enrollment to CONFIRMED, marks the
// agent CONFIRMED, and issues an api_key for the agent. Returns the plaintext
// api_key (one-time view, never re-fetchable). Rejects expired rows directly
// rather than relying on the sweeper to flip them.
func (s *Service) Confirm(ctx context.Context, agentID, orgID int64) (string, time.Time, error) {
	var (
		plaintext string
		expires   time.Time
	)
	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		pe, err := s.store.GetPendingEnrollmentByAgent(ctx, agentID, orgID)
		if err != nil {
			if fleeterror.IsNotFoundError(err) {
				return fleeterror.NewNotFoundError("no pending enrollment for agent")
			}
			return logInternal("lookup pending enrollment", clientErrConfirmAgent, err)
		}
		if pe.Status != StatusAwaitingConfirmation {
			return fleeterror.NewFailedPreconditionErrorf("enrollment in status %s; cannot confirm", pe.Status)
		}
		if !pe.ExpiresAt.After(time.Now().UTC()) {
			return fleeterror.NewFailedPreconditionError("enrollment expired")
		}
		now := time.Now().UTC()
		rows, err := s.store.ConfirmEnrollment(ctx, pe.ID, now)
		if err != nil {
			return logInternal("confirm enrollment", clientErrConfirmAgent, err)
		}
		if rows == 0 {
			return fleeterror.NewFailedPreconditionError("enrollment state changed; refresh and retry")
		}
		if _, err := s.store.SetAgentEnrollmentStatus(ctx, "CONFIRMED", agentID, orgID); err != nil {
			return logInternal("update agent status", clientErrConfirmAgent, err)
		}
		key, apiKey, err := s.apiKeySvc.CreateAgent(ctx, agentID, orgID, agentApiKeyLabel, nil)
		if err != nil {
			return err
		}
		plaintext = key
		if apiKey.ExpiresAt != nil {
			expires = *apiKey.ExpiresAt
		}
		return nil
	}); err != nil {
		return "", time.Time{}, err
	}
	return plaintext, expires, nil
}

// Cancel marks a PENDING enrollment as cancelled. Returns NotFound if the
// enrollment doesn't exist, doesn't belong to the org, or is past PENDING.
func (s *Service) Cancel(ctx context.Context, enrollmentID, orgID int64) error {
	rows, err := s.store.CancelPendingEnrollment(ctx, enrollmentID, orgID, time.Now().UTC())
	if err != nil {
		return logInternal("cancel enrollment", clientErrCancel, err)
	}
	if rows == 0 {
		return fleeterror.NewNotFoundError("enrollment not cancellable")
	}
	return nil
}

// SweepExpired flips PENDING/AWAITING_CONFIRMATION rows past their TTL to
// EXPIRED. Should run periodically alongside session cleanup.
func (s *Service) SweepExpired(ctx context.Context) (int64, error) {
	return s.store.SweepExpiredEnrollments(ctx, time.Now().UTC())
}

// ListAgents returns all non-deleted agents for an organization.
func (s *Service) ListAgents(ctx context.Context, orgID int64) ([]Agent, error) {
	agents, err := s.store.ListAgentsForOrganization(ctx, orgID)
	if err != nil {
		return nil, logInternal("list agents", clientErrListAgents, err)
	}
	return agents, nil
}

// IdentityFingerprint produces the operator-visible fingerprint for an
// identity public key. Hex-encoded, 16 chars (8 bytes of SHA-256).
func IdentityFingerprint(identityPubkey []byte) string {
	h := sha256.Sum256(identityPubkey)
	return hex.EncodeToString(h[:8])
}

func hashCode(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// logInternal records the raw error server-side and returns a generic
// client-safe internal error so backend details don't leak over the wire.
func logInternal(op, clientMsg string, err error) error {
	if err == nil {
		return fleeterror.NewInternalError(clientMsg)
	}
	slog.Error("agent enrollment internal error", "op", op, "error", err)
	return fleeterror.NewInternalError(clientMsg)
}

// ErrInvalidStatus is sentinel for state-machine violations callers may want
// to distinguish from generic NotFound.
var ErrInvalidStatus = errors.New("invalid pending_enrollment status for operation")
