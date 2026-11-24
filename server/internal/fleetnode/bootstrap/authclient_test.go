package bootstrap

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type uploadCompressionGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler
	report *pb.ReportDiscoveredDevicesRequest
	ack    *pb.ControlAck
	chunk  []byte
}

func (g *uploadCompressionGateway) ReportDiscoveredDevices(_ context.Context, req *connect.Request[pb.ReportDiscoveredDevicesRequest]) (*connect.Response[pb.ReportDiscoveredDevicesResponse], error) {
	g.report = req.Msg
	return connect.NewResponse(&pb.ReportDiscoveredDevicesResponse{}), nil
}

func (g *uploadCompressionGateway) ControlStream(_ context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	if _, err := stream.Receive(); err != nil {
		return fmt.Errorf("receive control hello: %w", err)
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: &pb.ControlAccepted{ServerTime: timestamppb.Now()}}}); err != nil {
		return fmt.Errorf("send control accepted: %w", err)
	}
	req, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive control acknowledgment: %w", err)
	}
	g.ack = req.GetAck()
	return nil
}

func (g *uploadCompressionGateway) UploadCommandArtifact(_ context.Context, stream *connect.ClientStream[pb.UploadCommandArtifactRequest]) (*connect.Response[pb.UploadCommandArtifactResponse], error) {
	for stream.Receive() {
		if chunk := stream.Msg().GetChunk(); chunk != nil {
			g.chunk = append(g.chunk, chunk.GetData()...)
		}
	}
	return connect.NewResponse(&pb.UploadCommandArtifactResponse{}), stream.Err()
}

func TestAuthenticatedClient_CompressesUploads(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"small discovery", "large discovery", "telemetry ack", "artifact stream"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			gateway := &uploadCompressionGateway{}
			_, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(gateway)
			type wireRequest struct {
				header http.Header
				body   []byte
			}
			observed := make(chan wireRequest, 1)
			server := testutil.NewH2CServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var wire bytes.Buffer
				r.Body = struct {
					io.Reader
					io.Closer
				}{io.TeeReader(r.Body, &wire), r.Body}
				handler.ServeHTTP(w, r)
				observed <- wireRequest{r.Header.Clone(), wire.Bytes()}
			}))
			client, err := NewAuthenticatedGatewayClient(server.URL, func() string { return "session-token" })
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			payload := bytes.Repeat([]byte("telemetry"), 100_000)
			var report *pb.ReportDiscoveredDevicesRequest
			switch mode {
			case "small discovery", "large discovery":
				report = &pb.ReportDiscoveredDevicesRequest{CommandId: "discovery-command"}
				count := 1
				if mode == "large discovery" {
					count = 64
				}
				for i := range count {
					report.Devices = append(report.Devices, &pb.DiscoveredDeviceReport{DeviceIdentifier: fmt.Sprintf("miner-%d", i), IpAddress: "10.0.0.1", Port: "8080", DriverName: "proto"})
				}
				_, err = client.ReportDiscoveredDevices(ctx, connect.NewRequest(report))
				require.NoError(t, err)
			case "telemetry ack":
				stream := client.ControlStream(ctx)
				require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
				_, err = stream.Receive()
				require.NoError(t, err)
				require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{CommandId: "telemetry-command", Succeeded: true, Payload: payload}}}))
				require.NoError(t, stream.CloseRequest())
				_, err = stream.Receive()
				require.ErrorIs(t, err, io.EOF)
				require.NoError(t, stream.CloseResponse())
			case "artifact stream":
				stream := client.UploadCommandArtifact(ctx)
				require.NoError(t, stream.Send(&pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Header{Header: &pb.CommandArtifactUploadHeader{CommandId: "artifact-command"}}}))
				require.NoError(t, stream.Send(&pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Chunk{Chunk: &pb.CommandArtifactChunk{Data: payload}}}))
				_, err = stream.CloseAndReceive()
				require.NoError(t, err)
			}
			var wire wireRequest
			select {
			case wire = <-observed:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.Equal(t, "Bearer session-token", wire.header.Get("Authorization"))
			switch mode {
			case "small discovery":
				require.Empty(t, wire.header.Get("Content-Encoding"))
				require.True(t, proto.Equal(report, gateway.report))
			case "large discovery":
				require.Equal(t, "gzip", wire.header.Get("Content-Encoding"))
				require.Less(t, len(wire.body), proto.Size(report))
				require.True(t, proto.Equal(report, gateway.report))
			default:
				header := "Grpc-Encoding"
				if mode == "artifact stream" {
					header = "Connect-Content-Encoding"
				}
				require.Equal(t, "gzip", wire.header.Get(header))
				require.Less(t, len(wire.body), len(payload)/10)
				var flags []byte
				for frame := wire.body; len(frame) > 0; {
					require.GreaterOrEqual(t, len(frame), 5)
					length := int(binary.BigEndian.Uint32(frame[1:5]))
					require.GreaterOrEqual(t, len(frame), 5+length)
					flags = append(flags, frame[0])
					frame = frame[5+length:]
				}
				require.Equal(t, []byte{0, 1}, flags, "small header is uncompressed; large data message is compressed")
				if mode == "artifact stream" {
					require.Equal(t, payload, gateway.chunk)
				} else {
					require.Equal(t, payload, gateway.ack.GetPayload())
				}
			}
		})
	}
}

type captureGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	mu                 sync.Mutex
	authHeadersSeen    []string
	heartbeatProtocols []string
	controlProtocols   []string
}

func (c *captureGateway) UploadHeartbeat(_ context.Context, req *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	c.mu.Lock()
	c.authHeadersSeen = append(c.authHeadersSeen, req.Header().Get("Authorization"))
	c.heartbeatProtocols = append(c.heartbeatProtocols, req.Peer().Protocol)
	c.mu.Unlock()
	return connect.NewResponse(&pb.UploadHeartbeatResponse{ReceivedAt: timestamppb.Now()}), nil
}

func (c *captureGateway) ControlStream(_ context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	c.mu.Lock()
	c.authHeadersSeen = append(c.authHeadersSeen, stream.RequestHeader().Get("Authorization"))
	c.controlProtocols = append(c.controlProtocols, stream.Peer().Protocol)
	c.mu.Unlock()
	if _, err := stream.Receive(); err != nil {
		return fmt.Errorf("receive control hello: %w", err)
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{
		Accepted: &pb.ControlAccepted{ServerTime: timestamppb.Now()},
	}}); err != nil {
		return fmt.Errorf("send control accepted: %w", err)
	}
	return nil
}

func (c *captureGateway) headers() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.authHeadersSeen))
	copy(out, c.authHeadersSeen)
	return out
}

func (c *captureGateway) protocols() ([]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.heartbeatProtocols...), append([]string(nil), c.controlProtocols...)
}

func TestAuthenticatedClient_AttachesBearerHeaderPerCall(t *testing.T) {
	t.Parallel()

	// Arrange
	fake := &captureGateway{}
	mux := http.NewServeMux()
	path, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, h)
	srv := testutil.NewH2CServer(t, mux)

	var token string
	client, err := NewAuthenticatedGatewayClient(srv.URL, func() string { return token })
	require.NoError(t, err)

	// Act
	token = "t1"
	_, err = client.UploadHeartbeat(context.Background(), connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
	require.NoError(t, err)
	token = "t2"
	_, err = client.UploadHeartbeat(context.Background(), connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
	require.NoError(t, err)

	// Assert
	got := fake.headers()
	require.Len(t, got, 2)
	assert.Equal(t, "Bearer t1", got[0])
	assert.Equal(t, "Bearer t2", got[1])
}

func TestAuthenticatedClient_RejectsEmptyToken(t *testing.T) {
	t.Parallel()

	// Arrange
	srv := testutil.NewH2CServer(t, http.NewServeMux())
	client, err := NewAuthenticatedGatewayClient(srv.URL, func() string { return "" })
	require.NoError(t, err)

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = client.UploadHeartbeat(ctx, connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))

	// Assert
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestAuthenticatedClient_UsesGRPCOnlyForControlStream(t *testing.T) {
	t.Parallel()

	// Arrange
	fake := &captureGateway{}
	mux := http.NewServeMux()
	path, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, h)
	srv := testutil.NewH2CServer(t, mux)
	client, err := NewAuthenticatedGatewayClient(srv.URL, func() string { return "session-token" })
	require.NoError(t, err)

	// Act: a representative unary RPC remains Connect, while ControlStream uses gRPC.
	_, err = client.UploadHeartbeat(t.Context(), connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
	require.NoError(t, err)
	stream := client.ControlStream(t.Context())
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
	_, err = stream.Receive()
	require.NoError(t, err)
	require.NoError(t, stream.CloseRequest())
	require.NoError(t, stream.CloseResponse())

	// Assert
	heartbeatProtocols, controlProtocols := fake.protocols()
	require.Equal(t, []string{connect.ProtocolConnect}, heartbeatProtocols)
	require.Equal(t, []string{connect.ProtocolGRPC}, controlProtocols)
	require.Equal(t, []string{"Bearer session-token", "Bearer session-token"}, fake.headers())
}

func TestAuthenticatedClient_ControlStreamsDialFreshConnections(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	remoteAddrs := map[string][]string{}
	path, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(&captureGateway{})
	mux := http.NewServeMux()
	mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		remoteAddrs[r.URL.Path] = append(remoteAddrs[r.URL.Path], r.RemoteAddr)
		mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	srv := testutil.NewH2CServer(t, mux)
	client, err := NewAuthenticatedGatewayClient(srv.URL, func() string { return "session-token" })
	require.NoError(t, err)

	for range 2 {
		_, err = client.UploadHeartbeat(t.Context(), connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
		require.NoError(t, err)
		stream := client.ControlStream(t.Context())
		require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
		_, err = stream.Receive()
		require.NoError(t, err)
		require.NoError(t, stream.CloseRequest())
		require.NoError(t, stream.CloseResponse())
	}

	mu.Lock()
	defer mu.Unlock()
	heartbeats := remoteAddrs[fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadHeartbeatProcedure]
	controls := remoteAddrs[fleetnodegatewayv1connect.FleetNodeGatewayServiceControlStreamProcedure]
	require.Len(t, heartbeats, 2)
	require.Len(t, controls, 2)
	assert.Equal(t, heartbeats[0], heartbeats[1], "heartbeats keep their pooled connection")
	assert.NotEqual(t, controls[0], controls[1], "each control stream dials a fresh connection")
	assert.NotContains(t, controls, heartbeats[0], "control streams do not share the heartbeat connection")
}
