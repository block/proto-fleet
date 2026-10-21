// Package ha coordinates stable Fleet Node ownership and physical-node leases.
// Credential keys travel only as source-sealed envelopes through this service.
package ha

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const LeaseDuration = 30 * time.Second
const confirmedEnrollmentStatus = "CONFIRMED"

type Route struct {
	OwnerNodeID, PhysicalNodeID, Epoch int64
	InstanceID, ControlSessionID       string
	EncryptionPublicKey                []byte
	Preparing                          bool
	ExpiresAt                          time.Time
}

type Service struct {
	queries              sqlc.Querier
	registry             *control.Registry
	mu                   sync.Mutex
	preparing            map[int64]bool
	connectMu            sync.Mutex
	connecting           map[int64]*connectionGate
	activity             *activity.Service
	transactor           interfaces.Transactor
	invalidateLeadership func(context.Context, int64, int64)
}

func NewService(conn *sql.DB, registry *control.Registry) *Service {
	return &Service{queries: db.NewFailoverResettingQuerier(db.NewRetryDB(conn)), registry: registry, preparing: make(map[int64]bool), connecting: make(map[int64]*connectionGate)}
}

func (s *Service) q(ctx context.Context) sqlc.Querier {
	if q := db.GetTxQueries(ctx); q != nil {
		return q
	}
	return s.queries
}

type connectionGate struct {
	mu   sync.Mutex
	refs int
}

func (s *Service) lockConnection(nodeID int64) func() {
	s.connectMu.Lock()
	gate := s.connecting[nodeID]
	if gate == nil {
		gate = &connectionGate{}
		s.connecting[nodeID] = gate
	}
	gate.refs++
	s.connectMu.Unlock()
	gate.mu.Lock()
	return func() {
		gate.mu.Unlock()
		s.connectMu.Lock()
		gate.refs--
		if gate.refs == 0 {
			delete(s.connecting, nodeID)
		}
		s.connectMu.Unlock()
	}
}

func (s *Service) Connect(ctx context.Context, nodeID, orgID int64, hello *pb.ControlHello, sessionID string) error {
	return s.ConnectStream(ctx, nodeID, orgID, hello, sessionID, s.registry.HARegistration(nodeID))
}

// ConnectStream serializes only one physical node's connection writes. Newer
// registrations can evict this stream while SQL is in flight; the pointer-bound
// setter then rejects it, and cleanup removes its stale SQL row before a newer
// ConnectStream may persist. Registry.mu is never held during database I/O.
func (s *Service) ConnectStream(ctx context.Context, nodeID, orgID int64, hello *pb.ControlHello, sessionID string, registration *control.Stream) error {
	if hello.GetHaCapable() && (hello.GetInstanceId() == "" || len(hello.GetCredentialKeyFingerprint()) != 32) {
		return fleeterror.NewInvalidArgumentError("HA-capable Hello requires an instance and credential key fingerprint")
	}
	unlock := s.lockConnection(nodeID)
	defer unlock()
	if registration == nil || !registration.IsCurrent() {
		return fleeterror.NewUnauthenticatedError("control registration replaced")
	}
	fingerprint := hello.GetCredentialKeyFingerprint()
	if fingerprint == nil {
		fingerprint = []byte{}
	}
	if err := s.q(ctx).UpsertFleetNodeHAConnection(ctx, sqlc.UpsertFleetNodeHAConnectionParams{NodeID: nodeID, OrgID: orgID, HaCapable: hello.GetHaCapable(), InstanceID: hello.GetInstanceId(), ControlSessionID: sessionID, CredentialKeyFingerprint: fingerprint}); err != nil {
		return err
	}
	bound := false
	defer func() {
		if !bound {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := s.q(cleanup).DeleteFleetNodeHAConnection(cleanup, sqlc.DeleteFleetNodeHAConnectionParams{NodeID: nodeID, OrgID: orgID, ControlSessionID: sessionID}); err != nil {
				slog.Warn("stale HA control registration cleanup failed", "fleet_node_id", nodeID, "err", err)
			}
		}
	}()
	if err := s.q(ctx).ResetFleetNodeHAMemberOnFingerprintMismatch(ctx, sqlc.ResetFleetNodeHAMemberOnFingerprintMismatchParams{NodeID: nodeID, OrgID: orgID}); err != nil {
		return err
	}
	bound = registration.SetHAConnection(orgID, hello, sessionID)
	if !bound {
		return fleeterror.NewUnauthenticatedError("control registration replaced")
	}
	return nil
}

