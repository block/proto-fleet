package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/infrastructure/cryptohash"
)

const (
	challengeBytes      = 32
	sessionTokenBytes   = 32
	defaultChallengeTTL = 30 * time.Second
	defaultSessionTTL   = 24 * time.Hour

	clientErrAuth = "fleet node authentication failed"
	component     = "fleet node auth"
)

type Store interface {
	UpsertChallenge(ctx context.Context, challenge []byte, fleetNodeID int64, expiresAt time.Time) error
	ConsumeChallenge(ctx context.Context, challenge []byte, now time.Time) (fleetNodeID int64, err error)
	SweepExpiredChallenges(ctx context.Context, now time.Time) (int64, error)

	UpsertSession(ctx context.Context, tokenHash string, fleetNodeID int64, expiresAt time.Time) error
	GetSessionFleetNode(ctx context.Context, tokenHash string, now time.Time) (*ResolvedFleetNode, error)
	SweepExpiredSessions(ctx context.Context, now time.Time) (int64, error)
}

// ResolvedFleetNode is the join of a fleet_node_session and its fleet_node row.
type ResolvedFleetNode struct {
	FleetNodeID        int64
	OrgID              int64
	Name               string
	IdentityPubkey     []byte
	SessionExpiresAt   time.Time
	SessionFingerprint string
}

type OperationalMetrics interface {
	AuthOutcome(ctx context.Context, operation, result string)
}

type Service struct {
	metrics         OperationalMetrics
	store           Store
	enrollmentStore enrollment.Store
	apiKeySvc       *apikey.Service
	challengeTTL    time.Duration
	sessionTTL      time.Duration
}

func NewService(store Store, enrollmentStore enrollment.Store, apiKeySvc *apikey.Service) *Service {
	return &Service{
		store:           store,
		enrollmentStore: enrollmentStore,
		apiKeySvc:       apiKeySvc,
		challengeTTL:    defaultChallengeTTL,
		sessionTTL:      defaultSessionTTL,
	}
}

// WithOperationalMetrics must be called before the service is shared.
func (s *Service) WithOperationalMetrics(metrics OperationalMetrics) *Service {
	s.metrics = metrics
	return s
}

func (s *Service) observeAuth(ctx context.Context, operation string, err error, rejection ...string) {
	if s.metrics == nil {
		return
	}
	result := "success"
	if err != nil {
		code := connect.CodeOf(err)
		var fe fleeterror.FleetError
		if errors.As(err, &fe) {
			code = fe.GRPCCode
		}
		switch {
		case code == connect.CodeUnauthenticated || code == connect.CodePermissionDenied:
			switch operation {
			case "session":
				result = "session_rejected"
			case "complete_handshake":
				result = "challenge_rejected"
			default:
				result = "credential_rejected"
			}
			if len(rejection) > 0 && rejection[0] != "" {
				result = rejection[0]
			}
		case code == connect.CodeFailedPrecondition:
			result = "enrollment_required"
		case code == connect.CodeCanceled:
			result = "canceled"
		case code == connect.CodeDeadlineExceeded:
			result = "timeout"
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			result = "timeout"
		case errors.Is(ctx.Err(), context.Canceled):
			result = "canceled"
		default:
			result = "internal"
		}
	}
	s.metrics.AuthOutcome(ctx, operation, result)
}

