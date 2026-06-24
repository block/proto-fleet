package enrollment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/cryptohash"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const (
	codeRandomBytes      = 32
	defaultCodeTTL       = 1 * time.Hour
	fleetNodeApiKeyLabel = "FleetNode enrollment" //nolint:gosec // label, not a credential

	clientErrCreateCode        = "failed to create enrollment code"
	clientErrResolveCode       = "enrollment lookup failed"
	clientErrRegisterFleetNode = "fleet node registration failed"
	clientErrConfirmFleetNode  = "fleet node confirmation failed"
	clientErrCancel            = "enrollment cancellation failed"
	clientErrListFleetNodes    = "failed to list fleet nodes"
	clientErrGetFleetNode      = "failed to get fleet node"
	clientErrRevokeFleetNode   = "fleet node revocation failed"
	clientErrUpdateLastSeen    = "heartbeat update failed"

	component = "fleet node enrollment"
)

type PendingEnrollmentStore interface {
	CreatePendingEnrollment(ctx context.Context, codeHash string, orgID, createdBy int64, expiresAt time.Time) (*PendingEnrollment, error)
	GetPendingEnrollmentByCodeHash(ctx context.Context, codeHash string) (*PendingEnrollment, error)
	GetPendingEnrollmentByFleetNode(ctx context.Context, agentID, orgID int64) (*PendingEnrollment, error)
	BindEnrollmentToFleetNode(ctx context.Context, enrollmentID, agentID int64) (int64, error)
	ConfirmEnrollment(ctx context.Context, enrollmentID int64, consumedAt time.Time) (int64, error)
	CancelPendingEnrollment(ctx context.Context, enrollmentID, orgID int64, consumedAt time.Time) (int64, error)
	CancelEnrollmentForFleetNode(ctx context.Context, agentID, orgID int64, consumedAt time.Time) (int64, error)
	SweepExpiredEnrollments(ctx context.Context, now time.Time) (int64, error)
}

type AgentStore interface {
	CreateFleetNode(ctx context.Context, orgID int64, name string, identityPubkey []byte) (*FleetNode, error)
	GetFleetNodeByID(ctx context.Context, agentID, orgID int64) (*FleetNode, error)
	GetFleetNodeByIDUnscoped(ctx context.Context, agentID int64) (*FleetNode, error)
	// LockFleetNodeByID is GetFleetNodeByID with SELECT ... FOR UPDATE. Both Confirm
	// and RevokeFleetNode call this at the start of their TX so they take the
	// fleet_node-row lock in the same order; without it, Confirm's
	// pending_enrollment-then-fleet_node UPDATE order vs RevokeFleetNode's reverse
	// order races into deadlocks.
	LockFleetNodeByID(ctx context.Context, agentID, orgID int64) (*FleetNode, error)
	ListFleetNodesForOrganization(ctx context.Context, orgID int64) ([]FleetNodeListing, error)
	SetFleetNodeEnrollmentStatus(ctx context.Context, status FleetNodeStatus, agentID, orgID int64) (int64, error)
	SoftDeleteFleetNode(ctx context.Context, agentID, orgID int64, deletedAt time.Time) (int64, error)
	SoftDeleteFleetNodesForExpiredEnrollments(ctx context.Context, now time.Time) (int64, error)
	UpdateLastSeen(ctx context.Context, fleetNodeID, orgID int64, now time.Time) (int64, error)
}

type RevocationCleanupStore interface {
	ListDeviceIDsForFleetNode(ctx context.Context, fleetNodeID, orgID int64) ([]int64, error)
	DeleteMinerCredentialsForFleetNode(ctx context.Context, fleetNodeID, orgID int64) (int64, error)
	DeletePairingsForFleetNode(ctx context.Context, fleetNodeID, orgID int64) (int64, error)
}

type Store interface {
	PendingEnrollmentStore
	AgentStore
	RevocationCleanupStore
}

type Service struct {
	store       Store
	apiKeySvc   *apikey.Service
	transactor  stores.Transactor
	activitySvc *activity.Service

	invalidateMiner func(context.Context, int64)
}

