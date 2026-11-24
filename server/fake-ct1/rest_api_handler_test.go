package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// testConfig builds a small container so tests stay fast while still exercising
// the multi-tank / multi-module / multi-slot tree.
func testConfig() ContainerConfig {
	return ContainerConfig{
		InstanceID:     "test",
		ServerID:       "ct1-test",
		Hostname:       "fake-ct1-test",
		Version:        "2.0.0",
		Tanks:          2,
		ModulesPerTank: 3,
		SlotsPerModule: 3,
		AsicsPerBoard:  312,
	}
}

func newTestServer(t *testing.T) (*httptest.Server, *ContainerState) {
	t.Helper()
	state := newContainerState(testConfig(), time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	state.now = func() time.Time { return state.StartTime }
	mux := http.NewServeMux()
	NewRESTApiHandler(state).RegisterRoutes(mux)
	srv := httptest.NewServer(jsonErrorMiddleware(mux))
	t.Cleanup(srv.Close)
	return srv, state
}

func getJSON(t *testing.T, srv *httptest.Server, path string, out any) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func postJSON(t *testing.T, srv *httptest.Server, path string, body any, out any) int {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

func TestContainerSetpointFloat32Bounds(t *testing.T) {
	for _, value := range []float64{1e39, -1e39, math.MaxFloat32, -math.MaxFloat32, 19.9, 20, 32, 55, 55.1} {
		srv, state := newTestServer(t)
		initial := state.ContainerSetpoints["dry_cooler_auto_temp_c"]
		status := postJSON(t, srv, "/api/v2/power/container/setpoints/dry_cooler_auto_temp_c", setContainerSetpointRequest{Value: value}, nil)
		wantStatus, wantValue := http.StatusOK, value
		if math.Abs(value) > math.MaxFloat32 {
			wantStatus, wantValue = http.StatusBadRequest, initial
		}
		if status != wantStatus {
			t.Errorf("setpoint %g: status %d, want %d", value, status, wantStatus)
		}
		state.mu.RLock()
		got := state.ContainerSetpoints["dry_cooler_auto_temp_c"]
		state.mu.RUnlock()
		if got != wantValue {
			t.Errorf("setpoint %g: persisted %g, want %g", value, got, wantValue)
		}
	}
}

func TestTopology(t *testing.T) {
	state := NewContainerState(testConfig())
	if got, want := state.ModuleCount(), 6; got != want {
		t.Errorf("module count = %d, want %d", got, want)
	}
	if got, want := state.BoardCount(), 18; got != want {
		t.Errorf("board count = %d, want %d", got, want)
	}
}

func TestFullContainerTopology(t *testing.T) {
	cfg := ContainerConfig{InstanceID: "full", Tanks: defaultTanks, ModulesPerTank: defaultModulesPerTank, SlotsPerModule: defaultSlotsPerModule, AsicsPerBoard: defaultAsicsPerBoard}
	state := NewContainerState(cfg)
	if got, want := state.ModuleCount(), 96; got != want {
		t.Errorf("full module count = %d, want 96", got)
	}
	if got, want := state.BoardCount(), 288; got != want {
		t.Errorf("full board count = %d, want 288", got)
	}
}

func TestDemoModuleStates(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			cfg := testConfig()
			cfg.Tanks = 6
			cfg.ModulesPerTank = 16
			cfg.DemoModuleStates = enabled
			state := NewContainerState(cfg)
			mux := http.NewServeMux()
			NewRESTApiHandler(state).RegisterRoutes(mux)
			srv := httptest.NewServer(jsonErrorMiddleware(mux))
			defer srv.Close()
			var response containerMapResponse
			getJSON(t, srv, "/api/v2/container/map", &response)
			if len(response.ContainerMap.Tanks) != 6 {
				t.Fatal("missing tanks")
			}
			for _, tank := range response.ContainerMap.Tanks {
				for _, module := range tank.Modules {
					for index, slot := range module.Slots {
						if slot.Hashboard == nil {
							t.Fatal("missing board")
						}
						board := slot.Hashboard
						wantState, wantIssues, wantWarning := miningStateMining, 0, false
						if enabled && tank.ID == 2 {
							switch module.ID {
							case 3:
								wantState = miningStateIdle
							case 6:
								wantState = miningStateDisconnected
							case 11:
								wantState = miningStateError
								wantIssues = index + 1
								wantWarning = true
							}
						}
						if enabled && tank.ID == 4 {
							switch module.ID {
							case 4:
								wantState = miningStateIdle
							case 9:
								wantState = miningStateDisconnected
							}
						}
						if enabled && tank.ID == 3 && module.ID == 8 && index == 0 {
							wantState = miningStateError
							wantIssues = 1
						}
						if board.MiningState != wantState || len(board.Health.Errors) != wantIssues || board.Health.PsuThermal.OverTemperatureWarning != wantWarning {
							t.Errorf("tank %d module %d slot %d: state=%s errors=%d PSU warning=%t; want %s/%d/%t", tank.ID, module.ID, index, board.MiningState, len(board.Health.Errors), board.Health.PsuThermal.OverTemperatureWarning, wantState, wantIssues, wantWarning)
						}
						if wantState != miningStateMining && (board.Health.Hashrate != 0 || board.Health.Power != 0) {
							t.Error("non-mining board reports active power/hashrate")
						}
					}
				}
			}
			var status miningStatusResponse
			getJSON(t, srv, "/api/v2/mining/status", &status)
			wantActive := 288
			if enabled {
				wantActive = 272
			}
			if status.ActiveBoards != wantActive {
				t.Errorf("active boards = %d, want %d", status.ActiveBoards, wantActive)
			}
			for _, tankModule := range []moduleCoord{{Tank: 2, Module: 11}, {Tank: 3, Module: 8}} {
				if status := postJSON(t, srv, "/api/v2/mining/scoped/start", scopeOnlyRequest{Scope: scope{Modules: []moduleCoord{tankModule}}}, nil); status != http.StatusOK {
					t.Fatalf("start status = %d", status)
				}
			}
			var restarted containerMapResponse
			getJSON(t, srv, "/api/v2/container/map", &restarted)
			for _, tank := range restarted.ContainerMap.Tanks {
				for _, module := range tank.Modules {
					for _, slot := range module.Slots {
						if len(slot.Hashboard.Health.Errors) != 0 || slot.Hashboard.Health.PsuThermal.OverTemperatureWarning {
							t.Error("restarted board still reports demo errors")
						}
					}
				}
			}
		})
	}
}

