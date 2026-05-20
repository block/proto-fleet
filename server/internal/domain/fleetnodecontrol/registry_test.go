package fleetnodecontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestRegistry_ReRegisterEvictsPriorStream(t *testing.T) {
	// Arrange
	r := NewRegistry()
	first, err := r.Register(7)
	require.NoError(t, err)
	events, _, err := r.Send(context.Background(), 7, &gatewaypb.ControlCommand{CommandId: "in-flight"})
	require.NoError(t, err)
	<-first.Outgoing

	// Act
	second, err := r.Register(7)
	require.NoError(t, err)
	defer second.Unregister()

	// Assert: prior stream's outgoing closed
	select {
	case _, ok := <-first.Outgoing:
		assert.False(t, ok, "prior outgoing channel should be closed after re-register")
	case <-time.After(time.Second):
		t.Fatal("prior outgoing channel not closed within 1s")
	}

	// Assert: prior in-flight command channel closed
	select {
	case _, ok := <-events:
		assert.False(t, ok, "prior in-flight command channel should be closed after re-register")
	case <-time.After(time.Second):
		t.Fatal("prior in-flight events channel not closed within 1s")
	}

	// Assert: prior Unregister is a safe no-op (doesn't clobber new stream)
	first.Unregister()
	// New stream still works.
	_, _, err = r.Send(context.Background(), 7, &gatewaypb.ControlCommand{CommandId: "after-evict"})
	require.NoError(t, err)
}

func TestRegistry_SendWithoutStreamReturnsErrNoActiveStream(t *testing.T) {
	// Arrange
	r := NewRegistry()

	// Act
	_, _, err := r.Send(context.Background(), 9, &gatewaypb.ControlCommand{CommandId: "x"})

	// Assert
	assert.True(t, errors.Is(err, ErrNoActiveStream))
}

func TestRegistry_SendDeliversCommandAndRoutesAck(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s, err := r.Register(42)
	require.NoError(t, err)
	defer s.Unregister()

	// Act
	events, cleanup, err := r.Send(context.Background(), 42, &gatewaypb.ControlCommand{CommandId: "cmd-1", Payload: []byte("p")})
	require.NoError(t, err)
	defer cleanup()

	// Assert: agent receives the command on the outgoing channel
	select {
	case cmd, ok := <-s.Outgoing:
		require.True(t, ok)
		assert.Equal(t, "cmd-1", cmd.GetCommandId())
		assert.Equal(t, []byte("p"), cmd.GetPayload())
	case <-time.After(time.Second):
		t.Fatal("expected command on outgoing channel")
	}

	// Act 2: agent publishes a batch then an ack
	r.PublishBatch(42, "cmd-1", &pairingpb.DiscoverResponse{Devices: []*pairingpb.Device{{DeviceIdentifier: "d1"}}})
	s.PublishAck(&gatewaypb.ControlAck{CommandId: "cmd-1", Succeeded: true})

	// Assert 2
	gotBatch := receive(t, events)
	require.NotNil(t, gotBatch.Batch)
	require.Len(t, gotBatch.Batch.GetDevices(), 1)

	gotAck := receive(t, events)
	require.NotNil(t, gotAck.Ack)
	assert.True(t, gotAck.Ack.GetSucceeded())
}

func TestRegistry_DuplicateCommandIDRejected(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s, err := r.Register(1)
	require.NoError(t, err)
	defer s.Unregister()
	_, cleanup, err := r.Send(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "dup"})
	require.NoError(t, err)
	defer cleanup()
	// Drain the dispatched command so the second Send can proceed past
	// the outgoing channel even if it were accepted.
	<-s.Outgoing

	// Act
	_, _, err = r.Send(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "dup"})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestRegistry_UnregisterClosesInFlightChannels(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s, err := r.Register(99)
	require.NoError(t, err)
	events, _, err := r.Send(context.Background(), 99, &gatewaypb.ControlCommand{CommandId: "drop"})
	require.NoError(t, err)
	<-s.Outgoing

	// Act
	s.Unregister()

	// Assert: channel closed so dispatchers wake up rather than block
	select {
	case _, ok := <-events:
		assert.False(t, ok, "channel should be closed after unregister")
	case <-time.After(time.Second):
		t.Fatal("expected channel close after unregister")
	}
}

func TestRegistry_PublishBatchSilentOnUnknownCommand(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s, err := r.Register(5)
	require.NoError(t, err)
	defer s.Unregister()

	// Act + Assert (no panic, no goroutine leak)
	r.PublishBatch(5, "stale", &pairingpb.DiscoverResponse{})
	r.PublishBatch(404, "anything", &pairingpb.DiscoverResponse{})
}

func receive(t *testing.T, ch <-chan CommandEvent) CommandEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		require.True(t, ok, "channel closed unexpectedly")
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return CommandEvent{}
	}
}
