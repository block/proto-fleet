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
		outgoing: make(chan *gatewaypb.ControlCommand, 1),
		done:     make(chan struct{}),
	}
	r.conns[fleetNodeID] = conn
	return &Stream{r: r, fleetNodeID: fleetNodeID, conn: conn, Outgoing: conn.outgoing, Done: conn.done}
}

// Unregister tears the connection down so a blocked Send/handler wakes. No-op if
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

// PublishAck routes an agent ack to the in-flight command.
func (s *Stream) PublishAck(ack *gatewaypb.ControlAck) {
	s.r.deliver(s.fleetNodeID, ack.GetCommandId(), CommandEvent{Ack: ack})
}

// PublishBatch routes an agent discovery batch to the in-flight command.
func (r *Registry) PublishBatch(fleetNodeID int64, commandID string, batch *pairingpb.DiscoverResponse) {
	r.deliver(fleetNodeID, commandID, CommandEvent{Batch: batch})
}

// AdmitReport reserves quota for deviceCount devices against the in-flight
// command. Returns errNoInFlightCommand if commandID isn't in flight, or
// ErrReportQuotaExceeded past maxReportsPerCommand.
func (r *Registry) AdmitReport(fleetNodeID int64, commandID string, deviceCount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return errNoInFlightCommand
	}
	if cmd.reported+deviceCount > maxReportsPerCommand {
		return ErrReportQuotaExceeded
	}
	cmd.reported += deviceCount
	return nil
}

// ReportScopeFor returns the scan-scope matcher for the in-flight command, or
// (nil, false) if commandID isn't in flight. ok=true with a nil matcher means
// the command is in flight but unconstrained. Callers filter reported devices
// through the matcher so a node can't report outside the requested scope.
func (r *Registry) ReportScopeFor(fleetNodeID int64, commandID string) (ReportScope, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return nil, false
	}
	return cmd.scope, true
}

// deliver routes an event to the in-flight command under the mutex. The send is
// non-blocking (events is buffered and never closed); overflow is dropped.
func (r *Registry) deliver(fleetNodeID int64, commandID string, ev CommandEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd := r.inflightFor(fleetNodeID, commandID)
	if cmd == nil {
		return // unknown/stale command_id
	}
	select {
	case cmd.events <- ev:
	default:
		slog.Warn("dropping fleet node control event; operator stream not draining",
			"fleet_node_id", fleetNodeID, "command_id", commandID)
	}
}
