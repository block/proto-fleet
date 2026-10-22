package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

const haRenewInterval = 5 * time.Second
const haMaxLease = 30 * time.Second
const haSafetyMargin = 5 * time.Second

var errHAAuthorityLost = errors.New("HA control authority lost after command dispatch")

type haPendingRequest struct {
	sent     time.Time
	sequence uint64
}

// haAuthority is deliberately process/session local: a disk snapshot never
// carries authority into a new incarnation or control connection.
type haAuthority struct {
	mu                                sync.Mutex
	nodeID                            int64
	instanceID, sessionID             string
	sequence, applied, workerSequence uint64
	pending                           map[string]haPendingRequest
	current                           *pb.HALeaseResponse
	assignment                        *pb.ControlAccepted
	deadline                          time.Time
	timer                             *time.Timer
	workers                           map[uint64]context.CancelCauseFunc
	closed                            bool
	expired                           chan struct{}
}

func newHAAuthority(nodeID int64, instanceID, sessionID string) *haAuthority {
	return &haAuthority{nodeID: nodeID, instanceID: instanceID, sessionID: sessionID, pending: make(map[string]haPendingRequest), workers: make(map[uint64]context.CancelCauseFunc), expired: make(chan struct{})}
}

// Accepted supplies assignment scope without conferring a timed HA grant.
// Standalone sessions retain this scope for key export without lease traffic.
func (a *haAuthority) accept(accepted *pb.ControlAccepted) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assignment = accepted
	a.current = &pb.HALeaseResponse{OwnerNodeId: accepted.GetHaOwnerNodeId(), OrgId: accepted.GetOrgId(), Preparing: accepted.GetPreparing()}
	if accepted.GetHaOwnerNodeId() > 0 && !accepted.GetPreparing() {
		now := time.Now()
		a.setDeadlineLocked(now.Add(haMaxLease-haSafetyMargin), now)
	}
}
func (r *RunCmd) processInstanceID() (string, error) {
	r.haInstanceOnce.Do(func() {
		b := make([]byte, 16)
		_, r.haInstanceErr = rand.Read(b)
		r.haInstanceID = hex.EncodeToString(b)
	})
	return r.haInstanceID, r.haInstanceErr
}
func (a *haAuthority) request(now time.Time) *pb.HALeaseRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expireLocked(now)
	if a.closed {
		return nil
	}
	a.sequence++
	id := fmt.Sprintf("%s-%d", a.instanceID, a.sequence)
	// Keep correlation bounded if a server stops responding indefinitely.
	for key, request := range a.pending {
		if now.Sub(request.sent) >= haMaxLease {
			delete(a.pending, key)
		}
	}
	a.pending[id] = haPendingRequest{sent: now, sequence: a.sequence}
	return &pb.HALeaseRequest{RequestId: id, AcknowledgedEpoch: a.acknowledgedEpochLocked()}
}
func (a *haAuthority) acknowledgedEpoch() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.acknowledgedEpochLocked()
}
func (a *haAuthority) acknowledgedEpochLocked() int64 {
	if a.current != nil && a.current.GetGranted() && !a.current.GetPreparing() {
		return a.current.GetLeaseEpoch()
	}
	return 0
}
func (a *haAuthority) apply(response *pb.HALeaseResponse, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expireLocked(now)
	pending, ok := a.pending[response.GetRequestId()]
	if !ok || a.closed || pending.sequence <= a.applied || response.GetInstanceId() != a.instanceID || response.GetControlSessionId() != a.sessionID || response.GetOrgId() <= 0 {
		return false
	}
	delete(a.pending, response.GetRequestId())
	if a.assignment != nil && (a.assignment.GetHaOwnerNodeId() == 0 || a.assignment.GetPreparing() || response.GetOwnerNodeId() != a.assignment.GetHaOwnerNodeId() || response.GetOrgId() != a.assignment.GetOrgId() || response.GetPreparing()) {
		return false
	}
	if response.GetLeaseDurationMs() <= haSafetyMargin.Milliseconds() {
		return false
	}
	duration := time.Duration(min(response.GetLeaseDurationMs(), haMaxLease.Milliseconds())) * time.Millisecond
	// Measure authority from the original local monotonic send time, not from a
	// delayed receipt or a wall-clock timestamp supplied by the server.
	deadline := pending.sent.Add(duration - haSafetyMargin)
	if duration <= haSafetyMargin || !now.Before(deadline) {
		return false
	}
	if a.current != nil && response.GetOwnerNodeId() == a.current.GetOwnerNodeId() && response.GetLeaseEpoch() < a.current.GetLeaseEpoch() {
		return false
	}
	if response.GetGranted() && (response.GetPreparing() || response.GetOwnerNodeId() <= 0 || response.GetActiveNodeId() != a.nodeID || response.GetLeaseEpoch() <= 0) {
		return false
	}
	selfHeldDenial := !response.GetGranted() && !response.GetPreparing() && response.GetActiveNodeId() == a.nodeID
	if selfHeldDenial {
		return false
	}
	if a.current != nil && (a.current.GetOwnerNodeId() != response.GetOwnerNodeId() || a.current.GetLeaseEpoch() != response.GetLeaseEpoch() || a.current.GetGranted() != response.GetGranted() || a.current.GetPreparing() != response.GetPreparing()) {
		a.cancelWorkersLocked()
	}
	a.current = response
	a.applied = pending.sequence
	a.setDeadlineLocked(deadline, now)
	return true
}

