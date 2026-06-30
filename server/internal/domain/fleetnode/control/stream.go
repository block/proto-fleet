package control

import (
	"log/slog"
	"sync"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
)

// Agent/ControlStream side of a Registry entry (fleetnode/gateway handler):
// commands out, acks and batches in, a closed Done means disconnect.

// Stream is the ControlStream handler's handle on its connection.
type Stream struct {
	r           *Registry
	fleetNodeID int64
	conn        *connection
	Outgoing    <-chan *gatewaypb.ControlCommand
	Done        <-chan struct{}
}

// Register installs a connection for fleetNodeID, newest-wins: any existing one
// is evicted via teardown, so its handler wakes on Done and its deferred
// Unregister no-ops by pointer identity.
func (r *Registry) Register(fleetNodeID int64) *Stream {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, exists := r.conns[fleetNodeID]; exists {
		teardown(old)
	}
	conn := &connection{
		outgoing: make(chan *gatewaypb.ControlCommand, outgoingBuffer),
		done:     make(chan struct{}),
		cmds:     make(map[string]*inflightCommand),
	}
	r.conns[fleetNodeID] = conn
	return &Stream{r: r, fleetNodeID: fleetNodeID, conn: conn, Outgoing: conn.outgoing, Done: conn.done}
}

// Unregister tears the connection down so blocked senders/the handler wake. No-op if
// already evicted (newest-wins replacement).
func (s *Stream) Unregister() {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	conn, ok := s.r.conns[s.fleetNodeID]
	if !ok || conn != s.conn {
		return
	}
	teardown(conn)
	delete(s.r.conns, s.fleetNodeID)
}

// PublishAck routes an agent ack to its in-flight command: a report-bearing command
// receives it as the terminal event on `events`; an ack-only command receives it on
// `ack`. Unknown/stale/duplicate command_ids are dropped.
func (s *Stream) PublishAck(ack *gatewaypb.ControlAck) {
	s.r.deliverAck(s.fleetNodeID, ack)
}

// PublishBatch routes an agent discovery batch to the in-flight report-bearing command.
func (r *Registry) PublishBatch(fleetNodeID int64, commandID string, batch *pairingpb.DiscoverResponse) {
	r.deliverEvent(fleetNodeID, commandID, CommandEvent{Batch: batch})
}

// PublishPairResults routes an agent pairing batch to the in-flight command.
func (r *Registry) PublishPairResults(fleetNodeID int64, commandID string, results []*gatewaypb.FleetNodePairResult) {
	r.deliverEvent(fleetNodeID, commandID, CommandEvent{PairResults: results})
}

// AdmitReport reserves quota for deviceCount devices against the in-flight
// report-bearing command of kind want (a discovery command_id can't admit pair
// results or vice versa). Returns errNoInFlightCommand or ErrReportQuotaExceeded.
func (r *Registry) AdmitReport(fleetNodeID int64, commandID string, deviceCount int, want ReportKind) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil || !cmd.reportBearing() || cmd.kind != want {
		return errNoInFlightCommand
	}
	if cmd.reported+deviceCount > cmd.maxReports {
		return ErrReportQuotaExceeded
	}
	cmd.reported += deviceCount
	return nil
}

// ArtifactTransferRelease releases a slot reserved by an AcquireCommandArtifact*
// call. It is safe to call more than once.
type ArtifactTransferRelease func()

// AcquireCommandArtifactUpload reserves one per-node upload slot before the
// stream's first message is read. The returned release is bound to this lease,
// not to the fleet node's replaceable ControlStream connection.
func (r *Registry) AcquireCommandArtifactUpload(fleetNodeID int64) (ArtifactTransferRelease, error) {
	return r.acquireCommandArtifactSlot(fleetNodeID, r.commandArtifactUploads, MaxConcurrentCommandArtifactUploadsPerFleetNode)
}

// AcquireCommandArtifactDownload reserves one per-node download slot before the
// server starts streaming artifact bytes.
func (r *Registry) AcquireCommandArtifactDownload(fleetNodeID int64) (ArtifactTransferRelease, error) {
	return r.acquireCommandArtifactSlot(fleetNodeID, r.commandArtifactDownloads, maxConcurrentCommandArtifactDownloadsPerFleetNode)
}

