// Package fleetnodecontrol holds the in-memory registry of active
// ControlStream connections. Single-instance fleetd only; HA fleetd would
// need a distributed task queue.
package fleetnodecontrol

import (
	"context"
	"errors"
	"sync"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// CommandEvent carries one message of a command's result stream. Exactly
// one of Batch or Ack is set per event.
type CommandEvent struct {
	Batch *pairingpb.DiscoverResponse
	Ack   *gatewaypb.ControlAck
}

type Registry struct {
	mu      sync.Mutex
	streams map[int64]*nodeStream
}

func NewRegistry() *Registry {
	return &Registry{streams: make(map[int64]*nodeStream)}
}

type nodeStream struct {
	outgoing chan *gatewaypb.ControlCommand
	commands map[string]chan CommandEvent
}

type Stream struct {
	r           *Registry
	fleetNodeID int64
	Outgoing    <-chan *gatewaypb.ControlCommand
}

// Register reserves a slot for fleetNodeID. A second concurrent connection
// from the same node (e.g. agent reconnect before the server noticed the
// old stream died) returns FailedPrecondition so the agent backs off.
func (r *Registry) Register(fleetNodeID int64) (*Stream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.streams[fleetNodeID]; exists {
		return nil, fleeterror.NewFailedPreconditionError("fleet node already has an active control stream")
	}
	outgoing := make(chan *gatewaypb.ControlCommand, 1)
	ns := &nodeStream{
		outgoing: outgoing,
		commands: make(map[string]chan CommandEvent),
	}
	r.streams[fleetNodeID] = ns
	return &Stream{r: r, fleetNodeID: fleetNodeID, Outgoing: outgoing}, nil
}

// Unregister closes all in-flight command channels so blocked dispatchers
// wake up and fail their operator streams instead of hanging.
func (s *Stream) Unregister() {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	ns, ok := s.r.streams[s.fleetNodeID]
	if !ok {
		return
	}
	for _, ch := range ns.commands {
		close(ch)
	}
	delete(s.r.streams, s.fleetNodeID)
}

func (s *Stream) PublishAck(ack *gatewaypb.ControlAck) {
	s.r.publish(s.fleetNodeID, ack.GetCommandId(), CommandEvent{Ack: ack})
}

// ErrNoActiveStream is returned by Send when the target fleet_node has
// no ControlStream connected. Mapped to FailedPrecondition by callers.
var ErrNoActiveStream = errors.New("no active control stream for fleet_node")

// Send dispatches a command to the named fleet_node and returns a channel
// that receives the discovery batches (via PublishBatch) and the final
// ControlAck. The channel closes when the caller calls cleanup or the
// stream disconnects.
func (r *Registry) Send(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand) (<-chan CommandEvent, func(), error) {
	r.mu.Lock()
	ns, ok := r.streams[fleetNodeID]
	if !ok {
		r.mu.Unlock()
		return nil, nil, ErrNoActiveStream
	}
	if _, exists := ns.commands[cmd.GetCommandId()]; exists {
		r.mu.Unlock()
		return nil, nil, fleeterror.NewFailedPreconditionError("command_id already in flight for fleet_node")
	}
	events := make(chan CommandEvent, 16)
	ns.commands[cmd.GetCommandId()] = events
	r.mu.Unlock()

	cleanup := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		ns2 := r.streams[fleetNodeID]
		if ns2 == nil {
			return
		}
		// Compare-by-value so a stale cleanup can't drop a fresh entry
		// that re-used the same command_id after Unregister/Register.
		if ns2.commands[cmd.GetCommandId()] == events {
			delete(ns2.commands, cmd.GetCommandId())
			close(events)
		}
	}

	select {
	case ns.outgoing <- cmd:
		return events, cleanup, nil
	case <-ctx.Done():
		cleanup()
		return nil, nil, fleeterror.NewInternalErrorf("send command: %v", ctx.Err())
	}
}

func (r *Registry) PublishBatch(fleetNodeID int64, commandID string, batch *pairingpb.DiscoverResponse) {
	r.publish(fleetNodeID, commandID, CommandEvent{Batch: batch})
}

// publish routes an event to the in-flight command's channel. Non-blocking:
// dropping is safer than stalling the gateway RPC. Stale command_ids are a
// silent no-op (the only path that hits this is an agent reporting against
// a command that has already cleaned up).
func (r *Registry) publish(fleetNodeID int64, commandID string, ev CommandEvent) {
	r.mu.Lock()
	ns := r.streams[fleetNodeID]
	if ns == nil {
		r.mu.Unlock()
		return
	}
	ch := ns.commands[commandID]
	r.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- ev:
	default:
	}
}
