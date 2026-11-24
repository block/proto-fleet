package main

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestDemoBoardAndPsuFaultsDoNotFaultPowerFeeds(t *testing.T) {
	cfg := testConfig()
	cfg.Tanks, cfg.ModulesPerTank, cfg.DemoModuleStates = 6, 16, true
	state := newContainerState(cfg, time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	state.now = func() time.Time { return state.StartTime }
	handler, cleanup, err := newHTTPHandler(state, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	var layout containerMapResponse
	getJSON(t, srv, "/api/v2/container/map", &layout)
	boardFault := layout.ContainerMap.Tanks[2].Modules[7].Slots[0].Hashboard
	if len(boardFault.Health.Errors) != 1 || boardFault.Health.Errors[0].Cause != "HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT" {
		t.Fatalf("Tank 3 / Module 8 must retain its board-only fault: %+v", boardFault.Health.Errors)
	}
	psuFault := layout.ContainerMap.Tanks[1].Modules[10].Slots[2].Hashboard
	hasPsuFault := false
	for _, issue := range psuFault.Health.Errors {
		hasPsuFault = hasPsuFault || issue.Cause == "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY"
	}
	if !hasPsuFault || !psuFault.Health.PsuThermal.ThermalStateValid || !psuFault.Health.PsuThermal.OverTemperatureWarning {
		t.Fatalf("Tank 2 / Module 11 must retain its PSU fault preview: %+v", psuFault.Health)
	}

	var telemetry containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &telemetry)
	for _, feed := range telemetry.PowerFeeds {
		if feed.Faulted {
			t.Errorf("REST: board/PSU errors must not imply an upstream feed fault: %s", feed.PowerFeedID)
		}
	}
	for name, options := range map[string][]connect.ClientOption{
		"Connect": nil, "gRPC-Web": {connect.WithGRPCWeb()},
	} {
		power := mdkv2connect.NewPowerServiceClient(http.DefaultClient, srv.URL, options...)
		feeds, err := power.ListPowerFeeds(t.Context(), connect.NewRequest(&mdk.ListPowerFeedsRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		if len(feeds.Msg.PowerFeeds) != 96 {
			t.Fatalf("%s: expected 96 module feeds", name)
		}
		for _, feed := range feeds.Msg.PowerFeeds {
			if feed.Faulted == nil || feed.GetFaulted() {
				t.Errorf("%s: feed must explicitly report no fault: %s", name, feed.PowerFeedId)
			}
		}
	}
}

func TestPsuDiagnosticsAcrossTransports(t *testing.T) {
	for _, miningState := range []string{miningStateMining, miningStateIdle, miningStateDisconnected} {
		t.Run(miningState, func(t *testing.T) {
			srv, state := newConnectTestServer(t)
			module := state.Modules[0]
			for _, slot := range module.Slots {
				slot.Board.State = miningState
			}
			state.advance(state.StartTime)
			state.now = func() time.Time { return state.StartTime.Add(10 * time.Minute) }

			var raw json.RawMessage
			getJSON(t, srv, "/api/v2/container/map", &raw)
			rest := new(mdk.GetContainerMapResponse)
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, rest); err != nil {
				t.Fatal(err)
			}
			responses := map[string]*mdk.GetContainerMapResponse{"REST": rest}
			for name, options := range map[string][]connect.ClientOption{
				"Connect": nil, "gRPC-Web": {connect.WithGRPCWeb()},
			} {
				client := mdkv2connect.NewHashboardServiceClient(http.DefaultClient, srv.URL, options...)
				response, err := client.GetContainerMap(t.Context(), connect.NewRequest(&mdk.GetContainerMapRequest{}))
				if err != nil {
					t.Fatal(err)
				}
				responses[name] = response.Msg
			}
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
			responses["gRPC"], err = mdk.NewHashboardServiceClient(conn).GetContainerMap(t.Context(), &mdk.GetContainerMapRequest{})
			if err != nil {
				t.Fatal(err)
			}

			first := rest.ContainerMap.Tanks[0].Modules[0].Slots[0].Hashboard
			identity := first.GetPsuIdentity()
			if identity.GetVendor() == "" || identity.GetModel() == "" || identity.GetSerial() == "" || identity.GetRevision() == "" || identity.GetFirmwareVersion() == "" {
				t.Errorf("incomplete PSU identity: %v", identity)
			}
			electrical := first.Health.GetPsuElectrical()
			closeTo(t, "DC current reconciles with output power", electrical.GetOutputVoltage()*electrical.GetOutputCurrent(), electrical.GetOutputPower(), 1e-8)
			// The simulator's module feeds model balanced three-phase AC at unity power factor.
			closeTo(t, "AC current reconciles with input power", math.Sqrt(3)*electrical.GetInputVoltage()*electrical.GetInputCurrent(), electrical.GetInputPower(), 1e-8)
			if miningState == miningStateMining && (electrical.GetInputCurrent() <= 0 || electrical.GetOutputCurrent() <= 0) {
				t.Error("mining PSU currents must be positive")
			}
			if miningState == miningStateIdle && (electrical.GetInputCurrent() != 0 || electrical.GetOutputCurrent() != 0) {
				t.Error("stopped PSU currents must be zero")
			}
			thermal := first.Health.GetPsuThermal()
			if thermal.GetInletTemperature() <= 0 || thermal.GetSrHeatsinkTemperature() <= 0 || thermal.GetPfcHeatsinkTemperature() <= 0 {
				t.Fatalf("missing individual temperatures: %v", thermal)
			}
			if miningState == miningStateDisconnected {
				if thermal.GetThermalStateValid() || electrical.GetSampleAgeMs() != 600_000 || thermal.GetTemperaturesAgeMs() != 600_000 || thermal.GetThermalStateAgeMs() != 600_000 {
					t.Fatalf("offline PSU must retain aged evidence: %v / %v", electrical, thermal)
				}
			} else if !thermal.GetThermalStateValid() || electrical.GetSampleAgeMs() != 0 {
				t.Fatal("connected PSU must report fresh, valid evidence")
			}
			for transport, response := range responses {
				for _, slot := range response.ContainerMap.Tanks[0].Modules[0].Slots {
					board := slot.Hashboard
					if !proto.Equal(identity, board.PsuIdentity) || !proto.Equal(electrical, board.Health.PsuElectrical) || !proto.Equal(thermal, board.Health.PsuThermal) {
						t.Errorf("%s: module boards must share identical PSU identity and samples", transport)
					}
				}
			}
		})
	}
}
