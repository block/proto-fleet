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
)

func TestRegistry_ReRegisterEvictsPriorStream(t *testing.T) {
	// Arrange
	r := NewRegistry()
	first := r.Register(7)
	session, err := r.Send(context.Background(), 7, &gatewaypb.ControlCommand{CommandId: "in-flight"}, nil, ReportKindDiscovery, nil)
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
	_, err = r.Send(context.Background(), 7, &gatewaypb.ControlCommand{CommandId: "after-evict"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
}

func TestRegistry_SendWithoutStreamReturnsErrNoActiveStream(t *testing.T) {
	// Arrange
	r := NewRegistry()

	// Act
	_, err := r.Send(context.Background(), 9, &gatewaypb.ControlCommand{CommandId: "x"}, nil, ReportKindDiscovery, nil)

	// Assert
	assert.True(t, errors.Is(err, ErrNoActiveStream))
}

func TestRegistry_SendDeliversCommandAndRoutesAck(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(42)
	defer s.Unregister()

	// Act
	session, err := r.Send(context.Background(), 42, &gatewaypb.ControlCommand{CommandId: "cmd-1", Payload: []byte("p")}, nil, ReportKindDiscovery, nil)
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

func TestRegistry_TerminalAckDeliveredWhenEventBufferFull(t *testing.T) {
	// Arrange: a report-bearing command whose event buffer is filled to capacity
	// with best-effort batches the operator has not drained.
	r := NewRegistry()
	s := r.Register(1)
	defer s.Unregister()
	session, err := r.Send(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "discover"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	require.Equal(t, "discover", recvCommandID(t, s))
	for range commandEventBuffer {
		r.PublishBatch(1, "discover", &pairingpb.DiscoverResponse{Devices: []*pairingpb.Device{{DeviceIdentifier: "d"}}})
	}

	// Act: the terminal ack arrives while the buffer is full.
	s.PublishAck(&gatewaypb.ControlAck{CommandId: "discover", Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})

	// Assert: the ack survives (one best-effort batch is evicted to make room),
	// rather than being dropped and stranding the operator until the timeout.
	var acks, batches int
	events := session.Events()
drain:
	for {
		select {
		case ev := <-events:
			if ev.Ack != nil {
				acks++
				assert.True(t, ev.Ack.GetSucceeded())
			}
			if ev.Batch != nil {
				batches++
			}
		default:
			break drain
		}
	}
	assert.Equal(t, 1, acks, "terminal ack must survive a full event buffer")
	assert.Equal(t, commandEventBuffer-1, batches, "exactly one best-effort batch is evicted for the ack")
}

func TestRegistry_ConcurrentCommandsNotRejected(t *testing.T) {
	// Arrange: a discovery is already in flight.
	r := NewRegistry()
	s := r.Register(1)
	defer s.Unregister()
	session, err := r.Send(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "discover"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	require.Equal(t, "discover", recvCommandID(t, s))

	// Act: an ack-only command dispatches concurrently rather than being rejected.
	results := make(chan cmdResult, 1)
	go func() {
		ack, sendErr := r.SendCommand(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "m1"})
		results <- cmdResult{ack: ack, err: sendErr}
	}()
	require.Equal(t, "m1", recvCommandID(t, s)) // dispatched ⇒ registered

	// Assert: its terminal ack resolves the blocked SendCommand.
	s.PublishAck(&gatewaypb.ControlAck{CommandId: "m1", Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
	res := recvResult(t, results)
	require.NoError(t, res.err)
	require.NotNil(t, res.ack)
	assert.True(t, res.ack.GetSucceeded())
}

func TestRegistry_SendCommandWithoutStreamReturnsErrNoActiveStream(t *testing.T) {
	// Act
	_, err := NewRegistry().SendCommand(context.Background(), 9, &gatewaypb.ControlCommand{CommandId: "x"})

	// Assert
	assert.ErrorIs(t, err, ErrNoActiveStream)
}

func TestRegistry_SendCommandUnblocksOnDisconnect(t *testing.T) {
	// Arrange: an ack-only command is in flight.
	r := NewRegistry()
	s := r.Register(1)
	results := make(chan cmdResult, 1)
	go func() {
		ack, sendErr := r.SendCommand(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "m1"})
		results <- cmdResult{ack: ack, err: sendErr}
	}()
	require.Equal(t, "m1", recvCommandID(t, s))

	// Act: the stream disconnects before any ack.
	s.Unregister()

	// Assert
	res := recvResult(t, results)
	assert.ErrorIs(t, res.err, ErrNoActiveStream)
	assert.Nil(t, res.ack)
}

func TestRegistry_SendCommandUnblocksOnCtxCancel(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(1)
	defer s.Unregister()
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan cmdResult, 1)
	go func() {
		ack, sendErr := r.SendCommand(ctx, 1, &gatewaypb.ControlCommand{CommandId: "m1"})
		results <- cmdResult{ack: ack, err: sendErr}
	}()
	require.Equal(t, "m1", recvCommandID(t, s))

	// Act: caller's context expires before the agent acks.
	cancel()

	// Assert: it returns an error and frees the slot for a re-send of the same id.
	res := recvResult(t, results)
	require.Error(t, res.err)
	assert.Nil(t, res.ack)
	_, err := r.SendCommand(canceledCtx(), 1, &gatewaypb.ControlCommand{CommandId: "m1"})
	require.Error(t, err) // not errDuplicateCommandID; the slot was freed
	assert.False(t, errors.Is(err, ErrNoActiveStream))
}

func TestRegistry_AckRoutesByKind(t *testing.T) {
	// Arrange: an ack-only command in flight.
	r := NewRegistry()
	s := r.Register(1)
	defer s.Unregister()
	results := make(chan cmdResult, 1)
	go func() {
		ack, sendErr := r.SendCommand(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "mk"})
		results <- cmdResult{ack: ack, err: sendErr}
	}()
	require.Equal(t, "mk", recvCommandID(t, s))

	// Assert: an ack-only command is not a report channel; the report path rejects it.
	assert.ErrorIs(t, r.AdmitReport(1, "mk", 1, ReportKindDiscovery), errNoInFlightCommand)
	_, ok := r.ReportScopeFor(1, "mk")
	assert.False(t, ok)

	// Act + Assert: its ack is delivered to the SendCommand waiter, not dropped.
	s.PublishAck(&gatewaypb.ControlAck{CommandId: "mk", Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
	res := recvResult(t, results)
	require.NoError(t, res.err)
	require.NotNil(t, res.ack)
}

func TestRegistry_TeardownClosesAllInFlightCommands(t *testing.T) {
	// Arrange: a discovery and an ack-only command are both in flight.
	r := NewRegistry()
	s := r.Register(1)
	session, err := r.Send(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "discover"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	require.Equal(t, "discover", recvCommandID(t, s))
	results := make(chan cmdResult, 1)
	go func() {
		ack, sendErr := r.SendCommand(context.Background(), 1, &gatewaypb.ControlCommand{CommandId: "mk"})
		results <- cmdResult{ack: ack, err: sendErr}
	}()
	require.Equal(t, "mk", recvCommandID(t, s))

	// Act: re-register evicts the connection, tearing down every in-flight command.
	s2 := r.Register(1)
	defer s2.Unregister()

	// Assert: the discovery session's Done closes and the ack-only waiter unblocks.
	assertClosed(t, session.Done())
	res := recvResult(t, results)
	assert.ErrorIs(t, res.err, ErrNoActiveStream)
}

func TestRegistry_AdmitReportEnforcesQuota(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(77)
	defer s.Unregister()
	session, err := r.Send(context.Background(), 77, &gatewaypb.ControlCommand{CommandId: "scan"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-s.Outgoing

	// Act + Assert: reports up to the cap are admitted; the batch crossing it is rejected.
	require.NoError(t, r.AdmitReport(77, "scan", maxReportsPerCommand-1, ReportKindDiscovery))
	require.NoError(t, r.AdmitReport(77, "scan", 1, ReportKindDiscovery))
	assert.ErrorIs(t, r.AdmitReport(77, "scan", 1, ReportKindDiscovery), ErrReportQuotaExceeded)

	// Assert: a command_id that is not in flight is rejected as such.
	assert.ErrorIs(t, r.AdmitReport(77, "other", 1, ReportKindDiscovery), errNoInFlightCommand)
	assert.ErrorIs(t, r.AdmitReport(404, "scan", 1, ReportKindDiscovery), errNoInFlightCommand)
}

func TestRegistry_AdmitReportRejectsCrossKind(t *testing.T) {
	// Arrange: one discovery command and one pair command in flight on the same node.
	r := NewRegistry()
	s := r.Register(5)
	defer s.Unregister()
	discSession, err := r.Send(context.Background(), 5, &gatewaypb.ControlCommand{CommandId: "disc"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer discSession.Close()
	<-s.Outgoing
	pairSession, err := r.Send(context.Background(), 5, &gatewaypb.ControlCommand{CommandId: "pair"}, nil, ReportKindPair, nil)
	require.NoError(t, err)
	defer pairSession.Close()
	<-s.Outgoing

	// Act + Assert: each command_id admits only reports of its own kind. A node
	// can't upload discovery rows against a pair command_id (or vice versa).
	assert.NoError(t, r.AdmitReport(5, "disc", 1, ReportKindDiscovery))
	assert.ErrorIs(t, r.AdmitReport(5, "disc", 1, ReportKindPair), errNoInFlightCommand)
	assert.NoError(t, r.AdmitReport(5, "pair", 1, ReportKindPair))
	assert.ErrorIs(t, r.AdmitReport(5, "pair", 1, ReportKindDiscovery), errNoInFlightCommand)
}

func sendPair(t *testing.T, r *Registry, fleetNodeID int64, commandID string, pair *PairMeta) (*Session, *Stream) {
	t.Helper()
	s := r.Register(fleetNodeID)
	session, err := r.Send(context.Background(), fleetNodeID, &gatewaypb.ControlCommand{CommandId: commandID}, nil, ReportKindPair, pair)
	require.NoError(t, err)
	<-s.Outgoing
	return session, s
}

func TestRegistry_AdmitAndScopePairResults_ScopesAndConsumes(t *testing.T) {
	// Arrange: a pair command scoped to three targets.
	r := NewRegistry()
	pair := &PairMeta{OrgID: 9, AssignedBy: nil, Targets: map[string]struct{}{"a": {}, "b": {}, "c": {}}}
	session, s := sendPair(t, r, 3, "p", pair)
	defer s.Unregister()
	defer session.Close()

	// Act: one in-scope ("a"), one out-of-scope ("zzz"), one replay of "a".
	kept, meta, err := r.AdmitAndScopePairResults(3, "p", []*gatewaypb.FleetNodePairResult{
		{DeviceIdentifier: "a"}, {DeviceIdentifier: "zzz"}, {DeviceIdentifier: "a"},
	})

	// Assert: only the first in-scope "a" is kept; out-of-scope + replay dropped;
	// meta carries the operator context for the gateway to persist with.
	require.NoError(t, err)
	require.Len(t, kept, 1)
	assert.Equal(t, "a", kept[0].GetDeviceIdentifier())
	assert.Equal(t, int64(9), meta.OrgID)
}

func TestRegistry_AdmitAndScopePairResults_RejectsEmptyAndKind(t *testing.T) {
	// Arrange
	r := NewRegistry()
	pair := &PairMeta{OrgID: 1, Targets: map[string]struct{}{"a": {}}}
	session, s := sendPair(t, r, 4, "p", pair)
	defer s.Unregister()
	defer session.Close()
	discSession, err := r.Send(context.Background(), 4, &gatewaypb.ControlCommand{CommandId: "d"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer discSession.Close()
	<-s.Outgoing

	// Act + Assert: empty batch rejected (would consume no quota).
	_, _, err = r.AdmitAndScopePairResults(4, "p", nil)
	assert.ErrorIs(t, err, ErrEmptyReport)

	// An oversized batch admits only the in-scope rows; quota is charged per
	// consumed target, so the extra row is dropped rather than rejecting the batch.
	kept, _, err := r.AdmitAndScopePairResults(4, "p", []*gatewaypb.FleetNodePairResult{
		{DeviceIdentifier: "a"}, {DeviceIdentifier: "b"},
	})
	require.NoError(t, err)
	require.Len(t, kept, 1)
	assert.Equal(t, "a", kept[0].GetDeviceIdentifier())

	// A discovery command_id is not a pair command.
	_, _, err = r.AdmitAndScopePairResults(4, "d", []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: "a"}})
	assert.ErrorIs(t, err, errNoInFlightCommand)
}

func TestRegistry_AdmitAndScopePairResults_DuplicatesDoNotStarveLaterTargets(t *testing.T) {
	// Arrange: two targets; the node first reports a duplicated identifier.
	r := NewRegistry()
	pair := &PairMeta{OrgID: 1, Targets: map[string]struct{}{"a": {}, "b": {}}}
	session, s := sendPair(t, r, 5, "p", pair)
	defer s.Unregister()
	defer session.Close()

	// Act: [a, a] consumes only "a"; a later report for "b" must still be admitted.
	kept, _, err := r.AdmitAndScopePairResults(5, "p", []*gatewaypb.FleetNodePairResult{
		{DeviceIdentifier: "a"}, {DeviceIdentifier: "a"},
	})
	require.NoError(t, err)
	require.Len(t, kept, 1)
	later, _, err := r.AdmitAndScopePairResults(5, "p", []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: "b"}})

	// Assert
	require.NoError(t, err)
	require.Len(t, later, 1)
	assert.Equal(t, "b", later[0].GetDeviceIdentifier())
}

func TestRegistry_ReinstatePairTargets_AllowsRetryAfterPersistFailure(t *testing.T) {
	// Arrange: a consumed target whose persistence failed.
	r := NewRegistry()
	pair := &PairMeta{OrgID: 1, Targets: map[string]struct{}{"a": {}}}
	session, s := sendPair(t, r, 6, "p", pair)
	defer s.Unregister()
	defer session.Close()
	kept, _, err := r.AdmitAndScopePairResults(6, "p", []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: "a"}})
	require.NoError(t, err)
	require.Len(t, kept, 1)

	// Act: the gateway reinstates the target after the persist failure.
	r.ReinstatePairTargets(6, "p", []string{"a"})

	// Assert: a retried report for the same command re-admits the identifier.
	retried, _, err := r.AdmitAndScopePairResults(6, "p", []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: "a"}})
	require.NoError(t, err)
	require.Len(t, retried, 1)
	assert.Equal(t, "a", retried[0].GetDeviceIdentifier())
}

func TestRegistry_UnregisterSignalsInFlightCommandDone(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s := r.Register(99)
	session, err := r.Send(context.Background(), 99, &gatewaypb.ControlCommand{CommandId: "drop"}, nil, ReportKindDiscovery, nil)
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

	session, err := r.Send(context.Background(), 11, &gatewaypb.ControlCommand{CommandId: "flood"}, nil, ReportKindDiscovery, nil)
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
// Run with `-race`: delivery looks up conn.cmds under the mutex and never closes the
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
			session, sendErr := r.Send(context.Background(), 101, &gatewaypb.ControlCommand{CommandId: "race-cmd"}, nil, ReportKindDiscovery, nil)
			if sendErr != nil {
				// Send only fails here if the connection was evicted mid-call; fine, race continues.
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
			}, nil, ReportKindDiscovery, nil)
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

// cmdResult captures an async SendCommand outcome for a test goroutine.
type cmdResult struct {
	ack *gatewaypb.ControlAck
	err error
}

// recvCommandID drains one dispatched command off the agent's outgoing channel and
// returns its command_id. Receiving it proves the command was registered (addCmd runs
// before the enqueue), so a subsequent PublishAck routes deterministically.
func recvCommandID(t *testing.T, s *Stream) string {
	t.Helper()
	select {
	case cmd, ok := <-s.Outgoing:
		require.True(t, ok, "outgoing channel closed unexpectedly")
		return cmd.GetCommandId()
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatched command")
		return ""
	}
}

func recvResult(t *testing.T, ch <-chan cmdResult) cmdResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SendCommand result")
		return cmdResult{}
	}
}

func assertClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed")
	case <-time.After(time.Second):
		t.Fatal("channel not closed within 1s")
	}
}

func canceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestConnectedFleetNodeIDs_ReflectsRegisterAndUnregister(t *testing.T) {
	// Arrange
	r := NewRegistry()
	s1 := r.Register(1)
	s2 := r.Register(2)

	// Act + Assert: both connected.
	assert.ElementsMatch(t, []int64{1, 2}, r.ConnectedFleetNodeIDs())

	// Act + Assert: unregistering one drops it.
	s1.Unregister()
	assert.ElementsMatch(t, []int64{2}, r.ConnectedFleetNodeIDs())

	// Act + Assert: empty once all are gone.
	s2.Unregister()
	assert.Empty(t, r.ConnectedFleetNodeIDs())
}