func NewService(store Store, apiKeySvc *apikey.Service, transactor stores.Transactor, activitySvc *activity.Service) *Service {
	return &Service{store: store, apiKeySvc: apiKeySvc, transactor: transactor, activitySvc: activitySvc}
}

// WithMinerInvalidator wires miner-cache eviction for node revocation. Revoking
// deletes fleet_node_device rows and credentials, so any cached remote-node miner
// for those devices must be evicted before another command can reuse its descriptor.
func (s *Service) WithMinerInvalidator(invalidate func(context.Context, int64)) {
	s.invalidateMiner = invalidate
}

// CreateCode mints an enrollment code. Plaintext is returned exactly once;
// only the SHA-256 hash is persisted.
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
	s.logActivity(ctx, "create_enrollment_code", fmt.Sprintf("Created fleet node enrollment code (expires %s)", expiresAt.Format(time.RFC3339)))
	return plaintext, expiresAt, nil
}

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

// RegisterFleetNode runs in a transaction so a partial failure cannot leave an
// orphan fleet_node row behind a still-PENDING enrollment code.
func (s *Service) RegisterFleetNode(ctx context.Context, plaintextCode, name string, identityPubkey []byte) (*FleetNode, *PendingEnrollment, error) {
	var (
		agent *FleetNode
		pe    *PendingEnrollment
	)
	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		var err error
		pe, err = s.resolveCode(ctx, plaintextCode)
		if err != nil {
			return err
		}
		agent, err = s.store.CreateFleetNode(ctx, pe.OrgID, name, identityPubkey)
		if err != nil {
			// Concurrent Register calls with the same identity_pubkey or
			// (org_id, name) lose on the partial unique indexes; surface as
			// a precondition failure instead of a 500.
			if db.IsUniqueViolationError(err) {
				return fleeterror.NewFailedPreconditionError("fleet node identity or name already in use")
			}
			return logInternal("create fleet node", clientErrRegisterFleetNode, err)
		}
		bound, err := s.store.BindEnrollmentToFleetNode(ctx, pe.ID, agent.ID)
		if err != nil {
			return logInternal("bind enrollment", clientErrRegisterFleetNode, err)
		}
		if bound == 0 {
			return fleeterror.NewFailedPreconditionError("enrollment code already consumed")
		}
		pe.Status = StatusAwaitingConfirmation
		pe.FleetNodeID = &agent.ID
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return agent, pe, nil
}

