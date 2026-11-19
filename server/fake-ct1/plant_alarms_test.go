package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
)

func alarmConfig(demo, resetUnsupported bool) ContainerConfig {
	cfg := testConfig()
	cfg.DemoPlantAlarms = demo
	cfg.AlarmResetUnsupported = resetUnsupported
	return cfg
}

func newAlarmConnectServer(t *testing.T, cfg ContainerConfig) (*httptest.Server, *ContainerState) {
	t.Helper()
	state := newContainerState(cfg, time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	state.now = func() time.Time { return state.StartTime }
	handler, cleanup, err := newHTTPHandler(state, "")
	if err != nil {
		t.Fatalf("new HTTP handler: %v", err)
	}
	t.Cleanup(cleanup)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, state
}

func alarmIDs(alarms []*mdk.PlantAlarm) []string {
	ids := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		ids = append(ids, alarm.GetId())
	}
	return ids
}

func TestPlantAlarmsAreQuietByDefault(t *testing.T) {
	state := newContainerState(testConfig(), time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	telemetry := state.containerTelemetry()
	if len(telemetry.Alarms) != 0 || telemetry.Plant.AlarmActive || telemetry.Plant.CriticalAlarmActive || telemetry.Plant.BuzzerActive {
		t.Fatalf("default plant should report no alarms: %+v", telemetry)
	}
	if !telemetry.AlarmMuteSupported {
		t.Fatal("health must keep reporting mute as supported")
	}
}

func TestPlantAlarmStateMachine(t *testing.T) {
	state := newContainerState(alarmConfig(true, false), time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	telemetry := state.containerTelemetry()
	if len(telemetry.Alarms) != 2 || !telemetry.Plant.AlarmActive || !telemetry.Plant.CriticalAlarmActive || !telemetry.Plant.BuzzerActive {
		t.Fatalf("demo alarms not seeded: %+v", telemetry)
	}

	// Mute only silences the buzzer.
	if err := state.muteContainerAlarm(); err != nil {
		t.Fatal(err)
	}
	telemetry = state.containerTelemetry()
	if telemetry.Plant.BuzzerActive || len(telemetry.Alarms) != 2 || !telemetry.Plant.AlarmActive {
		t.Fatalf("mute must leave alarms active: %+v", telemetry)
	}

	// Reset clears the latched alarm whose condition cleared and keeps the live one.
	if err := state.resetContainerAlarm(); err != nil {
		t.Fatal(err)
	}
	telemetry = state.containerTelemetry()
	if len(telemetry.Alarms) != 1 || telemetry.Alarms[0].ID != "tank-b-outlet-over-temperature" {
		t.Fatalf("reset must keep only the condition-present alarm: %+v", telemetry.Alarms)
	}
	if !telemetry.Plant.AlarmActive || !telemetry.Plant.CriticalAlarmActive || telemetry.Plant.BuzzerActive {
		t.Fatalf("flags after reset: %+v", telemetry.Plant)
	}
}

func TestPlantAlarmResetUnsupportedKeepsEverything(t *testing.T) {
	state := newContainerState(alarmConfig(true, true), time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	err := state.resetContainerAlarm()
	if err == nil {
		t.Fatal("reset must be refused when configured unsupported")
	}
	if len(state.plantAlarms) != 2 || !state.buzzerActive {
		t.Fatalf("a refused reset must not change state: %+v", state.plantAlarms)
	}
}

func TestConnectAlarmCapabilityHealthMuteAndReset(t *testing.T) {
	srv, _ := newAlarmConnectServer(t, alarmConfig(true, false))
	ctx := context.Background()
	options := []connect.ClientOption{connect.WithGRPCWeb()}
	power := mdkv2connect.NewPowerServiceClient(http.DefaultClient, srv.URL, options...)
	telemetry := mdkv2connect.NewTelemetryServiceClient(http.DefaultClient, srv.URL, options...)

	controls, err := power.GetContainerControls(ctx, connect.NewRequest(&mdk.GetContainerControlsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if !controls.Msg.GetAlarmMuteSupported() || !controls.Msg.GetAlarmResetSupported() {
		t.Fatalf("controls must advertise mute and reset: %v", controls.Msg)
	}

	health := func() *mdk.GetContainerHealthResponse {
		t.Helper()
		response, err := telemetry.GetContainerHealth(ctx, connect.NewRequest(&mdk.GetContainerHealthRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		return response.Msg
	}
	before := health()
	if got := alarmIDs(before.GetAlarms()); len(got) != 2 || got[0] != "coolant-low-pressure" {
		t.Fatalf("alarms = %v", got)
	}
	if before.GetAlarms()[0].GetSeverity() != mdk.Severity_SEVERITY_WARNING || before.GetAlarms()[1].GetSeverity() != mdk.Severity_SEVERITY_CRITICAL {
		t.Fatalf("severities = %v", before.GetAlarms())
	}
	plant := before.GetPlant()
	if plant.AlarmActive == nil || !*plant.AlarmActive || plant.CriticalAlarmActive == nil || !*plant.CriticalAlarmActive || plant.BuzzerActive == nil || !*plant.BuzzerActive {
		t.Fatalf("plant flags = %v", plant)
	}
	if before.AlarmMuteSupported == nil || !*before.AlarmMuteSupported {
		t.Fatal("health must report mute support")
	}

	if _, err := power.MuteContainerAlarm(ctx, connect.NewRequest(&mdk.MuteContainerAlarmRequest{})); err != nil {
		t.Fatal(err)
	}
	muted := health()
	if muted.GetPlant().BuzzerActive == nil || *muted.GetPlant().BuzzerActive || len(muted.GetAlarms()) != 2 {
		t.Fatalf("after mute: %v", muted)
	}
	if _, err := power.ResetContainerAlarm(ctx, connect.NewRequest(&mdk.ResetContainerAlarmRequest{})); err != nil {
		t.Fatal(err)
	}
	if got := alarmIDs(health().GetAlarms()); len(got) != 1 || got[0] != "tank-b-outlet-over-temperature" {
		t.Fatalf("after reset: %v", got)
	}
}

func TestConnectAlarmResetUnsupportedIsUnimplemented(t *testing.T) {
	srv, _ := newAlarmConnectServer(t, alarmConfig(true, true))
	ctx := context.Background()
	power := mdkv2connect.NewPowerServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
	controls, err := power.GetContainerControls(ctx, connect.NewRequest(&mdk.GetContainerControlsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if !controls.Msg.GetAlarmMuteSupported() || controls.Msg.AlarmResetSupported == nil || *controls.Msg.AlarmResetSupported {
		t.Fatalf("reset must be advertised as explicitly unsupported: %v", controls.Msg)
	}
	_, err = power.ResetContainerAlarm(ctx, connect.NewRequest(&mdk.ResetContainerAlarmRequest{}))
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("reset error = %v, want Unimplemented", err)
	}
	if _, err := power.MuteContainerAlarm(ctx, connect.NewRequest(&mdk.MuteContainerAlarmRequest{})); err != nil {
		t.Fatalf("mute must stay supported: %v", err)
	}
}

func TestRESTAlarmRoutes(t *testing.T) {
	srv, state := newTestServer(t)
	state.mu.Lock()
	state.plantAlarms = demoPlantAlarms()
	state.buzzerActive = true
	state.mu.Unlock()

	var controls containerControlsResponse
	getJSON(t, srv, "/api/v2/power/container/controls", &controls)
	if !controls.AlarmMuteSupported || !controls.AlarmResetSupported {
		t.Fatalf("controls = %+v", controls)
	}
	var health containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &health)
	if len(health.Alarms) != 2 || !health.Plant.BuzzerActive || !health.AlarmMuteSupported {
		t.Fatalf("health = %+v", health)
	}

	if code := postJSON(t, srv, "/api/v2/power/container/alarm/mute", struct{}{}, nil); code != http.StatusOK {
		t.Fatalf("mute status = %d", code)
	}
	getJSON(t, srv, "/api/v2/telemetry/container", &health)
	if health.Plant.BuzzerActive || len(health.Alarms) != 2 {
		t.Fatalf("after mute: %+v", health)
	}
	if code := postJSON(t, srv, "/api/v2/power/container/alarm/reset", struct{}{}, nil); code != http.StatusOK {
		t.Fatalf("reset status = %d", code)
	}
	getJSON(t, srv, "/api/v2/telemetry/container", &health)
	if len(health.Alarms) != 1 {
		t.Fatalf("after reset: %+v", health.Alarms)
	}

	state.mu.Lock()
	state.Config.AlarmResetUnsupported = true
	state.mu.Unlock()
	if code := postJSON(t, srv, "/api/v2/power/container/alarm/reset", struct{}{}, nil); code != http.StatusNotImplemented {
		t.Fatalf("unsupported reset status = %d, want 501", code)
	}
}
