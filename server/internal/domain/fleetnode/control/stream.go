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

// AdmitReport reserves quota for deviceCount devices against the in-flight
// report-bearing command. Returns errNoInFlightCommand if commandID isn't an
// in-flight report-bearing command, or ErrReportQuotaExceeded past maxReportsPerCommand.
func (r *Registry) AdmitReport(fleetNodeID int64, commandID string, deviceCount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil || !cmd.reportBearing() {
		return errNoInFlightCommand
	}
	if cmd.reported+deviceCount > maxReportsPerCommand {
		return ErrReportQuotaExceeded
	}
	cmd.reported += deviceCount
	return nil
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