// Disconnect does not shorten the lease: the old process must lose its local
// authority before a replacement can become active. Revocation is explicit.
func (s *Service) Disconnect(ctx context.Context, nodeID, orgID int64, sessionID string) error {
	return s.q(ctx).DeleteFleetNodeHAConnection(ctx, sqlc.DeleteFleetNodeHAConnectionParams{NodeID: nodeID, OrgID: orgID, ControlSessionID: sessionID})
}

func (s *Service) Resolve(ctx context.Context, ownerID, orgID int64) (Route, error) {
	q := s.q(ctx)
	node, err := q.GetFleetNodeByIDUnscoped(ctx, ownerID)
	if err != nil {
		return Route{}, err
	}
	if orgID != 0 && node.OrgID != orgID {
		return Route{}, fleeterror.NewNotFoundError("fleet node not found")
	}
	orgID = node.OrgID
	// Public routing always starts with the stable owner. A physical standby is
	// never a second owner that can acquire independent device assignments.
	member, e := q.GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: ownerID, OrgID: orgID})
	if e == nil && member.OwnerNodeID != ownerID {
		return Route{}, fleeterror.NewFailedPreconditionError("standby is not a device owner")
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return Route{}, e
	}
	group, err := q.GetFleetNodeHAGroup(ctx, sqlc.GetFleetNodeHAGroupParams{OwnerNodeID: ownerID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		if node.EnrollmentStatus != confirmedEnrollmentStatus {
			return Route{}, fleeterror.NewFailedPreconditionError("fleet node is not confirmed")
		}
		return Route{OwnerNodeID: ownerID, PhysicalNodeID: ownerID, EncryptionPublicKey: node.EncryptionPubkey}, nil
	}
	if err != nil {
		return Route{}, err
	}
	if !group.Acknowledged || !group.LeaseExpiresAt.After(time.Now()) {
		return Route{}, fleeterror.NewUnavailableErrorf("fleet node HA group has no acknowledged active lease")
	}
	physical, err := q.GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: group.ActiveNodeID, OrgID: orgID})
	if err != nil {
		return Route{}, err
	}
	if physical.EnrollmentStatus != confirmedEnrollmentStatus {
		return Route{}, fleeterror.NewUnavailableErrorf("active fleet node is revoked")
	}
	hello, sessionID, connected := s.registry.HAConnection(group.ActiveNodeID)
	if !connected || sessionID != group.ControlSessionID || hello.GetInstanceId() != group.InstanceID {
		return Route{}, fleeterror.NewUnavailableErrorf("active fleet node connection unavailable")
	}
	return Route{OwnerNodeID: ownerID, PhysicalNodeID: group.ActiveNodeID, Epoch: group.LeaseEpoch, InstanceID: group.InstanceID, ControlSessionID: group.ControlSessionID, EncryptionPublicKey: physical.EncryptionPubkey, ExpiresAt: group.LeaseExpiresAt}, nil
}

func (s *Service) Eligible(ctx context.Context, nodeID, orgID int64) (int64, string, error) {
	node, err := s.q(ctx).GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: nodeID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || fleeterror.IsNotFoundError(err) {
			return 0, "fleet node not found", nil
		}
		return 0, "", err
	}
	if node.EnrollmentStatus != confirmedEnrollmentStatus {
		return 0, "fleet node is not confirmed", nil
	}
	hello, _, connected := s.registry.HAConnection(nodeID)
	if !connected {
		return 0, "control stream disconnected", nil
	}
	if !hello.GetHaCapable() || len(hello.GetCredentialKeyFingerprint()) != 32 {
		return 0, "fleet node upgrade required", nil
	}
	ownerID := nodeID
	member, err := s.q(ctx).GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if err == nil {
		ownerID = member.OwnerNodeID
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, "", err
	}
	route, err := s.Resolve(ctx, ownerID, orgID)
	if err != nil {
		if fleeterror.IsUnavailableError(err) || fleeterror.IsNotFoundError(err) || errors.Is(err, sql.ErrNoRows) {
			return 0, "fleet node is not the active member", nil
		}
		return 0, "", err
	}
	if route.PhysicalNodeID != nodeID {
		return 0, "fleet node is not the active member", nil
	}
	return ownerID, "", nil
}

