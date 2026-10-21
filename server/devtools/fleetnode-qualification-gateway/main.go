// The package qualification fixture accepts standalone nodes so their real
// plugins initialize before systemd reports readiness.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type gateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler
}

func (gateway) UploadHeartbeat(context.Context, *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	return connect.NewResponse(&pb.UploadHeartbeatResponse{}), nil
}

func (gateway) ControlStream(_ context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	if _, err := stream.Receive(); err != nil {
		return fmt.Errorf("receive hello: %w", err)
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: &pb.ControlAccepted{OrgId: 1, ControlSessionId: "package-qualification"}}}); err != nil {
		return fmt.Errorf("accept standalone node: %w", err)
	}
	for {
		if _, err := stream.Receive(); err != nil {
			return fmt.Errorf("receive control message: %w", err)
		}
	}
}

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(gateway{})
	mux.Handle(path, handler)
	fmt.Printf("http://%s\n", listener.Addr())
	server := &http.Server{Handler: h2c.NewHandler(mux, &http2.Server{})} //nolint:gosec // Loopback-only fixture; control streams intentionally have no request timeout.
	log.Fatal(server.Serve(listener))
}
