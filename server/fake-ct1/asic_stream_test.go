package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestStreamASICHealthSendsLayoutOnceThenReadings(t *testing.T) {
	previous := asicStreamInterval
	asicStreamInterval = 5 * time.Millisecond
	t.Cleanup(func() { asicStreamInterval = previous })

	srv, state := newConnectTestServer(t)
	var mac string
	for candidate, board := range state.boardsByMAC {
		if board.isMining() {
			mac = candidate
			break
		}
	}
	telemetry := mdkv2connect.NewTelemetryServiceClient(http.DefaultClient, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := telemetry.StreamASICHealth(ctx, connect.NewRequest(&mdk.StreamASICHealthRequest{Identifier: mac}))
	if err != nil {
		t.Fatalf("StreamASICHealth: %v", err)
	}
	defer stream.Close()

	var frames []*mdk.StreamASICHealthResponse
	for len(frames) < 3 && stream.Receive() {
		frames = append(frames, stream.Msg())
	}
	if len(frames) < 3 {
		t.Fatalf("frames = %d, want 3: %v", len(frames), stream.Err())
	}
	first := frames[0]
	if first.GetMacAddress() != mac {
		t.Fatalf("mac = %q, want %q", first.GetMacAddress(), mac)
	}
	if first.GetLayout().GetTotalAsics() != int32(state.Config.AsicsPerBoard) || len(first.GetAsics()) != state.Config.AsicsPerBoard {
		t.Fatalf("first frame layout total = %d with %d ASICs, want %d", first.GetLayout().GetTotalAsics(), len(first.GetAsics()), state.Config.AsicsPerBoard)
	}
	if first.GetTimestamp() == nil {
		t.Fatal("first frame has no timestamp")
	}

	unary, err := (&grpcAPI{state: state}).GetHashboardASICs(ctx, &mdk.GetHashboardASICsRequest{Identifier: mac})
	if err != nil {
		t.Fatalf("GetHashboardASICs: %v", err)
	}
	for _, frame := range frames[1:] {
		if frame.GetLayout() != nil {
			t.Fatal("unchanged layout was resent")
		}
		if len(frame.GetAsics()) != len(unary.GetAsics()) || frame.GetAsics()[0].GetHashrateGhs10() != unary.GetAsics()[0].GetHashrateGhs10() {
			t.Fatal("stream readings differ from GetHashboardASICs in the same sample interval")
		}
	}
}

func TestStreamASICHealthRejectsUnknownBoard(t *testing.T) {
	srv, _ := newConnectTestServer(t)
	telemetry := mdkv2connect.NewTelemetryServiceClient(http.DefaultClient, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for identifier, want := range map[string]connect.Code{"": connect.CodeInvalidArgument, "02:00:00:00:00:00": connect.CodeNotFound} {
		stream, err := telemetry.StreamASICHealth(ctx, connect.NewRequest(&mdk.StreamASICHealthRequest{Identifier: identifier}))
		if err != nil {
			t.Fatalf("StreamASICHealth(%q): %v", identifier, err)
		}
		if stream.Receive() {
			t.Fatalf("StreamASICHealth(%q) sent a frame", identifier)
		}
		var connectErr *connect.Error
		if !errors.As(stream.Err(), &connectErr) || connectErr.Code() != want {
			t.Fatalf("StreamASICHealth(%q) error = %v, want %s", identifier, stream.Err(), want)
		}
		stream.Close()
	}
}

// The Fleet CT1 plugin reads the stream over native gRPC, not Connect.
func TestStreamASICHealthOverNativeGRPC(t *testing.T) {
	previous := asicStreamInterval
	asicStreamInterval = 5 * time.Millisecond
	t.Cleanup(func() { asicStreamInterval = previous })

	state := NewContainerState(ContainerConfig{ServerID: "grpc-stream", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 3, AsicsPerBoard: 312, Seed: 1})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(state)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(8<<20)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var mac string
	for candidate, board := range state.boardsByMAC {
		if board.isMining() {
			mac = candidate
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	telemetry := mdk.NewTelemetryServiceClient(conn)

	stream, err := telemetry.StreamASICHealth(ctx, &mdk.StreamASICHealthRequest{Identifier: mac})
	if err != nil {
		t.Fatalf("StreamASICHealth: %v", err)
	}
	first, err := stream.Recv()
	if err != nil {
		t.Fatalf("first frame: %v", err)
	}
	second, err := stream.Recv()
	if err != nil {
		t.Fatalf("second frame: %v", err)
	}
	if first.GetMacAddress() != mac || first.GetLayout() == nil || len(first.GetAsics()) != 312 || first.GetTimestamp() == nil {
		t.Fatalf("first frame: mac %q, layout %v, %d ASICs", first.GetMacAddress(), first.GetLayout() != nil, len(first.GetAsics()))
	}
	if second.GetLayout() != nil || len(second.GetAsics()) != 312 {
		t.Fatalf("second frame: layout %v, %d ASICs", second.GetLayout() != nil, len(second.GetAsics()))
	}

	unknown, err := telemetry.StreamASICHealth(ctx, &mdk.StreamASICHealthRequest{Identifier: "00:00:00:00:00:00"})
	if err == nil {
		_, err = unknown.Recv()
	}
	if status.Code(err) != codes.NotFound {
		t.Fatalf("unknown board: %v, want NotFound", err)
	}
}
