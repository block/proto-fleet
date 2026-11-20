package control

import (
	"context"
	"errors"
	"testing"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestDispatchMetricsFollowOriginalStream(t *testing.T) {
	var results []string
	r := NewRegistry().WithDispatchObserver(func(_ context.Context, kind, class, result string, d time.Duration) {
		require.Equal(t, "empty", kind)
		require.Equal(t, "general", class)
		require.GreaterOrEqual(t, d, time.Duration(0))
		results = append(results, result)
	})
	stream := r.Register(7)
	_, _, err := r.addCmd(7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &inflightCommand{id: "cmd", done: make(chan struct{})})
	require.NoError(t, err)
	cmd := &gatewaypb.ControlCommand{CommandId: "cmd"}
	finish := stream.ObserveCommandSend(t.Context(), cmd)
	r.Register(7)
	finish(errors.New("private transport error"))
	stream.ObserveCommandSend(t.Context(), cmd)(nil)
	require.Equal(t, []string{"send_failed"}, results)
}