func (s *Service) SetEnrollmentIntent(ctx context.Context, enrollmentID, orgID, ownerID int64) error {
	rows, err := s.q(ctx).SetEnrollmentStandbyOwner(ctx, sqlc.SetEnrollmentStandbyOwnerParams{EnrollmentID: enrollmentID, OrgID: orgID, OwnerNodeID: sql.NullInt64{Int64: ownerID, Valid: true}})
	if err != nil {
		return err
	}
	if rows != 1 {
		return fleeterror.NewFailedPreconditionError("enrollment state changed")
	}
	return nil
}

// Confirm runs inside enrollment's transaction; no stream I/O or row lock is
// retained while provisioning. The original owner remains the canonical ID.
func (s *Service) Confirm(ctx context.Context, nodeID, orgID, ownerID int64) error {
	route, err := s.Resolve(ctx, ownerID, orgID)
	if err != nil {
		return err
	}
	hello, _, ok := s.registry.HAConnection(route.PhysicalNodeID)
	if !ok || !hello.GetHaCapable() || len(hello.GetCredentialKeyFingerprint()) != 32 {
		return fleeterror.NewFailedPreconditionError("standby source is unavailable or requires upgrade")
	}
	q := s.q(ctx)
	if err = q.CreateFleetNodeHAGroup(ctx, sqlc.CreateFleetNodeHAGroupParams{OwnerNodeID: ownerID, OrgID: orgID, CredentialKeyFingerprint: hello.GetCredentialKeyFingerprint()}); err != nil {
		return err
	}
	if _, err = q.LockFleetNodeHAGroup(ctx, sqlc.LockFleetNodeHAGroupParams{OwnerNodeID: ownerID, OrgID: orgID}); err != nil {
		return err
	}
	if existing, e := q.GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: route.PhysicalNodeID, OrgID: orgID}); e != nil || existing.EnrollmentStatus != confirmedEnrollmentStatus {
		return fleeterror.NewFailedPreconditionError("standby source is revoked")
	}
	if err = q.CreateFleetNodeHAMember(ctx, sqlc.CreateFleetNodeHAMemberParams{NodeID: ownerID, OrgID: orgID, OwnerNodeID: ownerID, Ready: true, StatusReason: ""}); err != nil {
		return err
	}
	if err := q.CreateFleetNodeHAMember(ctx, sqlc.CreateFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID, OwnerNodeID: ownerID, Ready: false, StatusReason: "preparing credential key"}); err != nil {
		return err
	}
	// Publish before commit so an already-dispatched standalone write cannot
	// race adoption into a retryable interruption. On rollback this conservatively
	// retains terminal treatment of unknown outcomes; it grants no authority.
	s.registry.AdoptHAOwner(ownerID)
	return nil
}

func (s *Service) WithAudit(service *activity.Service, transactor interfaces.Transactor) {
	s.activity = service
	s.transactor = transactor
}
func (s *Service) WithLeadershipInvalidator(invalidate func(context.Context, int64, int64)) {
	s.invalidateLeadership = invalidate
}
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
	if s.transactor != nil {
		return s.transactor.RunInTx(ctx, fn)
	}
	return fn(ctx)
}
func (s *Service) audit(ctx context.Context, orgID, ownerID, nodeID, epoch int64, eventType, key, description string) error {
	if s.activity == nil {
		return nil
	}
	scope := "fleet_node"
	label := fmt.Sprintf("%d", ownerID)
	err := s.activity.LogStrict(ctx, activitymodels.Event{Category: activitymodels.CategorySystem, Type: eventType, Description: description, ActorType: activitymodels.ActorSystem, OrganizationID: &orgID, ScopeType: &scope, ScopeLabel: &label, IdempotencyKey: key, Metadata: map[string]any{"owner_node_id": ownerID, "physical_node_id": nodeID, "lease_epoch": epoch}})
	if err != nil {
		return fmt.Errorf("persist HA activity: %w", err)
	}
	return nil
}