// Confirm runs in a transaction: confirm enrollment, mark agent CONFIRMED,
// issue the api_key. The plaintext api_key is returned exactly once. Rejects
// expired rows directly so the sweeper can be slow without expanding the
// confirmable window.
func (s *Service) Confirm(ctx context.Context, agentID, orgID int64) (string, time.Time, error) {
	var (
		plaintext string
		expires   time.Time
		agentName string
	)
	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		// Lock the fleet node row first so Confirm and RevokeFleetNode always acquire
		// row locks in the same order (agent -> pending_enrollment).
		agent, err := s.store.LockFleetNodeByID(ctx, agentID, orgID)
		if err != nil {
			if fleeterror.IsNotFoundError(err) {
				return fleeterror.NewNotFoundError("fleet node not found")
			}
			return logInternal("fleet node lock", clientErrConfirmFleetNode, err)
		}
		if agent.EnrollmentStatus == FleetNodeStatusRevoked {
			return fleeterror.NewFailedPreconditionError("fleet node is revoked; cannot confirm")
		}
		pe, err := s.store.GetPendingEnrollmentByFleetNode(ctx, agentID, orgID)
		if err != nil {
			if fleeterror.IsNotFoundError(err) {
				return fleeterror.NewFailedPreconditionError("no enrollment awaiting confirmation for fleet node")
			}
			return logInternal("lookup pending enrollment", clientErrConfirmFleetNode, err)
		}
		if !pe.ExpiresAt.After(time.Now().UTC()) {
			return fleeterror.NewFailedPreconditionError("enrollment expired")
		}
		now := time.Now().UTC()
		rows, err := s.store.ConfirmEnrollment(ctx, pe.ID, now)
		if err != nil {
			return logInternal("confirm enrollment", clientErrConfirmFleetNode, err)
		}
		if rows == 0 {
			return fleeterror.NewFailedPreconditionError("enrollment state changed; refresh and retry")
		}
		// SetFleetNodeEnrollmentStatus filters by deleted_at IS NULL, so a
		// concurrent RevokeFleetNode that soft-deleted the fleet node between the
		// initial read above and this update will affect zero rows. Reject
		// instead of minting an api_key for a revoked agent.
		statusRows, err := s.store.SetFleetNodeEnrollmentStatus(ctx, FleetNodeStatusConfirmed, agentID, orgID)
		if err != nil {
			return logInternal("update fleet node status", clientErrConfirmFleetNode, err)
		}
		if statusRows == 0 {
			return fleeterror.NewFailedPreconditionError("fleet node state changed; refresh and retry")
		}
		key, apiKey, err := s.apiKeySvc.CreateFleetNode(ctx, agentID, orgID, fleetNodeApiKeyLabel, nil)
		if err != nil {
			return err
		}
		plaintext = key
		agentName = agent.Name
		if apiKey.ExpiresAt != nil {
			expires = *apiKey.ExpiresAt
		}
		return nil
	}); err != nil {
		return "", time.Time{}, err
	}
	s.logActivity(ctx, "confirm_fleet_node", fmt.Sprintf("Confirmed fleet node '%s' (id=%d)", agentName, agentID))
	return plaintext, expires, nil
}

// RevokeFleetNode locks an agent out and soft-deletes its row so the same
// identity_pubkey or org-local name can be re-enrolled. Marks
// enrollment_status REVOKED, cancels any AWAITING_CONFIRMATION
// pending_enrollment so the fleet node can't be resurrected by a later Confirm,
// revokes the fleet node's api_keys, and soft-deletes the fleet node. The
// agent_session join filter on enrollment_status causes any in-flight session
// to fail to resolve on the next call; challenge rows expire on their own
// 30s TTL.
func (s *Service) RevokeFleetNode(ctx context.Context, agentID, orgID int64) error {
	var agentName string
	var deviceIDs []int64
	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		// Lock-then-mutate the fleet node row first; matches Confirm's lock order
		// (agent -> pending_enrollment) so the two flows can't deadlock.
		agent, err := s.store.LockFleetNodeByID(ctx, agentID, orgID)
		if err != nil {
			if fleeterror.IsNotFoundError(err) {
				return fleeterror.NewNotFoundError("fleet node not found")
			}
			return logInternal("fleet node lock", clientErrRevokeFleetNode, err)
		}
		now := time.Now().UTC()
		if _, err := s.store.SetFleetNodeEnrollmentStatus(ctx, FleetNodeStatusRevoked, agentID, orgID); err != nil {
			return logInternal("set fleet node revoked", clientErrRevokeFleetNode, err)
		}
		if _, err := s.store.CancelEnrollmentForFleetNode(ctx, agentID, orgID, now); err != nil {
			return logInternal("cancel pending enrollment", clientErrRevokeFleetNode, err)
		}
		if _, err := s.apiKeySvc.RevokeForFleetNode(ctx, agentID, orgID); err != nil {
			return err
		}
		var listErr error
		deviceIDs, listErr = s.store.ListDeviceIDsForFleetNode(ctx, agentID, orgID)
		if listErr != nil {
			return logInternal("list fleet node device ids", clientErrRevokeFleetNode, listErr)
		}
		if _, err := s.store.DeleteMinerCredentialsForFleetNode(ctx, agentID, orgID); err != nil {
			return logInternal("delete miner credentials for fleet node", clientErrRevokeFleetNode, err)
		}
		if _, err := s.store.DeletePairingsForFleetNode(ctx, agentID, orgID); err != nil {
			return logInternal("delete pairings for fleet node", clientErrRevokeFleetNode, err)
		}
		if _, err := s.store.SoftDeleteFleetNode(ctx, agentID, orgID, now); err != nil {
			return logInternal("soft delete fleet node", clientErrRevokeFleetNode, err)
		}
		agentName = agent.Name
		return nil
	}); err != nil {
		return err
	}
	if s.invalidateMiner != nil {
		for _, deviceID := range deviceIDs {
			s.invalidateMiner(ctx, deviceID)
		}
	}
	s.logActivity(ctx, "revoke_fleet_node", fmt.Sprintf("Revoked fleet node '%s' (id=%d)", agentName, agentID))
	return nil
}