func TestSystemInfo(t *testing.T) {
	srv, _ := newTestServer(t)
	var resp systemInfoResponse
	getJSON(t, srv, "/api/v2/system/info", &resp)
	if resp.Info.Product != "CT1" {
		t.Errorf("product = %q, want CT1", resp.Info.Product)
	}
	if resp.Info.Version == "" {
		t.Error("version is empty")
	}
}

func TestSystemStatus(t *testing.T) {
	srv, _ := newTestServer(t)
	var resp systemStatusResponse
	getJSON(t, srv, "/api/v2/system/status", &resp)
	if resp.Status.TotalBoards != 18 {
		t.Errorf("totalBoards = %d, want 18", resp.Status.TotalBoards)
	}
	if resp.Status.ActiveBoards != 18 {
		t.Errorf("activeBoards = %d, want 18", resp.Status.ActiveBoards)
	}
	if resp.Status.Health != "HEALTH_STATUS_HEALTHY" {
		t.Errorf("health = %q, want HEALTH_STATUS_HEALTHY", resp.Status.Health)
	}
}

func TestContainerMap(t *testing.T) {
	srv, _ := newTestServer(t)
	var resp containerMapResponse
	getJSON(t, srv, "/api/v2/container/map", &resp)
	if len(resp.ContainerMap.Tanks) != 2 {
		t.Fatalf("tanks = %d, want 2", len(resp.ContainerMap.Tanks))
	}
	boards := 0
	for _, tank := range resp.ContainerMap.Tanks {
		for _, m := range tank.Modules {
			for _, slot := range m.Slots {
				if slot.Hashboard == nil {
					t.Fatalf("slot %s has no hashboard", slot.BreakerID)
				}
				if slot.Hashboard.ExpectedAsicCount != 312 {
					t.Errorf("expectedAsicCount = %d, want 312", slot.Hashboard.ExpectedAsicCount)
				}
				if slot.Hashboard.Location.HashboardPosition < 1 || slot.Hashboard.Location.HashboardPosition > 3 {
					t.Errorf("hashboardPosition = %d, want 1-3", slot.Hashboard.Location.HashboardPosition)
				}
				boards++
			}
		}
	}
	if boards != 18 {
		t.Errorf("mapped boards = %d, want 18", boards)
	}
}

func TestMiningStatus(t *testing.T) {
	srv, _ := newTestServer(t)
	var resp miningStatusResponse
	getJSON(t, srv, "/api/v2/mining/status", &resp)
	if resp.OperatingState != "MINING_OPERATING_STATE_MINING" {
		t.Errorf("operatingState = %q", resp.OperatingState)
	}
	if resp.ActiveBoards != 18 {
		t.Errorf("activeBoards = %d, want 18", resp.ActiveBoards)
	}
	if resp.Hashrate <= 0 {
		t.Error("hashrate should be positive while mining")
	}
	if resp.BoardsByState[miningStateMining] != 18 {
		t.Errorf("boardsByState mining = %d, want 18", resp.BoardsByState[miningStateMining])
	}
}

func TestPools(t *testing.T) {
	srv, _ := newTestServer(t)
	var pools listPoolsResponse
	getJSON(t, srv, "/api/v2/pools", &pools)
	if len(pools.Pools) != 3 {
		t.Fatalf("pools = %d, want 3", len(pools.Pools))
	}
	if pools.Pools[0].URL != defaultActivePool || !pools.Pools[0].Enabled {
		t.Errorf("pool = %+v, want enabled default pool", pools.Pools[0])
	}

	var diagnostics poolDiagnosticsResponse
	getJSON(t, srv, "/api/v2/pools/diagnostics", &diagnostics)
	if len(diagnostics.Pools) != 3 || diagnostics.Pools[0].ConnState != "POOL_CONN_STATE_ACTIVE" {
		t.Fatalf("diagnostics = %+v, want primary active and two backups", diagnostics.Pools)
	}
	for i, pool := range pools.Pools {
		if pool.ID != int64str(i+1) || pool.Priority != int32(i) || !pool.Enabled {
			t.Errorf("pool %d = %+v, want stable ID, ordered priority and enabled", i, pool)
		}
		diagnostic := diagnostics.Pools[i]
		if diagnostic.PoolID != pool.ID || diagnostic.PoolName != pool.Name {
			t.Errorf("diagnostic %+v does not match pool %+v", diagnostic, pool)
		}
		if i > 0 && (diagnostic.ConnState != "POOL_CONN_STATE_IDLE" || diagnostic.IsActive || !diagnostic.IsReady) {
			t.Errorf("backup diagnostic = %+v, want ready standby", diagnostic)
		}
	}
	if diagnostics.ActivePoolID == nil || *diagnostics.ActivePoolID != 1 {
		t.Errorf("activePoolId = %v, want 1", diagnostics.ActivePoolID)
	}

	postJSON(t, srv, "/api/v2/mining/stop", struct{}{}, nil)
	var stopped poolDiagnosticsResponse
	getJSON(t, srv, "/api/v2/pools/diagnostics", &stopped)
	if stopped.ActivePoolID != nil {
		t.Errorf("activePoolId = %v, want omitted while stopped", *stopped.ActivePoolID)
	}
	if len(stopped.Pools) != 3 {
		t.Fatalf("diagnostics = %+v, want three idle pools", stopped.Pools)
	}
	for _, pool := range stopped.Pools {
		if pool.ConnState != "POOL_CONN_STATE_IDLE" || pool.IsActive {
			t.Errorf("diagnostic = %+v, want idle pool", pool)
		}
	}
}

