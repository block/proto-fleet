// Package agentauth implements the agent-side handshake state machine and
// session-token resolution used by AgentAuthInterceptor.
//
// Flow:
//
//	BeginHandshake(api_key, identity_pubkey)   -> challenge (one-shot, ~30s TTL)
//	CompleteHandshake(challenge, signature)    -> session_token (~24h TTL)
//	ResolveSession(session_token)              -> Agent (used by interceptor)
package agentauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/agentenrollment"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	challengeBytes      = 32
	sessionTokenBytes   = 32
	defaultChallengeTTL = 30 * time.Second
	defaultSessionTTL   = 24 * time.Hour

	clientErrBeginHandshake    = "agent authentication failed"
	clientErrCompleteHandshake = "agent authentication failed"
	clientErrResolveSession    = "agent authentication failed"
)

type Store interface {
	CreateChallenge(ctx context.Context, challenge []byte, agentID int64, expiresAt time.Time) error
	ConsumeChallenge(ctx context.Context, challenge []byte, now time.Time) (agentID int64, err error)
	SweepExpiredChallenges(ctx context.Context, now time.Time) (int64, error)

	CreateSession(ctx context.Context, tokenHash string, agentID int64, expiresAt time.Time) error
	GetSessionAgent(ctx context.Context, tokenHash string, now time.Time) (*ResolvedAgent, error)
	SweepExpiredSessions(ctx context.Context, now time.Time) (int64, error)
}

// ResolvedAgent is the join of an agent_session and its agent row, returned by
// GetSessionAgent for use by AgentAuthInterceptor.
type ResolvedAgent struct {
	AgentID        int64
	OrgID          int64
	Name           string
	IdentityPubkey []byte
}

type Service struct {
	store           Store
	enrollmentStore agentenrollment.Store
	apiKeySvc       *apikey.Service
	challengeTTL    time.Duration
	sessionTTL      time.Duration
}

func NewService(store Store, enrollmentStore agentenrollment.Store, apiKeySvc *apikey.Service) *Service {
	return &Service{
		store:           store,
		enrollmentStore: enrollmentStore,
		apiKeySvc:       apiKeySvc,
		challengeTTL:    defaultChallengeTTL,
		sessionTTL:      defaultSessionTTL,
	}
}

// BeginHandshake validates the agent's api_key and returns a fresh challenge
// nonce that must be signed with the agent's identity key. Verifies the
// supplied identity_pubkey matches the one stored at enrollment to prevent a
// leaked api_key from being used with a different keypair.
func (s *Service) BeginHandshake(ctx context.Context, apiKeyPlaintext string, identityPubkey []byte) ([]byte, time.Time, error) {
	apiKey, err := s.apiKeySvc.Validate(ctx, apiKeyPlaintext)
	if err != nil {
		return nil, time.Time{}, err
	}
	if apiKey.SubjectKind != interfaces.ApiKeySubjectKindAgent || apiKey.AgentID == nil {
		return nil, time.Time{}, fleeterror.NewUnauthenticatedError("invalid api key")
	}
	agent, err := s.enrollmentStore.GetAgentByID(ctx, *apiKey.AgentID, apiKey.OrganizationID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, time.Time{}, fleeterror.NewUnauthenticatedError("invalid api key")
		}
		return nil, time.Time{}, logInternal("agent lookup", clientErrBeginHandshake, err)
	}
	if agent.EnrollmentStatus != "CONFIRMED" {
		return nil, time.Time{}, fleeterror.NewFailedPreconditionError("agent enrollment not confirmed")
	}
	if !equalBytes(agent.IdentityPubkey, identityPubkey) {
		return nil, time.Time{}, fleeterror.NewUnauthenticatedError("identity_pubkey mismatch")
	}

	challenge := make([]byte, challengeBytes)
	if _, err := rand.Read(challenge); err != nil {
		return nil, time.Time{}, logInternal("generate challenge", clientErrBeginHandshake, err)
	}
	expiresAt := time.Now().UTC().Add(s.challengeTTL)
	if err := s.store.CreateChallenge(ctx, challenge, agent.ID, expiresAt); err != nil {
		return nil, time.Time{}, logInternal("store challenge", clientErrBeginHandshake, err)
	}
	s.apiKeySvc.RecordSuccessfulUse(ctx, apiKey)
	return challenge, expiresAt, nil
}

// CompleteHandshake atomically consumes the challenge (DELETE ... RETURNING),
// verifies the signature against the bound agent's identity key, and issues a
// short-lived session token. Replaying a consumed challenge returns
// Unauthenticated.
func (s *Service) CompleteHandshake(ctx context.Context, challenge, signature []byte) (string, time.Time, error) {
	now := time.Now().UTC()
	agentID, err := s.store.ConsumeChallenge(ctx, challenge, now)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return "", time.Time{}, fleeterror.NewUnauthenticatedError("challenge expired or not found")
		}
		return "", time.Time{}, logInternal("consume challenge", clientErrCompleteHandshake, err)
	}

	agent, err := s.enrollmentStore.GetAgentByIDUnscoped(ctx, agentID)
	if err != nil {
		return "", time.Time{}, logInternal("agent lookup", clientErrCompleteHandshake, err)
	}
	if !ed25519.Verify(agent.IdentityPubkey, challenge, signature) {
		return "", time.Time{}, fleeterror.NewUnauthenticatedError("signature verification failed")
	}

	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, logInternal("generate session token", clientErrCompleteHandshake, err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expiresAt := now.Add(s.sessionTTL)
	if err := s.store.CreateSession(ctx, hashToken(plaintext), agentID, expiresAt); err != nil {
		return "", time.Time{}, logInternal("store session", clientErrCompleteHandshake, err)
	}
	return plaintext, expiresAt, nil
}

// ResolveSession looks up the agent bound to a session token. Used by
// AgentAuthInterceptor to populate request context.
func (s *Service) ResolveSession(ctx context.Context, sessionTokenPlaintext string) (*ResolvedAgent, error) {
	row, err := s.store.GetSessionAgent(ctx, hashToken(sessionTokenPlaintext), time.Now().UTC())
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, fleeterror.NewUnauthenticatedError("invalid session token")
		}
		return nil, logInternal("session lookup", clientErrResolveSession, err)
	}
	return row, nil
}

// logInternal records the raw error server-side and returns a generic
// client-safe internal error so backend details don't leak over the wire.
func logInternal(op, clientMsg string, err error) error {
	if err == nil {
		return fleeterror.NewInternalError(clientMsg)
	}
	slog.Error("agent auth internal error", "op", op, "error", err)
	return fleeterror.NewInternalError(clientMsg)
}

// SweepExpired drops expired challenges and sessions.
func (s *Service) SweepExpired(ctx context.Context) (challenges int64, sessions int64, err error) {
	now := time.Now().UTC()
	challenges, err = s.store.SweepExpiredChallenges(ctx, now)
	if err != nil {
		return 0, 0, err
	}
	sessions, err = s.store.SweepExpiredSessions(ctx, now)
	return challenges, sessions, err
}

func hashToken(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ErrChallengeNotFound is sentinel for challenge consumption failures.
var ErrChallengeNotFound = errors.New("challenge not found")