func (r *Registry) acquireCommandArtifactSlot(fleetNodeID int64, slots map[int64]int, limit int) (ArtifactTransferRelease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conns[fleetNodeID] == nil {
		return nil, ErrNoActiveStream
	}
	if slots[fleetNodeID] >= limit {
		return nil, ErrArtifactTransferLimitExceeded
	}
	slots[fleetNodeID]++

	var once sync.Once
	return func() {
		once.Do(func() {
			r.releaseCommandArtifactSlot(fleetNodeID, slots)
		})
	}, nil
}

func (r *Registry) releaseCommandArtifactSlot(fleetNodeID int64, slots map[int64]int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if slots[fleetNodeID] <= 1 {
		delete(slots, fleetNodeID)
		return
	}
	slots[fleetNodeID]--
}

// AdmitCommandArtifact atomically verifies that a fleet node artifact transfer
// matches the in-flight server-issued command. Admission marks the expectation
// in-progress so concurrent transfers for the same expectation are rejected.
func (r *Registry) AdmitCommandArtifact(fleetNodeID int64, commandID string, want ArtifactExpectation) error {
	_, err := r.AdmitCommandArtifactTransfer(fleetNodeID, commandID, want)
	return err
}

// AdmitCommandArtifactTransfer admits an artifact transfer and returns the
// command's done signal so handlers can abort byte movement if the command is
// removed by completion, timeout, or ControlStream replacement.
func (r *Registry) AdmitCommandArtifactTransfer(fleetNodeID int64, commandID string, want ArtifactExpectation) (<-chan struct{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return nil, errNoInFlightCommand
	}
	exp := cmd.artifactExpectationFor(want)
	if exp == nil {
		return nil, ErrArtifactNotExpected
	}
	if exp.MaxSizeBytes > 0 && want.SizeBytes > exp.MaxSizeBytes {
		return nil, ErrArtifactTooLarge
	}
	if exp.inProgress || exp.completed {
		return nil, ErrArtifactAlreadyTransferred
	}
	if exp.attempts >= maxCommandArtifactTransferAttempts {
		return nil, ErrArtifactTransferAttemptsExceeded
	}
	exp.attempts++
	exp.inProgress = true
	return cmd.done, nil
}

// CompleteCommandArtifactTransfer marks an admitted transfer as completed.
// No-op if the command is gone or the expectation no longer matches.
func (r *Registry) CompleteCommandArtifactTransfer(fleetNodeID int64, commandID string, want ArtifactExpectation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return
	}
	exp := cmd.artifactExpectationFor(want)
	if exp == nil {
		return
	}
	exp.inProgress = false
	exp.completed = true
}

// CompleteCommandArtifactUpload marks an upload as completed and stores the
// artifact reference so a retry can idempotently recover the response while the
// command remains in flight. It returns false if the command or expectation
// disappeared before completion.
func (r *Registry) CompleteCommandArtifactUpload(fleetNodeID int64, commandID string, want ArtifactExpectation, ref *gatewaypb.CommandArtifactRef) bool {
	if want.Direction != ArtifactDirectionUpload || ref == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return false
	}
	exp := cmd.artifactExpectationFor(want)
	if exp == nil {
		return false
	}
	exp.inProgress = false
	exp.completed = true
	exp.uploadRef = cloneCommandArtifactRef(ref)
	return true
}

// CompletedCommandArtifactUpload returns the stored artifact ref for a completed
// upload expectation. ok is false for absent, in-progress, non-upload, or
// not-yet-completed expectations.
func (r *Registry) CompletedCommandArtifactUpload(fleetNodeID int64, commandID string, want ArtifactExpectation) (*gatewaypb.CommandArtifactRef, bool) {
	if want.Direction != ArtifactDirectionUpload {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return nil, false
	}
	exp := cmd.artifactExpectationFor(want)
	if exp == nil || !exp.completed || exp.uploadRef == nil {
		return nil, false
	}
	return cloneCommandArtifactRef(exp.uploadRef), true
}