func TestContainerTelemetryPUE(t *testing.T) {
	srv, _ := newTestServer(t)
	var resp containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &resp)
	if resp.Plant.PUE < 1.0 || resp.Plant.PUE > 2.0 {
		t.Errorf("pue = %f, want ~1.04", resp.Plant.PUE)
	}
	if resp.Plant.FacilityPowerW <= resp.Plant.TotalPowerW {
		t.Errorf("facility power %f should exceed IT power %f", resp.Plant.FacilityPowerW, resp.Plant.TotalPowerW)
	}
	if len(resp.Phases) != 3 {
		t.Errorf("phases = %d, want 3", len(resp.Phases))
	}
	if len(resp.CoolingFans) == 0 || len(resp.PowerFeeds) == 0 || len(resp.PowerDistributions) == 0 {
		t.Error("container telemetry should include fans, feeds, and power distributions")
	}
}

func TestContainerCoolingActivityFollowsControls(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, tc := range []struct {
		name   string
		pump   bool
		cooler bool
	}{
		{name: "both off"},
		{name: "pump only", pump: true},
		{name: "cooler only", cooler: true},
		{name: "both on", pump: true, cooler: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, enabled := range map[string]bool{"coolant_pump": tc.pump, "dry_cooler": tc.cooler} {
				if status := postJSON(t, srv, "/api/v2/power/container/controls/"+name, setContainerControlRequest{Enabled: enabled}, nil); status != http.StatusOK {
					t.Fatalf("%s control status = %d, want 200", name, status)
				}
			}
			var telemetry containerTelemetryResponse
			getJSON(t, srv, "/api/v2/telemetry/container", &telemetry)
			if telemetry.Plant.CoolantPumpActive != tc.pump {
				t.Errorf("pump active = %t, want %t", telemetry.Plant.CoolantPumpActive, tc.pump)
			}
			if telemetry.Plant.HeatRejectionActive != tc.cooler {
				t.Errorf("cooler active = %t, want %t", telemetry.Plant.HeatRejectionActive, tc.cooler)
			}
			// Inspect the JSON keys too: an observed zero must not disappear as
			// an omitted optional sensor when the pump is switched off.
			var wire struct {
				Plant map[string]any `json:"plant"`
			}
			getJSON(t, srv, "/api/v2/telemetry/container", &wire)
			for _, field := range []string{
				"coolantPumpSpeedHz", "pumpInletPressurePa", "pumpOutletPressurePa", "pumpPressureDiffPa",
				"heatRejectionInletPressurePa", "heatRejectionOutletPressurePa", "heatRejectionPressureDiffPa",
			} {
				value, ok := wire.Plant[field].(float64)
				if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
					t.Fatalf("%s must be a finite JSON number, got %v", field, wire.Plant[field])
				}
			}
			plant := telemetry.Plant
			if plant.PumpInletPressurePa <= 0 || plant.HeatRejectionOutletPressurePa <= 0 {
				t.Fatalf("expected positive static pressure: %+v", plant)
			}
			closeTo(t, "pump pressure rise", plant.PumpPressureDiffPa, plant.PumpOutletPressurePa-plant.PumpInletPressurePa, 1e-8)
			closeTo(t, "cooler pressure drop", plant.HeatRejectionPressureDiffPa, plant.HeatRejectionInletPressurePa-plant.HeatRejectionOutletPressurePa, 1e-8)
			if tc.pump {
				if plant.CoolantPumpSpeedHz < 49 || plant.CoolantPumpSpeedHz > 51 || plant.PumpPressureDiffPa <= 0 || plant.HeatRejectionPressureDiffPa <= 0 {
					t.Fatalf("running pump telemetry: %+v", plant)
				}
			} else if plant.CoolantPumpSpeedHz != 0 || plant.PumpPressureDiffPa != 0 || plant.HeatRejectionPressureDiffPa != 0 {
				t.Fatalf("stopped pump must report zero speed and differentials: %+v", plant)
			}
		})
	}
}

func TestContainerDryCoolerSettingsPersistIntoTelemetry(t *testing.T) {
	srv, _ := newTestServer(t)

	var controlResponse setContainerControlResponse
	if status := postJSON(t, srv, "/api/v2/power/container/controls/dry_cooler_auto", setContainerControlRequest{Enabled: false}, &controlResponse); status != http.StatusOK {
		t.Fatalf("control status = %d, want 200", status)
	}
	if controlResponse.Control.Enabled || !controlResponse.Control.StateKnown {
		t.Errorf("control response = %+v, want known and disabled", controlResponse.Control)
	}

	var setpointResponse setContainerSetpointResponse
	if status := postJSON(t, srv, "/api/v2/power/container/setpoints/dry_cooler_auto_temp_c", setContainerSetpointRequest{Value: 32.5}, &setpointResponse); status != http.StatusOK {
		t.Fatalf("setpoint status = %d, want 200", status)
	}
	if setpointResponse.Setpoint.Value != 32.5 {
		t.Errorf("setpoint response = %+v, want 32.5", setpointResponse.Setpoint)
	}

	var telemetry containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &telemetry)
	if telemetry.Setpoints[0].Value != 32.5 {
		t.Errorf("telemetry target = %f, want 32.5", telemetry.Setpoints[0].Value)
	}
	for _, control := range telemetry.Controls {
		if control.Name == "dry_cooler_auto" && control.Enabled {
			t.Error("telemetry dry_cooler_auto should remain disabled")
		}
	}
}

