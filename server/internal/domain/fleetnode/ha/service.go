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
	// FailoverDelay is how long an active node must stay disconnected before
	// its devices move. HA-capable nodes drop a silent stream within 15s.
	FailoverDelay = 30 * time.Second

	tickInterval = 5 * time.Second
	// startupGrace lets nodes reconnect after fleetd starts or becomes the
	// active instance; the control registry starts empty.
	startupGrace   = 90 * time.Second
	provisionRetry = 30 * time.Second
	commandTimeout = 15 * time.Second

	// HAProtocolVersion is the first command protocol with key transfer and
	// stream keepalive.
	HAProtocolVersion = gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6

	confirmedStatus = "CONFIRMED"

	reasonWaitingForNodes = "Waiting for both nodes to connect"
	reasonUpgradeRequired = "Both nodes must run a Fleet Node version that supports HA"
	reasonTransferFailed  = "Credential key transfer failed; retrying"
)

var errPairChanged = errors.New("HA pair changed")

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
		return err
	}
	if standby {
		return fleeterror.NewFailedPreconditionError("fleet node is an HA standby; use its active node")
	}
	return nil
}

// HandOffBeforeRevoke moves an active node's devices to its standby when the
// standby is ready and connected, so they stay paired after the revoke.
func (s *Service) HandOffBeforeRevoke(ctx context.Context, orgID, nodeID int64) error {
	pair, err := s.q(ctx).GetFleetNodeHAPairForNode(ctx, sqlc.GetFleetNodeHAPairForNodeParams{OrgID: orgID, NodeID: nodeID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if pair.ActiveNodeID != nodeID || !pair.StandbyReady || !s.registry.Connected(pair.StandbyNodeID) {
		return nil
	}
	if err := s.failover(ctx, pair.OrgID, pair.ID, pair.ActiveNodeID, pair.StandbyNodeID, "before revoking the active node"); err != nil && !errors.Is(err, errPairChanged) {
		return err
	}
	return nil
}

// DeletePair removes nodeID's pair. Revocation calls it under the node lock.
func (s *Service) DeletePair(ctx context.Context, orgID, nodeID int64) error {
	return s.q(ctx).DeleteFleetNodeHAPairForNode(ctx, sqlc.DeleteFleetNodeHAPairForNodeParams{OrgID: orgID, NodeID: nodeID})
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
			if pastGrace && s.activeDown(pair.ActiveNodeID) && s.registry.Connected(pair.StandbyNodeID) {
				err := s.failover(ctx, pair.OrgID, pair.ID, pair.ActiveNodeID, pair.StandbyNodeID, "after its control stream disconnected")
				if err != nil && !errors.Is(err, errPairChanged) && ctx.Err() == nil {
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

func (s *Service) activeDown(nodeID int64) bool {
	since, disconnected := s.registry.DisconnectedSince(nodeID)
	return disconnected && time.Since(since) >= s.failoverDelay
}

func (s *Service) notReadyReason(pair sqlc.ListFleetNodeHAPairsRow) string {
	if !s.registry.Connected(pair.ActiveNodeID) || !s.registry.Connected(pair.StandbyNodeID) {
		return reasonWaitingForNodes
	}
	if s.registry.CommandProtocolUpgradeRequiredForVersion(pair.ActiveNodeID, HAProtocolVersion) ||
		s.registry.CommandProtocolUpgradeRequiredForVersion(pair.StandbyNodeID, HAProtocolVersion) {
		return reasonUpgradeRequired
	}
	return ""
}

// startTransfer claims pairID unless a transfer is running or recently failed.
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

// transferKey has the active node seal its credential key to the standby's
// encryption key and the standby install it. fleetd relays the sealed key and
// compares the fingerprints both nodes report.
func (s *Service) transferKey(ctx context.Context, pair sqlc.ListFleetNodeHAPairsRow) error {
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

// failover moves the active node's devices and discovery attribution to the
// standby and swaps the roles. Locks follow revocation's order: the active
// node's devices, both fleet_node rows, then the pair. errPairChanged means
// another failover or a revoke got there first.
func (s *Service) failover(ctx context.Context, orgID, pairID, activeID, standbyID int64, why string) error {
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
	// Cached miners still route to the old node.
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
