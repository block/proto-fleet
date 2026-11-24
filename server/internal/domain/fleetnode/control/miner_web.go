package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"buf.build/go/protovalidate"
	"golang.org/x/net/http/httpguts"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
)

const (
	MaxConcurrentMinerWebRequestsPerNode = 4
	MaxConcurrentMinerWebRequests        = 4
	MaxMinerWebResponseBytes             = 64 << 20
	MinerWebChunkBytes                   = 64 << 10
	minerWebProtocolVersion              = pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8
)

type minerWebResult struct {
	head          *pb.MinerWebResponseHead
	body          bytes.Buffer
	complete      bool
	err           error
	abandoned     bool
	receivedFrame bool
}

// SupportsMinerWeb reflects the live connection, not a version string reported
// in a heartbeat. Disconnected or old nodes cannot advertise an embedded view.
func (r *Registry) SupportsMinerWeb(fleetNodeID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn := r.conns[fleetNodeID]
	return conn != nil && conn.maxCommandProtocolVersion >= minerWebProtocolVersion
}

// SupportsMinerWebForDispatch checks the live connection before dispatch and
// records early protocol rejections. Use SupportsMinerWeb for display.
func (r *Registry) SupportsMinerWebForDispatch(fleetNodeID int64) bool {
	return r.SupportsCommandProtocolVersion(fleetNodeID, minerWebProtocolVersion)
}

// SendMinerWeb collects response frames separately from the small terminal ack.
// Neither partial bytes nor an OK ack without an end frame are a successful
// HTTP response. Callers must retain their binding/admission until this returns.
func (r *Registry) SendMinerWeb(ctx context.Context, fleetNodeID int64, cmd *pb.ControlCommand) (response *http.Response, resultErr error) {
	ctx, span := startCommandDispatchSpan(ctx, fleetNodeID, "miner_web", cmd)
	defer func() { finishCommandDispatchSpan(span, resultErr) }()
	var envelope pb.AgentCommand
	if err := proto.Unmarshal(cmd.GetPayload(), &envelope); err != nil {
		return nil, commandNotEnqueued(fmt.Errorf("decode miner web command: %w", err))
	}
	timeout := envelope.GetMinerWeb().GetTimeout()
	if timeout == nil || timeout.CheckValid() != nil || timeout.AsDuration() <= 0 || timeout.AsDuration() > runtimepolicy.MinerWebRequestTimeout {
		return nil, commandNotEnqueued(fmt.Errorf("miner web command requires an execution timeout within %s", runtimepolicy.MinerWebRequestTimeout))
	}
	// Bound server admission and response state independently of the node's
	// execution timer, which starts only after delivery. Keep this local deadline
	// for abandoned requests too; a short browser timeout must not recycle slots
	// fast enough to flood the outbound command queue.
	deadline := time.Now().Add(runtimepolicy.MinerWebResponseTimeout)
	responseCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ctx = responseCtx
	select {
	case r.minerWebSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, commandNotEnqueued(ctx.Err())
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { <-r.minerWebSlots }) }
	detached := false
	defer func() {
		if resultErr != nil && !detached {
			release()
		}
	}()
	c := &inflightCommand{id: cmd.GetCommandId(), ack: make(chan *pb.ControlAck, 1), done: make(chan struct{}), web: &minerWebResult{}}
	outgoing, connDone, err := r.addCmd(fleetNodeID, minerWebProtocolVersion, c)
	if err != nil {
		return nil, commandNotEnqueued(err)
	}
	dispatched, terminal := false, false
	defer func() {
		if dispatched && !terminal {
			// Keep the admission reservation until acknowledgment or the server's
			// lifecycle bound, but free response bytes immediately. Expiry is not
			// proof remote execution stopped: node-local slots bound execution,
			// and callers must preserve uncertain delivery for interrupted writes.
			detached = true
			r.mu.Lock()
			c.web.abandoned = true
			c.web.body = bytes.Buffer{}
			c.web.head = nil
			r.mu.Unlock()
			go func() {
				timer := time.NewTimer(time.Until(deadline))
				defer timer.Stop()
				select {
				case <-c.ack:
				case <-timer.C:
				}
				r.removeCmd(fleetNodeID, c)
				release()
			}()
		} else {
			r.removeCmd(fleetNodeID, c)
		}
	}()
	if err := r.enqueue(ctx, outgoing, connDone, cmd); err != nil {
		return nil, commandNotEnqueued(err)
	}
	dispatched = true
	var ack *pb.ControlAck
	select {
	case ack = <-c.ack:
	case <-c.done:
		select {
		case ack = <-c.ack:
		default:
			return nil, ErrNoActiveStream
		}
	case <-ctx.Done():
		return nil, fmt.Errorf("miner web request canceled: %w", ctx.Err())
	}
	terminal = true
	recordCommandAck(span, ack)
	r.mu.Lock()
	defer r.mu.Unlock()
	if ack.GetCode() != pb.AckCode_ACK_CODE_OK || !ack.GetSucceeded() {
		err := AckFailure(ack, "miner web request")
		if ack.GetCode() == pb.AckCode_ACK_CODE_BUSY && !ack.GetSucceeded() && !c.web.receivedFrame {
			return nil, &commandRejectedBeforeDispatchError{err}
		}
		return nil, err
	}
	if c.web.err != nil {
		return nil, c.web.err
	}
	if c.web.head == nil || !c.web.complete {
		return nil, fleeterror.NewUnavailableErrorf("fleet node returned an incomplete miner response")
	}
	headers := make(http.Header)
	for _, h := range c.web.head.GetHeaders() {
		headers.Add(h.GetName(), h.GetValue())
	}
	// The buffered body determines framing. HEAD needs no synthesized length.
	headers.Del("Content-Length")
	return &http.Response{StatusCode: int(c.web.head.GetStatusCode()), Header: headers, Body: &minerWebBody{ReadCloser: io.NopCloser(bytes.NewReader(c.web.body.Bytes())), release: release}}, nil
}