func TestTargetPower(t *testing.T) {
	srv, _ := newTestServer(t)
	var initial targetPowerResponse
	getJSON(t, srv, "/api/v2/mining/target-power", &initial)
	if initial.Watts != boardPowerTargetW {
		t.Fatalf("initial watts = %f, want %f", initial.Watts, boardPowerTargetW)
	}

	setTarget := func(watts float64) {
		t.Helper()
		requestBody, err := json.Marshal(targetPowerRequest{Watts: watts, PerformanceMode: "PERFORMANCE_MODE_EFFICIENCY"})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v2/mining/target-power", bytes.NewReader(requestBody))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT target-power status = %d", resp.StatusCode)
		}
	}

	setTarget(2800)

	var updated targetPowerResponse
	getJSON(t, srv, "/api/v2/mining/target-power", &updated)
	if updated.Watts != 2800 {
		t.Fatalf("updated watts = %f, want 2800", updated.Watts)
	}

	setTarget(boardPowerTargetMaxW + 1000)
	getJSON(t, srv, "/api/v2/mining/target-power", &updated)
	if updated.Watts != boardPowerTargetMaxW {
		t.Fatalf("clamped watts = %f, want %f", updated.Watts, boardPowerTargetMaxW)
	}

	setTarget(1)
	getJSON(t, srv, "/api/v2/mining/target-power", &updated)
	if updated.Watts != boardPowerTargetMinW {
		t.Fatalf("clamped watts = %f, want %f", updated.Watts, boardPowerTargetMinW)
	}
}

func TestTimeSeries(t *testing.T) {
	srv, state := newTestServer(t)
	var response timeSeriesResponse
	getJSON(t, srv, "/api/v2/telemetry/timeseries?metric=TIME_SERIES_METRIC_HASHRATE&range.start=2026-09-17T11:00:00Z&range.end=2026-09-17T12:00:00Z&bucketSize=BUCKET_SIZE_5M", &response)
	if len(response.Buckets) != 12 {
		t.Fatalf("buckets = %d, want 12", len(response.Buckets))
	}
	if response.Unit != "TH/s" || response.Buckets[0].Avg <= 0 {
		t.Fatalf("unexpected time-series response: unit=%q avg=%f", response.Unit, response.Buckets[0].Avg)
	}

	var fractionalTimestampResponse timeSeriesResponse
	getJSON(t, srv, "/api/v2/telemetry/timeseries?metric=TIME_SERIES_METRIC_HASHRATE&range.start=2026-09-17T11:00:00.000Z&range.end=2026-09-17T12:00:00.000Z&bucketSize=BUCKET_SIZE_5M", &fractionalTimestampResponse)
	if len(fractionalTimestampResponse.Buckets) != 12 {
		t.Fatalf("fractional timestamp buckets = %d, want 12", len(fractionalTimestampResponse.Buckets))
	}

	var boardResponse timeSeriesResponse
	getJSON(t, srv, "/api/v2/telemetry/timeseries?metric=TIME_SERIES_METRIC_HASHRATE&range.start=2026-09-17T11:00:00Z&range.end=2026-09-17T12:00:00Z&bucketSize=BUCKET_SIZE_5M&identifier="+state.eachBoard()[0].MAC, &boardResponse)
	if boardResponse.Buckets[0].Avg >= response.Buckets[0].Avg {
		t.Fatalf("board average %f should be below container average %f", boardResponse.Buckets[0].Avg, response.Buckets[0].Avg)
	}
}

func TestTimeSeriesIdleAvailability(t *testing.T) {
	srv, state := newTestServer(t)
	postJSON(t, srv, "/api/v2/mining/stop", struct{}{}, nil)
	state.mu.Lock()
	state.now = func() time.Time { return state.StartTime.Add(time.Hour) }
	state.mu.Unlock()
	for _, tc := range []struct {
		metric string
		count  int64str
		zero   bool
	}{
		{timeSeriesMetricTemperature, 5, false}, {timeSeriesMetricHashrate, 5, true}, {timeSeriesMetricPower, 5, true},
		{"TIME_SERIES_METRIC_PLANT_POWER", 5, true}, {"TIME_SERIES_METRIC_PLANT_FACILITY_POWER", 5, false},
	} {
		var result timeSeriesResponse
		getJSON(t, srv, "/api/v2/telemetry/timeseries?metric="+tc.metric+"&range.start=2026-09-18T00:00:00Z&range.end=2026-09-18T01:00:00Z&bucketSize=BUCKET_SIZE_5M", &result)
		for _, bucket := range result.Buckets {
			if bucket.Count != tc.count || (bucket.Avg == 0) != tc.zero {
				t.Fatalf("%s: %+v", tc.metric, bucket)
			}
		}
	}
	// Stopping now must not change the initial scenario's pre-start history.
	var past timeSeriesResponse
	getJSON(t, srv, "/api/v2/telemetry/timeseries?metric="+timeSeriesMetricHashrate+"&range.start=2026-09-17T11:00:00Z&range.end=2026-09-17T12:00:00Z&bucketSize=BUCKET_SIZE_5M", &past)
	if past.Buckets[0].Avg <= 0 {
		t.Fatal("stop rewrote past mining")
	}
}

