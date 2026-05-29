/*
Package control is the in-memory registry of active ControlStream connections.
Single-instance fleetd only; HA would need a distributed queue.

# Mental model

Two goroutines meet at one connection (fields guarded by Registry.mu) and hand
off through channels; neither touches the other's goroutine:

	agent ControlStream    ── Register() ─►  *Stream   (fleetnode/gateway)
	operator Discover RPC  ── Send()     ─►  *Session  (fleetnode/admin)

	connection
	  outgoing  ──►  command to the agent      (cap 1, never closed)
	  done      ──►  disconnect, wakes both    (closed once on evict)
	  cmd  ─►  inflightCommand                  (nil when idle)
	         events  ◄──  batches + final ack   (buffered, never closed)
	         done    ──►  command end           (closed once)

Invariant: data channels (outgoing, events) are never closed, only GC'd; done
channels are closed once, by their owner (teardown: connection.done + cmd.done;
Session.Close: cmd.done). The command slot is freed by pointer identity
(conn.cmd == s.cmd) so a stale Close can't drop a newer command.
*/
package control

import (
	"errors"
	"sync"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
)

// commandEventBuffer sizes a command's event channel; overflow drops the event
// (logged) rather than blocking the gateway RPC.
const commandEventBuffer = 64

// maxReportsPerCommand caps devices reported per command at the scan ceiling
// (targets × ports): a broad scan isn't truncated, a runaway agent is bounded.
const maxReportsPerCommand = discoverylimits.MaxScanTargets * discoverylimits.MaxPortsPerIP

var (
	// ErrNoActiveStream: no ControlStream for the fleet_node (callers map to FailedPrecondition).
	ErrNoActiveStream = errors.New("no active control stream for fleet_node")

	// errNoInFlightCommand: AdmitReport's command_id isn't in flight. Unexported;
	// callers map it to FailedPrecondition like any non-quota admit failure.
	errNoInFlightCommand = errors.New("no in-flight command for fleet_node")

	// ErrReportQuotaExceeded: a report would exceed maxReportsPerCommand.
	ErrReportQuotaExceeded = errors.New("discovery report quota exceeded for command")
)

// CommandEvent is one message of a command's result stream: exactly one of Batch
// or Ack is set.
type CommandEvent struct {
	Batch *pairingpb.DiscoverResponse
	Ack   *gatewaypb.ControlAck
}

// ReportScope reports whether a device discovered at (ipAddress, port) falls
// within the scan scope an in-flight command requested. The caller supplies it
// to Send; the report path checks every device against it so a node can't report
// devices outside what it was asked to scan. A nil ReportScope is unconstrained.
type ReportScope func(ipAddress, port string) bool

// connection is the server's view of one agent ControlStream, holding at most one
// in-flight command (the agent rejects a second with ACK_CODE_BUSY). Fields
// guarded by Registry.mu.
type connection struct {
	outgoing chan *gatewaypb.ControlCommand // commands to the ControlStream handler (cap 1, never closed)
	done     chan struct{}                  // closed once on evict/Unregister; wakes the handler and a blocked Send
	cmd      *inflightCommand               // the in-flight command, or nil when idle
}

// inflightCommand is the operator side of an in-flight discovery command.
type inflightCommand struct {
	id       string
	scope    ReportScope       // admits only reported devices within the requested scan scope; nil = unconstrained
	events   chan CommandEvent // batches + final ack to the operator (buffered, never closed)
	reported int               // cumulative admitted device count, for quota
	done     chan struct{}     // closed once on teardown; wakes the operator loop
}

type Registry struct {
	mu    sync.Mutex
	conns map[int64]*connection
}

func NewRegistry() *Registry {
	return &Registry{conns: make(map[int64]*connection)}
}

// teardown closes connection.done and cmd.done (if any). Caller holds Registry.mu
// and must then remove/replace the conn so teardown can't run twice.
func teardown(conn *connection) {
	close(conn.done)
	if conn.cmd != nil {
		close(conn.cmd.done)
		conn.cmd = nil
	}
}

// inflightFor returns the command in flight for fleetNodeID iff it matches
// commandID, else nil. Caller holds Registry.mu.
func (r *Registry) inflightFor(fleetNodeID int64, commandID string) *inflightCommand {
	conn := r.conns[fleetNodeID]
	if conn == nil || conn.cmd == nil || conn.cmd.id != commandID {
		return nil
	}
	return conn.cmd
}
