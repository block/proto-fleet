package control

import (
	"bytes"
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimepolicy"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestMinerWebResponsePreservesLargeBodyAndHTTPFailure(t *testing.T) {
	r := NewRegistry()
	s, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
	require.NoError(t, err)
	defer s.Unregister()
	data := bytes.Repeat([]byte("x"), 2<<20)
	go func() {
		cmd := <-s.Outgoing
		s.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: cmd.CommandId, Part: &pb.MinerWebResponseChunk_Head{Head: &pb.MinerWebResponseHead{StatusCode: 409, Headers: []*pb.MinerWebHeader{{Name: "Content-Type", Value: "application/json"}}}}})
		for offset := 0; offset < len(data); offset += 65536 {
			s.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: cmd.CommandId, Part: &pb.MinerWebResponseChunk_Data{Data: data[offset : offset+65536]}})
		}
		s.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: cmd.CommandId, Part: &pb.MinerWebResponseChunk_End{End: true}})
		s.PublishAck(&pb.ControlAck{CommandId: cmd.CommandId, Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	response, err := r.SendMinerWeb(ctx, 7, webCommand(t, "web", time.Second))
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, 409, response.StatusCode)
	require.Equal(t, "application/json", response.Header.Get("Content-Type"))
	got, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, data, got)
}

func TestMinerWebRequiresCompleteResponseAndCurrentStream(t *testing.T) {
	for _, scenario := range []string{"missing end", "duplicate head", "data before head", "data after end", "wrong node", "stale stream", "failed ack"} {
		t.Run(scenario, func(t *testing.T) {
			r := NewRegistry()
			stale, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
			require.NoError(t, err)
			s, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
			require.NoError(t, err)
			defer s.Unregister()
			other, err := r.RegisterAuthenticated(8, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
			require.NoError(t, err)
			defer other.Unregister()
			go func() {
				cmd := <-s.Outgoing
				publisher := s
				if scenario == "wrong node" {
					publisher = other
				}
				if scenario == "stale stream" {
					publisher = stale
				}
				head := &pb.MinerWebResponseChunk{CommandId: cmd.CommandId, Part: &pb.MinerWebResponseChunk_Head{Head: &pb.MinerWebResponseHead{StatusCode: 200}}}
				data := &pb.MinerWebResponseChunk{CommandId: cmd.CommandId, Part: &pb.MinerWebResponseChunk_Data{Data: []byte("body")}}
				if scenario == "data before head" {
					publisher.PublishMinerWebResponse(data)
				}
				publisher.PublishMinerWebResponse(head)
				if scenario == "duplicate head" {
					publisher.PublishMinerWebResponse(head)
				}
				if scenario != "missing end" {
					publisher.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: cmd.CommandId, Part: &pb.MinerWebResponseChunk_End{End: true}})
				}
				if scenario == "data after end" {
					publisher.PublishMinerWebResponse(data)
				}
				ack := &pb.ControlAck{CommandId: cmd.CommandId, Succeeded: true, Code: pb.AckCode_ACK_CODE_OK}
				if scenario == "failed ack" {
					ack.Succeeded = false
					ack.Code = pb.AckCode_ACK_CODE_INTERNAL
				}
				s.PublishAck(ack)
			}()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			response, err := r.SendMinerWeb(ctx, 7, webCommand(t, "web", time.Second))
			if response != nil {
				require.NoError(t, response.Body.Close())
			}
			require.Error(t, err)
			require.Nil(t, response)
		})
	}
}

func TestMinerWebCapabilityAndPreDispatchRefusal(t *testing.T) {
	r := NewRegistry()
	require.False(t, r.SupportsMinerWeb(7))
	legacy := r.Register(7)
	defer legacy.Unregister()
	require.False(t, r.SupportsMinerWeb(7))
	response, err := r.SendMinerWeb(t.Context(), 7, webCommand(t, "web", time.Second))
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Error(t, err)
	require.True(t, IsCommandNotEnqueued(err))
	select {
	case <-legacy.Outgoing:
		t.Fatal("sent web command to old node")
	default:
	}
	current, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
	require.NoError(t, err)
	require.True(t, r.SupportsMinerWeb(7))
	current.Unregister()
	require.False(t, r.SupportsMinerWeb(7))
}

func TestMinerWebStaleAckCannotCompleteCurrentResponse(t *testing.T) {
	r := NewRegistry()
	stale, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
	require.NoError(t, err)
	current, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
	require.NoError(t, err)
	defer current.Unregister()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		command := <-current.Outgoing
		current.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: command.CommandId, Part: &pb.MinerWebResponseChunk_Head{Head: &pb.MinerWebResponseHead{StatusCode: 200}}})
		current.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: command.CommandId, Part: &pb.MinerWebResponseChunk_End{End: true}})
		stale.PublishAck(&pb.ControlAck{CommandId: command.CommandId, Code: pb.AckCode_ACK_CODE_OK, Succeeded: true})
		cancel()
	}()
	response, err := r.SendMinerWeb(ctx, 7, webCommand(t, "current", time.Second))
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Error(t, err)
	require.Nil(t, response)
	require.Len(t, r.minerWebSlots, 1)
	current.PublishAck(&pb.ControlAck{CommandId: "current", Code: pb.AckCode_ACK_CODE_INTERNAL})
	require.Eventually(t, func() bool { return len(r.minerWebSlots) == 0 }, time.Second, time.Millisecond)
}

func TestMinerWebDisconnectAndCapacityRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		s, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
		require.NoError(t, err)
		go func() { <-s.Outgoing; s.Unregister() }()
		response, err := r.SendMinerWeb(t.Context(), 7, webCommand(t, "disconnect", time.Second))
		if response != nil {
			require.NoError(t, response.Body.Close())
		}
		require.Error(t, err)
		require.False(t, IsCommandNotEnqueued(err))
		require.Len(t, r.minerWebSlots, 1)
		require.Eventually(t, func() bool { return len(r.minerWebSlots) == 0 }, runtimepolicy.MinerWebResponseTimeout+time.Second, time.Millisecond)
		// Exhausted byte reservations wait only until the caller's deadline, before enqueue.
		for range MaxConcurrentMinerWebRequests {
			r.minerWebSlots <- struct{}{}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		response, err = r.SendMinerWeb(ctx, 7, webCommand(t, "capacity", time.Second))
		if response != nil {
			require.NoError(t, response.Body.Close())
		}
		require.ErrorIs(t, err, context.Canceled)
		require.True(t, IsCommandNotEnqueued(err))

	})
}

func webCommand(t *testing.T, id string, timeout time.Duration) *pb.ControlCommand {
	t.Helper()
	payload, err := proto.Marshal(&pb.AgentCommand{Command: &pb.AgentCommand_MinerWeb{MinerWeb: &pb.MinerWebRequest{Timeout: durationpb.New(timeout)}}})
	require.NoError(t, err)
	return &pb.ControlCommand{CommandId: id, Payload: payload}
}

func TestMinerWebCancellationRetainsAdmissionReservation(t *testing.T) {
	r := NewRegistry()
	s, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
	require.NoError(t, err)
	defer s.Unregister()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results := make(chan error, 4)
	ids := []string{"slow-1", "slow-2", "slow-3", "slow-4"}
	for _, id := range ids {
		command := webCommand(t, id, 5*time.Second)
		go func() {
			response, err := r.SendMinerWeb(ctx, 7, command)
			if response != nil {
				_ = response.Body.Close()
			}
			results <- err
		}()
		select {
		case <-s.Outgoing:
		case <-time.After(time.Second):
			t.Fatal("command not sent")
		}

		s.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: id, Part: &pb.MinerWebResponseChunk_Head{Head: &pb.MinerWebResponseHead{StatusCode: 200}}})
		s.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: id, Part: &pb.MinerWebResponseChunk_Data{Data: []byte("buffered before cancel")}})
	}
	cancel()
	for range ids {
		require.ErrorIs(t, <-results, context.Canceled)
	}
	require.Len(t, r.minerWebSlots, 4, "browser cancellation does not stop node execution")
	// Canceled callers must not retain or accumulate body data while waiting.
	s.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: ids[0], Part: &pb.MinerWebResponseChunk_Data{Data: []byte("discard me")}})
	r.mu.Lock()
	retainedBytes := s.conn.cmds[ids[0]].web.body.Len()
	r.mu.Unlock()
	require.Zero(t, retainedBytes)
	replacementCtx, stop := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer stop()
	response, err := r.SendMinerWeb(replacementCtx, 7, webCommand(t, "replacement", time.Second))
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, IsCommandNotEnqueued(err))
	select {
	case <-s.Outgoing:
		t.Fatal("replacement sent while node still occupied")
	default:
	}
	for _, id := range ids {
		s.PublishAck(&pb.ControlAck{CommandId: id, Code: pb.AckCode_ACK_CODE_INTERNAL})
	}
	require.Eventually(t, func() bool { return len(r.minerWebSlots) == 0 }, time.Second, time.Millisecond)
	// Once the old requests finish, the replacement can reach the node.
	go func() {
		command := <-s.Outgoing
		s.PublishAck(&pb.ControlAck{CommandId: command.CommandId, Code: pb.AckCode_ACK_CODE_BUSY})
	}()
	response, err = r.SendMinerWeb(t.Context(), 7, webCommand(t, "after-completion", time.Second))
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Error(t, err)
	require.False(t, IsCommandNotEnqueued(err))
}

func TestMinerWebCancellationCleanupAtServerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		s, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
		require.NoError(t, err)
		defer s.Unregister()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { <-s.Outgoing; cancel() }()
		response, err := r.SendMinerWeb(ctx, 7, webCommand(t, "expires", 100*time.Millisecond))
		if response != nil {
			require.NoError(t, response.Body.Close())
		}
		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, r.minerWebSlots, 1)
		require.Eventually(t, func() bool { return len(r.minerWebSlots) == 0 }, runtimepolicy.MinerWebResponseTimeout+time.Second, time.Millisecond)
		r.mu.Lock()
		remaining := len(s.conn.cmds)
		r.mu.Unlock()
		require.Zero(t, remaining)

	})
}