func TestModuleRunIn(t *testing.T) {
	srv, _ := newTestServer(t)
	var response moduleRunInResponse
	config := map[string]any{
		"sampleInterval": "10s", "cooldownDuration": "120s", "stopMiningOnFinish": true,
		"tuningSweep": map[string]any{
			"dwellDuration": "1800s",
			"powerTarget":   map[string]any{"startPowerTargetWatts": 1200, "endPowerTargetWatts": 1200, "stepPowerTargetWatts": 1},
		},
	}
	status := postJSON(t, srv, "/api/v2/modules/T1-M01/run-in?taskId=test-task", config, &response)
	if status != http.StatusOK {
		t.Fatalf("run-in status = %d", status)
	}
	if response.TaskID != "test-task" || response.ModuleID != "T1-M01" || len(response.Boards) != 3 {
		t.Fatalf("unexpected run-in response: %+v", response)
	}
}

func TestPlantTimeSeries(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, test := range []struct{ metric, identifier, unit string }{
		{"PLANT_POWER", "", "W"},
		{"PLANT_FACILITY_POWER", "", "W"},
		{"PLANT_HEAT_REJECTION_POWER", "", "W"},
		{"PLANT_DISTRIBUTION_POWER", "pdu-1", "W"},
		{"PLANT_FAN_POWER", "dry-cooler-fan-1", "W"},
		{"PLANT_INSIDE_AMBIENT_TEMPERATURE", "", "C"},
		{"PLANT_OUTSIDE_AMBIENT_TEMPERATURE", "", "C"},
		{"PLANT_INLET_TEMPERATURE", "", "C"},
		{"PLANT_OUTLET_TEMPERATURE", "", "C"},
	} {
		t.Run(test.metric, func(t *testing.T) {
			var response timeSeriesResponse
			path := "/api/v2/telemetry/timeseries?metric=TIME_SERIES_METRIC_" + test.metric + "&identifier=" + test.identifier + "&range.start=2026-09-17T11:00:00Z&range.end=2026-09-17T12:00:00Z&bucketSize=BUCKET_SIZE_5M"
			getJSON(t, srv, path, &response)
			if response.Unit != test.unit || len(response.Buckets) != 12 || response.Buckets[0].Avg <= 0 {
				t.Fatalf("unexpected plant history: %+v", response)
			}
		})
	}
	response, err := http.Get(srv.URL + "/api/v2/telemetry/timeseries?metric=TIME_SERIES_METRIC_PLANT_FAN_POWER&identifier=not-a-fan&range.start=2026-09-17T11:00:00Z&range.end=2026-09-17T12:00:00Z&bucketSize=BUCKET_SIZE_5M")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown fan status = %d", response.StatusCode)
	}
}

func TestPlantFanHistoryMatchesAdvertisedFans(t *testing.T) {
	srv, _ := newTestServer(t)
	var telemetry containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &telemetry)
	if len(telemetry.CoolingFans) != 10 {
		t.Fatalf("advertised fans = %d, want 10", len(telemetry.CoolingFans))
	}
	const query = "/api/v2/telemetry/timeseries?metric=TIME_SERIES_METRIC_PLANT_FAN_POWER&range.start=2026-09-17T11:00:00Z&range.end=2026-09-17T12:00:00Z&bucketSize=BUCKET_SIZE_5M&identifier="
	for _, fan := range telemetry.CoolingFans {
		t.Run(fan.ComponentID, func(t *testing.T) {
			var history timeSeriesResponse
			getJSON(t, srv, query+fan.ComponentID, &history)
			if history.Unit != "W" || len(history.Buckets) != 12 {
				t.Fatalf("unexpected fan history: %+v", history)
			}
			for _, bucket := range history.Buckets {
				if bucket.Count != 5 || bucket.Avg <= 0 {
					t.Fatalf("unexpected fan bucket: %+v", bucket)
				}
			}
		})
	}
	for _, identifier := range []string{"", "not-a-fan", "dry-cooler-fan-0", "dry-cooler-fan-11", "dry-cooler-fan-01"} {
		t.Run("unknown/"+identifier, func(t *testing.T) {
			response, err := http.Get(srv.URL + query + identifier)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("unknown fan status = %d, want 404", response.StatusCode)
			}
		})
	}
}

func TestHashboardAsics(t *testing.T) {
	srv, state := newTestServer(t)
	mac := state.eachBoard()[0].MAC
	var resp asicsResponse
	getJSON(t, srv, "/api/v2/hashboards/"+mac+"/asics", &resp)
	if len(resp.Asics) != 312 {
		t.Errorf("asics = %d, want 312", len(resp.Asics))
	}
	if resp.Layout.TotalAsics != 312 {
		t.Errorf("layout totalAsics = %d, want 312", resp.Layout.TotalAsics)
	}
	if resp.Layout.StackCount*resp.Layout.AsicsPerStack != 312 {
		t.Errorf("stackCount*asicsPerStack = %d, want 312", resp.Layout.StackCount*resp.Layout.AsicsPerStack)
	}
}

func TestHashboardAsicsBySerial(t *testing.T) {
	srv, state := newTestServer(t)
	serial := state.eachBoard()[0].Serial
	var resp asicsResponse
	getJSON(t, srv, "/api/v2/hashboards/"+serial+"/asics", &resp)
	if len(resp.Asics) != 312 {
		t.Errorf("asics by serial = %d, want 312", len(resp.Asics))
	}
}