// AdmitCompletedCommandArtifactUploadRetry reserves a retry for an upload that
// already completed while its command is still in flight. The retry is charged
// against the same attempt cap as fresh uploads and returns the command's done
// signal so the gateway can abort replay drain work if the command disappears.
func (r *Registry) AdmitCompletedCommandArtifactUploadRetry(fleetNodeID int64, commandID string, want ArtifactExpectation) (<-chan struct{}, error) {
	if want.Direction != ArtifactDirectionUpload {
		return nil, ErrArtifactNotExpected
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return nil, errNoInFlightCommand
	}
	exp := cmd.artifactExpectationFor(want)
	if exp == nil || !exp.completed || exp.uploadRef == nil {
		return nil, ErrArtifactAlreadyTransferred
	}
	if exp.inProgress {
		return nil, ErrArtifactAlreadyTransferred
	}
	if exp.attempts >= maxCommandArtifactTransferAttempts {
		return nil, ErrArtifactTransferAttemptsExceeded
	}
	exp.attempts++
	exp.inProgress = true
	return cmd.done, nil
}

// FinishCompletedCommandArtifactUploadRetry releases the in-progress marker for
// a completed-upload retry. No-op if the command disappeared.
func (r *Registry) FinishCompletedCommandArtifactUploadRetry(fleetNodeID int64, commandID string, want ArtifactExpectation) {
	if want.Direction != ArtifactDirectionUpload {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return
	}
	exp := cmd.artifactExpectationFor(want)
	if exp == nil || !exp.completed {
		return
	}
	exp.inProgress = false
}

func cloneCommandArtifactRef(ref *gatewaypb.CommandArtifactRef) *gatewaypb.CommandArtifactRef {
	if ref == nil {
		return nil
	}
	return &gatewaypb.CommandArtifactRef{
		ArtifactId: ref.GetArtifactId(),
		Purpose:    ref.GetPurpose(),
		Filename:   ref.GetFilename(),
		SizeBytes:  ref.GetSizeBytes(),
		Sha256:     ref.GetSha256(),
	}
}

// ReinstateCommandArtifactTransfer clears an in-progress expectation after the
// gateway fails before the transfer completes. No-op if the command is gone or
// the expectation no longer matches.
func (r *Registry) ReinstateCommandArtifactTransfer(fleetNodeID int64, commandID string, want ArtifactExpectation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return
	}
	exp := cmd.artifactExpectationFor(want)
	if exp == nil || exp.completed {
		return
	}
	exp.inProgress = false
}

// ReinstateCommandArtifactUpload clears an admitted upload expectation after the
// gateway failed before it could durably return an artifact reference. No-op if
// the command is gone, the expectation no longer matches, or it is not an upload.
func (r *Registry) ReinstateCommandArtifactUpload(fleetNodeID int64, commandID string, want ArtifactExpectation) {
	if want.Direction != ArtifactDirectionUpload {
		return
	}
	r.ReinstateCommandArtifactTransfer(fleetNodeID, commandID, want)
}

func (c *inflightCommand) artifactExpectationFor(want ArtifactExpectation) *artifactExpectation {
	for i := range c.artifacts {
		exp := &c.artifacts[i]
		if artifactExpectationMatches(exp.ArtifactExpectation, want) {
			return exp
		}
	}
	return nil
}

func artifactExpectationMatches(exp, want ArtifactExpectation) bool {
	if exp.Direction != want.Direction {
		return false
	}
	if exp.Purpose != want.Purpose || exp.Purpose == gatewaypb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_UNSPECIFIED {
		return false
	}
	if exp.Direction == ArtifactDirectionDownload {
		if exp.ArtifactID == "" || want.ArtifactID == "" || exp.ArtifactID != want.ArtifactID {
			return false
		}
	} else if exp.ArtifactID != "" && exp.ArtifactID != want.ArtifactID {
		return false
	}
	if exp.DeviceIdentifier != "" && exp.DeviceIdentifier != want.DeviceIdentifier {
		return false
	}
	return true
}

// PairPersistMeta is the operator context the gateway needs to persist a pair
// result authoritatively, returned by AdmitAndScopePairResults.
type PairPersistMeta struct {
	OrgID      int64
	AssignedBy *int64
}

