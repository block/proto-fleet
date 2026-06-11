/*
Package control is the in-memory registry of active ControlStream connections.
Single-instance fleetd only; HA would need a distributed queue.

# Mental model

Goroutines meet at one connection (fields guarded by Registry.mu) and hand off
through channels; neither touches the other's goroutine:

	agent ControlStream    ── Register()     ─►  *Stream   (fleetnode/gateway)
	operator Discover RPC  ── Send()         ─►  *Session  (fleetnode/admin)
	command worker         ── SendCommand()  ─►  blocks for the terminal ack

	connection
	  outgoing  ──►  commands to the agent       (buffered, never closed)
	  done      ──►  disconnect, wakes everyone  (closed once on evict)
	  cmds      ─►  command_id → inflightCommand (many concurrent)
	         events  ◄──  batches + terminal ack  (report-bearing; buffered, never closed)
	         ack     ◄──  the terminal ack        (ack-only; cap 1, never closed)
	         done    ──►  command end             (closed once)

A command is one of two shapes, told apart by which result channel it carries:
  - report-bearing (discovery, and the pairing effort's pair): streams device
    batches plus a terminal ack on `events`, and admits reports against
    `scope`/`reported` quota.
  - ack-only (per-miner command): just the terminal ack on `ack`; no reports.

Invariant: data channels (outgoing, events, ack) are never closed, only GC'd; done
channels are closed once, by their owner (teardown closes connection.done + every
cmd.done; Session.Close / SendCommand free their own cmd via removeCmd). A command is
freed by pointer identity (removeCmd) so a stale Close/timeout can't drop a newer
command that reused its command_id.
*/
package control

import (
	"errors"
	"sync"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
)

// commandEventBuffer sizes a report-bearing command's event channel; overflow drops
// the event (logged) rather than blocking the gateway RPC.
const commandEventBuffer = 64

// outgoingBuffer sizes the per-connection outbound command queue. It comfortably
// exceeds the node's command worker-pool ceiling so concurrent Send/SendCommand to
// one node enqueue without serializing behind the gateway's single drain loop.
const outgoingBuffer = 64

// maxReportsPerCommand caps devices reported per command at the scan ceiling
// (targets × ports): a broad scan isn't truncated, a runaway agent is bounded.
const maxReportsPerCommand = discoverylimits.MaxScanTargets * discoverylimits.MaxPortsPerIP

var (
	// ErrNoActiveStream: no ControlStream for the fleet_node (callers map to FailedPrecondition).
	ErrNoActiveStream = errors.New("no active control stream for fleet_node")

	// errNoInFlightCommand: AdmitReport's command_id isn't an in-flight report-bearing
	// command. Unexported; callers map it to FailedPrecondition like any non-quota admit failure.
	errNoInFlightCommand = errors.New("no in-flight command for fleet_node")

	// ErrReportQuotaExceeded: a report would exceed the command's report quota.
	ErrReportQuotaExceeded = errors.New("report quota exceeded for command")

	// ErrEmptyReport: a pair report carried no results (consumes no quota);
	// callers map it to InvalidArgument.
	ErrEmptyReport = errors.New("report carried no results")

	// errDuplicateCommandID: a command_id is already in flight for the fleet_node.
	// id.GenerateID() makes this practically impossible; callers map it to Internal.
	errDuplicateCommandID = errors.New("duplicate command_id in flight for fleet_node")
)

// CommandEvent is one message of a command's result stream: exactly one of Batch,
// PairResults, or Ack is set.
type CommandEvent struct {
	Batch       *pairingpb.DiscoverResponse
	PairResults []*gatewaypb.FleetNodePairResult
	Ack         *gatewaypb.ControlAck
}

// ReportScope reports whether a device discovered at (ipAddress, port) falls
// within the scan scope an in-flight command requested. The caller supplies it
// to Send; the report path checks every device against it so a node can't report
// devices outside what it was asked to scan. A nil ReportScope is unconstrained.
type ReportScope func(ipAddress, port string) bool

// ReportKind tags a report-bearing command so a discovery command_id admits only
// discovered-device reports and a pair command_id only pair results -- otherwise an
// authenticated node could poison inventory across the wrong command.
type ReportKind int

const (
	ReportKindDiscovery ReportKind = iota
	ReportKindPair
)

