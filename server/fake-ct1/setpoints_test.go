package main

import (
	"context"
	"math"
	"net"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

var setpointCases = []containerSetpoint{
	{Name: "dry_cooler_auto_temp_c", Value: 31.5, Unit: "°C"},
	{Name: "coolant_pump_speed_hz", Value: 43.25, Unit: "Hz"},
	{Name: "dry_cooler_manual_speed_percent", Value: 63.5, Unit: "%"},
	{Name: "air_conditioner_1_temp_c", Value: 24.5, Unit: "°C"},
	{Name: "air_conditioner_2_temp_c", Value: 25.5, Unit: "°C"},
}

func assertSetpoint(t *testing.T, points []containerSetpoint, want containerSetpoint) {
	t.Helper()
	for _, point := range points {
		if point.Name == want.Name {
			if point != want {
				t.Fatalf("setpoint = %+v, want %+v", point, want)
			}
			return
		}
	}
	t.Fatalf("missing setpoint %s in %+v", want.Name, points)
}

func TestContainerSetpointsRoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, want := range setpointCases {
		t.Run(want.Name, func(t *testing.T) {
			var written setContainerSetpointResponse
			if code := postJSON(t, srv, "/api/v2/power/container/setpoints/"+want.Name, setContainerSetpointRequest{Value: want.Value}, &written); code != http.StatusOK {
				t.Fatalf("write status = %d, want 200", code)
			}
			assertSetpoint(t, []containerSetpoint{written.Setpoint}, want)
			var controls containerControlsResponse
			getJSON(t, srv, "/api/v2/power/container/controls", &controls)
			assertSetpoint(t, controls.Setpoints, want)
			var health containerTelemetryResponse
			getJSON(t, srv, "/api/v2/telemetry/container", &health)
			assertSetpoint(t, health.Setpoints, want)
		})
	}
	if code := postJSON(t, srv, "/api/v2/power/container/setpoints/missing", setContainerSetpointRequest{Value: 32}, nil); code != http.StatusNotFound {
		t.Fatalf("unknown setpoint status = %d, want 404", code)
	}
}

func TestContainerSetpointAcceptsFiniteFloat32Rounding(t *testing.T) {
	srv, _ := newTestServer(t)
	// This double rounds to the largest finite float32 PLC value.
	const value = 3.4028235e38
	if code := postJSON(t, srv, "/api/v2/power/container/setpoints/coolant_pump_speed_hz", setContainerSetpointRequest{Value: value}, nil); code != http.StatusOK {
		t.Fatalf("finite float32 wire value status = %d, want 200", code)
	}
}

func TestRPCContainerSetpointsRoundTrip(t *testing.T) {
	srv, state := newConnectTestServer(t)
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
	power := mdk.NewPowerServiceClient(conn)
	browser := mdkv2connect.NewPowerServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	for _, want := range setpointCases {
		t.Run(want.Name, func(t *testing.T) {
			written, err := power.SetContainerSetpoint(ctx, &mdk.SetContainerSetpointRequest{Name: want.Name, Value: want.Value})
			if err != nil {
				t.Fatal(err)
			}
			if point := written.GetSetpoint(); point.GetName() != want.Name || point.GetValue() != want.Value || point.GetUnit() != want.Unit {
				t.Fatalf("gRPC setpoint = %v, want %+v", point, want)
			}
			observed, err := browser.GetContainerControls(ctx, connect.NewRequest(&mdk.GetContainerControlsRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			points := []containerSetpoint{}
			for _, point := range observed.Msg.GetSetpoints() {
				points = append(points, containerSetpoint{Name: point.GetName(), Value: point.GetValue(), Unit: point.GetUnit()})
			}
			assertSetpoint(t, points, want)
			// A Connect write must also be readable through the REST surface.
			want.Value += 1
			writtenBrowser, err := browser.SetContainerSetpoint(ctx, connect.NewRequest(&mdk.SetContainerSetpointRequest{Name: want.Name, Value: want.Value}))
			if err != nil {
				t.Fatal(err)
			}
			if point := writtenBrowser.Msg.GetSetpoint(); point.GetValue() != want.Value || point.GetUnit() != want.Unit {
				t.Fatalf("Connect setpoint = %v, want %+v", point, want)
			}
			var controls containerControlsResponse
			getJSON(t, srv, "/api/v2/power/container/controls", &controls)
			assertSetpoint(t, controls.Setpoints, want)
		})
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 1e39, -1e39} {
		if _, err := power.SetContainerSetpoint(ctx, &mdk.SetContainerSetpointRequest{Name: "coolant_pump_speed_hz", Value: value}); status.Code(err) != codes.InvalidArgument {
			t.Errorf("non-float32 value %g: %v", value, err)
		}
	}
	if _, err := power.SetContainerSetpoint(ctx, &mdk.SetContainerSetpointRequest{Name: "missing", Value: 32}); status.Code(err) != codes.NotFound {
		t.Errorf("unknown setpoint: %v", err)
	}
}

func TestContainerSetpointsAffectDemoEquipment(t *testing.T) {
	srv, _ := newTestServer(t)
	var before containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &before)
	for _, point := range setpointCases[1:3] {
		if code := postJSON(t, srv, "/api/v2/power/container/setpoints/"+point.Name, setContainerSetpointRequest{Value: point.Value}, nil); code != http.StatusOK {
			t.Fatalf("%s write status = %d", point.Name, code)
		}
	}
	var automatic containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &automatic)
	closeTo(t, "pump follows speed command", automatic.Plant.CoolantPumpSpeedHz/before.Plant.CoolantPumpSpeedHz, 43.25/50, 1e-8)
	closeTo(t, "flow remains an imposed thermal condition", automatic.Plant.FlowRateLpm, before.Plant.FlowRateLpm, 1e-8)
	for i, fan := range automatic.CoolingFans {
		closeTo(t, "manual setpoint does not change automatic fan speed", fan.SpeedPct, before.CoolingFans[i].SpeedPct, 1e-8)
	}
	postJSON(t, srv, "/api/v2/power/container/controls/dry_cooler_auto", setContainerControlRequest{Enabled: false}, nil)
	var manual containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &manual)
	for _, fan := range manual.CoolingFans {
		closeTo(t, "manual fan follows speed command", fan.SpeedPct, 63.5, 1)
	}
	for _, name := range []string{"coolant_pump_speed_hz", "dry_cooler_manual_speed_percent"} {
		postJSON(t, srv, "/api/v2/power/container/setpoints/"+name, setContainerSetpointRequest{Value: 0}, nil)
	}
	var zero containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &zero)
	if zero.Plant.CoolantPumpSpeedHz != 0 {
		t.Fatalf("zero pump command speed = %g", zero.Plant.CoolantPumpSpeedHz)
	}
	for _, fan := range zero.CoolingFans {
		if fan.SpeedPct != 0 || fan.PowerW != 0 {
			t.Fatalf("zero manual command fan = %+v", fan)
		}
	}
	postJSON(t, srv, "/api/v2/power/container/controls/coolant_pump", setContainerControlRequest{Enabled: false}, nil)
	postJSON(t, srv, "/api/v2/power/container/controls/dry_cooler", setContainerControlRequest{Enabled: false}, nil)
	var off containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &off)
	if off.Plant.CoolantPumpSpeedHz != 0 {
		t.Fatalf("disabled pump speed = %g", off.Plant.CoolantPumpSpeedHz)
	}
	for _, fan := range off.CoolingFans {
		if fan.SpeedPct != 0 || fan.PowerW != 0 {
			t.Fatalf("disabled cooler fan = %+v", fan)
		}
	}
}
