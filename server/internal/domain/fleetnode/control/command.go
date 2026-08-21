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
// a non-OK code is NOT a Go error here; the caller inspects ack.Code/Succeeded.
func (r *Registry) SendCommand(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	return r.SendCommandWithArtifacts(ctx, fleetNodeID, cmd, nil)
}

// SendCommandWithArtifacts dispatches an ack-only command with optional
// artifact-transfer expectations attached to its command_id.
func (r *Registry) SendCommandWithArtifacts(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand, artifacts []ArtifactExpectation) (*gatewaypb.ControlAck, error) {
	ack, _, err := r.SendCommandWithArtifactResults(ctx, fleetNodeID, cmd, artifacts)
	return ack, err
}

// SendCommandWithArtifactResults dispatches an ack-only command with optional
// artifact-transfer expectations and returns any completed upload refs alongside
// the terminal ack. Refs are snapshotted before the in-flight command is removed.
func (r *Registry) SendCommandWithArtifactResults(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand, artifacts []ArtifactExpectation) (*gatewaypb.ControlAck, []*gatewaypb.CommandArtifactRef, error) {
	c := &inflightCommand{
		id:        cmd.GetCommandId(),
		ack:       make(chan *gatewaypb.ControlAck, 1), // never closed
		artifacts: cloneArtifactExpectations(artifacts),
		done:      make(chan struct{}),
	}
	outgoing, connDone, err := r.addCmd(fleetNodeID, c)
	if err != nil {
		if errors.Is(err, errDuplicateCommandID) {
			return nil, nil, fleeterror.NewInternalError(err.Error())
		}
		return nil, nil, err // ErrNoActiveStream
	}
	// Always free the slot: on ack, disconnect, or ctx expiry.
	defer r.removeCmd(fleetNodeID, c)

	if err := r.enqueue(ctx, outgoing, connDone, cmd); err != nil {
		return nil, nil, err
	}

	select {
	case ack := <-c.ack:
		refs, resultErr := r.completedUploadRefs(ack, c)
		return ack, refs, resultErr
	case <-c.done:
		// teardown raced the ack; drain a late ack before giving up so select
		// randomness can't drop a terminal result that landed with the teardown.
		select {
		case ack := <-c.ack:
			refs, resultErr := r.completedUploadRefs(ack, c)
			return ack, refs, resultErr
		default:
			return nil, nil, ErrNoActiveStream
		}
	case <-ctx.Done():
		return nil, nil, fleeterror.NewInternalErrorf("await ack: %w", ctx.Err())
	}
}

func (r *Registry) completedUploadRefs(ack *gatewaypb.ControlAck, c *inflightCommand) ([]*gatewaypb.CommandArtifactRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	refs := make([]*gatewaypb.CommandArtifactRef, 0, len(c.artifacts))
	missingUploads := 0
	for _, exp := range c.artifacts {
		if exp.Direction != ArtifactDirectionUpload {
			continue
		}
		if !exp.completed || exp.uploadRef == nil {
			missingUploads++
			continue
		}
		refs = append(refs, cloneCommandArtifactRef(exp.uploadRef))
	}
	if ack.GetCode() == gatewaypb.AckCode_ACK_CODE_OK && ack.GetSucceeded() && missingUploads > 0 {
		return refs, fleeterror.NewInternalErrorf("fleet node reported command success before %d expected artifact upload(s) completed", missingUploads)
	}
	return refs, nil
}
