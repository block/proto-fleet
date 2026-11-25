package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/infrastructure/runtimemetrics"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestNodeOperationResult(t *testing.T) {
	for _, tc := range []struct {
		code    pb.AckCode
		success bool
		want    string
	}{
		{pb.AckCode_ACK_CODE_OK, true, "success"}, {pb.AckCode_ACK_CODE_OK, false, "failure"},
		{pb.AckCode_ACK_CODE_BUSY, false, "busy"}, {pb.AckCode_ACK_CODE_UNAUTHENTICATED, false, "miner_auth_rejected"},
		{pb.AckCode_ACK_CODE_UNIMPLEMENTED, false, "unsupported"}, {pb.AckCode(999), false, "unknown_ack"},
		{pb.AckCode_ACK_CODE_FAILED_PRECONDITION, false, "failed_precondition"},
	} {
		require.Equal(t, tc.want, nodeOperationResult(&pb.ControlAck{Code: tc.code, Succeeded: tc.success}, nil))
	}
	require.Equal(t, "missing_ack", nodeOperationResult(nil, nil))
	require.Equal(t, "ack_send_failed", nodeOperationResult(&pb.ControlAck{Code: pb.AckCode_ACK_CODE_OK, Succeeded: true}, errors.New("private transport details")))
}

func TestControlSessionBalancesOperationMetrics(t *testing.T) {
	for _, result := range []string{"success", "busy", "ack_send_failed"} {
		t.Run(result, func(t *testing.T) {
			provider, err := runtimemetrics.Start(runtimemetrics.Config{Enabled: true, Address: "127.0.0.1:0"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			metrics, err := provider.NewNodeMetrics()
			require.NoError(t, err)
			cmd := &RunCmd{
				DiscoveryRanges:  []string{"10.0.0.0/8"},
				discoverer:       &stubDiscoverer{},
				operationMetrics: metrics,
			}
			if result == "busy" {
				cmd.initControlConcurrency()
				release, ok := cmd.tryAcquireControlCommandSlot(controlCommandAdmissionExclusive)
				require.True(t, ok)
				defer release()
			}
			gateway := &controlFakeGateway{}
			gateway.queue(discoverPayload(t, discoverIPList([]string{"10.0.0.5"}, []string{"4028"})))
			_, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(gateway)
			server := testutil.NewH2CServer(t, handler)
			options := []connect.ClientOption{connect.WithGRPC()}
			if result == "ack_send_failed" {
				options = append(options, connect.WithInterceptors(failControlAck{}))
			}
			client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), server.URL, options...)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			logger := discardLogger(t)
			go func() { done <- cmd.runControlSession(ctx, logger, client, &bootstrap.State{FleetNodeID: 7}) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("control session did not stop")
				}
				cmd.waitForControlWorkers(logger)
			})

			httpClient := &http.Client{Timeout: time.Second}
			const labels = "admission_class=\"exclusive\",kind=\"discovery\""
			outcomeLabels := labels + ",result=\"" + result + "\""
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				resp, err := httpClient.Get("http://" + provider.Addr() + "/metrics")
				require.NoError(c, err)
				defer resp.Body.Close()
				require.Equal(c, http.StatusOK, resp.StatusCode)
				body, err := io.ReadAll(resp.Body)
				require.NoError(c, err)
				scrape := string(body)
				require.Contains(c, scrape, "fleet_node_operation_attempts_total{"+labels+"} 1\n")
				require.Contains(c, scrape, "fleet_node_operation_outcomes_total{"+outcomeLabels+"} 1\n")
				require.Equal(c, 1, strings.Count(scrape, "fleet_node_operation_outcomes_total{"))
				require.Contains(c, scrape, "fleet_node_operation_duration_seconds_count{"+outcomeLabels+"} 1\n")
				require.Contains(c, scrape, "fleet_node_operations_inflight{"+labels+"} 0\n")
			}, 5*time.Second, 20*time.Millisecond)
		})
	}
}

type failControlAck struct{}

func (failControlAck) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc { return next }

func (failControlAck) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func (failControlAck) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return failControlAckConn{next(ctx, spec)}
	}
}

type failControlAckConn struct{ connect.StreamingClientConn }

func (c failControlAckConn) Send(msg any) error {
	if request, ok := msg.(*pb.ControlStreamRequest); ok && request.GetAck() != nil {
		return errors.New("control ack send failed")
	}
	return c.StreamingClientConn.Send(msg) //nolint:wrapcheck // Preserve the stream error.
}
