package agentauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/agentenrollment"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/cryptohash"
)

const (
	challengeBytes      = 32
	sessionTokenBytes   = 32
	defaultChallengeTTL = 30 * time.Second
	defaultSessionTTL   = 24 * time.Hour

	clientErrAuth = "agent authentication failed"
	component     = "agent auth"
)

type Store interface {
	CreateChallenge(ctx context.Context, challenge []byte, agentID int64, expiresAt time.Time) error
	ConsumeChallenge(ctx context.Context, challenge []byte, now time.Time) (agentID int64, err error)
	SweepExpiredChallenges(ctx context.Context, now time.Time) (int64, error)

	CreateSession(ctx context.Context, tokenHash string, agentID int64, expiresAt time.Time) error
	GetSessionAgent(ctx context.Context, tokenHash string, now time.Time) (*ResolvedAgent, error)
	SweepExpiredSessions(ctx context.Context, now time.Time) (int64, error)
}

// ResolvedAgent is the join of an agent_session and its agent row.
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

func (s *Service) BeginHandshake(ctx context.Context, apiKeyPlaintext string, identityPubkey []byte) ([]byte, time.Time, error) {
	apiKey, err := s.apiKeySvc.Validate(ctx, apiKeyPlaintext)
	if err != nil {
		return nil, time.Time{}, err
	}
	if !apiKey.IsAgentKey() {
		return nil, time.Time{}, fleeterror.NewUnauthenticatedError("invalid api key")
	}
	agent, err := s.enrollmentStore.GetAgentByID(ctx, *apiKey.AgentID, apiKey.OrganizationID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, time.Time{}, fleeterror.NewUnauthenticatedError("invalid api key")
		}
		return nil, time.Time{}, logInternal("agent lookup", clientErrAuth, err)
	}
	if agent.EnrollmentStatus != agentenrollment.AgentStatusConfirmed {
		return nil, time.Time{}, fleeterror.NewFailedPreconditionError("agent enrollment not confirmed")
	}
	// Constant-time compare on the supplied vs enrolled identity pubkey: the
	// supplied bytes come straight off the wire and a timing side-channel here
	// would let a leaked api_key be probed against multiple candidate keys.
	if subtle.ConstantTimeCompare(agent.IdentityPubkey, identityPubkey) != 1 {
		return nil, time.Time{}, fleeterror.NewUnauthenticatedError("identity_pubkey mismatch")
	}

	challenge := make([]byte, challengeBytes)
	if _, err := rand.Read(challenge); err != nil {
		return nil, time.Time{}, logInternal("generate challenge", clientErrAuth, err)
	}
	expiresAt := time.Now().UTC().Add(s.challengeTTL)
	if err := s.store.CreateChallenge(ctx, challenge, agent.ID, expiresAt); err != nil {
		return nil, time.Time{}, logInternal("store challenge", clientErrAuth, err)
	}
	s.apiKeySvc.RecordSuccessfulUse(ctx, apiKey)
	return challenge, expiresAt, nil
}

func (s *Service) CompleteHandshake(ctx context.Context, challenge, signature []byte) (string, time.Time, error) {
	now := time.Now().UTC()
	// ConsumeChallenge is DELETE ... RETURNING; a replayed challenge finds
	// nothing and returns NotFound.
	agentID, err := s.store.ConsumeChallenge(ctx, challenge, now)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return "", time.Time{}, fleeterror.NewUnauthenticatedError("challenge expired or not found")
		}
		return "", time.Time{}, logInternal("consume challenge", clientErrAuth, err)
	}

	agent, err := s.enrollmentStore.GetAgentByIDUnscoped(ctx, agentID)
	if err != nil {
		return "", time.Time{}, logInternal("agent lookup", clientErrAuth, err)
	}
	if !ed25519.Verify(agent.IdentityPubkey, challenge, signature) {
		return "", time.Time{}, fleeterror.NewUnauthenticatedError("signature verification failed")
	}

	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, logInternal("generate session token", clientErrAuth, err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expiresAt := now.Add(s.sessionTTL)
	if err := s.store.CreateSession(ctx, hashToken(plaintext), agentID, expiresAt); err != nil {
		return "", time.Time{}, logInternal("store session", clientErrAuth, err)
	}
	return plaintext, expiresAt, nil
}

func (s *Service) ResolveSession(ctx context.Context, sessionTokenPlaintext string) (*ResolvedAgent, error) {
	row, err := s.store.GetSessionAgent(ctx, hashToken(sessionTokenPlaintext), time.Now().UTC())
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, fleeterror.NewUnauthenticatedError("invalid session token")
		}
		return nil, logInternal("session lookup", clientErrAuth, err)
	}
	return row, nil
}

func (s *Service) SweepExpired(ctx context.Context) (challenges int64, sessions int64, err error) {
	now := time.Now().UTC()
	challenges, err = s.store.SweepExpiredChallenges(ctx, now)
	if err != nil {
		return 0, 0, err
	}
	sessions, err = s.store.SweepExpiredSessions(ctx, now)
	return challenges, sessions, err
}

func logInternal(op, clientMsg string, err error) error {
	return fleeterror.LogInternal(component, op, clientMsg, err)
}

func hashToken(plaintext string) string {
	return cryptohash.Sha256Hex(plaintext)
}
