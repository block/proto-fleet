package control

import (
	"context"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Operator/DiscoverOnFleetNode side of a Registry entry (fleetnode/admin
// handler): results in, a closed Done means the connection died first.

// Session is the operator's handle while a command is in flight. Events delivers
// batches + the final ack; Done closes if the connection dies first. Caller must
// Close when done, freeing the slot for the next Send.
type Session struct {
	r           *Registry
	fleetNodeID int64
	cmd         *inflightCommand
}

func (s *Session) Events() <-chan CommandEvent { return s.cmd.events }
func (s *Session) Done() <-chan struct{}       { return s.cmd.done }

// Close frees the command slot and signals Done. Idempotent and identity-guarded
// so a stale Close can't drop a newer command.
func (s *Session) Close() {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	conn := s.r.conns[s.fleetNodeID]
	if conn != nil && conn.cmd == s.cmd {
		conn.cmd = nil
		close(s.cmd.done)
	}
}

// Send dispatches a command and returns a Session for its batches + final ack.
// scope bounds which reported devices the report path will admit for this
// command (nil = unconstrained). Only one command runs per node, so a second
// Send returns FailedPrecondition.
func (r *Registry) Send(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand, scope ReportScope) (*Session, error) {
	r.mu.Lock()
	conn, ok := r.conns[fleetNodeID]
	if !ok {
		r.mu.Unlock()
		return nil, ErrNoActiveStream
	}
	if conn.cmd != nil {
		r.mu.Unlock()
		return nil, fleeterror.NewFailedPreconditionError("a command is already in flight for fleet_node")
	}
	c := &inflightCommand{
		id:     cmd.GetCommandId(),
		scope:  scope,
		events: make(chan CommandEvent, commandEventBuffer),
		done:   make(chan struct{}),
	}
	conn.cmd = c
	outgoing, connDone := conn.outgoing, conn.done
	r.mu.Unlock()

	session := &Session{r: r, fleetNodeID: fleetNodeID, cmd: c}
	select {
	case outgoing <- cmd:
		return session, nil
	case <-connDone:
		// connection evicted between lookup and send
		session.Close()
		return nil, ErrNoActiveStream
	case <-ctx.Done():
		session.Close()
		return nil, fleeterror.NewInternalErrorf("send command: %v", ctx.Err())
	}
}
