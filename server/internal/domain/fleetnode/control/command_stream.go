package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	// MaxConcurrentCommandStreamsPerNode bounds the streaming commands one node
	// relays at once. Each holds a node command slot until its viewer leaves or
	// its lifetime passes.
	MaxConcurrentCommandStreamsPerNode = 4
	// MaxCommandFrameBytes bounds one frame's payload. A full 4,096-ASIC board
	// frame with its layout encodes to well under this.
	MaxCommandFrameBytes = 512 << 10

	// CommandStreamProtocolVersion is the first node protocol that sends
	// CommandFrame and accepts ControlCancel.
	CommandStreamProtocolVersion = gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V17

	// cancelBuffer sizes a connection's pending ControlCancel queue. A cancel
	// dropped on overflow is sent again when the node's next frame for the
	// command arrives.
	cancelBuffer = 64
)

// ErrCommandStreamCapacity reports that the node already relays its maximum
// number of streaming commands. The command was not sent.
var ErrCommandStreamCapacity = errors.New("fleet node command stream capacity reached")

// CommandStream describes one streaming command: a command whose node reports
// frames before its terminal ack.
type CommandStream struct {
	// Kind names the command in traces.
	Kind string
	// MinimumVersion is the node protocol the command itself needs. Streaming
	// always needs at least CommandStreamProtocolVersion.
	MinimumVersion gatewaypb.CommandProtocolVersion
	// OnFrame receives each relayed frame payload. An error ends the stream
	// and cancels the command on the node.
	OnFrame func(payload []byte) error
	// Coalesce is called when a frame replaces one the caller has not read
	// yet. It returns the payload to keep, so state that only the skipped
	// frame carried can move onto its replacement. nil keeps next as is.
	Coalesce func(skipped, next []byte) []byte
}

// commandFrames holds the newest unread frame of one streaming command. Only
// the owning connection's receive loop writes it, and only the command's
// caller reads it.
type commandFrames struct {
	mu       sync.Mutex
	coalesce func(skipped, next []byte) []byte
	payload  []byte
	unread   bool
	invalid  bool
	// ready wakes the caller (cap 1, never closed).
	ready chan struct{}
}

func (f *commandFrames) put(payload []byte) {
	f.mu.Lock()
	switch {
	case len(payload) > MaxCommandFrameBytes:
		f.invalid = true
	case f.unread && f.coalesce != nil:
		f.payload = f.coalesce(f.payload, payload)
	default:
		f.payload = payload
	}
	f.unread = true
	f.mu.Unlock()
	select {
	case f.ready <- struct{}{}:
	default:
	}
}

// take returns the unread frame, if any. An oversized frame is reported as
// unavailable and stays reported.
func (f *commandFrames) take() ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.invalid {
		return nil, false, fleeterror.NewUnavailableErrorf("fleet node sent an oversized command frame")
	}
	if !f.unread {
		return nil, false, nil
	}
	payload := f.payload
	f.payload, f.unread = nil, false
	return payload, true, nil
}

func commandStreamsLocked(conn *connection) int {
	count := 0
	for _, pending := range conn.cmds {
		if pending.frames != nil {
			count++
		}
	}
	return count
}

// StreamCommand dispatches a streaming command and calls stream.OnFrame with
// each frame payload the node relays, until the node's terminal ack. A caller
// that falls behind gets the newest frame, after stream.Coalesce has seen the
// one it replaces. When ctx ends, OnFrame fails or the node sends an oversized
// frame, it asks the node to cancel the command and returns without waiting
// for the ack. A node already at MaxConcurrentCommandStreamsPerNode is refused
// with ErrCommandStreamCapacity before anything is sent.
func (r *Registry) StreamCommand(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand, stream CommandStream) (resultAck *gatewaypb.ControlAck, resultErr error) {
	ctx, span := startCommandDispatchSpan(ctx, fleetNodeID, stream.Kind, cmd)
	defer func() { finishCommandDispatchSpan(span, resultErr) }()

	c := &inflightCommand{
		id:     cmd.GetCommandId(),
		ack:    make(chan *gatewaypb.ControlAck, 1),
		frames: &commandFrames{coalesce: stream.Coalesce, ready: make(chan struct{}, 1)},
		done:   make(chan struct{}),
	}
	outgoing, connDone, err := r.addCmd(fleetNodeID, max(stream.MinimumVersion, CommandStreamProtocolVersion), c)
	if err != nil {
		if errors.Is(err, errDuplicateCommandID) {
			err = fleeterror.NewInternalError(err.Error())
		}
		return nil, commandNotEnqueued(err)
	}
	// Runs after any cancel below, so the cancel still finds the command.
	defer r.removeCmd(fleetNodeID, c)
	if err := r.enqueue(ctx, outgoing, connDone, cmd); err != nil {
		return nil, commandNotEnqueued(err)
	}

	relay := func() error {
		payload, ok, err := c.frames.take()
		if err != nil || !ok {
			return err
		}
		return stream.OnFrame(payload)
	}
	for {
		select {
		case <-c.frames.ready:
			if err := relay(); err != nil {
				r.cancelOnNode(fleetNodeID, c)
				return nil, err
			}
		case ack := <-c.ack:
			// The node sends its frames before its ack; relay one still unread.
			if err := relay(); err != nil {
				return nil, err
			}
			recordCommandAck(span, ack)
			return ack, nil
		case <-c.done:
			select {
			case ack := <-c.ack:
				recordCommandAck(span, ack)
				return ack, nil
			default:
				return nil, ErrNoActiveStream
			}
		case <-ctx.Done():
			r.cancelOnNode(fleetNodeID, c)
			return nil, fmt.Errorf("command stream ended: %w", context.Cause(ctx))
		}
	}
}

// cancelOnNode queues a ControlCancel for c while c is still in flight.
func (r *Registry) cancelOnNode(fleetNodeID int64, c *inflightCommand) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn := r.conns[fleetNodeID]
	if conn == nil || conn.cmds[c.id] != c {
		return
	}
	queueCancelLocked(fleetNodeID, conn, c.id)
}

func queueCancelLocked(fleetNodeID int64, conn *connection, commandID string) {
	select {
	case conn.cancels <- commandID:
	default:
		slog.Warn("dropping fleet node command cancel; control stream not draining",
			"fleet_node_id", fleetNodeID, "command_id", commandID)
	}
}

// PublishCommandFrame accepts frames only on the exact authenticated stream
// that owns the outstanding streaming command, and never blocks the control
// receive loop. A frame for a command the server no longer waits for is
// answered with another cancel, which covers a cancel that was dropped or that
// reached the node before the command did.
func (s *Stream) PublishCommandFrame(frame *gatewaypb.CommandFrame) {
	s.r.mu.Lock()
	var frames *commandFrames
	if s.r.conns[s.fleetNodeID] == s.conn {
		if c := s.conn.cmds[frame.GetCommandId()]; c != nil {
			frames = c.frames
		} else if frame.GetCommandId() != "" {
			queueCancelLocked(s.fleetNodeID, s.conn, frame.GetCommandId())
		}
	}
	s.r.mu.Unlock()
	if frames == nil {
		return
	}
	if len(frame.GetPayload()) > MaxCommandFrameBytes {
		slog.Warn("refusing oversized command frame from fleet node",
			"fleet_node_id", s.fleetNodeID, "command_id", frame.GetCommandId(), "bytes", len(frame.GetPayload()))
	}
	// Coalescing runs outside the registry lock, so one slow stream cannot
	// hold up other nodes.
	frames.put(frame.GetPayload())
}