func TestHashboardAsicsUnknown(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/api/v2/hashboards/does-not-exist/asics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestScopedPowerByModule(t *testing.T) {
	srv, state := newTestServer(t)
	req := scopedPowerRequest{Scope: scope{Modules: []moduleCoord{{Tank: 1, Module: 1}}}, Watts: 2700}
	var resp setPowerScopedResponse
	status := postJSON(t, srv, "/api/v2/mining/scoped/power", req, &resp)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if resp.BoardsUpdated != 3 {
		t.Errorf("boardsUpdated = %d, want 3", resp.BoardsUpdated)
	}
	// Verify state mutated on the matched module's boards only.
	state.mu.RLock()
	defer state.mu.RUnlock()
	for _, b := range state.eachBoard() {
		if b.TankID == 1 && b.ModuleNum == 1 {
			if !b.ScopedWattsSet || b.ScopedWatts != 2700 {
				t.Errorf("board %s not updated: set=%v watts=%f", b.MAC, b.ScopedWattsSet, b.ScopedWatts)
			}
		} else if b.ScopedWattsSet {
			t.Errorf("board %s outside scope was updated", b.MAC)
		}
	}
}

func TestScopedPowerClamps(t *testing.T) {
	srv, _ := newTestServer(t)
	req := scopedPowerRequest{Watts: 99999} // above max, empty scope = all boards
	var resp setPowerScopedResponse
	postJSON(t, srv, "/api/v2/mining/scoped/power", req, &resp)
	if resp.BoardsUpdated != 18 {
		t.Errorf("boardsUpdated = %d, want 18 (empty scope = all)", resp.BoardsUpdated)
	}
	for _, r := range resp.Results {
		if r.Message == "" {
			t.Errorf("expected clamp message for board %s", r.MACAddress)
		}
	}
}

func TestScopedPowerClear(t *testing.T) {
	srv, state := newTestServer(t)
	postJSON(t, srv, "/api/v2/mining/scoped/power", scopedPowerRequest{Watts: 2700}, &setPowerScopedResponse{})
	var resp clearPowerScopedResponse
	postJSON(t, srv, "/api/v2/mining/scoped/power/clear", scopeOnlyRequest{}, &resp)
	if resp.BoardsUpdated != 18 {
		t.Errorf("boardsUpdated = %d, want 18", resp.BoardsUpdated)
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	for _, b := range state.eachBoard() {
		if b.ScopedWattsSet {
			t.Errorf("board %s scoped watts not cleared", b.MAC)
		}
	}
}

func TestScopedStartStop(t *testing.T) {
	srv, state := newTestServer(t)

	// Stop a single board's module via scope.
	var stopResp stopScopedResponse
	postJSON(t, srv, "/api/v2/mining/scoped/stop", scopeOnlyRequest{Scope: scope{Modules: []moduleCoord{{Tank: 2, Module: 2}}}}, &stopResp)
	if stopResp.BoardsStopped != 3 {
		t.Errorf("boardsStopped = %d, want 3", stopResp.BoardsStopped)
	}

	var startResp startScopedResponse
	postJSON(t, srv, "/api/v2/mining/scoped/start", scopeOnlyRequest{Scope: scope{Modules: []moduleCoord{{Tank: 2, Module: 2}}}}, &startResp)
	if startResp.BoardsStarted != 3 {
		t.Errorf("boardsStarted = %d, want 3", startResp.BoardsStarted)
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	for _, b := range state.eachBoard() {
		if b.TankID == 2 && b.ModuleNum == 2 && b.State != miningStateMining {
			t.Errorf("board %s state = %q, want mining", b.MAC, b.State)
		}
	}
}

func TestModuleFixedVoltageSetAndClear(t *testing.T) {
	srv, state := newTestServer(t)
	moduleID := state.Modules[0].ModuleID

	var setResp moduleVoltageResponse
	status := postPut(t, srv, "/api/v2/mining/modules/"+moduleID+"/voltage/fixed", setModuleVoltageRequest{Millivolts: 28000}, &setResp)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d", status)
	}
	if got := setResp.Setting.FixedMillivolts; got == nil || *got != 28000 {
		t.Errorf("fixedMillivolts = %v, want 28000", got)
	}
	if setResp.BoardsUpdated != 3 {
		t.Errorf("boardsUpdated = %d, want 3", setResp.BoardsUpdated)
	}

	var clrResp moduleVoltageResponse
	status = doDelete(t, srv, "/api/v2/mining/modules/"+moduleID+"/voltage/fixed", &clrResp)
	if status != http.StatusOK {
		t.Fatalf("DELETE status = %d", status)
	}
	if got := clrResp.Setting.FixedMillivolts; got != nil {
		t.Errorf("fixedMillivolts after clear = %v, want absent (automatic)", *got)
	}
}

func TestModuleFixedVoltageUnknown(t *testing.T) {
	srv, _ := newTestServer(t)
	status := postPut(t, srv, "/api/v2/mining/modules/NOPE/voltage/fixed", setModuleVoltageRequest{Millivolts: 28000}, nil)
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestMiningStartStopGlobal(t *testing.T) {
	srv, state := newTestServer(t)
	if status := postJSON(t, srv, "/api/v2/mining/stop", struct{}{}, nil); status != http.StatusOK {
		t.Fatalf("stop status = %d", status)
	}
	var st systemStatusResponse
	getJSON(t, srv, "/api/v2/system/status", &st)
	if st.Status.Mining {
		t.Error("expected mining=false after stop")
	}
	if st.Status.ActiveBoards != 0 {
		t.Errorf("activeBoards = %d, want 0 after stop", st.Status.ActiveBoards)
	}
	var telemetry containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &telemetry)
	feedIDs := make(map[string]struct{})
	for _, board := range state.eachBoard() {
		feedIDs[board.PowerFeedID] = struct{}{}
	}
	if len(telemetry.PowerFeeds) != len(feedIDs) {
		t.Fatalf("powerFeeds = %d, want %d after stop", len(telemetry.PowerFeeds), len(feedIDs))
	}
	for _, feed := range telemetry.PowerFeeds {
		if feed.Active || feed.PowerW != 0 || feed.CurrentA != 0 {
			t.Errorf("stopped feed = %+v, want inactive with zero power and current", feed)
		}
	}
	if len(telemetry.PowerDistributions) != state.Config.Tanks || telemetry.PowerDistributions[0].ActiveFeedCount != 0 {
		t.Errorf("active feed count after stop = %+v, want 0", telemetry.PowerDistributions)
	}
	var history timeSeriesResponse
	getJSON(t, srv, "/api/v2/telemetry/timeseries?metric=TIME_SERIES_METRIC_HASHRATE&range.start=2026-09-17T11:00:00Z&range.end=2026-09-17T12:00:00Z&bucketSize=BUCKET_SIZE_5M", &history)
	if history.Unit != "TH/s" || len(history.Buckets) == 0 || history.Buckets[0].Avg <= 0 {
		t.Fatalf("stopped time series = %+v, want unchanged historical TH/s buckets", history)
	}

	postJSON(t, srv, "/api/v2/mining/start", struct{}{}, nil)
	getJSON(t, srv, "/api/v2/system/status", &st)
	if !st.Status.Mining || st.Status.ActiveBoards != 18 {
		t.Errorf("after start: mining=%v active=%d", st.Status.Mining, st.Status.ActiveBoards)
	}
}

// TestUnknownPathReturnsJSON404 verifies the middleware rewrites ServeMux's
// default text/plain 404 into the JSON error envelope.
func TestUnknownPathReturnsJSON404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/api/v2/does/not/exist")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if _, ok := body["message"]; !ok {
		t.Errorf("404 body missing message: %v", body)
	}
}