// PublishMinerWebResponse accepts frames only on the exact authenticated stream
// which owns the outstanding command. It never blocks the control receive loop
// on a browser, and discards all later data after a malformed or oversized frame.
func (s *Stream) PublishMinerWebResponse(frame *pb.MinerWebResponseChunk) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if s.r.conns[s.fleetNodeID] != s.conn {
		return
	}
	c := s.conn.cmds[frame.GetCommandId()]
	if c == nil || c.web == nil {
		return
	}
	c.web.receivedFrame = true
	if c.web.abandoned || c.web.err != nil {
		return
	}
	fail := func(reason string) {
		c.web.err = fleeterror.NewUnavailableErrorf("invalid fleet node miner response: %s", reason)
		c.web.body = bytes.Buffer{}
	}
	if proto.Size(frame) > MinerWebChunkBytes+1024 {
		fail("frame exceeds limit")
		return
	}
	if err := protovalidate.Validate(frame); err != nil {
		fail("invalid frame")
		return
	}
	if c.web.complete {
		fail("data after completion")
		return
	}
	switch part := frame.GetPart().(type) {
	case *pb.MinerWebResponseChunk_Head:
		if c.web.head != nil {
			fail("duplicate headers")
			return
		}
		for _, h := range part.Head.GetHeaders() {
			if !httpguts.ValidHeaderFieldName(h.GetName()) || !httpguts.ValidHeaderFieldValue(h.GetValue()) {
				fail("invalid headers")
				return
			}
		}
		c.web.head = part.Head
	case *pb.MinerWebResponseChunk_Data:
		if c.web.head == nil {
			fail("body before headers")
			return
		}
		if len(part.Data) > MaxMinerWebResponseBytes-c.web.body.Len() {
			fail("body exceeds limit")
			return
		}
		_, _ = c.web.body.Write(part.Data)
	case *pb.MinerWebResponseChunk_End:
		if c.web.head == nil {
			fail("completion before headers")
			return
		}
		c.web.complete = true
	default:
		fail(fmt.Sprintf("unknown frame %T", part))
	}
}

type minerWebBody struct {
	io.ReadCloser
	release func()
}

func (b *minerWebBody) Close() error {
	defer b.release()
	if err := b.ReadCloser.Close(); err != nil {
		return fmt.Errorf("close node response: %w", err)
	}
	return nil
}

// commandRejectedBeforeDispatchError preserves definitive node rejection evidence
// separately from local enqueue failures. Neither outcome started miner I/O.
type commandRejectedBeforeDispatchError struct{ error }

func (e *commandRejectedBeforeDispatchError) Unwrap() error { return e.error }

// IsCommandRejectedBeforeDispatch reports a terminal rejection before miner I/O.
func IsCommandRejectedBeforeDispatch(err error) bool {
	var rejected *commandRejectedBeforeDispatchError
	return errors.As(err, &rejected)
}
