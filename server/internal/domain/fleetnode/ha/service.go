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
const controlStreamDisconnectedReason = "control stream disconnected"
const notActiveMemberReason = "fleet node is not the active member"

type Route = control.RouteBinding

type Service struct {
	conn                 *sql.DB
	queries              sqlc.Querier
	registry             *control.Registry
	mu                   sync.Mutex
	preparing            map[int64]*preparation
	activating           map[int64]bool
	connectMu            sync.Mutex
	connecting           map[int64]*connectionGate
	activity             *activity.Service
	transactor           interfaces.Transactor
	invalidateLeadership func(context.Context, int64, int64)
}

func NewService(conn *sql.DB, registry *control.Registry) *Service {
	return &Service{conn: conn, queries: db.NewFailoverResettingQuerier(db.NewRetryDB(conn)), registry: registry, preparing: make(map[int64]*preparation), activating: make(map[int64]bool), connecting: make(map[int64]*connectionGate)}
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
	member, err := s.q(ctx).GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if member.RevokedAt.Valid {
			return fleeterror.NewUnauthenticatedError("fleet node revoked")
		}
		group, err := s.q(ctx).GetFleetNodeHAGroup(ctx, sqlc.GetFleetNodeHAGroupParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
		if err != nil {
			return err
		}
		if (member.Ready || nodeID == member.OwnerNodeID) && !bytes.Equal(hello.GetCredentialKeyFingerprint(), group.CredentialKeyFingerprint) {
			return fleeterror.NewFailedPreconditionError("credential key conflicts with HA group")
		}
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
	bound = registration.SetHAConnection(orgID, hello, sessionID)
	if !bound {
		return fleeterror.NewUnauthenticatedError("control registration replaced")
	}
	if err == nil && !member.Ready {
		return s.confirmFingerprint(ctx, nodeID, orgID, sessionID)
	}
	return nil
}

// Configure supplies authority before any device work starts. A first standby's
// preparation leaves the original source in its existing standalone scope.
func (s *Service) Configure(ctx context.Context, nodeID, orgID int64, accepted *pb.ControlAccepted) error {
	accepted.OrgId = orgID
	member, err := s.q(ctx).GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if member.RevokedAt.Valid {
		return fleeterror.NewUnauthenticatedError("fleet node revoked")
	}
	group, err := s.q(ctx).GetFleetNodeHAGroup(ctx, sqlc.GetFleetNodeHAGroupParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
	if err != nil {
		return err
	}
	accepted.Preparing = !member.Ready
	accepted.CredentialKeyFingerprint = bytes.Clone(group.CredentialKeyFingerprint)
	if group.Enabled || nodeID != member.OwnerNodeID {
		accepted.HaOwnerNodeId = member.OwnerNodeID
	}
	return nil
}

func (s *Service) confirmFingerprint(ctx context.Context, nodeID, orgID int64, sessionID string) error {
	return s.runInTx(ctx, func(ctx context.Context) error {
		rows, err := s.q(ctx).ConfirmFleetNodeHAMemberFingerprint(ctx, sqlc.ConfirmFleetNodeHAMemberFingerprintParams{NodeID: nodeID, OrgID: orgID, ControlSessionID: sessionID})
		if err != nil || rows == 0 {
			return err
		}
		member, err := s.q(ctx).GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
		if err != nil {
			return err
		}
		key := fmt.Sprintf("fleet_node.ha.ready/%d/%s", nodeID, sessionID)
		return s.audit(ctx, orgID, member.OwnerNodeID, nodeID, 0, "fleet_node.ha.ready", key, fmt.Sprintf("Fleet Node %d ready as standby for HA owner %d", nodeID, member.OwnerNodeID))
	})
}

// Disconnect does not shorten the lease: the old process must lose its local
// authority before a replacement can become active. Revocation is explicit.
func (s *Service) Disconnect(ctx context.Context, nodeID, orgID int64, sessionID string) error {
	return s.q(ctx).DeleteFleetNodeHAConnection(ctx, sqlc.DeleteFleetNodeHAConnectionParams{NodeID: nodeID, OrgID: orgID, ControlSessionID: sessionID})
}

func (s *Service) Resolve(ctx context.Context, ownerID, orgID int64) (Route, error) {
	state, err := s.q(ctx).GetFleetNodeHARoute(ctx, sqlc.GetFleetNodeHARouteParams{ID: ownerID, OrgID: orgID})
	if err != nil {
		return Route{}, err
	}
	if state.MemberOwnerNodeID != ownerID {
		return Route{}, fleeterror.NewFailedPreconditionError("standby is not a device owner")
	}
	if !state.Enabled {
		if state.EnrollmentStatus != confirmedEnrollmentStatus {
			return Route{}, fleeterror.NewFailedPreconditionError("fleet node is not confirmed")
		}
		return Route{OwnerNodeID: ownerID, PhysicalNodeID: ownerID, EncryptionPublicKey: state.EncryptionPubkey}, nil
	}
	route, ok := s.registry.HARoute(ownerID)
	if !state.Acknowledged || !state.LeaseExpiresAt.After(time.Now()) || state.PhysicalEnrollmentStatus != confirmedEnrollmentStatus || !ok || route.PhysicalNodeID != state.PhysicalNodeID || route.Epoch != state.LeaseEpoch || route.InstanceID != state.InstanceID || route.ControlSessionID != state.ControlSessionID {
		return Route{}, fleeterror.NewUnavailableErrorf("fleet node HA group has no acknowledged active connection")
	}
	return route, nil
}

func (s *Service) Eligible(ctx context.Context, nodeID, orgID int64) (int64, string, error) {
	hello, _, connected := s.registry.HAConnection(nodeID)
	if !connected {
		return 0, controlStreamDisconnectedReason, nil
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
			return 0, notActiveMemberReason, nil
		}
		return 0, "", err
	}
	if route.PhysicalNodeID != nodeID {
		return 0, notActiveMemberReason, nil
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
	source, err := q.LockFleetNodeByID(ctx, sqlc.LockFleetNodeByIDParams{ID: route.PhysicalNodeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewFailedPreconditionError("standby source is revoked")
	}
	if err != nil {
		return err
	}
	if source.EnrollmentStatus != confirmedEnrollmentStatus {
		return fleeterror.NewFailedPreconditionError("standby source is revoked")
	}
	if err = q.CreateFleetNodeHAGroup(ctx, sqlc.CreateFleetNodeHAGroupParams{OwnerNodeID: ownerID, OrgID: orgID, CredentialKeyFingerprint: hello.GetCredentialKeyFingerprint()}); err != nil {
		return err
	}
	group, err := q.LockFleetNodeHAGroup(ctx, sqlc.LockFleetNodeHAGroupParams{OwnerNodeID: ownerID, OrgID: orgID})
	if err != nil {
		return err
	}
	if !bytes.Equal(group.CredentialKeyFingerprint, hello.GetCredentialKeyFingerprint()) {
		return fleeterror.NewFailedPreconditionError("source credential key conflicts with HA group")
	}

	if err = q.CreateFleetNodeHAMember(ctx, sqlc.CreateFleetNodeHAMemberParams{NodeID: ownerID, OrgID: orgID, OwnerNodeID: ownerID, Ready: true, StatusReason: ""}); err != nil {
		return err
	}
	if err := q.CreateFleetNodeHAMember(ctx, sqlc.CreateFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID, OwnerNodeID: ownerID, Ready: false, StatusReason: "preparing credential key"}); err != nil {
		return err
	}
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
	if db.GetTxQueries(ctx) != nil {
		return fn(ctx)
	}
	var committed func()
	err := db.WithTransactionNoResult(ctx, s.conn, func(q sqlc.Querier) error {
		txCtx, commit := db.WithCommitHooks(db.WithTxQueries(ctx, q))
		committed = commit
		return fn(txCtx)
	})
	if err == nil {
		committed()
	}
	return err
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
	if !previous.Enabled {
		if nodeID == member.OwnerNodeID {
			resp.OwnerNodeId = 0
			resp.Preparing = false
			resp.ActiveNodeId = nodeID
		}
		return resp, nil
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
	if group.Acknowledged {
		node, err := q.GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: nodeID, OrgID: orgID})
		if err != nil {
			return nil, err
		}
		route := control.RouteBinding{OwnerNodeID: member.OwnerNodeID, PhysicalNodeID: nodeID, Epoch: group.LeaseEpoch, InstanceID: instanceID, ControlSessionID: sessionID, EncryptionPublicKey: node.EncryptionPubkey, ExpiresAt: group.LeaseExpiresAt}
		db.AfterCommit(ctx, func() { s.registry.SetHARoute(route) })
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
	db.AfterCommit(ctx, func() {
		if route, ok := s.registry.HARoute(member.OwnerNodeID); ok && route.PhysicalNodeID == nodeID {
			s.registry.InvalidateHARoute(member.OwnerNodeID)
		}
	})
	members, err := q.ListFleetNodeHAMembers(ctx, sqlc.ListFleetNodeHAMembersParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
	return member.OwnerNodeID, len(members) > 0, err
}

type preparation struct {
	sessionID string
	cancel    context.CancelFunc
}

// StartPreparation is called after Accepted has reached the stream. Retries
// start fresh, and all transient state disappears with this connection.
func (s *Service) StartPreparation(ctx context.Context, nodeID, orgID int64) {
	_, sessionID, connected := s.registry.HAConnection(nodeID)
	if !connected {
		return
	}
	s.mu.Lock()
	if old := s.preparing[nodeID]; old != nil {
		if old.sessionID == sessionID {
			s.mu.Unlock()
			return
		}
		old.cancel()
	}
	work, cancel := context.WithCancel(db.WithoutTransaction(ctx))
	pending := &preparation{sessionID: sessionID, cancel: cancel}
	s.preparing[nodeID] = pending
	s.mu.Unlock()
	go func() {
		defer cancel()
		defer func() {
			s.mu.Lock()
			if s.preparing[nodeID] == pending {
				delete(s.preparing, nodeID)
			}
			s.mu.Unlock()
		}()
		backoff := time.Second
		for {
			finished, err := s.prepare(work, nodeID, orgID, sessionID)
			if finished || work.Err() != nil {
				return
			}
			if err != nil {
				slog.Warn("fleet node standby credential preparation pending", "fleet_node_id", nodeID, "err", err)
			}
			timer := time.NewTimer(backoff)
			select {
			case <-work.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			backoff = min(backoff*2, 30*time.Second)
		}
	}()
}

func (s *Service) prepare(ctx context.Context, nodeID, orgID int64, sessionID string) (bool, error) {
	// Transfer retries are bounded; draining retains its gate until the stream
	// ends or existing commands reach their normal terminal completion.
	activationCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	q := s.q(ctx)
	member, err := q.GetFleetNodeHAMember(ctx, sqlc.GetFleetNodeHAMemberParams{NodeID: nodeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && member.RevokedAt.Valid) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	group, err := q.GetFleetNodeHAGroup(ctx, sqlc.GetFleetNodeHAGroupParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
	if err != nil {
		return false, err
	}
	_, currentSession, connected := s.registry.HAConnection(nodeID)
	if !connected || currentSession != sessionID {
		return true, nil
	}
	if member.Ready {
		if group.Enabled {
			return true, nil
		}
		if nodeID == member.OwnerNodeID {
			return true, nil
		}
		enabled, err := s.activate(activationCtx, member.OwnerNodeID, orgID)
		return enabled, err
	}
	if strings.HasPrefix(member.StatusReason, "failed: ") {
		return true, nil
	}
	recipient, err := q.GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: nodeID, OrgID: orgID})
	if err != nil {
		return false, err
	}
	members, err := q.ListFleetNodeHAMembers(ctx, sqlc.ListFleetNodeHAMembersParams{OwnerNodeID: member.OwnerNodeID, OrgID: orgID})
	if err != nil {
		return false, err
	}
	var source int64
	for _, m := range members {
		hello, _, ok := s.registry.HAConnection(m.NodeID)
		if m.NodeID != nodeID && m.Ready && m.EnrollmentStatus == confirmedEnrollmentStatus && ok && bytes.Equal(hello.GetCredentialKeyFingerprint(), group.CredentialKeyFingerprint) {
			source = m.NodeID
			break
		}
	}
	if source == 0 {
		return false, nil
	}
	transfer := &pb.HAKeyTransfer{OperationId: uuid.NewString(), OrgId: orgID, OwnerNodeId: member.OwnerNodeID, SourceNodeId: source, RecipientNodeId: nodeID, RecipientPublicKey: recipient.EncryptionPubkey, CredentialKeyFingerprint: group.CredentialKeyFingerprint, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
	result, err := s.transfer(ctx, source, transfer)
	if err != nil {
		return false, err
	}
	if result.GetEncryptedKey() == nil || !bytes.Equal(result.GetCredentialKeyFingerprint(), group.CredentialKeyFingerprint) {
		return true, s.failTransfer(ctx, nodeID, orgID, "source credential transfer fingerprint mismatch")
	}
	if err := protovalidate.Validate(result.GetEncryptedKey()); err != nil {
		return true, s.failTransfer(ctx, nodeID, orgID, "source returned invalid encrypted credential envelope")
	}
	transfer.EncryptedKey = result.GetEncryptedKey()
	// No ciphertext is checkpointed. Installation becomes ready only when a new
	// control-session Hello proves the expected key after durable installation.
	result, err = s.transfer(ctx, nodeID, transfer)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(result.GetCredentialKeyFingerprint(), group.CredentialKeyFingerprint) {
		return true, s.failTransfer(ctx, nodeID, orgID, "installed credential transfer fingerprint mismatch")
	}
	return false, nil
}

// Pause new standalone dispatch, drain previously admitted commands, then make
// HA durable and reconnect the original source into its new authority scope.
func (s *Service) activate(ctx context.Context, ownerID, orgID int64) (bool, error) {
	// One worker owns the dispatch pause, including cancellation cleanup. Another
	// ready standby must not resume dispatch while this worker commits activation.
	s.mu.Lock()
	if s.activating[ownerID] {
		s.mu.Unlock()
		return false, nil
	}
	s.activating[ownerID] = true
	s.mu.Unlock()
	committed := false
	paused := false
	defer func() {
		if paused && !committed {
			s.registry.AbortHAActivation(ownerID)
		}
		s.mu.Lock()
		delete(s.activating, ownerID)
		s.mu.Unlock()
	}()
	group, err := s.q(ctx).GetFleetNodeHAGroup(ctx, sqlc.GetFleetNodeHAGroupParams{OwnerNodeID: ownerID, OrgID: orgID})
	if err != nil {
		return false, err
	}
	if group.Enabled {
		return true, nil
	}
	paused = true
	for !s.registry.BeginHAActivation(ownerID) {
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, fmt.Errorf("wait for HA activation drain: %w", ctx.Err())
		case <-timer.C:
		}
	}
	err = s.runInTx(ctx, func(ctx context.Context) error {
		group, err := s.q(ctx).LockFleetNodeHAGroup(ctx, sqlc.LockFleetNodeHAGroupParams{OwnerNodeID: ownerID, OrgID: orgID})
		if err != nil {
			return err
		}
		if group.Enabled {
			db.AfterCommit(ctx, func() { s.registry.AbortHAActivation(ownerID) })
			return nil
		}
		rows, err := s.q(ctx).EnableFleetNodeHAGroup(ctx, sqlc.EnableFleetNodeHAGroupParams{OwnerNodeID: ownerID, OrgID: orgID})
		if err != nil {
			return err
		}
		if rows == 0 {
			return fleeterror.NewFailedPreconditionError("HA requires a ready standby")
		}
		db.AfterCommit(ctx, func() { s.registry.FinishHAActivation(ownerID) })
		return nil
	})
	committed = err == nil
	return committed, err
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

// CanonicalOwner maps a member for administrative previews without requiring
// that physical member to hold the lease. The organization is always scoped.
func (s *Service) CanonicalOwner(ctx context.Context, nodeID, orgID int64) (int64, error) {
	state, err := s.q(ctx).GetFleetNodeHARoute(ctx, sqlc.GetFleetNodeHARouteParams{ID: nodeID, OrgID: orgID})
	if err != nil {
		return 0, err
	}
	return state.MemberOwnerNodeID, nil
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

func (s *Service) failTransfer(ctx context.Context, nodeID, orgID int64, reason string) error {
	if err := s.q(ctx).SetFleetNodeHAMemberFailure(ctx, sqlc.SetFleetNodeHAMemberFailureParams{NodeID: nodeID, OrgID: orgID, StatusReason: "failed: " + reason}); err != nil {
		return err
	}
	return fmt.Errorf("%s", reason)
}

// NodeSummary combines membership status and source eligibility from one
// database snapshot and the same acknowledged in-process authority tuple.
type NodeSummary struct {
	Summary
	Eligible          bool
	EligibilityReason string
}

func (s *Service) ListSummaries(ctx context.Context, orgID int64) (map[int64]NodeSummary, error) {
	states, err := s.q(ctx).ListFleetNodeHAState(ctx, orgID)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]NodeSummary, len(states))
	for _, state := range states {
		summary := NodeSummary{Summary: Summary{OwnerNodeID: state.OwnerNodeID}}
		hello, session, connected := s.registry.HAConnection(state.NodeID)
		active := false
		if state.OwnerNodeID != 0 {
			summary.Role = "standby"
			summary.Reason = state.StatusReason
			switch {
			case state.RevokedAt.Valid || state.EnrollmentStatus != confirmedEnrollmentStatus:
				summary.Role = "unavailable"
				summary.Reason = "revoked"
			case !state.Ready:
				summary.Role = "preparing"
				if strings.HasPrefix(state.StatusReason, "failed: ") {
					summary.Role = "failed"
					summary.Reason = strings.TrimPrefix(state.StatusReason, "failed: ")
				}
			case !connected:
				summary.Role = "unavailable"
				summary.Reason = controlStreamDisconnectedReason
			case !state.Enabled && state.NodeID == state.OwnerNodeID:
				active = true
				summary.Role = "active"
			default:
				route, ok := s.registry.HARoute(state.OwnerNodeID)
				active = ok && state.Enabled && state.Acknowledged && state.LeaseExpiresAt.After(time.Now()) && route.PhysicalNodeID == state.NodeID && route.PhysicalNodeID == state.ActiveNodeID && route.Epoch == state.LeaseEpoch && route.InstanceID == state.InstanceID && route.ControlSessionID == state.ControlSessionID && hello.GetInstanceId() == route.InstanceID && session == route.ControlSessionID
				if active {
					summary.Role = "active"
				}
			}
		}
		switch {
		case state.EnrollmentStatus != confirmedEnrollmentStatus || state.RevokedAt.Valid:
			summary.EligibilityReason = "fleet node is not confirmed"
		case !connected:
			summary.EligibilityReason = controlStreamDisconnectedReason
		case !hello.GetHaCapable() || len(hello.GetCredentialKeyFingerprint()) != 32:
			summary.EligibilityReason = "fleet node upgrade required"
		case state.OwnerNodeID != 0 && (!active || !state.Ready || !bytes.Equal(hello.GetCredentialKeyFingerprint(), state.CredentialKeyFingerprint)):
			summary.EligibilityReason = notActiveMemberReason
		default:
			summary.Eligible = true
		}
		result[state.NodeID] = summary
	}
	return result, nil
}
