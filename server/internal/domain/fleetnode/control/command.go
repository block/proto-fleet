package control

import (
	"context"
	"errors"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// SendCommand dispatches a single ack-only ControlCommand to fleetNodeID and BLOCKS
// until the terminal ControlAck, the connection drops, or ctx expires. No batches,
// no report scope. This is the transport the remote-node Miner adapter calls from
// inside a command-execution worker; many such calls (and a concurrent discovery)
// may be in flight to one node at once.
//
// Returns ErrNoActiveStream if the node has no live ControlStream (callers map to
// FailedPrecondition). The returned *ControlAck is the agent's structured outcome:
// a non-OK code is NOT a Go error here — the caller inspects ack.Code/Succeeded.
func (r *Registry) SendCommand(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	c := &inflightCommand{
		id:   cmd.GetCommandId(),
		ack:  make(chan *gatewaypb.ControlAck, 1), // never closed
		done: make(chan struct{}),
	}
	outgoing, connDone, err := r.addCmd(fleetNodeID, c)
	if err != nil {
		if errors.Is(err, errDuplicateCommandID) {
			return nil, fleeterror.NewInternalError(err.Error())
		}
		return nil, err // ErrNoActiveStream
	}
	// Always free the slot: on ack, disconnect, or ctx expiry.
	defer r.removeCmd(fleetNodeID, c)

	select {
	case outgoing <- cmd:
	case <-connDone:
		return nil, ErrNoActiveStream
	case <-ctx.Done():
		return nil, fleeterror.NewInternalErrorf("send command: %v", ctx.Err())
	}

	select {
	case ack := <-c.ack:
		return ack, nil
	case <-c.done:
		// teardown raced the ack; drain a late ack before giving up so select
		// randomness can't drop a terminal result that landed with the teardown.
		select {
		case ack := <-c.ack:
			return ack, nil
		default:
			return nil, ErrNoActiveStream
		}
	case <-ctx.Done():
		return nil, fleeterror.NewInternalErrorf("await ack: %v", ctx.Err())
	}
}
