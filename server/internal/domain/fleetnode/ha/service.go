// Package ha runs two-node Fleet Node high availability. A pair's active node
// owns its devices. The standby holds the same miner credential key, so when
// the active node's control stream stays down, Run moves the devices to the
// standby and swaps the roles. There is no automatic failback: a returning
// node serves as the pair's standby.
package ha

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const (
	// FailoverDelay is how long an active node must stay disconnected, with
	// its standby connected, before its devices move. HA-capable nodes drop a
	// silent stream within 15s.
	FailoverDelay = 30 * time.Second

	tickInterval   = 5 * time.Second
	startupGrace   = 90 * time.Second
	provisionRetry = 30 * time.Second
	commandTimeout = 15 * time.Second

	// HAProtocolVersion is the first command protocol with key transfer and
	// stream keepalive.
	HAProtocolVersion = gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7

	confirmedStatus = "CONFIRMED"

	reasonWaitingForNodes = "Waiting for both nodes to connect"
	reasonUpgradeRequired = "Both nodes must run a Fleet Node version that supports HA"
	reasonTransferFailed  = "Credential key transfer failed; retrying"
)

var (
	errPairChanged    = errors.New("HA pair changed")
	errFailoverNotDue = errors.New("HA failover is no longer due")
)

type failoverReason string

const (
	automaticFailover failoverReason = "after its control stream disconnected"
	revokedActive     failoverReason = "after revoking the active node"
)

type Service struct {
	queries         sqlc.Querier
	transactor      interfaces.Transactor
	registry        *control.Registry
	activity        *activity.Service
	invalidateMiner func(context.Context, int64)

	failoverDelay  time.Duration
	startupGrace   time.Duration
	tickInterval   time.Duration
	provisionRetry time.Duration

	mu           sync.Mutex
	provisioning map[int64]bool      // pair IDs with a transfer in flight
	failedAt     map[int64]time.Time // pair IDs whose last transfer failed
}

func NewService(conn *sql.DB, transactor interfaces.Transactor, registry *control.Registry, activitySvc *activity.Service, invalidateMiner func(context.Context, int64)) *Service {
	return &Service{
		queries:         db.NewFailoverResettingQuerier(db.NewRetryDB(conn)),
		transactor:      transactor,
		registry:        registry,
		activity:        activitySvc,
		invalidateMiner: invalidateMiner,
		failoverDelay:   FailoverDelay,
		startupGrace:    startupGrace,
		tickInterval:    tickInterval,
		provisionRetry:  provisionRetry,
		provisioning:    make(map[int64]bool),
		failedAt:        make(map[int64]time.Time),
	}
}

func (s *Service) q(ctx context.Context) sqlc.Querier {
	if q := db.GetTxQueries(ctx); q != nil {
		return q
	}
	return s.queries
}

// CheckStandbyTarget reports whether a new node may enroll as targetID's standby.
func (s *Service) CheckStandbyTarget(ctx context.Context, orgID, targetID int64) error {
	node, err := s.q(ctx).GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: targetID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewNotFoundError("standby target fleet node not found")
	}
	if err != nil {
		return err
	}
	return s.checkTarget(ctx, orgID, targetID, node.EnrollmentStatus)
}

func (s *Service) checkTarget(ctx context.Context, orgID, targetID int64, status string) error {
	if status != confirmedStatus {
		return fleeterror.NewFailedPreconditionError("standby target must be a confirmed fleet node")
	}
	paired, err := s.q(ctx).FleetNodeInHAPair(ctx, sqlc.FleetNodeInHAPairParams{OrgID: orgID, NodeID: targetID})
	if err != nil {
		return err
	}
	if paired {
		return fleeterror.NewFailedPreconditionError("fleet node is already in an HA pair")
	}
	return nil
}

