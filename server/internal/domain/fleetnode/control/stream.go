package control

import (
	"log/slog"

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