func (s *Service) Lease(ctx context.Context, nodeID, orgID int64, instanceID, sessionID string, req *pb.HALeaseRequest) (*pb.HALeaseResponse, error) {
	var response *pb.HALeaseResponse
	err := s.runInTx(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.lease(ctx, nodeID, orgID, instanceID, sessionID, req)
		return err
	})
	if err == nil {
		s.prepareAsync(ctx, nodeID, orgID)
	}
	return response, err
}

func (s *Service) lease(ctx context.Context, nodeID, orgID int64, instanceID, sessionID string, req *pb.HALeaseRequest) (*pb.HALeaseResponse, error) {
	q := s.q(ctx)
	rows, err := q.TouchFleetNodeHAConnection(ctx, sqlc.TouchFleetNodeHAConnectionParams{NodeID: nodeID, OrgID: orgID, InstanceID: instanceID, ControlSessionID: sessionID})
	if err != nil {
		return nil, err
	}
	if rows != 1 {
		return nil, fleeterror.NewUnauthenticatedError("control session replaced")
	}
	resp := &pb.HALeaseResponse{RequestId: req.GetRequestId(), OrgId: orgID, OwnerNodeId: nodeID, InstanceId: instanceID, ControlSessionId: sessionID, LeaseDurationMs: LeaseDuration.Milliseconds()}
	member, err := q.GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		node, e := q.GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: nodeID, OrgID: orgID})
		if e != nil {
			return nil, e
		}
		if node.EnrollmentStatus != confirmedEnrollmentStatus {
			return nil, fleeterror.NewUnauthenticatedError("fleet node revoked")
		}
		resp.OwnerNodeId = 0
		resp.Granted = false
		resp.ActiveNodeId = nodeID
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	resp.OwnerNodeId = member.OwnerNodeID
	resp.Preparing = !member.Ready
	if member.RevokedAt.Valid {
		return nil, fleeterror.NewUnauthenticatedError("fleet node revoked")
	}
	previous, err := q.LockFleetNodeHAGroup(ctx, sqlc.LockFleetNodeHAGroupParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
	if err != nil {
		return nil, err
	}
	group, err := q.AcquireOrRenewFleetNodeHALease(ctx, sqlc.AcquireOrRenewFleetNodeHALeaseParams{NodeID: nodeID, OrgID: orgID, OwnerNodeID: member.OwnerNodeID, InstanceID: instanceID, ControlSessionID: sessionID, AcknowledgedEpoch: req.GetAcknowledgedEpoch()})
	if errors.Is(err, sql.ErrNoRows) {
		group, err = q.GetFleetNodeHAGroup(ctx, sqlc.GetFleetNodeHAGroupParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
		if err != nil {
			return nil, err
		}
		resp.ActiveNodeId = group.ActiveNodeID
		resp.LeaseEpoch = group.LeaseEpoch
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	if group.Acknowledged && (!previous.Acknowledged || previous.LeaseEpoch != group.LeaseEpoch) {
		key := fmt.Sprintf("fleet_node.ha.leadership/%d/%d", group.OwnerNodeID, group.LeaseEpoch)
		if err := s.audit(ctx, orgID, group.OwnerNodeID, nodeID, group.LeaseEpoch, "fleet_node.ha.leadership", key, fmt.Sprintf("Fleet Node HA owner %d active on physical node %d (epoch %d)", group.OwnerNodeID, nodeID, group.LeaseEpoch)); err != nil {
			return nil, err
		}
		if s.invalidateLeadership != nil {
			invalidate := func() {
				work, cancel := context.WithTimeout(context.WithoutCancel(db.WithoutTransaction(ctx)), 5*time.Second)
				defer cancel()
				s.invalidateLeadership(work, group.OwnerNodeID, orgID)
			}
			if !db.AfterCommit(ctx, invalidate) {
				invalidate()
			}
		}
	}
	resp.Granted = true
	resp.ActiveNodeId = group.ActiveNodeID
	resp.LeaseEpoch = group.LeaseEpoch
	return resp, nil
}

func (s *Service) EffectiveOwner(ctx context.Context, nodeID, orgID int64) (int64, error) {
	member, err := s.q(ctx).GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return nodeID, nil
	}
	if err != nil {
		return 0, err
	}
	route, err := s.Resolve(ctx, member.OwnerNodeID, orgID)
	if err != nil {
		// Reports have already passed physical connection and in-flight command
		// admission. Preserve the original source's pre-adoption work while its
		// first grant is being acknowledged; new dispatch still requires Resolve.
		if nodeID == member.OwnerNodeID && member.Ready && !member.RevokedAt.Valid && fleeterror.IsUnavailableError(err) {
			group, groupErr := s.q(ctx).GetFleetNodeHAGroup(ctx, sqlc.GetFleetNodeHAGroupParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
			if groupErr != nil {
				return 0, groupErr
			}
			if group.LeaseEpoch == 0 || (group.LeaseEpoch == 1 && group.ActiveNodeID == nodeID && !group.Acknowledged && group.LeaseExpiresAt.After(time.Now())) {
				return member.OwnerNodeID, nil
			}
		}
		return 0, err
	}
	if route.PhysicalNodeID != nodeID {
		return 0, fleeterror.NewFailedPreconditionError("reporting fleet node is not active")
	}
	return member.OwnerNodeID, nil
}

// Revoke is called in the authentication-revocation transaction. It denies
// routing immediately while retaining the old lease expiry before promotion.
// It preserves shared data
// until the last nonrevoked member has gone.
func (s *Service) Revoke(ctx context.Context, nodeID, orgID int64) (ownerID int64, retain bool, err error) {
	q := s.q(ctx)
	member, err := q.GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return nodeID, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if _, err = q.LockFleetNodeHAGroup(ctx, sqlc.LockFleetNodeHAGroupParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID}); err != nil {
		return 0, false, err
	}
	if err = q.RevokeFleetNodeHAMember(ctx, sqlc.RevokeFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID}); err != nil {
		return 0, false, err
	}
	if err = q.ExpireFleetNodeHALease(ctx, sqlc.ExpireFleetNodeHALeaseParams{ActiveNodeID: nodeID, OrgID: orgID}); err != nil {
		return 0, false, err
	}
	members, err := q.ListFleetNodeHAMembers(ctx, sqlc.ListFleetNodeHAMembersParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
	return member.OwnerNodeID, len(members) > 0, err
}

func (s *Service) prepareAsync(ctx context.Context, nodeID, orgID int64) {
	s.mu.Lock()
	if s.preparing[nodeID] {
		s.mu.Unlock()
		return
	}
	s.preparing[nodeID] = true
	s.mu.Unlock()
	go func() {
		defer func() { s.mu.Lock(); delete(s.preparing, nodeID); s.mu.Unlock() }()
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		if err := s.prepare(work, nodeID, orgID); err != nil {
			slog.Warn("fleet node standby credential preparation pending", "fleet_node_id", nodeID, "err", err)
		}
	}()
}

func (s *Service) prepare(ctx context.Context, nodeID, orgID int64) error {
	q := s.q(ctx)
	member, err := q.GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (member.Ready || member.RevokedAt.Valid)) {
		return nil
	}
	if err != nil {
		return err
	}
	group, err := q.GetFleetNodeHAGroup(ctx, sqlc.GetFleetNodeHAGroupParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
	if err != nil {
		return err
	}
	_, recipientSession, connected := s.registry.HAConnection(nodeID)
	if !connected {
		return nil
	}
	recipient, err := q.GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: nodeID, OrgID: orgID})
	if err != nil {
		return err
	}

	operation, err := q.GetFleetNodeHATransfer(ctx, nodeID)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !missing {
		return err
	}
	if !missing && operation.Failed {
		return nil
	}
	needSource := missing || !operation.ExpiresAt.After(time.Now()) || len(operation.EncryptedKey) == 0
	if needSource {
		members, e := q.ListFleetNodeHAMembers(ctx, sqlc.ListFleetNodeHAMembersParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
		if e != nil {
			return e
		}
		var source int64
		for _, m := range members {
			hello, _, ok := s.registry.HAConnection(m.NodeID)
			if m.Ready && ok && bytes.Equal(hello.GetCredentialKeyFingerprint(), group.CredentialKeyFingerprint) {
				source = m.NodeID
				break
			}
		}
		if source == 0 {
			return nil
		}
		err = q.CreateFleetNodeHATransfer(ctx, sqlc.CreateFleetNodeHATransferParams{RecipientNodeID: nodeID, OperationID: uuid.NewString(), OwnerNodeID: member.OwnerNodeID, SourceNodeID: source})
		if err != nil {
			return err
		}
	}
	operation, err = q.ClaimFleetNodeHATransfer(ctx, nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	transfer := &pb.HAKeyTransfer{OperationId: operation.OperationID, OrgId: orgID, OwnerNodeId: member.OwnerNodeID, SourceNodeId: operation.SourceNodeID, RecipientNodeId: nodeID, RecipientPublicKey: recipient.EncryptionPubkey, CredentialKeyFingerprint: group.CredentialKeyFingerprint, ExpiresAt: timestamppb.New(operation.ExpiresAt)}
	if len(operation.EncryptedKey) == 0 {
		result, e := s.transfer(ctx, operation.SourceNodeID, transfer)
		if e != nil {
			return e
		}
		if result.GetEncryptedKey() == nil || !bytes.Equal(result.GetCredentialKeyFingerprint(), group.CredentialKeyFingerprint) {
			return s.failTransfer(ctx, nodeID, orgID, operation.OperationID, "source credential transfer fingerprint mismatch")
		}
		if e := protovalidate.Validate(result.GetEncryptedKey()); e != nil {
			return s.failTransfer(ctx, nodeID, orgID, operation.OperationID, "source returned invalid encrypted credential envelope")
		}
		transfer.EncryptedKey = result.GetEncryptedKey()
		ciphertext, e := proto.Marshal(transfer.EncryptedKey)
		if e != nil {
			return fmt.Errorf("encode sealed credential transfer: %w", e)
		}
		rows, e := q.SetFleetNodeHATransferCiphertext(ctx, sqlc.SetFleetNodeHATransferCiphertextParams{EncryptedKey: ciphertext, RecipientNodeID: nodeID, OperationID: operation.OperationID})
		if e != nil {
			return e
		}
		if rows != 1 {
			return fmt.Errorf("credential transfer expired")
		}
	} else {
		transfer.EncryptedKey = &pb.NodeEncryptedPayload{}
		if err = proto.Unmarshal(operation.EncryptedKey, transfer.EncryptedKey); err != nil {
			return fmt.Errorf("decode persisted sealed credential transfer: %w", err)
		}
	}
	result, err := s.transfer(ctx, nodeID, transfer)
	if err != nil {
		return err
	}
	if !bytes.Equal(result.GetCredentialKeyFingerprint(), group.CredentialKeyFingerprint) {
		return s.failTransfer(ctx, nodeID, orgID, operation.OperationID, "installed credential transfer fingerprint mismatch")
	}
	rows, err := q.SetFleetNodeHAConnectionFingerprint(ctx, sqlc.SetFleetNodeHAConnectionFingerprintParams{CredentialKeyFingerprint: group.CredentialKeyFingerprint, NodeID: nodeID, OrgID: orgID, ControlSessionID: recipientSession})
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("recipient connection changed during credential transfer")
	}
	if !s.registry.UpdateHAFingerprint(nodeID, recipientSession, group.CredentialKeyFingerprint) {
		return fmt.Errorf("recipient connection changed")
	}
	return s.runInTx(ctx, func(ctx context.Context) error {
		rows, err := s.q(ctx).CompleteFleetNodeHATransfer(ctx, sqlc.CompleteFleetNodeHATransferParams{NodeID: nodeID, OrgID: orgID, OperationID: operation.OperationID, ControlSessionID: recipientSession})
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("credential transfer expired or recipient revoked")
		}
		key := fmt.Sprintf("fleet_node.ha.ready/%d/%s", nodeID, recipientSession)
		return s.audit(ctx, orgID, member.OwnerNodeID, nodeID, 0, "fleet_node.ha.ready", key, fmt.Sprintf("Fleet Node %d ready as standby for HA owner %d", nodeID, member.OwnerNodeID))
	})
}

