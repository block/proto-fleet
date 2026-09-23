package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func closeTo(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s = %g, want %g ± %g", name, got, want, tolerance)
	}
}

func TestFieldScenarioAccounting(t *testing.T) {
	for _, demo := range []bool{true, false} {
		cfg := testConfig()
		cfg.Tanks = 6
		cfg.ModulesPerTank = 16
		cfg.DemoModuleStates = demo
		s := newContainerState(cfg, time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
		s.now = func() time.Time { return s.StartTime }
		mux := http.NewServeMux()
		NewRESTApiHandler(s).RegisterRoutes(mux)
		srv := httptest.NewServer(mux)
		var cm containerMapResponse
		getJSON(t, srv, "/api/v2/container/map", &cm)
		var mining miningStatusResponse
		getJSON(t, srv, "/api/v2/mining/status", &mining)
		var plant containerTelemetryResponse
		getJSON(t, srv, "/api/v2/telemetry/container", &plant)
		dc, ac, hr := 0.0, 0.0, 0.0
		for _, tank := range cm.ContainerMap.Tanks {
			for _, module := range tank.Modules {
				modulePower := 0.0
				for _, slot := range module.Slots {
					b := slot.Hashboard
					p := b.Health
					dc += p.Power
					modulePower += p.Power
					hr += p.Hashrate
					closeTo(t, "board current", p.Current*p.Voltage, p.Power, 1e-8)
					if b.MiningState == miningStateMining {
						closeTo(t, "board power", p.Power, 3000, 15)
						closeTo(t, "efficiency", p.Power/p.Hashrate, 14.9, .1)
					}
				}
				psu := module.Slots[0].Hashboard.Health.PsuElectrical
				closeTo(t, "PSU DC", psu.OutputPower, modulePower, 1e-8)
				closeTo(t, "PSU efficiency", psu.InputPower*(14.9/15.5), modulePower, 1e-8)
				ac += psu.InputPower
			}
		}
		closeTo(t, "mining DC", mining.Power, dc, 1e-7)
		closeTo(t, "mining hashrate", mining.Hashrate, hr, 1e-7)
		closeTo(t, "plant IT AC", plant.Plant.TotalPowerW, ac, 1e-7)
		if len(plant.PowerDistributions) != 6 || len(plant.PowerFeeds) != 96 {
			t.Fatalf("want six PDUs / 96 feeds, got %d / %d", len(plant.PowerDistributions), len(plant.PowerFeeds))
		}
		fanPower, phasePower, feedPower, pduPower := 0.0, 0.0, 0.0, 0.0
		for _, f := range plant.CoolingFans {
			closeTo(t, "fan law", f.PowerW, 3000*math.Pow(f.SpeedPct/80, 3), 1e-8)
			fanPower += f.PowerW
		}
		for _, p := range plant.Phases {
			phasePower += p.PowerW
			closeTo(t, "phase current", p.CurrentA*p.VoltageV*plant.Plant.PowerFactor/math.Sqrt(3), p.PowerW, 1e-7)
		}
		for _, f := range plant.PowerFeeds {
			feedPower += f.PowerW
		}
		for _, p := range plant.PowerDistributions {
			pduPower += p.ActivePowerW
		}
		closeTo(t, "feeds", feedPower, ac, 1e-7)
		closeTo(t, "PDUs", pduPower, ac, 1e-7)
		closeTo(t, "facility", plant.Plant.FacilityPowerW, ac+fanPower+18000, 1e-7)
		closeTo(t, "phases", phasePower, plant.Plant.FacilityPowerW, 1e-7)
		closeTo(t, "PUE", plant.Plant.PUE, plant.Plant.FacilityPowerW/ac, 1e-9)
		rise := plant.Plant.OutletTemperatureC - plant.Plant.InletTemperatureC
		if rise < 7 || rise > 8 {
			t.Fatalf("fluid rise = %g", rise)
		}
		closeTo(t, "flow heat balance", plant.Plant.FlowRateLpm*.8*2*rise/60, (ac+16000)/1000, 1e-7)
		srv.Close()
	}
}

func TestAsicHashrateReconciles(t *testing.T) {
	s := NewContainerState(testConfig())
	b := s.eachBoard()[0]
	board := s.hashboardDTO(b)
	asics := s.asicsResponse(b)
	sum := 0
	low, high := 10000, 0
	for _, a := range asics.Asics {
		sum += a.HashrateGhs10
		low = min(low, a.TemperatureDc)
		high = max(high, a.TemperatureDc)
	}
	closeTo(t, "ASIC sum", float64(sum)/10000, board.Health.Hashrate, .0001)
	if high-low < 100 {
		t.Fatalf("missing spatial temperature gradient: %d..%d", low, high)
	}
}

func TestVoltageEnvelopeAndMeasurements(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, mv := range []int64{0, 25999, 26000, 28000, 30000, 30001} {
		var result moduleVoltageResponse
		status := postPut(t, srv, "/api/v2/mining/modules/T1-M01/voltage/fixed", setModuleVoltageRequest{Millivolts: int64str(mv)}, &result)
		if mv < 26000 || mv > 30000 {
			if status != 400 {
				t.Fatalf("accepted %d mV", mv)
			}
			continue
		}
		if status != 200 {
			t.Fatalf("rejected %d mV", mv)
		}
		var cm containerMapResponse
		getJSON(t, srv, "/api/v2/container/map", &cm)
		for _, slot := range cm.ContainerMap.Tanks[0].Modules[0].Slots {
			closeTo(t, "voltage override", slot.Hashboard.Health.Voltage, float64(mv)/1000, 0)
			closeTo(t, "PSU voltage", slot.Hashboard.Health.PsuElectrical.OutputVoltage, float64(mv)/1000, 0)
		}
	}
}

func TestCoolingShutdownAndZeroLoad(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, name := range []string{"coolant_pump", "dry_cooler"} {
		postJSON(t, srv, "/api/v2/power/container/controls/"+name, setContainerControlRequest{Enabled: false}, nil)
	}
	var result containerTelemetryResponse
	getJSON(t, srv, "/api/v2/telemetry/container", &result)
	if result.Plant.FlowRateLpm != 0 || result.Plant.OutletTemperatureC != result.Plant.InletTemperatureC || result.Tank.PumpActive || result.Tank.CoolerActive {
		t.Fatalf("shutdown telemetry: %+v", result)
	}
	for _, fan := range result.CoolingFans {
		if fan.PowerW != 0 || fan.SpeedPct != 0 {
			t.Fatalf("off fan: %+v", fan)
		}
	}
	closeTo(t, "disabled cooling power", result.Plant.FacilityPowerW, result.Plant.TotalPowerW+2000, 1e-8)
	postJSON(t, srv, "/api/v2/mining/stop", struct{}{}, nil)
	getJSON(t, srv, "/api/v2/telemetry/container", &result)
	if result.Plant.PUE != 0 || result.Plant.TotalPowerW != 0 || result.Plant.FacilityPowerW != 2000 {
		t.Fatalf("idle power: %+v", result.Plant)
	}
}

func TestSlowDeterministicSamplesAndStaleDevices(t *testing.T) {
	cfg := testConfig()
	cfg.Tanks = 6
	cfg.ModulesPerTank = 16
	cfg.DemoModuleStates = true
	cfg.Seed = 42
	start := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	a, b := newContainerState(cfg, start), newContainerState(cfg, start)
	for _, elapsed := range []time.Duration{0, time.Minute, 10 * time.Minute} {
		a.advance(start.Add(elapsed))
		b.advance(start.Add(elapsed))
		if a.containerTelemetry().Plant != b.containerTelemetry().Plant {
			t.Fatal("seeded plant telemetry differs")
		}
		for i, board := range a.eachBoard() {
			if a.boardSample(board) != b.boardSample(b.eachBoard()[i]) {
				t.Fatal("seeded simulation differs")
			}
		}
	}
	offline := a.modulesByID["T2-M06"].Slots[0].Board
	stale := a.hashboardDTO(offline)
	if stale.Health.HashrailOK || stale.Health.RefclksOK || stale.Health.PsuThermal.ThermalStateValid || stale.Health.PsuElectrical.SampleAgeMs != 600000 {
		t.Fatalf("offline validity: %+v", stale.Health)
	}
	sleeping := a.hashboardDTO(a.modulesByID["T2-M03"].Slots[0].Board)
	if sleeping.Health.AverageAsicTemperatureC >= 40 {
		t.Fatalf("sleeping temperature did not decay: %g", sleeping.Health.AverageAsicTemperatureC)
	}
	warning := a.hashboardDTO(a.modulesByID["T2-M11"].Slots[0].Board).Health.PsuThermal
	if !warning.OverTemperatureWarning || warning.OverTemperatureFault || warning.InletTemperature != 80 || warning.SrHeatsinkTemperature != 88 || warning.PfcHeatsinkTemperature != 85 {
		t.Fatalf("warning scenario: %+v", warning)
	}
	sample := a.boardSample(a.eachBoard()[0])
	a.advance(a.sampleTime.Add(5 * time.Second))
	next := a.boardSample(a.eachBoard()[0])
	if math.Abs(sample.Power-next.Power) > 1 {
		t.Fatal("measurement variation too fast")
	}
}

func TestHistoryPreservesControlTransitions(t *testing.T) {
	srv, s := newTestServer(t)
	advance := func(minutes int) {
		s.mu.Lock()
		s.now = func() time.Time { return s.StartTime.Add(time.Duration(minutes) * time.Minute) }
		s.mu.Unlock()
	}
	history := func(metric, identifier string) timeSeriesResponse {
		var response timeSeriesResponse
		getJSON(t, srv, "/api/v2/telemetry/timeseries?metric="+metric+"&identifier="+identifier+"&range.start=2026-09-18T00:00:00Z&range.end=2026-09-18T00:06:00Z&bucketSize=BUCKET_SIZE_1M", &response)
		return response
	}
	advance(2)
	postJSON(t, srv, "/api/v2/mining/scoped/power", scopedPowerRequest{Watts: 2500}, nil)
	advance(4)
	postJSON(t, srv, "/api/v2/mining/stop", struct{}{}, nil)
	postJSON(t, srv, "/api/v2/power/container/controls/dry_cooler", setContainerControlRequest{Enabled: false}, nil)
	advance(6)
	first := history(timeSeriesMetricPower, "")
	if first.Buckets[0].Avg < 18*2985 || first.Buckets[2].Avg > 18*2513 || first.Buckets[4].Avg != 0 {
		t.Fatalf("missing transitions: %+v", first.Buckets)
	}
	fans := history("TIME_SERIES_METRIC_PLANT_FAN_POWER", "dry-cooler-fan-1")
	if fans.Buckets[0].Avg <= 0 || fans.Buckets[4].Avg != 0 {
		t.Fatalf("fan history: %+v", fans.Buckets)
	}
	postJSON(t, srv, "/api/v2/mining/start", struct{}{}, nil)
	after := history(timeSeriesMetricPower, "")
	if !reflect.DeepEqual(first, after) {
		t.Fatal("start rewrote prior history")
	}
	// Pool shares use the same recorded mining transitions.
	var shares poolShareHistoryResponse
	getJSON(t, srv, "/api/v2/telemetry/shares/history?poolId=1&grouping=SHARE_QUALITY_HISTORY_GROUPING_TOTAL&range.start=2026-09-18T00:00:00Z&range.end=2026-09-18T01:00:00Z&bucketSize=BUCKET_SIZE_1H", &shares)
	// Four mining minutes before stop, plus resumed minute 6; future is unobserved.
	if shares.Buckets[0].Accepted != 5*199 || shares.Buckets[0].Rejected != 5 || shares.Buckets[0].SampleCount != 7 {
		t.Fatalf("share transitions: %+v", shares)
	}
}

func TestHistoricalPoolFailoverAndCollectionGap(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, id := range []string{"1", "2", "3"} {
		var result poolShareHistoryResponse
		getJSON(t, srv, "/api/v2/telemetry/shares/history?poolId="+id+"&grouping=SHARE_QUALITY_HISTORY_GROUPING_TOTAL&range.start=2026-09-17T00:00:00Z&range.end=2026-09-17T04:00:00Z&bucketSize=BUCKET_SIZE_1H", &result)
		if result.Buckets[0].SampleCount != 55 || result.Buckets[0].ReportingCoverage >= 1 {
			t.Fatal("missing collection gap")
		}
		for i, b := range result.Buckets {
			active := (id == "1" && i != 2) || (id == "2" && i == 2)
			if active {
				closeTo(t, "share acceptance", float64(b.Accepted)/float64(b.Accepted+b.Rejected), .995, 1e-9)
			} else if b.Accepted != 0 || b.Rejected != 0 {
				t.Fatalf("standby shares for pool %s", id)
			}
		}
	}
}

func TestHistoryIncludesBriefStopsAndRejectsWrongComponents(t *testing.T) {
	srv, s := newTestServer(t)
	s.mu.Lock()
	s.now = func() time.Time { return s.StartTime.Add(30 * time.Second) }
	s.mu.Unlock()
	postJSON(t, srv, "/api/v2/mining/stop", struct{}{}, nil)
	s.mu.Lock()
	s.now = func() time.Time { return s.StartTime.Add(45 * time.Second) }
	s.mu.Unlock()
	postJSON(t, srv, "/api/v2/mining/start", struct{}{}, nil)
	s.mu.Lock()
	s.now = func() time.Time { return s.StartTime.Add(time.Minute) }
	s.mu.Unlock()
	path := "/api/v2/telemetry/timeseries?range.start=2026-09-18T00:00:00Z&range.end=2026-09-18T00:01:00Z&bucketSize=BUCKET_SIZE_1M&metric="
	var history timeSeriesResponse
	getJSON(t, srv, path+timeSeriesMetricPower, &history)
	if history.Buckets[0].Min != 0 || history.Buckets[0].Max <= 0 || history.Buckets[0].Count != 3 {
		t.Fatalf("brief stop lost: %+v", history)
	}
	for _, query := range []string{
		"TIME_SERIES_METRIC_PLANT_PHASE_POWER&identifier=pdu-t1",
		"TIME_SERIES_METRIC_PLANT_DISTRIBUTION_POWER&identifier=dry-cooler-fan-1",
		"TIME_SERIES_METRIC_PLANT_FAN_POWER&identifier=pdu-t1",
		"TIME_SERIES_METRIC_POWER_FEED_POWER&identifier=missing",
		"POWER",
	} {
		resp, err := http.Get(srv.URL + path + query)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatalf("accepted invalid series %s", query)
		}
	}
}