func TestMinerWebRejectsMissingOrUnboundedTimeout(t *testing.T) {
	for _, command := range []*pb.ControlCommand{
		{CommandId: "missing"},
		{CommandId: "malformed", Payload: []byte{0xff}},
		webCommand(t, "unbounded", time.Hour),
		webCommand(t, "negative", -time.Second),
		webCommand(t, "zero", 0),
	} {
		t.Run(command.CommandId, func(t *testing.T) {
			r := NewRegistry()
			s, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
			require.NoError(t, err)
			defer s.Unregister()
			response, err := r.SendMinerWeb(t.Context(), 7, command)
			if response != nil {
				require.NoError(t, response.Body.Close())
			}
			require.Error(t, err)
			require.True(t, IsCommandNotEnqueued(err))
			require.Empty(t, r.minerWebSlots)
			select {
			case <-s.Outgoing:
				t.Fatal("invalid command enqueued")
			default:
			}
		})
	}
}

func TestMinerWebResponseWaitAllowsDelayedDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		s, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
		require.NoError(t, err)
		defer s.Unregister()
		go func() {
			cmd := <-s.Outgoing
			// Transit exceeds the node execution budget. The server still waits
			// under its own response budget instead of expiring the node request.
			time.Sleep(40 * time.Second)
			s.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: cmd.CommandId, Part: &pb.MinerWebResponseChunk_Head{Head: &pb.MinerWebResponseHead{StatusCode: 200}}})
			s.PublishMinerWebResponse(&pb.MinerWebResponseChunk{CommandId: cmd.CommandId, Part: &pb.MinerWebResponseChunk_End{End: true}})
			s.PublishAck(&pb.ControlAck{CommandId: cmd.CommandId, Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
		}()
		response, err := r.SendMinerWeb(t.Context(), 7, webCommand(t, "delayed", runtimepolicy.MinerWebRequestTimeout))
		require.NoError(t, err)
		require.Len(t, r.minerWebSlots, 1, "response bytes retain their reservation until consumed")
		require.NoError(t, response.Body.Close())
		require.Empty(t, r.minerWebSlots)
	})
}

func TestMinerWebServerBoundsAdmissionAndMissingAcknowledgment(t *testing.T) {
	for _, admissionBlocked := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			r := NewRegistry()
			s, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
			require.NoError(t, err)
			defer s.Unregister()
			if admissionBlocked {
				for range MaxConcurrentMinerWebRequests {
					r.minerWebSlots <- struct{}{}
				}
			} else {
				go func() { <-s.Outgoing }() // Connected node never acknowledges.
			}
			start := time.Now()
			response, err := r.SendMinerWeb(t.Context(), 7, webCommand(t, "bounded", time.Second))
			if response != nil {
				_ = response.Body.Close()
			}
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, admissionBlocked, IsCommandNotEnqueued(err))
			require.Equal(t, runtimepolicy.MinerWebResponseTimeout, time.Since(start))
			synctest.Wait()
			if !admissionBlocked {
				require.Empty(t, r.minerWebSlots)
			}
			r.mu.Lock()
			remaining := len(s.conn.cmds)
			r.mu.Unlock()
			require.Zero(t, remaining)
		})
	}
}

func TestMinerWebReconnectRetainsAdmissionUntilServerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		old, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		results := make(chan error, MaxConcurrentMinerWebRequests)
		ids := []string{"one", "two", "three", "four"}
		for _, id := range ids {
			command := webCommand(t, id, time.Second)
			go func() {
				response, err := r.SendMinerWeb(ctx, 7, command)
				if response != nil {
					_ = response.Body.Close()
				}
				results <- err
			}()
			select {
			case <-old.Outgoing:
			case <-time.After(time.Second):
				t.Fatal("command not sent")
			}
		}
		current, err := r.RegisterAuthenticated(7, "session", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
		require.NoError(t, err)
		defer current.Unregister()
		for range ids {
			select {
			case err := <-results:
				require.ErrorIs(t, err, ErrNoActiveStream)
			case <-time.After(time.Second):
				t.Fatal("caller did not return on stream replacement")
			}
		}
		require.Len(t, r.minerWebSlots, MaxConcurrentMinerWebRequests)
		for _, id := range ids {
			old.PublishAck(&pb.ControlAck{CommandId: id, Code: pb.AckCode_ACK_CODE_INTERNAL})
		}
		replacementCtx, stop := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer stop()
		response, err := r.SendMinerWeb(replacementCtx, 7, webCommand(t, "replacement", time.Second))
		if response != nil {
			require.NoError(t, response.Body.Close())
		}
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.True(t, IsCommandNotEnqueued(err))
		select {
		case <-current.Outgoing:
			t.Fatal("reconnect bypassed retained capacity")
		default:
		}
		require.Eventually(t, func() bool { return len(r.minerWebSlots) == 0 }, runtimepolicy.MinerWebResponseTimeout+time.Second, time.Millisecond)

	})
}
