package control

import (
	"context"
	"errors"
	"sync"
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
	first := r.Register(7)
	session, err := r.Send(context.Background(), 7, &gatewaypb.ControlCommand{CommandId: "in-flight"}, nil)
	require.NoError(t, err)
	<-first.Outgoing

	// Act
	second := r.Register(7)
	defer second.Unregister()

	// Assert: prior stream's done channel closed (eviction signal)
	select {
	case _, ok := <-first.Done:
		assert.False(t, ok, "prior stream's done channel should be closed after re-register")
	case <-time.After(time.Second):
		t.Fatal("prior stream's done channel not closed within 1s")
	}

	// Assert: prior in-flight command's done signal closed
	select {
	case _, ok := <-session.Done():
		assert.False(t, ok, "prior command's done channel should be closed after re-register")
	case <-time.After(time.Second):
		t.Fatal("prior command's done channel not closed within 1s")
	}

	// Assert: prior Unregister is a safe no-op (doesn't clobber new stream)
	first.Unregister()
	_, err = r.Send(context.Background(), 7, &gatewaypb.ControlCommand{CommandId: "after-evict"}, nil)
	require.NoError(t, err)
}

func TestRegistry_SendWithoutStreamReturnsErrNoActiveStream(t *testing.T) {
	// Arrange
	r := NewRegistry()

	// Act
	_, err := r.Send(context.Background(), 9, &gatewaypb.ControlCommand{CommandId: "x"}, nil)

	// Assert
	assert.True(t, errors.Is(err, ErrNoActiveStream))
}

func TestRegistry_SendDeliversCommandAndRoutesAck(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(42)
	defer s.Unregister()

	// Act
	session, err := r.Send(context.Background(), 42, &gatewaypb.ControlCommand{CommandId: "cmd-1", Payload: []byte("p")}, nil)
	require.NoError(t, err)
	defer session.Close()

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
	gotBatch := receive(t, session.Events())
	require.NotNil(t, gotBatch.Batch)
	require.Len(t, gotBatch.Batch.GetDevices(), 1)

	gotAck := receive(t, session.Events())
	require.NotNil(t, gotAck.Ack)
	assert.True(t, gotAck.Ack.GetSucceeded())
}

func TestRegistry_SecondCommandRejectedWhileInFlight(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(1)
	defer s.Unregister()
	session, err := r.Send(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "first"}, nil)
	require.NoError(t, err)
	defer session.Close()
	// Drain the dispatched command so the second Send can proceed past
	// the outgoing channel even if it were accepted.
	<-s.Outgoing

	// Act: a second command is rejected while one is in flight, even with a new id.
	_, err = r.Send(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "second"}, nil)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestRegistry_AdmitReportEnforcesQuota(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(77)
	defer s.Unregister()
	session, err := r.Send(context.Background(), 77, &gatewaypb.ControlCommand{CommandId: "scan"}, nil)
	require.NoError(t, err)
	defer session.Close()
	<-s.Outgoing

	// Act + Assert: reports up to the cap are admitted; the batch crossing it is rejected.
	require.NoError(t, r.AdmitReport(77, "scan", maxReportsPerCommand-1))
	require.NoError(t, r.AdmitReport(77, "scan", 1))
	assert.ErrorIs(t, r.AdmitReport(77, "scan", 1), ErrReportQuotaExceeded)

	// Assert: a command_id that is not in flight is rejected as such.
	assert.ErrorIs(t, r.AdmitReport(77, "other", 1), errNoInFlightCommand)
	assert.ErrorIs(t, r.AdmitReport(404, "scan", 1), errNoInFlightCommand)
}

func TestRegistry_UnregisterSignalsInFlightCommandDone(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(99)
	session, err := r.Send(context.Background(), 99, &gatewaypb.ControlCommand{CommandId: "drop"}, nil)
	require.NoError(t, err)
	<-s.Outgoing

	// Act
	s.Unregister()

	// Assert: command's done signal closes so the operator loop wakes rather than blocks
	select {
	case _, ok := <-session.Done():
		assert.False(t, ok, "command's done channel should close after unregister")
	case <-time.After(time.Second):
		t.Fatal("expected command done close after unregister")
	}
}