func TestFiveDayBackfillAndRetention(t *testing.T) {
	srv, _ := newTestServer(t)
	var response timeSeriesResponse
	getJSON(t, srv, "/api/v2/telemetry/timeseries?metric=TIME_SERIES_METRIC_PLANT_FACILITY_POWER&range.start=2026-09-13T00:00:00Z&range.end=2026-09-18T00:00:00Z&bucketSize=BUCKET_SIZE_1H", &response)
	if len(response.Buckets) != 120 {
		t.Fatalf("five-day buckets: %d", len(response.Buckets))
	}
	for _, b := range response.Buckets {
		if b.Count != 60 || b.Min <= 0 || b.Min >= b.Max || b.Avg < b.Min || b.Avg > b.Max {
			t.Fatalf("invalid hour: %+v", b)
		}
	}
	if response.Buckets[0].Avg == response.Buckets[24].Avg {
		t.Fatal("flat multi-day history")
	}
	getJSON(t, srv, "/api/v2/telemetry/timeseries?metric=TIME_SERIES_METRIC_HASHRATE&range.start=2026-09-10T00:00:00Z&range.end=2026-09-10T01:00:00Z&bucketSize=BUCKET_SIZE_1H", &response)
	if response.Buckets[0].Count != 0 {
		t.Fatal("invented expired samples")
	}
}