func (s *Service) transfer(ctx context.Context, nodeID int64, transfer *pb.HAKeyTransfer) (*pb.HAKeyTransferResult, error) {
	ack, err := s.registry.SendProvisioningCommand(ctx, nodeID, &pb.AgentCommand{Command: &pb.AgentCommand_HaKeyTransfer{HaKeyTransfer: transfer}})
	if err != nil {
		return nil, err
	}
	if !ack.GetSucceeded() || ack.GetCode() != pb.AckCode_ACK_CODE_OK {
		return nil, fmt.Errorf("credential transfer rejected: %s", ack.GetErrorMessage())
	}
	result := &pb.HAKeyTransferResult{}
	if err = proto.Unmarshal(ack.GetPayload(), result); err != nil {
		return nil, fmt.Errorf("decode credential transfer acknowledgement: %w", err)
	}
	return result, nil
}

// Summary exposes membership and preparation without inventing a second public
// group resource. The owner ID is the existing stable Fleet Node device owner.
type Summary struct {
	OwnerNodeID  int64
	Role, Reason string
}

func (s *Service) Summary(ctx context.Context, nodeID, orgID int64) (Summary, error) {
	q := s.q(ctx)
	member, err := q.GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, nil
	}
	if err != nil {
		return Summary{}, err
	}
	out := Summary{OwnerNodeID: member.OwnerNodeID, Role: "standby", Reason: member.StatusReason}
	if member.RevokedAt.Valid {
		out.Role = "unavailable"
		out.Reason = "revoked"
		return out, nil
	}
	if !member.Ready {
		out.Role = "preparing"
		transfer, e := q.GetFleetNodeHATransfer(ctx, nodeID)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return Summary{}, e
		}
		if e == nil && transfer.Failed {
			out.Role = "failed"
			out.Reason = strings.TrimPrefix(member.StatusReason, "failed: ")
		}
		return out, nil
	}
	group, err := q.GetFleetNodeHAGroup(ctx, sqlc.GetFleetNodeHAGroupParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
	if err != nil {
		return Summary{}, err
	}
	_, _, connected := s.registry.HAConnection(nodeID)
	if !connected {
		out.Role = "unavailable"
		out.Reason = "control stream disconnected"
		return out, nil
	}
	if group.ActiveNodeID == nodeID && group.Acknowledged && group.LeaseExpiresAt.After(time.Now()) {
		out.Role = "active"
	}
	return out, nil
}