// PairMeta is the operator context a pair command carries so the gateway can
// persist results authoritatively, scope them to the dispatched targets, and bound
// the report quota. nil for discovery.
type PairMeta struct {
	OrgID      int64
	AssignedBy *int64              // operator user id; nullable end-to-end
	Targets    map[string]struct{} // dispatched device identifiers; also the report quota
}

// connection is the server's view of one agent ControlStream. It can hold many
// concurrent in-flight commands keyed by command_id. Fields guarded by Registry.mu.
type connection struct {
	outgoing chan *gatewaypb.ControlCommand // commands to the ControlStream handler (buffered, never closed)
	done     chan struct{}                  // closed once on evict/Unregister; wakes the handler and blocked senders
	cmds     map[string]*inflightCommand    // in-flight commands by command_id
}

// inflightCommand is the operator/worker side of one in-flight command. Its result
// channel determines its shape: report-bearing commands carry `events` (plus
// scope/reported); ack-only commands carry `ack`.
type inflightCommand struct {
	id string

	// report-bearing (events != nil): discovery/pairing batches + terminal ack.
	kind       ReportKind        // which gateway report RPC may admit reports for this command
	scope      ReportScope       // admits only reported devices within the requested scope; nil = unconstrained
	events     chan CommandEvent // buffered, never closed
	reported   int               // cumulative admitted device count, for quota
	maxReports int               // per-command report quota: scan ceiling for discovery, target count for pair

	// pair-only: gateway persistence metadata, scoped to the dispatched targets
	// (consuming each to bar replay). nil for discovery/ack-only commands.
	pair *PairMeta

	// ack-only (ack != nil): the terminal ack for a per-miner command.
	ack chan *gatewaypb.ControlAck // cap 1, never closed

	done chan struct{} // closed once on teardown/free; wakes the waiter
}

// reportBearing is true for commands that stream device reports (discovery, pairing)
// and receive their terminal ack on `events`; false for ack-only per-miner commands.
func (c *inflightCommand) reportBearing() bool { return c.events != nil }

type Registry struct {
	mu    sync.Mutex
	conns map[int64]*connection
}

func NewRegistry() *Registry {
	return &Registry{conns: make(map[int64]*connection)}
}

// ConnectedFleetNodeIDs returns the fleet_node IDs with an active ControlStream
// right now. Used by fan-out discovery to target only nodes the server can reach;
// callers intersect this with the org's CONFIRMED nodes. Order is unspecified.
func (r *Registry) ConnectedFleetNodeIDs() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]int64, 0, len(r.conns))
	for id := range r.conns {
		ids = append(ids, id)
	}
	return ids
}

// teardown closes connection.done and every in-flight command's done. Caller holds
// Registry.mu and must then remove/replace the conn so teardown can't run twice.
func teardown(conn *connection) {
	close(conn.done)
	for id, c := range conn.cmds {
		close(c.done)
		delete(conn.cmds, id)
	}
}

// inflightFor returns the command in flight for (fleetNodeID, commandID), or nil.
// Caller holds Registry.mu.
func (r *Registry) inflightFor(fleetNodeID int64, commandID string) *inflightCommand {
	conn := r.conns[fleetNodeID]
	if conn == nil {
		return nil
	}
	return conn.cmds[commandID] // nil if absent
}

// addCmd registers c under its command_id, returning errNoActiveStream if the node
// has no connection or errDuplicateCommandID on a colliding id. On success it also
// returns the connection's outgoing/done channels so the caller can enqueue without
// re-locking. Caller must NOT hold Registry.mu.
func (r *Registry) addCmd(fleetNodeID int64, c *inflightCommand) (chan *gatewaypb.ControlCommand, chan struct{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn, ok := r.conns[fleetNodeID]
	if !ok {
		return nil, nil, ErrNoActiveStream
	}
	if _, dup := conn.cmds[c.id]; dup {
		return nil, nil, errDuplicateCommandID
	}
	conn.cmds[c.id] = c
	return conn.outgoing, conn.done, nil
}

// removeCmd deletes c from its connection iff the stored pointer is still c (identity
// guard so a stale Close/timeout can't drop a newer command that reused the id) and
// closes c.done. Idempotent: a no-op if teardown already freed it.
func (r *Registry) removeCmd(fleetNodeID int64, c *inflightCommand) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn := r.conns[fleetNodeID]
	if conn == nil {
		return
	}
	if cur, ok := conn.cmds[c.id]; ok && cur == c {
		delete(conn.cmds, c.id)
		close(c.done)
	}
}