func (s *Service) BeginHandshake(ctx context.Context, apiKeyPlaintext string, identityPubkey []byte) (_ []byte, _ time.Time, resultErr error) {
	defer func() { s.observeAuth(ctx, "begin_handshake", resultErr) }()
	apiKey, err := s.apiKeySvc.Validate(ctx, apiKeyPlaintext)
	if err != nil {
		return nil, time.Time{}, err
	}
	agentID, ok := apiKey.AsFleetNode()
	if !ok {
		return nil, time.Time{}, fleeterror.NewUnauthenticatedError("invalid api key")
	}
	agent, err := s.enrollmentStore.GetFleetNodeByID(ctx, agentID, apiKey.OrganizationID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, time.Time{}, fleeterror.NewUnauthenticatedError("invalid api key")
		}
		return nil, time.Time{}, logInternal("fleet node lookup", clientErrAuth, err)
	}
	if agent.EnrollmentStatus != enrollment.FleetNodeStatusConfirmed {
		return nil, time.Time{}, fleeterror.NewFailedPreconditionError("fleet node enrollment not confirmed")
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
	// UpsertChallenge atomically replaces any prior row for this fleet node (one
	// challenge per fleet node enforced by uq_fleet_node_auth_challenge_fleet_node_id), so
	// concurrent BeginHandshakes can't leave multiple valid challenges.
	if err := s.store.UpsertChallenge(ctx, challenge, agent.ID, expiresAt); err != nil {
		return nil, time.Time{}, logInternal("store challenge", clientErrAuth, err)
	}
	s.apiKeySvc.RecordSuccessfulUse(ctx, apiKey)
	return challenge, expiresAt, nil
}

func (s *Service) CompleteHandshake(ctx context.Context, challenge, signature []byte) (_ string, _ time.Time, _ int64, resultErr error) {
	rejection := ""
	defer func() { s.observeAuth(ctx, "complete_handshake", resultErr, rejection) }()
	now := time.Now().UTC()
	// ConsumeChallenge is DELETE ... RETURNING; a replayed challenge finds
	// nothing and returns NotFound.
	agentID, err := s.store.ConsumeChallenge(ctx, challenge, now)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return "", time.Time{}, 0, fleeterror.NewUnauthenticatedError("challenge expired or not found")
		}
		return "", time.Time{}, 0, logInternal("consume challenge", clientErrAuth, err)
	}

	agent, err := s.enrollmentStore.GetFleetNodeByIDUnscoped(ctx, agentID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return "", time.Time{}, 0, fleeterror.NewUnauthenticatedError("fleet node not found")
		}
		return "", time.Time{}, 0, logInternal("fleet node lookup", clientErrAuth, err)
	}
	if !ed25519.Verify(agent.IdentityPubkey, challenge, signature) {
		rejection = "credential_rejected"
		return "", time.Time{}, 0, fleeterror.NewUnauthenticatedError("signature verification failed")
	}

	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, 0, logInternal("generate session token", clientErrAuth, err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expiresAt := now.Add(s.sessionTTL)
	// UpsertSession atomically replaces any prior session for this fleet node
	// (one session per fleet node enforced by uq_fleet_node_session_fleet_node_id), so
	// re-authentication rotates the bearer token instead of accumulating.
	if err := s.store.UpsertSession(ctx, SessionFingerprint(plaintext), agentID, expiresAt); err != nil {
		return "", time.Time{}, 0, logInternal("store session", clientErrAuth, err)
	}
	return plaintext, expiresAt, agentID, nil
}

func (s *Service) ResolveSession(ctx context.Context, sessionTokenPlaintext string) (_ *ResolvedFleetNode, resultErr error) {
	defer func() { s.observeAuth(ctx, "session", resultErr) }()
	fingerprint := SessionFingerprint(sessionTokenPlaintext)
	row, err := s.store.GetSessionFleetNode(ctx, fingerprint, time.Now().UTC())
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, fleeterror.NewUnauthenticatedError("invalid session token")
		}
		return nil, logInternal("session lookup", clientErrAuth, err)
	}
	row.SessionFingerprint = fingerprint
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

// No transactor wraps these handshake flows, so a retryable PG error has no
// retry layer to consume it and would surface raw SQLSTATE through the RPC
// error mapper. Always sanitize.
func logInternal(op, clientMsg string, err error) error {
	return fleeterror.LogInternal(component, op, clientMsg, err)
}

// SessionFingerprint identifies a bearer session without retaining its secret.
func SessionFingerprint(plaintext string) string {
	return cryptohash.Sha256Hex(plaintext)
}
