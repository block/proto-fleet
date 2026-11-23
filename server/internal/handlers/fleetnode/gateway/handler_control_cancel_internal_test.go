package gateway

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

// A cancel never reaches the node before a command queued ahead of it.
func TestSendControlCancelSendsQueuedCommandsFirst(t *testing.T) {
	outgoing := make(chan *pb.ControlCommand, 4)
	outgoing <- &pb.ControlCommand{CommandId: "stream"}
	outgoing <- &pb.ControlCommand{CommandId: "other"}
	var sent []string
	send := func(resp *pb.ControlStreamResponse) error {
		if cmd := resp.GetCommand(); cmd != nil {
			sent = append(sent, "command "+cmd.GetCommandId())
		}
		if cancel := resp.GetCancel(); cancel != nil {
			sent = append(sent, "cancel "+cancel.GetCommandId())
		}
		return nil
	}

	sendCommand := func(cmd *pb.ControlCommand) error {
		return sendControlCommand(send, cmd)
	}
	require.NoError(t, sendControlCancel(send, sendCommand, outgoing, "stream"))
	require.Equal(t, []string{"command stream", "command other", "cancel stream"}, sent)
	require.Empty(t, outgoing)
}

func TestSendControlCancelStopsOnASendError(t *testing.T) {
	outgoing := make(chan *pb.ControlCommand, 1)
	outgoing <- &pb.ControlCommand{CommandId: "stream"}
	closed := errors.New("stream closed")
	calls := 0
	send := func(*pb.ControlStreamResponse) error {
		calls++
		return closed
	}
	sendCommand := func(cmd *pb.ControlCommand) error {
		return sendControlCommand(send, cmd)
	}
	err := sendControlCancel(send, sendCommand, outgoing, "stream")
	require.ErrorIs(t, err, closed)
	require.Equal(t, 1, calls, "no cancel is sent after the queued command fails")
}
