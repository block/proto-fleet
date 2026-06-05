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
// terminal ack. scope bounds which reported devices are admitted (nil =
// unconstrained); kind tags the admitting report RPC; pair is non-nil only for
// pairing (gateway persistence context; its target set caps the report quota).
// Many commands may be in flight per node concurrently.
func (r *Registry) Send(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand, scope ReportScope, kind ReportKind, pair *PairMeta) (*Session, error) {
	maxReports := maxReportsPerCommand
	if pair != nil {
		maxReports = len(pair.Targets)
	}
	c := &inflightCommand{
		id:         cmd.GetCommandId(),
		kind:       kind,
		scope:      scope,
		events:     make(chan CommandEvent, commandEventBuffer),
		maxReports: maxReports,
		pair:       pair,
		done:       make(chan struct{}),
	}
	outgoing, connDone, err := r.addCmd(fleetNodeID, c)
	if err != nil {
		if errors.Is(err, errDuplicateCommandID) {
			return nil, fleeterror.NewInternalError(err.Error())
		}
		return nil, err // ErrNoActiveStream
	}

	session := &Session{r: r, fleetNodeID: fleetNodeID, cmd: c}
	if err := r.enqueue(ctx, outgoing, connDone, cmd); err != nil {
		session.Close()
		return nil, err
	}
	return session, nil
}

// enqueue hands cmd to the connection's outbound queue, returning ErrNoActiveStream
// if the connection drops first or an Internal error if ctx expires. The caller owns
// freeing the inflight entry on failure (Session.Close / removeCmd).
func (r *Registry) enqueue(ctx context.Context, outgoing chan<- *gatewaypb.ControlCommand, connDone <-chan struct{}, cmd *gatewaypb.ControlCommand) error {
	select {
	case outgoing <- cmd:
		return nil
	case <-connDone:
		return ErrNoActiveStream
	case <-ctx.Done():
		return fleeterror.NewInternalErrorf("send command: %v", ctx.Err())
	}
}