// UpdateLastSeen advances last_seen_at on the fleet_node row. 0 rows
// affected means the fleet_node was soft-deleted (or scoped to a different
// org) between the auth interceptor's session lookup and now; surface
// NotFound so the daemon's session resolver fails on its next refresh.
func (s *Service) UpdateLastSeen(ctx context.Context, fleetNodeID, orgID int64, now time.Time) error {
	rows, err := s.store.UpdateLastSeen(ctx, fleetNodeID, orgID, now)
	if err != nil {
		return logInternal("update last_seen_at", clientErrUpdateLastSeen, err)
	}
	if rows == 0 {
		return fleeterror.NewNotFoundError("fleet node not found")
	}
	return nil
}

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
// EXPIRED and soft-deletes any fleet_node rows bound to them so their
// identity_pubkey and org-local name aren't permanently consumed.
func (s *Service) SweepExpired(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	// Soft-delete the fleet nodes first so the partial unique indexes on fleet_node
	// (uq_fleet_node_identity_pubkey, uq_fleet_node_org_name) free up before any retry
	// observes the EXPIRED enrollment row.
	if _, err := s.store.SoftDeleteFleetNodesForExpiredEnrollments(ctx, now); err != nil {
		return 0, logInternal("soft delete expired fleet nodes", clientErrCancel, err)
	}
	return s.store.SweepExpiredEnrollments(ctx, now)
}

func (s *Service) ListFleetNodes(ctx context.Context, orgID int64) ([]FleetNodeListing, error) {
	agents, err := s.store.ListFleetNodesForOrganization(ctx, orgID)
	if err != nil {
		return nil, logInternal("list fleet nodes", clientErrListFleetNodes, err)
	}
	return agents, nil
}

func (s *Service) GetFleetNodeByID(ctx context.Context, fleetNodeID, orgID int64) (*FleetNode, error) {
	agent, err := s.store.GetFleetNodeByID(ctx, fleetNodeID, orgID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, err
		}
		return nil, logInternal("get fleet node", clientErrGetFleetNode, err)
	}
	return agent, nil
}

// IdentityFingerprint is the short hex form the operator visually compares to
// the value the fleet node prints locally on first run. 16 hex chars = 64 bits of
// SHA-256, enough to reject a substituted-pubkey attack with a brief glance.
func IdentityFingerprint(identityPubkey []byte) string {
	h := sha256.Sum256(identityPubkey)
	return hex.EncodeToString(h[:8])
}

func hashCode(plaintext string) string {
	return cryptohash.Sha256Hex(plaintext)
}

// logInternal sanitizes errors before they leave a domain method, but lets
// retryable PostgreSQL errors (deadlock_detected, serialization_failure) pass
// through unwrapped so the SQL transactor's retry helper can see them and
// re-run the TX.
func logInternal(op, clientMsg string, err error) error {
	if db.IsRetryablePostgresError(err) {
		return err
	}
	return fleeterror.LogInternal(component, op, clientMsg, err)
}

// logActivity records an operator-driven enrollment event. No-ops when the
// activity service is nil (e.g. integration tests) or when the call has no
// session info on its context (e.g. background sweepers).
func (s *Service) logActivity(ctx context.Context, eventType, description string) {
	if s.activitySvc == nil {
		return
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return
	}
	s.activitySvc.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           eventType,
		Description:    description,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})
}