// AdmitAndScopePairResults is the single atomic gate for the gateway's
// authoritative pair persistence: it returns only results whose device_identifier
// was a dispatched target, consuming each so a node can't replay it. Quota is
// charged per consumed target (not raw rows), so duplicate or out-of-scope rows
// in a batch can't starve later valid reports; consumption itself caps total
// admissions at the dispatched target count. Returns ErrEmptyReport for an empty
// batch or errNoInFlightCommand if commandID isn't an in-flight pair command.
func (r *Registry) AdmitAndScopePairResults(fleetNodeID int64, commandID string, results []*gatewaypb.FleetNodePairResult) ([]*gatewaypb.FleetNodePairResult, PairPersistMeta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil || !cmd.reportBearing() || cmd.kind != ReportKindPair || cmd.pair == nil {
		return nil, PairPersistMeta{}, errNoInFlightCommand
	}
	if len(results) == 0 {
		return nil, PairPersistMeta{}, ErrEmptyReport
	}

	kept := make([]*gatewaypb.FleetNodePairResult, 0, len(results))
	for _, res := range results {
		id := res.GetDeviceIdentifier()
		if _, ok := cmd.pair.Targets[id]; !ok {
			// Outside the dispatched targets or already consumed; anomalous for a node.
			slog.Warn("dropping fleet node pair result outside the requested targets or already seen",
				"fleet_node_id", fleetNodeID, "device_identifier", id)
			continue
		}
		delete(cmd.pair.Targets, id)
		kept = append(kept, res)
	}
	cmd.reported += len(kept)
	return kept, PairPersistMeta{OrgID: cmd.pair.OrgID, AssignedBy: cmd.pair.AssignedBy}, nil
}

// ReinstatePairTargets returns identifiers to the in-flight pair command's target
// set after their persistence failed, so a retried report for the same command can
// be re-admitted; without this, the consume-on-admit replay bar would make a
// transient DB failure permanent for the command's lifetime. No-op for identifiers
// already present or commands no longer in flight.
func (r *Registry) ReinstatePairTargets(fleetNodeID int64, commandID string, identifiers []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil || cmd.kind != ReportKindPair || cmd.pair == nil {
		return
	}
	for _, id := range identifiers {
		if _, ok := cmd.pair.Targets[id]; ok {
			continue
		}
		cmd.pair.Targets[id] = struct{}{}
		cmd.reported--
	}
}

// ReportScopeFor returns the scan-scope matcher for the in-flight report-bearing
// command, or (nil, false) if commandID isn't one. ok=true with a nil matcher means
// the command is in flight but unconstrained. Callers filter reported devices
// through the matcher so a node can't report outside the requested scope.
func (r *Registry) ReportScopeFor(fleetNodeID int64, commandID string) (ReportScope, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil || !cmd.reportBearing() {
		return nil, false
	}
	return cmd.scope, true
}

// deliverEvent routes a batch/ack event to an in-flight report-bearing command under
// the mutex. The send is non-blocking (events is buffered and never closed); overflow
// is dropped.
func (r *Registry) deliverEvent(fleetNodeID int64, commandID string, ev CommandEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil || !cmd.reportBearing() {
		return // unknown/stale command_id, or not report-bearing
	}
	select {
	case cmd.events <- ev:
	default:
		slog.Warn("dropping fleet node control event; operator stream not draining",
			"fleet_node_id", fleetNodeID, "command_id", commandID)
	}
}

// deliverAck routes a terminal ack to its in-flight command under the mutex, by kind.
func (r *Registry) deliverAck(fleetNodeID int64, ack *gatewaypb.ControlAck) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, ack.GetCommandId())
	if cmd == nil {
		return // unknown/stale/duplicate command_id
	}
	if cmd.reportBearing() {
		// The terminal ack must reach the operator even when the batch buffer is
		// full, or RunOnNode strands until DiscoverCommandTimeout. Batches are
		// best-effort, so on a full buffer evict the oldest one to make room. Safe
		// under r.mu: every events producer holds it, so nothing refills the freed
		// slot before the retried send.
		ev := CommandEvent{Ack: ack}
		select {
		case cmd.events <- ev:
		default:
			select {
			case <-cmd.events:
			default:
			}
			select {
			case cmd.events <- ev:
			default:
				slog.Warn("dropping fleet node control ack; operator stream not draining",
					"fleet_node_id", fleetNodeID, "command_id", ack.GetCommandId())
			}
		}
		return
	}
	// ack-only: hand the terminal ack to the SendCommand waiter (cap 1, first wins).
	select {
	case cmd.ack <- ack:
	default:
	}
}