// CanonicalOwner maps a member for administrative previews without requiring
// that physical member to hold the lease. The organization is always scoped.
func (s *Service) CanonicalOwner(ctx context.Context, nodeID, orgID int64) (int64, error) {
	node, err := s.q(ctx).GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: nodeID, OrgID: orgID})
	if err != nil {
		return 0, err
	}
	member, err := s.q(ctx).GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return node.ID, nil
	}
	if err != nil {
		return 0, err
	}
	return member.OwnerNodeID, nil
}

// DispatchOwner accepts the stable owner or its current active physical member.
// A preparing or passive member cannot be targeted for ordinary device work.
func (s *Service) DispatchOwner(ctx context.Context, nodeID, orgID int64) (int64, error) {
	ownerID, err := s.CanonicalOwner(ctx, nodeID, orgID)
	if err != nil {
		return 0, err
	}
	route, err := s.Resolve(ctx, ownerID, orgID)
	if err != nil {
		return 0, err
	}
	if nodeID != ownerID && nodeID != route.PhysicalNodeID {
		return 0, fleeterror.NewFailedPreconditionError("fleet node is not the active member")
	}
	return ownerID, nil
}

func (s *Service) failTransfer(ctx context.Context, nodeID, orgID int64, operationID, reason string) error {
	if err := s.q(ctx).FailFleetNodeHATransfer(ctx, sqlc.FailFleetNodeHATransferParams{RecipientNodeID: nodeID, OperationID: operationID}); err != nil {
		return err
	}
	if err := s.q(ctx).SetFleetNodeHAMemberFailure(ctx, sqlc.SetFleetNodeHAMemberFailureParams{NodeID: nodeID, OrgID: orgID, StatusReason: "failed: " + reason}); err != nil {
		return err
	}
	return fmt.Errorf("%s", reason)
}
