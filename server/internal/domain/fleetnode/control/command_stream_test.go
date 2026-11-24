package control

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func streamCommand(id string) *pb.ControlCommand {
	return &pb.ControlCommand{CommandId: id, Payload: []byte{1}}
}

func commandFrame(commandID, payload string) *pb.CommandFrame {
	return &pb.CommandFrame{CommandId: commandID, Payload: []byte(payload)}
}

func registerStreamNode(t *testing.T, r *Registry, version pb.CommandProtocolVersion) *Stream {
	t.Helper()
	s, err := r.RegisterAuthenticated(7, "session", version)
	require.NoError(t, err)
	t.Cleanup(s.Unregister)
	return s
}

func onFrame(fn func(string) error) CommandStream {
	return CommandStream{Kind: "test_stream", OnFrame: func(payload []byte) error { return fn(string(payload)) }}
}

func ignoreFrames() CommandStream { return onFrame(func(string) error { return nil }) }

func requireNoCancel(t *testing.T, s *Stream) {
	t.Helper()
	select {
	case id := <-s.Cancels:
		t.Fatalf("unexpected cancel for %q", id)
	default:
	}
}

type streamResult struct {
	ack *pb.ControlAck
	err error
}

func TestCommandStreamRelaysFramePayloadsUntilTheAck(t *testing.T) {
	r := NewRegistry()
	s := registerStreamNode(t, r, CommandStreamProtocolVersion)
	got := make(chan string, 4)
	done := make(chan streamResult, 1)
	go func() {
		ack, err := r.StreamCommand(t.Context(), 7, streamCommand("s1"), onFrame(func(payload string) error {
			got <- payload
			return nil
		}))
		done <- streamResult{ack, err}
	}()
	cmd := <-s.Outgoing
	require.Equal(t, "s1", cmd.GetCommandId())
	s.PublishCommandFrame(commandFrame("s1", "one"))
	require.Equal(t, "one", <-got)
	// A frame published just before the ack still reaches the caller.
	s.PublishCommandFrame(commandFrame("s1", "two"))
	s.PublishAck(&pb.ControlAck{CommandId: "s1", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
	res := <-done
	require.NoError(t, res.err)
	require.Equal(t, pb.AckCode_ACK_CODE_OK, res.ack.GetCode())
	require.Equal(t, "two", <-got)
	requireNoCancel(t, s)
}

func TestCommandStreamGivesASlowCallerTheNewestFrameAfterCoalescing(t *testing.T) {
	r := NewRegistry()
	s := registerStreamNode(t, r, CommandStreamProtocolVersion)
	release := make(chan struct{})
	got := make(chan string, 4)
	stream := onFrame(func(payload string) error {
		got <- payload
		<-release
		return nil
	})
	// Carries what only the skipped frame held onto the frame that replaces it.
	stream.Coalesce = func(skipped, next []byte) []byte { return append(append(skipped, '+'), next...) }
	go func() { _, _ = r.StreamCommand(t.Context(), 7, streamCommand("s1"), stream) }()
	<-s.Outgoing
	s.PublishCommandFrame(commandFrame("s1", "one"))
	require.Equal(t, "one", <-got) // the caller is now busy with frame one
	s.PublishCommandFrame(commandFrame("s1", "two"))
	s.PublishCommandFrame(commandFrame("s1", "three"))
	close(release)
	require.Equal(t, "two+three", <-got, "one frame, with the skipped frame coalesced into it")
	s.PublishAck(&pb.ControlAck{CommandId: "s1", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
}

func TestCommandStreamCancelsTheNodeCommandWhenTheCallerLeaves(t *testing.T) {
	for name, leave := range map[string]func(cancel context.CancelFunc) error{
		"caller cancelled": func(cancel context.CancelFunc) error { cancel(); return nil },
		"send failed":      func(context.CancelFunc) error { return errors.New("viewer gone") },
	} {
		t.Run(name, func(t *testing.T) {
			r := NewRegistry()
			s := registerStreamNode(t, r, CommandStreamProtocolVersion)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan streamResult, 1)
			go func() {
				ack, err := r.StreamCommand(ctx, 7, streamCommand("s1"), onFrame(func(string) error { return leave(cancel) }))
				done <- streamResult{ack, err}
			}()
			<-s.Outgoing
			s.PublishCommandFrame(commandFrame("s1", "one"))
			require.Equal(t, "s1", <-s.Cancels)
			res := <-done
			require.Error(t, res.err)
			require.Nil(t, res.ack, "the caller does not wait for the node's ack")
			// A frame still in flight from the node is answered with another cancel.
			s.PublishCommandFrame(commandFrame("s1", "two"))
			require.Equal(t, "s1", <-s.Cancels)
		})
	}
}

func TestCommandStreamEndsOnAnOversizedFrame(t *testing.T) {
	r := NewRegistry()
	s := registerStreamNode(t, r, CommandStreamProtocolVersion)
	done := make(chan streamResult, 1)
	go func() {
		ack, err := r.StreamCommand(t.Context(), 7, streamCommand("s1"), ignoreFrames())
		done <- streamResult{ack, err}
	}()
	<-s.Outgoing
	s.PublishCommandFrame(&pb.CommandFrame{CommandId: "s1", Payload: bytes.Repeat([]byte{1}, MaxCommandFrameBytes+1)})
	res := <-done
	require.True(t, fleeterror.IsUnavailableError(res.err), "got %v", res.err)
	require.Equal(t, "s1", <-s.Cancels)
}

func TestCommandStreamAcceptsFramesOnlyFromTheOwningStream(t *testing.T) {
	r := NewRegistry()
	stale := registerStreamNode(t, r, CommandStreamProtocolVersion)
	s := registerStreamNode(t, r, CommandStreamProtocolVersion)
	got := make(chan string, 4)
	done := make(chan streamResult, 1)
	go func() {
		ack, err := r.StreamCommand(t.Context(), 7, streamCommand("s1"), onFrame(func(payload string) error {
			got <- payload
			return nil
		}))
		done <- streamResult{ack, err}
	}()
	<-s.Outgoing
	stale.PublishCommandFrame(commandFrame("s1", "from the replaced stream"))
	s.PublishAck(&pb.ControlAck{CommandId: "s1", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
	require.NoError(t, (<-done).err)
	require.Empty(t, got, "the replaced stream's frame never reaches the caller")
	requireNoCancel(t, stale)
}

func TestCommandStreamRefusesOldNodesAndExtraStreams(t *testing.T) {
	old := NewRegistry()
	registerStreamNode(t, old, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V16)
	_, err := old.StreamCommand(t.Context(), 7, streamCommand("s1"), ignoreFrames())
	require.ErrorIs(t, err, ErrCommandProtocolUpgradeRequired)
	require.True(t, IsCommandNotEnqueued(err))

	r := NewRegistry()
	s := registerStreamNode(t, r, CommandStreamProtocolVersion)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for i := range MaxConcurrentCommandStreamsPerNode {
		go func() { _, _ = r.StreamCommand(ctx, 7, streamCommand(string(rune('a'+i))), ignoreFrames()) }()
		<-s.Outgoing
	}
	_, err = r.StreamCommand(t.Context(), 7, streamCommand("extra"), ignoreFrames())
	require.ErrorIs(t, err, ErrCommandStreamCapacity)
	require.True(t, IsCommandNotEnqueued(err))
}

// Every way a stream ends frees its slot, so a node at capacity accepts a new
// stream once one of its streams has ended.
func TestCommandStreamFreesItsSlotOnEveryExit(t *testing.T) {
	for name, end := range map[string]func(s *Stream, cancel context.CancelFunc){
		"caller cancelled": func(_ *Stream, cancel context.CancelFunc) { cancel() },
		"node acked": func(s *Stream, _ context.CancelFunc) {
			s.PublishAck(&pb.ControlAck{CommandId: "ending", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
		},
		"frame refused": func(s *Stream, _ context.CancelFunc) { s.PublishCommandFrame(commandFrame("ending", "one")) },
		"oversized frame": func(s *Stream, _ context.CancelFunc) {
			s.PublishCommandFrame(&pb.CommandFrame{CommandId: "ending", Payload: bytes.Repeat([]byte{1}, MaxCommandFrameBytes+1)})
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := NewRegistry()
			s := registerStreamNode(t, r, CommandStreamProtocolVersion)
			holdCtx, release := context.WithCancel(t.Context())
			defer release()
			for i := range MaxConcurrentCommandStreamsPerNode - 1 {
				go func() { _, _ = r.StreamCommand(holdCtx, 7, streamCommand(string(rune('a'+i))), ignoreFrames()) }()
				<-s.Outgoing
			}
			endCtx, cancelEnd := context.WithCancel(t.Context())
			defer cancelEnd()
			done := make(chan streamResult, 1)
			go func() {
				ack, err := r.StreamCommand(endCtx, 7, streamCommand("ending"), onFrame(func(string) error { return errors.New("viewer gone") }))
				done <- streamResult{ack, err}
			}()
			<-s.Outgoing
			_, err := r.StreamCommand(t.Context(), 7, streamCommand("refused"), ignoreFrames())
			require.ErrorIs(t, err, ErrCommandStreamCapacity)

			end(s, cancelEnd)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("stream did not end")
			}
			go func() { _, _ = r.StreamCommand(holdCtx, 7, streamCommand("next"), ignoreFrames()) }()
			select {
			case cmd := <-s.Outgoing:
				require.Equal(t, "next", cmd.GetCommandId(), "the ended stream's slot is free again")
			case <-time.After(5 * time.Second):
				t.Fatal("a new stream was not sent after one ended")
			}
		})
	}
}

func TestCommandStreamEndsWhenTheNodeDisconnects(t *testing.T) {
	r := NewRegistry()
	s := registerStreamNode(t, r, CommandStreamProtocolVersion)
	done := make(chan streamResult, 1)
	go func() {
		ack, err := r.StreamCommand(t.Context(), 7, streamCommand("s1"), ignoreFrames())
		done <- streamResult{ack, err}
	}()
	<-s.Outgoing
	s.Unregister()
	select {
	case res := <-done:
		require.ErrorIs(t, res.err, ErrNoActiveStream)
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not end on disconnect")
	}
}

func TestCommandCancellableOnlyForReads(t *testing.T) {
	miner := func(action *pb.MinerCommand) *pb.AgentCommand {
		return &pb.AgentCommand{Command: &pb.AgentCommand_MinerCommand{MinerCommand: action}}
	}
	for name, tc := range map[string]struct {
		command *pb.AgentCommand
		want    bool
	}{
		"telemetry":         {&pb.AgentCommand{Command: &pb.AgentCommand_Telemetry{}}, true},
		"discovery":         {&pb.AgentCommand{Command: &pb.AgentCommand_Discover{}}, true},
		"board ASIC stream": {miner(&pb.MinerCommand{Action: &pb.MinerCommand_StreamBoardAsicReadings{}}), true},
		"read errors":       {miner(&pb.MinerCommand{Action: &pb.MinerCommand_GetErrors{}}), true},
		"reboot":            {miner(&pb.MinerCommand{Action: &pb.MinerCommand_Reboot{}}), false},
		"blink LED":         {miner(&pb.MinerCommand{Action: &pb.MinerCommand_BlinkLed{}}), false},
		"board identify":    {miner(&pb.MinerCommand{Action: &pb.MinerCommand_SetBoardIdentify{}}), false},
		"firmware update":   {miner(&pb.MinerCommand{Action: &pb.MinerCommand_FirmwareUpdate{}}), false},
		"undecodable power feed request": {miner(&pb.MinerCommand{Action: &pb.MinerCommand_ContainerPowerFeed{
			ContainerPowerFeed: &pb.ContainerPowerFeedAction{Request: []byte("{")},
		}}), false},
		"pairing":         {&pb.AgentCommand{Command: &pb.AgentCommand_Pair{}}, false},
		"miner web":       {&pb.AgentCommand{Command: &pb.AgentCommand_MinerWeb{}}, false},
		"empty":           {&pb.AgentCommand{}, false},
		"no command":      {nil, false},
		"empty miner cmd": {miner(&pb.MinerCommand{}), false},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, CommandCancellable(tc.command))
		})
	}
}