func (a *haAuthority) setDeadlineLocked(deadline, now time.Time) {
	a.deadline = deadline
	if a.timer != nil {
		a.timer.Stop()
	}
	a.timer = time.AfterFunc(deadline.Sub(now), func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.deadline.Equal(deadline) {
			a.expireLocked(deadline)
		}
	})
}
func (a *haAuthority) expireLocked(now time.Time) {
	if !a.closed && !a.deadline.IsZero() && !now.Before(a.deadline) {
		a.closed = true
		a.current = nil
		a.cancelWorkersLocked()
		close(a.expired)
	}
}
func (a *haAuthority) cancelWorkersLocked() {
	for _, cancel := range a.workers {
		cancel(errHAAuthorityLost)
	}
}
func (a *haAuthority) close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	a.current = nil
	if a.timer != nil {
		a.timer.Stop()
	}
	a.cancelWorkersLocked()
}
func (a *haAuthority) allowed(command *pb.ControlCommand, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.allowedLocked(command, now)
}

func (a *haAuthority) allowedLocked(command *pb.ControlCommand, now time.Time) bool {
	a.expireLocked(now)
	if a.closed {
		return false
	}
	if a.current == nil || a.current.GetPreparing() {
		return false
	}
	if a.current.GetOwnerNodeId() == 0 {
		return command.GetHaOwnerNodeId() == 0 && command.GetHaLeaseEpoch() == 0
	}
	return a.current.GetGranted() && command.GetHaOwnerNodeId() == a.current.GetOwnerNodeId() && command.GetHaLeaseEpoch() == a.current.GetLeaseEpoch() && command.GetIntendedInstanceId() == a.instanceID && command.GetControlSessionId() == a.sessionID
}
func (a *haAuthority) start(parent context.Context, command *pb.ControlCommand, now time.Time) (context.Context, func(), bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.allowedLocked(command, now) {
		return nil, func() {}, false
	}
	ctx, cancel := context.WithCancelCause(parent)
	a.workerSequence++
	id := a.workerSequence
	a.workers[id] = cancel
	return ctx, func() { cancel(context.Canceled); a.mu.Lock(); delete(a.workers, id); a.mu.Unlock() }, true
}
func (a *haAuthority) transferScope(now time.Time) (*pb.HALeaseResponse, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expireLocked(now)
	if a.closed || a.current == nil {
		return nil, false
	}
	return a.current, true
}

type haOwnerNodeIDKey struct{}

func discoveryOwnerNodeID(ctx context.Context, physicalNodeID int64) int64 {
	if ownerID, ok := ctx.Value(haOwnerNodeIDKey{}).(int64); ok && ownerID > 0 {
		return ownerID
	}
	return physicalNodeID
}

// Every completion after dispatch passes through this adapter. Losing local
// authority makes an ambiguous failure terminal, distinct from a command-local
// timeout or a refusal before the worker starts. A terminal result sent before
// authority loss remains unchanged; no cancellation callback emits another ACK.
type haCommandAcker struct {
	ctx   context.Context //nolint:containedctx // One worker's execution context, used only while that worker runs.
	inner acker
}

func (a *haCommandAcker) Send(request *pb.ControlStreamRequest) error {
	if ack := request.GetAck(); ack != nil && errors.Is(context.Cause(a.ctx), errHAAuthorityLost) && ambiguousHACompletion(ack) {
		request = &pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{CommandId: ack.GetCommandId(), Code: pb.AckCode_ACK_CODE_COMMAND_INTERRUPTED, ErrorMessage: "command interrupted after dispatch: HA authority lost; physical outcome unknown"}}}
	}
	return a.inner.Send(request)
}

func ambiguousHACompletion(ack *pb.ControlAck) bool {
	// Positively completed success and explicit admission/auth/protocol refusals
	// retain their evidence even when cancellation races acknowledgement delivery.
	code := ack.GetCode()
	return code == pb.AckCode_ACK_CODE_INTERNAL || code == pb.AckCode_ACK_CODE_SCAN_FAILED || code == pb.AckCode_ACK_CODE_REPORT_FAILED || code == pb.AckCode_ACK_CODE_PARTIAL
}