func TestRegistry_PublishBatchSilentOnUnknownCommand(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(5)
	defer s.Unregister()

	// Act + Assert (no panic, no goroutine leak)
	r.PublishBatch(5, "stale", &pairingpb.DiscoverResponse{})
	r.PublishBatch(404, "anything", &pairingpb.DiscoverResponse{})
}

func TestPublish_DropsWhenChannelFullWithoutBlocking(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(11)
	defer s.Unregister()

	session, err := r.Send(context.Background(), 11, &gatewaypb.ControlCommand{CommandId: "flood"}, nil)
	require.NoError(t, err)
	defer session.Close()
	<-s.Outgoing
	events := session.Events()

	// Act: fill the buffer, then publish a batch and an ack past capacity. The
	// excess events are dropped (logged) rather than blocking the publisher.
	for range commandEventBuffer {
		r.PublishBatch(11, "flood", &pairingpb.DiscoverResponse{})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.PublishBatch(11, "flood", &pairingpb.DiscoverResponse{})
		s.PublishAck(&gatewaypb.ControlAck{CommandId: "flood", Succeeded: true})
	}()

	// Assert: the over-capacity publishes return promptly without blocking.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked when the event channel was full")
	}

	// Assert: every buffered event is still deliverable.
	drained := 0
	for {
		select {
		case <-events:
			drained++
		default:
			require.Equal(t, commandEventBuffer, drained, "buffered events must all be deliverable before drops")
			return
		}
	}
}

// TestPublish_RaceWithCleanup exercises an agent's report/ack landing
// concurrently with the operator's Session.Close freeing the command slot.
// Run with `-race`: deliver reads conn.cmd under the mutex and never closes the
// events channel, so there is no "send on closed channel" hazard to trip.
func TestPublish_RaceWithCleanup(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	s := r.Register(101)
	defer s.Unregister()

	const iters = 200
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iters {
			session, sendErr := r.Send(context.Background(), 101, &gatewaypb.ControlCommand{CommandId: "race-cmd"}, nil)
			if sendErr != nil {
				// Send fails if the single-in-flight guard fires; that's fine — race continues.
				continue
			}
			<-s.Outgoing
			session.Close()
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iters * 4 {
			r.PublishBatch(101, "race-cmd", &pairingpb.DiscoverResponse{})
			s.PublishAck(&gatewaypb.ControlAck{CommandId: "race-cmd", Succeeded: true})
		}
	}()
	wg.Wait()
}

// TestSend_RaceWithReRegister exercises the path that previously panicked
// when Send wrote to ns.outgoing while a concurrent Register evicted the
// stream and closed old.outgoing. After the fix, Send selects on the
// stream's done channel and returns ErrNoActiveStream cleanly. Run with
// `-race`.
func TestSend_RaceWithReRegister(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	s := r.Register(202)
	defer s.Unregister()

	const iters = 200
	var wg sync.WaitGroup

	// Drainer: keeps the outgoing buffer empty so Send doesn't sit on
	// the buffer too long. Exits when the registry is unregistered.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-s.Done:
				return
			case <-s.Outgoing:
			}
		}
	}()

	// Reconnector: re-registers the same fleet_node id in a loop, each
	// time evicting the prior stream. Old streams' Done channels close.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iters {
			ns := r.Register(202)
			// drain new outgoing so this iteration doesn't deadlock the next sender
			go func(n *Stream) {
				for {
					select {
					case <-n.Done:
						return
					case <-n.Outgoing:
					}
				}
			}(ns)
		}
	}()

	// Sender: races Send against the reconnector. Before the fix, Send's
	// `ns.outgoing <- cmd` would panic on a closed channel.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range iters * 4 {
			session, sendErr := r.Send(context.Background(), 202, &gatewaypb.ControlCommand{
				CommandId: cmdID(i),
			}, nil)
			if sendErr == nil {
				session.Close()
			}
		}
	}()

	wg.Wait()
}

func cmdID(i int) string {
	return "race-" + string(rune('a'+(i%26)))
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
