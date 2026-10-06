package chat

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

type confirmationResult struct {
	resolution ConfirmationResolution
	err        error
}

func confirmationTestContext(ctx context.Context, userID, orgID int64) context.Context {
	return authn.SetInfo(ctx, &session.Info{UserID: userID, OrganizationID: orgID})
}

func TestConfirmationBrokerResumesAwaitingRequestForSameOperator(t *testing.T) {
	broker := NewConfirmationBroker(time.Second)
	ctx := confirmationTestContext(t.Context(), 7, 42)
	confirmationID := make(chan string, 1)
	result := make(chan confirmationResult, 1)

	go func() {
		decision, err := broker.Await(ctx, ConfirmationRequest{}, func(id string) error {
			confirmationID <- id
			return nil
		})
		result <- confirmationResult{resolution: decision, err: err}
	}()

	id := <-confirmationID
	require.NoError(t, broker.Resolve(ctx, id, ConfirmationApproved))
	resolved := <-result
	require.NoError(t, resolved.err)
	assert.Equal(t, ConfirmationApproved, resolved.resolution.Decision)
}

func TestConfirmationBrokerDoesNotExposePendingRequestAcrossOperators(t *testing.T) {
	broker := NewConfirmationBroker(time.Second)
	ownerCtx := confirmationTestContext(t.Context(), 7, 42)
	otherCtx := confirmationTestContext(t.Context(), 8, 42)
	confirmationID := make(chan string, 1)
	result := make(chan confirmationResult, 1)

	go func() {
		decision, err := broker.Await(ownerCtx, ConfirmationRequest{}, func(id string) error {
			confirmationID <- id
			return nil
		})
		result <- confirmationResult{resolution: decision, err: err}
	}()

	id := <-confirmationID
	err := broker.Resolve(otherCtx, id, ConfirmationApproved)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
	require.NoError(t, broker.Resolve(ownerCtx, id, ConfirmationCancelled))
	resolved := <-result
	require.NoError(t, resolved.err)
	assert.Equal(t, ConfirmationCancelled, resolved.resolution.Decision)
}

func TestConfirmationBrokerExpiresUnresolvedRequest(t *testing.T) {
	broker := NewConfirmationBroker(time.Millisecond)
	ctx := confirmationTestContext(context.Background(), 7, 42)

	decision, err := broker.Await(ctx, ConfirmationRequest{}, func(string) error { return nil })

	require.Error(t, err)
	assert.Empty(t, decision.Decision)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeDeadlineExceeded, fleetErr.GRPCCode)
}

func TestConfirmationExecutionUsesApprovalValuesAndStreamLifetime(t *testing.T) {
	type valueKey struct{}
	stream, cancelStream := context.WithCancel(confirmationTestContext(t.Context(), 7, 42))
	defer cancelStream()
	approval, cancelApproval := context.WithCancel(context.WithValue(confirmationTestContext(t.Context(), 7, 42), valueKey{}, "fresh"))
	broker := NewConfirmationBroker()
	resolution, err := broker.Await(stream, ConfirmationRequest{}, func(id string) error {
		err := broker.Resolve(approval, id, ConfirmationApproved)
		cancelApproval()
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, "fresh", resolution.Context.Value(valueKey{}))
	assert.NoError(t, resolution.Context.Err(), "finishing approval RPC must not cancel the write")
	assert.Equal(t, stream.Done(), resolution.Context.Done())
	cancelStream()
	assert.ErrorIs(t, resolution.Context.Err(), context.Canceled)
}
