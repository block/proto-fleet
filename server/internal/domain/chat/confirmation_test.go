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
	decision ConfirmationDecision
	err      error
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
		result <- confirmationResult{decision: decision, err: err}
	}()

	id := <-confirmationID
	require.NoError(t, broker.Resolve(ctx, id, ConfirmationApproved))
	resolved := <-result
	require.NoError(t, resolved.err)
	assert.Equal(t, ConfirmationApproved, resolved.decision)
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
		result <- confirmationResult{decision: decision, err: err}
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
	assert.Equal(t, ConfirmationCancelled, resolved.decision)
}

func TestConfirmationBrokerExpiresUnresolvedRequest(t *testing.T) {
	broker := NewConfirmationBroker(time.Millisecond)
	ctx := confirmationTestContext(context.Background(), 7, 42)

	decision, err := broker.Await(ctx, ConfirmationRequest{}, func(string) error { return nil })

	require.Error(t, err)
	assert.Empty(t, decision)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeDeadlineExceeded, fleetErr.GRPCCode)
}
