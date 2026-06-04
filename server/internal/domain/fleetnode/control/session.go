package control

import (
	"context"
	"errors"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Operator/DiscoverOnFleetNode side of a Registry entry (fleetnode/admin
// handler): results in, a closed Done means the connection died first.

// Session is the operator's handle while a report-bearing command is in flight.
// Events delivers batches + the terminal ack; Done closes if the connection dies
// first. Caller must Close when done, freeing the command.
type Session struct {
	r           *Registry
	fleetNodeID int64
	cmd         *inflightCommand
}

func (s *Session) Events() <-chan CommandEvent { return s.cmd.events }
func (s *Session) Done() <-chan struct{}       { return s.cmd.done }

// Close frees the command and signals Done. Idempotent and identity-guarded so a
// stale Close can't drop a newer command that reused this command_id.
func (s *Session) Close() {
	s.r.removeCmd(s.fleetNodeID, s.cmd)
}

// Send dispatches a report-bearing command and returns a Session for its batches +
// terminal ack. scope bounds which reported devices the report path will admit for
// this command (nil = unconstrained). Many commands may be in flight per node
// concurrently, so unlike before this no longer rejects a second Send.
func (r *Registry) Send(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand, scope ReportScope) (*Session, error) {
	c := &inflightCommand{
		id:     cmd.GetCommandId(),
		scope:  scope,
		events: make(chan CommandEvent, commandEventBuffer),
		done:   make(chan struct{}),
	}
	outgoing, connDone, err := r.addCmd(fleetNodeID, c)
	if err != nil {
		if errors.Is(err, errDuplicateCommandID) {
			return nil, fleeterror.NewInternalError(err.Error())
		}
		return nil, err // ErrNoActiveStream
	}

	session := &Session{r: r, fleetNodeID: fleetNodeID, cmd: c}
	select {
	case outgoing <- cmd:
		return session, nil
	case <-connDone:
		// connection evicted between addCmd and enqueue
		session.Close()
		return nil, ErrNoActiveStream
	case <-ctx.Done():
		session.Close()
		return nil, fleeterror.NewInternalErrorf("send command: %v", ctx.Err())
	}
}