// TestMethodNotAllowedReturnsJSON confirms a wrong method on a known path gets a
// JSON 405 (ServeMux 405 rewritten by the middleware), not text/plain.
func TestMethodNotAllowedReturnsJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Post(srv.URL+"/api/v2/system/info", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}
}

// TestTypoedScopeRejected guards the DisallowUnknownFields decode: a misspelled
// scope key must 400 rather than silently decode to a fleet-wide empty scope
// and stop every board.
func TestTypoedScopeRejected(t *testing.T) {
	srv, state := newTestServer(t)
	resp, err := http.Post(srv.URL+"/api/v2/mining/scoped/stop", "application/json",
		bytes.NewBufferString(`{"scoope":{"modules":[{"tank":1,"module":1}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown field", resp.StatusCode)
	}
	// No board should have been stopped by the rejected request.
	state.mu.RLock()
	defer state.mu.RUnlock()
	for _, b := range state.eachBoard() {
		if b.State != miningStateMining {
			t.Errorf("board %s state = %q, want mining (typo request must be a no-op)", b.MAC, b.State)
		}
	}
}

// TestScopedUnknownIdentifier verifies a scope mixing a valid MAC, a valid
// serial, and a bogus identifier: the two real boards apply once (deduped) and
// the unknown id is surfaced as an explicit unapplied result.
func TestScopedUnknownIdentifier(t *testing.T) {
	srv, state := newTestServer(t)
	boards := state.eachBoard()
	mac := boards[0].MAC
	serial := boards[1].Serial
	req := scopeOnlyRequest{Scope: scope{Identifiers: []string{mac, serial, "bogus-id"}}}

	var resp stopScopedResponse
	postJSON(t, srv, "/api/v2/mining/scoped/stop", req, &resp)
	if resp.BoardsStopped != 2 {
		t.Errorf("boardsStopped = %d, want 2", resp.BoardsStopped)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("results = %d, want 3 (2 boards + 1 unknown)", len(resp.Results))
	}
	var unknown *boardResult
	for i := range resp.Results {
		if resp.Results[i].MACAddress == "bogus-id" {
			unknown = &resp.Results[i]
		}
	}
	if unknown == nil {
		t.Fatal("unknown identifier not reported in results")
	}
	if unknown.Applied {
		t.Error("unknown identifier should not be applied")
	}
	if unknown.Message != "unknown identifier" {
		t.Errorf("unknown message = %q, want %q", unknown.Message, "unknown identifier")
	}
}

// TestScopedPowerWireShape asserts the on-the-wire JSON of a scoped-write
// response: a numeric boardsUpdated and a results array whose entries carry the
// grpc-gateway camelCase keys a real MDK v2 client parses.
func TestScopedPowerWireShape(t *testing.T) {
	srv, _ := newTestServer(t)
	raw := postRaw(t, srv, "/api/v2/mining/scoped/power",
		scopedPowerRequest{Scope: scope{Modules: []moduleCoord{{Tank: 1, Module: 1}}}, Watts: 2700})

	if _, ok := raw["boardsUpdated"].(float64); !ok {
		t.Errorf("boardsUpdated missing or not a number: %v", raw["boardsUpdated"])
	}
	results, ok := raw["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("results missing or empty: %v", raw["results"])
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result[0] not an object: %v", results[0])
	}
	for _, key := range []string{"macAddress", "applied", "message"} {
		if _, ok := first[key]; !ok {
			t.Errorf("result[0] missing key %q: %v", key, first)
		}
	}
}

// TestInt64FieldsEncodeAsStrings guards the protojson fidelity requirement:
// 64-bit integer fields are emitted as quoted JSON strings, not numbers.
func TestInt64FieldsEncodeAsStrings(t *testing.T) {
	srv, state := newTestServer(t)

	info := getRaw(t, srv, "/api/v2/system/info")
	infoObj, ok := info["info"].(map[string]any)
	if !ok {
		t.Fatalf("info = %v (%T), want JSON object", info["info"], info["info"])
	}
	if _, ok := infoObj["uptimeSeconds"].(string); !ok {
		t.Errorf("uptimeSeconds = %v (%T), want JSON string", infoObj["uptimeSeconds"], infoObj["uptimeSeconds"])
	}

	moduleID := state.Modules[0].ModuleID
	raw := putRaw(t, srv, "/api/v2/mining/modules/"+moduleID+"/voltage/fixed",
		setModuleVoltageRequest{Millivolts: 28000})
	setting, ok := raw["setting"].(map[string]any)
	if !ok {
		t.Fatalf("setting = %v (%T), want JSON object", raw["setting"], raw["setting"])
	}
	mv, ok := setting["fixedMillivolts"].(string)
	if !ok {
		t.Fatalf("fixedMillivolts = %v (%T), want JSON string", setting["fixedMillivolts"], setting["fixedMillivolts"])
	}
	if mv != "28000" {
		t.Errorf("fixedMillivolts = %q, want %q", mv, "28000")
	}
}

// getRaw / postRaw / putRaw decode a response body into an untyped map so tests
// can assert the exact wire shape (key presence, JSON types) rather than the
// Go-side struct, which hides string-vs-number encoding.
func getRaw(t *testing.T, srv *httptest.Server, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}

func postRaw(t *testing.T, srv *httptest.Server, path string, body any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}

func putRaw(t *testing.T, srv *httptest.Server, path string, body any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}

// postPut and doDelete issue PUT/DELETE requests with a JSON body.
func postPut(t *testing.T, srv *httptest.Server, path string, body any, out any) int {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return resp.StatusCode
}

func doDelete(t *testing.T, srv *httptest.Server, path string, out any) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return resp.StatusCode
}

func TestModulePsuSnapshots(t *testing.T) {
	srv, _ := newTestServer(t)
	var response containerMapResponse
	getJSON(t, srv, "/api/v2/container/map", &response)
	modules := response.ContainerMap.Tanks[0].Modules
	first := modules[0].Slots[0].Hashboard
	for _, slot := range modules[0].Slots {
		if slot.Hashboard.PsuIdentity != first.PsuIdentity || slot.Hashboard.Health.PsuElectrical != first.Health.PsuElectrical {
			t.Fatal("boards in a module must report the same PSU")
		}
	}
	if first.PsuIdentity.Serial == modules[1].Slots[0].Hashboard.PsuIdentity.Serial {
		t.Fatal("different modules must have distinct PSU identities")
	}
	if first.Health.PsuElectrical.InputPower <= 0 || !first.Health.PsuThermal.ThermalStateValid {
		t.Fatal("expected live PSU power and valid thermal status")
	}
}

func TestContainerControlsReadback(t *testing.T) {
	srv, _ := newTestServer(t)
	var response struct {
		Supported                   bool                `json:"supported"`
		Controls                    []containerControl  `json:"controls"`
		Setpoints                   []containerSetpoint `json:"setpoints"`
		MainCircuitBreakerSupported bool                `json:"mainCircuitBreakerSupported"`
	}
	getJSON(t, srv, "/api/v2/power/container/controls", &response)
	if !response.Supported || len(response.Controls) != 6 || response.MainCircuitBreakerSupported {
		t.Fatalf("unexpected equipment capabilities: %+v", response)
	}
	for _, control := range response.Controls {
		for _, enabled := range []bool{false, true} {
			if status := postJSON(t, srv, "/api/v2/power/container/controls/"+control.Name, setContainerControlRequest{Enabled: enabled}, nil); status != http.StatusOK {
				t.Fatalf("write %s: status %d", control.Name, status)
			}
			getJSON(t, srv, "/api/v2/power/container/controls", &response)
			var telemetry containerTelemetryResponse
			getJSON(t, srv, "/api/v2/telemetry/container", &telemetry)
			found := false
			for i, observed := range response.Controls {
				if observed != telemetry.Controls[i] {
					t.Fatalf("equipment readback differs from telemetry: %+v vs %+v", observed, telemetry.Controls[i])
				}
				if observed.Name == control.Name {
					found = true
					if !observed.StateKnown || observed.Enabled != enabled {
						t.Fatalf("incorrect readback: %+v, wanted %t", observed, enabled)
					}
				}
			}
			if !found {
				t.Fatalf("missing control %s", control.Name)
			}
		}
	}
	if len(response.Setpoints) != 5 || response.Setpoints[0].Name != "dry_cooler_auto_temp_c" {
		t.Fatalf("missing existing setpoint: %+v", response.Setpoints)
	}
}

func TestPowerFeedStateSwitchesOneOutlet(t *testing.T) {
	srv, _ := newTestServer(t)
	var response setPowerFeedStateResponse
	if status := postJSON(t, srv, "/api/v2/power/feeds/pdu-1-outlet-01/state", setPowerFeedStateRequest{Disabled: true}, &response); status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if response.PowerFeed.State != powerFeedStateDisabled || response.PowerFeed.Active || response.PowerFeed.PowerW != 0 {
		t.Fatalf("disabled feed = %+v", response.PowerFeed)
	}
	if status := postJSON(t, srv, "/api/v2/power/feeds/pdu-1-outlet-01/state", setPowerFeedStateRequest{}, &response); status != http.StatusOK || response.PowerFeed.State != powerFeedStateEnabled {
		t.Fatalf("status = %d, feed = %+v", status, response.PowerFeed)
	}
	if status := postJSON(t, srv, "/api/v2/power/feeds/pdu-9-outlet-01/state", setPowerFeedStateRequest{Disabled: true}, nil); status != http.StatusNotFound {
		t.Fatalf("unknown feed status = %d", status)
	}
}