// CreatePair pairs a newly confirmed standby with its target. It runs in the
// confirm transaction, after the standby's fleet_node lock, and locks the
// target so a concurrent revoke or second standby cannot interleave.
func (s *Service) CreatePair(ctx context.Context, orgID, activeID, standbyID int64) error {
	q := db.GetTxQueries(ctx)
	if q == nil {
		return fleeterror.NewInternalError("HA pair creation requires a transaction")
	}
	target, err := q.LockFleetNodeByID(ctx, sqlc.LockFleetNodeByIDParams{ID: activeID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewFailedPreconditionError("standby target fleet node no longer exists")
	}
	if err != nil {
		return err
	}
	if err := s.checkTarget(ctx, orgID, activeID, target.EnrollmentStatus); err != nil {
		return err
	}
	err = q.CreateFleetNodeHAPair(ctx, sqlc.CreateFleetNodeHAPairParams{
		OrgID:         orgID,
		ActiveNodeID:  activeID,
		StandbyNodeID: standbyID,
		StatusReason:  reasonWaitingForNodes,
	})
	if db.IsUniqueViolationError(err) {
		return fleeterror.NewFailedPreconditionError("fleet node is already in an HA pair")
	}
	return err
}

// RequireNotStandby rejects work that would give a standby devices of its own.
func (s *Service) RequireNotStandby(ctx context.Context, orgID, nodeID int64) error {
	standby, err := s.q(ctx).IsFleetNodeHAStandby(ctx, sqlc.IsFleetNodeHAStandbyParams{OrgID: orgID, StandbyNodeID: nodeID})
	if err != nil {
		return fleeterror.LogInternal("fleet node HA", "check standby", "failed to check fleet node HA role", err)
	}
	if standby {
		return fleeterror.NewFailedPreconditionError("fleet node is an HA standby; use its active node")
	}
	return nil
}

// ShouldHandOff reports whether revoking nodeID should move its devices to its
// standby: nodeID is a pair's active node and the standby is ready and connected.
func (s *Service) ShouldHandOff(ctx context.Context, orgID, nodeID int64) (bool, error) {
	pair, ok, err := s.readyPairOf(ctx, orgID, nodeID)
	return ok && s.registry.Connected(pair.StandbyNodeID), err
}

// HandOff moves a revoked active node's devices to its ready standby, so they
// stay paired. Revocation calls it after denying the node authentication,
// because failover waits for in-flight miner calls to release device locks.
func (s *Service) HandOff(ctx context.Context, orgID, nodeID int64) error {
	pair, ok, err := s.readyPairOf(ctx, orgID, nodeID)
	if err != nil || !ok {
		return err
	}
	if err := s.failover(ctx, pair.OrgID, pair.ID, pair.ActiveNodeID, pair.StandbyNodeID, revokedActive); err != nil && !errors.Is(err, errPairChanged) {
		return err
	}
	return nil
}

func (s *Service) readyPairOf(ctx context.Context, orgID, nodeID int64) (pair sqlc.FleetNodeHaPair, ok bool, err error) {
	pair, err = s.q(ctx).GetFleetNodeHAPairForNode(ctx, sqlc.GetFleetNodeHAPairForNodeParams{OrgID: orgID, NodeID: nodeID})
	if errors.Is(err, sql.ErrNoRows) {
		return pair, false, nil
	}
	if err != nil {
		return pair, false, err
	}
	return pair, pair.ActiveNodeID == nodeID && pair.StandbyReady, nil
}

// DeletePair removes a revoked node's pair unless an unresolved installation
// could still replace the surviving standby's credential key.
func (s *Service) DeletePair(ctx context.Context, orgID, nodeID int64) error {
	rows, err := s.q(ctx).DeleteFleetNodeHAPairForNode(ctx, sqlc.DeleteFleetNodeHAPairForNodeParams{OrgID: orgID, NodeID: nodeID})
	if err != nil || rows > 0 {
		return err
	}
	paired, err := s.q(ctx).FleetNodeInHAPair(ctx, sqlc.FleetNodeInHAPairParams{OrgID: orgID, NodeID: nodeID})
	if err != nil {
		return err
	}
	if paired {
		return fleeterror.NewFailedPreconditionError("fleet node authentication is revoked; standby credential key installation is pending; reconnect the standby and retry revocation")
	}
	return nil
}

// Run watches every pair until ctx ends. It fails over pairs whose active node
// has stayed disconnected and transfers the credential key to new standbys.
func (s *Service) Run(ctx context.Context) {
	started := time.Now()
	reportProgress := runtimejobs.TrackProgress(ctx, s.tickInterval)
	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()
	var transfers sync.WaitGroup
	defer transfers.Wait()
	for {
		s.tick(ctx, started, &transfers)
		reportProgress()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) tick(ctx context.Context, started time.Time, transfers *sync.WaitGroup) {
	s.mu.Lock()
	for pairID, failedAt := range s.failedAt {
		if time.Since(failedAt) >= s.provisionRetry {
			delete(s.failedAt, pairID)
		}
	}
	s.mu.Unlock()

	pairs, err := s.queries.ListFleetNodeHAPairs(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("failed to list fleet node HA pairs", "err", err)
		}
		return
	}
	pastGrace := time.Since(started) >= s.startupGrace
	for _, pair := range pairs {
		if pair.StandbyReady {
			if pastGrace && s.failoverDue(pair.ActiveNodeID, pair.StandbyNodeID) {
				err := s.failover(ctx, pair.OrgID, pair.ID, pair.ActiveNodeID, pair.StandbyNodeID, automaticFailover)
				if err != nil && !errors.Is(err, errPairChanged) && !errors.Is(err, errFailoverNotDue) && ctx.Err() == nil {
					slog.Error("fleet node HA failover failed", "pair_id", pair.ID, "active_node_id", pair.ActiveNodeID, "err", err)
				}
			}
			continue
		}
		reason := s.notReadyReason(pair)
		if reason == "" && s.startTransfer(pair.ID) {
			transfers.Add(1)
			go func() {
				defer transfers.Done()
				s.transfer(ctx, pair)
			}()
			continue
		}
		if reason != "" {
			s.setStatus(ctx, pair, reason)
		}
	}
}

func (s *Service) failoverDue(activeID, standbyID int64) bool {
	downSince, down := s.registry.DisconnectedSince(activeID)
	if !down {
		return false
	}
	upSince, up := s.registry.ConnectedSince(standbyID)
	if !up {
		return false
	}
	if upSince.After(downSince) {
		downSince = upSince
	}
	return time.Since(downSince) >= s.failoverDelay
}

func (s *Service) notReadyReason(pair sqlc.ListFleetNodeHAPairsRow) string {
	needsExport := len(pair.PendingCredentialKey) == 0
	if !s.registry.Connected(pair.StandbyNodeID) || (needsExport && !s.registry.Connected(pair.ActiveNodeID)) {
		return reasonWaitingForNodes
	}
	if s.registry.CommandProtocolUpgradeRequiredForVersion(pair.StandbyNodeID, HAProtocolVersion) ||
		(needsExport && s.registry.CommandProtocolUpgradeRequiredForVersion(pair.ActiveNodeID, HAProtocolVersion)) {
		return reasonUpgradeRequired
	}
	return ""
}

func (s *Service) startTransfer(pairID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provisioning[pairID] || time.Since(s.failedAt[pairID]) < s.provisionRetry {
		return false
	}
	s.provisioning[pairID] = true
	return true
}

func (s *Service) transfer(ctx context.Context, pair sqlc.ListFleetNodeHAPairsRow) {
	err := s.transferKey(ctx, pair)
	s.mu.Lock()
	delete(s.provisioning, pair.ID)
	if err != nil {
		s.failedAt[pair.ID] = time.Now()
	} else {
		delete(s.failedAt, pair.ID)
	}
	s.mu.Unlock()
	if err != nil && ctx.Err() == nil {
		slog.Warn("fleet node HA credential key transfer failed", "pair_id", pair.ID, "active_node_id", pair.ActiveNodeID, "standby_node_id", pair.StandbyNodeID, "err", err)
		s.setStatus(ctx, pair, reasonTransferFailed)
	}
}

func (s *Service) transferKey(ctx context.Context, pair sqlc.ListFleetNodeHAPairsRow) error {
	pending := pair.PendingCredentialKey
	if len(pending) == 0 {
		exported, err := s.sendKeyCommand(ctx, pair.ActiveNodeID, &gatewaypb.AgentCommand{
			Command: &gatewaypb.AgentCommand_ExportCredentialKey{ExportCredentialKey: &gatewaypb.ExportCredentialKey{
				RecipientPublicKey: pair.StandbyEncryptionPubkey,
			}},
		})
		if err != nil {
			return fmt.Errorf("export from active node: %w", err)
		}
		if exported.GetSealedKey() == nil {
			return errors.New("active node exported no sealed key")
		}
		payload, err := proto.Marshal(exported)
		if err != nil {
			return fmt.Errorf("marshal pending credential key: %w", err)
		}
		pending, err = s.queries.PrepareFleetNodeHAKeyInstall(ctx, sqlc.PrepareFleetNodeHAKeyInstallParams{
			ID: pair.ID, ActiveNodeID: pair.ActiveNodeID, StandbyNodeID: pair.StandbyNodeID,
			PendingCredentialKey: payload,
		})
		if err != nil {
			return err
		}
	}
	exported := &gatewaypb.CredentialKeyResult{}
	if err := proto.Unmarshal(pending, exported); err != nil {
		return fmt.Errorf("decode pending credential key: %w", err)
	}
	installed, err := s.sendKeyCommand(ctx, pair.StandbyNodeID, &gatewaypb.AgentCommand{
		Command: &gatewaypb.AgentCommand_InstallCredentialKey{InstallCredentialKey: &gatewaypb.InstallCredentialKey{
			SealedKey: exported.GetSealedKey(),
		}},
	})
	if err != nil {
		return fmt.Errorf("install on standby: %w", err)
	}
	if !bytes.Equal(installed.GetFingerprint(), exported.GetFingerprint()) {
		return errors.New("standby credential key fingerprint does not match the active node")
	}
	_, err = s.queries.MarkFleetNodeHAStandbyReady(ctx, sqlc.MarkFleetNodeHAStandbyReadyParams{
		ID:            pair.ID,
		ActiveNodeID:  pair.ActiveNodeID,
		StandbyNodeID: pair.StandbyNodeID,
	})
	return err
}

func (s *Service) sendKeyCommand(ctx context.Context, nodeID int64, command *gatewaypb.AgentCommand) (*gatewaypb.CredentialKeyResult, error) {
	payload, err := proto.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("marshal credential key command: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	ack, err := s.registry.SendCommand(ctx, nodeID, HAProtocolVersion, &gatewaypb.ControlCommand{CommandId: id.GenerateID(), Payload: payload})
	if err != nil {
		return nil, err
	}
	if !ack.GetSucceeded() {
		return nil, control.AckFailure(ack, "credential key")
	}
	result := &gatewaypb.CredentialKeyResult{}
	if err := proto.Unmarshal(ack.GetPayload(), result); err != nil {
		return nil, fmt.Errorf("decode credential key result: %w", err)
	}
	if err := protovalidate.Validate(result); err != nil {
		return nil, fmt.Errorf("invalid credential key result: %w", err)
	}
	return result, nil
}

func (s *Service) setStatus(ctx context.Context, pair sqlc.ListFleetNodeHAPairsRow, reason string) {
	if pair.StatusReason == reason {
		return
	}
	if _, err := s.queries.SetFleetNodeHAPairStatus(ctx, sqlc.SetFleetNodeHAPairStatusParams{
		StatusReason:  reason,
		ID:            pair.ID,
		ActiveNodeID:  pair.ActiveNodeID,
		StandbyNodeID: pair.StandbyNodeID,
	}); err != nil && ctx.Err() == nil {
		slog.Warn("failed to update fleet node HA status", "pair_id", pair.ID, "err", err)
	}
}

func (s *Service) failover(ctx context.Context, orgID, pairID, activeID, standbyID int64, why failoverReason) error {
	var moved []int64
	var activeName, standbyName string
	err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		q := db.GetTxQueries(ctx)
		if _, err := q.LockFleetNodeDevicesForRevocation(ctx, sqlc.LockFleetNodeDevicesForRevocationParams{FleetNodeID: activeID, OrgID: orgID}); err != nil {
			return err
		}
		active, err := q.LockFleetNodeByID(ctx, sqlc.LockFleetNodeByIDParams{ID: activeID, OrgID: orgID})
		if errors.Is(err, sql.ErrNoRows) {
			return errPairChanged
		}
		if err != nil {
			return err
		}
		standby, err := q.LockFleetNodeByID(ctx, sqlc.LockFleetNodeByIDParams{ID: standbyID, OrgID: orgID})
		if errors.Is(err, sql.ErrNoRows) {
			return errPairChanged
		}
		if err != nil {
			return err
		}
		if standby.EnrollmentStatus != confirmedStatus {
			return errPairChanged
		}
		pair, err := q.LockFleetNodeHAPair(ctx, sqlc.LockFleetNodeHAPairParams{ID: pairID, ActiveNodeID: activeID})
		if errors.Is(err, sql.ErrNoRows) {
			return errPairChanged
		}
		if err != nil {
			return err
		}
		if pair.StandbyNodeID != standbyID || !pair.StandbyReady {
			return errPairChanged
		}
		if why == automaticFailover {
			if !s.failoverDue(activeID, standbyID) {
				return errFailoverNotDue
			}
		} else if !s.registry.Connected(standbyID) {
			return fleeterror.NewFailedPreconditionError("HA standby disconnected before device hand-off")
		}
		moved, err = q.MoveFleetNodeDevices(ctx, sqlc.MoveFleetNodeDevicesParams{ToNodeID: standbyID, FromNodeID: activeID, OrgID: orgID})
		if err != nil {
			return err
		}
		if err := q.MoveDiscoveredDeviceAttribution(ctx, sqlc.MoveDiscoveredDeviceAttributionParams{
			ToNodeID:   sql.NullInt64{Int64: standbyID, Valid: true},
			FromNodeID: sql.NullInt64{Int64: activeID, Valid: true},
			OrgID:      orgID,
		}); err != nil {
			return err
		}
		activeName, standbyName = active.Name, standby.Name
		return q.SwapFleetNodeHAPair(ctx, pairID)
	})
	if err != nil {
		return err
	}
	if s.invalidateMiner != nil {
		for _, deviceID := range moved {
			s.invalidateMiner(ctx, deviceID)
		}
	}
	description := fmt.Sprintf("Moved %d devices from fleet node '%s' (id=%d) to standby '%s' (id=%d) %s", len(moved), activeName, activeID, standbyName, standbyID, why)
	slog.Info("fleet node HA failover", "pair_id", pairID, "from_node_id", activeID, "to_node_id", standbyID, "devices", len(moved))
	s.activity.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategorySystem,
		Type:           "fleet_node_failover",
		Description:    description,
		Result:         activitymodels.ResultSuccess,
		ActorType:      activitymodels.ActorSystem,
		OrganizationID: &orgID,
		Metadata:       map[string]any{"from_node_id": activeID, "to_node_id": standbyID, "device_count": len(moved)},
	})
	return nil
}
