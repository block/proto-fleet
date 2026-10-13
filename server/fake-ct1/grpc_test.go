package main

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestGRPCSharesContainerState(t *testing.T) {
	state := NewContainerState(ContainerConfig{ServerID: "grpc-test", Tanks: 6, ModulesPerTank: 16, SlotsPerModule: 3, AsicsPerBoard: 312, Seed: 1})
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
	ctx := context.Background()
	info, err := mdk.NewSystemServiceClient(conn).GetSystemInfo(ctx, &mdk.GetSystemInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if info.GetInfo().GetId() != "grpc-test" {
		t.Fatal(info)
	}
	layout, err := mdk.NewHashboardServiceClient(conn).GetContainerMap(ctx, &mdk.GetContainerMapRequest{})
	if err != nil {
		t.Fatal(err)
	}
	boards := 0
	for _, tank := range layout.GetContainerMap().Tanks {
		for _, m := range tank.Modules {
			boards += len(m.Slots)
		}
	}
	if boards != 288 {
		t.Fatalf("got %d boards", boards)
	}
	t.Logf("full-container protobuf bytes: %d", proto.Size(layout))
	jsonWire, err := protojson.Marshal(layout)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("equivalent uncompressed protojson bytes: %d", len(jsonWire))
	power := mdk.NewPowerServiceClient(conn)
	controls, err := power.GetContainerControls(ctx, &mdk.GetContainerControlsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(controls.Controls) == 0 {
		t.Fatal("no controls")
	}
	name := controls.Controls[0].Name
	response, err := power.SetContainerControl(ctx, &mdk.SetContainerControlRequest{Name: name, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetConfirmation().GetState() != mdk.PlantWriteConfirmationState_PLANT_WRITE_CONFIRMATION_STATE_CONFIRMED {
		t.Fatal(response)
	}
	state.mu.RLock()
	enabled := state.ContainerControls[name]
	state.mu.RUnlock()
	if enabled {
		t.Fatal("RPC write did not change shared REST state")
	}
	// Attaching the second transport must not reset recorded history.
	history := state.history
	NewRESTApiHandler(state)
	if state.history != history {
		t.Fatal("HTTP initialization replaced shared history")
	}
	telemetry, err := mdk.NewTelemetryServiceClient(conn).GetContainerHealth(ctx, &mdk.GetContainerHealthRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if telemetry.GetPlant().FacilityPowerW == nil {
		t.Fatal("missing facility power")
	}

	// Keep the simulator compatible with the collector's firmware PDU attribution.
	pdus := map[string]*mdk.PlantPowerDistribution{}
	for _, pdu := range telemetry.GetPowerDistributions() {
		pdus[pdu.GetComponentId()] = pdu
	}
	for _, tank := range layout.GetContainerMap().GetTanks() {
		id := fmt.Sprintf("pdu-%d", tank.GetId())
		if pdu := pdus[id]; pdu == nil || pdu.ActivePowerW == nil {
			t.Errorf("tank %d is missing its dedicated %s meter", tank.GetId(), id)
		}
		for _, module := range tank.GetModules() {
			want := fmt.Sprintf("%s-outlet-%02d", id, module.GetId())
			for _, slot := range module.GetSlots() {
				if slot.GetPowerFeedId() != want || slot.GetHashboard().GetLocation().GetPowerFeedId() != want {
					t.Errorf("tank %d module %d feed disagrees with firmware convention %s", tank.GetId(), module.GetId(), want)
				}
			}
		}
	}
}

// Compare equivalent wire representations, excluding network and normalization.
// The production client still retains diagnostic JSON, so this is not an
// end-to-end collector CPU benchmark or a hardware load estimate.
func BenchmarkContainerMapCodec(b *testing.B) {
	state := NewContainerState(ContainerConfig{ServerID: "benchmark", Tanks: 6, ModulesPerTank: 16, SlotsPerModule: 3, AsicsPerBoard: 312, Seed: 1})
	state.now = func() time.Time { return state.StartTime }
	api := &grpcAPI{state: state}
	layout, err := api.GetContainerMap(b.Context(), &mdk.GetContainerMapRequest{})
	if err != nil {
		b.Fatal(err)
	}
	for _, codec := range []struct {
		name      string
		marshal   func(proto.Message) ([]byte, error)
		unmarshal func([]byte, proto.Message) error
	}{{"protobuf", proto.Marshal, proto.Unmarshal}, {"protojson", protojson.Marshal, protojson.Unmarshal}} {
		data, err := codec.marshal(layout)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(codec.name+"/encode", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := codec.marshal(layout); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(codec.name+"/decode", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := codec.unmarshal(data, new(mdk.GetContainerMapResponse)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestGRPCScopedMiningPreservesScopeAndBoardFailures(t *testing.T) {
	state := NewContainerState(ContainerConfig{ServerID: "scoped-test", Tanks: 2, ModulesPerTank: 1, SlotsPerModule: 3, AsicsPerBoard: 312, Seed: 1})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(state)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := mdk.NewMiningServiceClient(conn)
	scope := &mdk.BoardScope{Modules: []*mdk.ModuleSelector{{Tank: 1, Module: 1}}}
	state.mu.Lock()
	for _, board := range state.eachBoard() {
		board.State = miningStateMining
	}
	state.mu.Unlock()
	stopped, err := client.StopMiningScoped(t.Context(), &mdk.StopMiningScopedRequest{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.BoardsStopped != 3 || len(stopped.Results) != 3 {
		t.Fatal(stopped)
	}
	state.mu.Lock()
	for _, board := range state.eachBoard() {
		if board.TankID == 1 && board.State != miningStateIdle {
			t.Error("selected board still mining")
		}
		if board.TankID == 2 && board.State != miningStateMining {
			t.Error("neighbor board affected")
		}
	}
	state.eachBoard()[0].Isolated = true
	state.mu.Unlock()
	started, err := client.StartMiningScoped(t.Context(), &mdk.StartMiningScopedRequest{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	if started.BoardsStarted != 2 || len(started.Results) != 3 {
		t.Fatal(started)
	}
	rejected := 0
	for _, result := range started.Results {
		if !result.Applied && result.Message == "board isolated" {
			rejected++
		}
	}
	if rejected != 1 {
		t.Fatal(started)
	}
}
