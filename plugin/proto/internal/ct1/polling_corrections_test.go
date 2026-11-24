package ct1

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPollingCorrectionsReadIndependentSourcesConcurrently(t *testing.T) {
	now := time.Now().UTC()
	var started atomic.Int32
	allStarted := make(chan struct{})
	client := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if started.Add(1) == 4 {
			close(allStarted)
		}
		select {
		case <-allStarted:
		case <-r.Context().Done():
			return
		}
		switch r.URL.Path {
		case "/api/v2/container/map":
			require.NoError(t, json.NewEncoder(w).Encode(pollingCorrectionsCompleteMap(now)))
		case "/api/v2/telemetry/container":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"timestamp": now, "plant": map[string]any{"facilityPowerW": 5000}}))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	got, err := client.Collect(ctx, "controller")
	require.NoError(t, err)
	require.Equal(t, int32(4), started.Load())
	require.NotNil(t, got.HashrateHS)
	require.Equal(t, 100e12, got.HashrateHS.Value)
	require.NotNil(t, got.PowerW)
	require.Equal(t, 5000.0, got.PowerW.Value)
	require.NotNil(t, got.EfficiencyJH, "mining-status and controls failures must not discard independent measurements")
	require.Nil(t, got.Container.Controls)
}

func TestPollingCorrectionsPDUMappingRejectsContradictoryEvidence(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name      string
		edit      func(*sdk.ContainerSnapshot, *mdk.GetContainerHealthResponse)
		available bool
		want      float64
	}{
		{name: "matching named feed", available: true, want: 1200},
		{name: "explicit zero meter", available: true, edit: func(_ *sdk.ContainerSnapshot, w *mdk.GetContainerHealthResponse) {
			w.PowerDistributions[0].ActivePowerW = new(0.0)
		}},
		{name: "wrong PDU in observed feed", edit: func(s *sdk.ContainerSnapshot, _ *mdk.GetContainerHealthResponse) {
			s.Tanks[0].Modules[0].Hashboards[0].Feed.PowerFeedID = "pdu-2-outlet-01"
		}},
		{name: "wrong PDU in expected feed", edit: func(s *sdk.ContainerSnapshot, _ *mdk.GetContainerHealthResponse) {
			s.Tanks[0].Modules[0].Hashboards[0].Feed.ExpectedPowerFeedID = "pdu-2-outlet-01"
		}},
		{name: "wiring conflict", edit: func(s *sdk.ContainerSnapshot, _ *mdk.GetContainerHealthResponse) {
			s.Tanks[0].Modules[0].Hashboards[0].Feed.WiringStatus = "conflict"
		}},
		{name: "configured tank mismatch", edit: func(s *sdk.ContainerSnapshot, _ *mdk.GetContainerHealthResponse) {
			s.Tanks[0].Modules[0].Hashboards[0].Feed.ConfiguredTank = 2
		}},
		{name: "shared with another tank", edit: func(s *sdk.ContainerSnapshot, _ *mdk.GetContainerHealthResponse) {
			s.Tanks = append(s.Tanks, sdk.ContainerTank{Position: 2, Modules: []sdk.ContainerModule{{Position: 1, Hashboards: []sdk.ContainerHashboard{{Feed: &sdk.ContainerFeedEvidence{PowerFeedID: "pdu-1-outlet-01"}}}}}})
		}},
		{name: "duplicate PDU readings", edit: func(_ *sdk.ContainerSnapshot, w *mdk.GetContainerHealthResponse) {
			w.PowerDistributions = append(w.PowerDistributions, w.PowerDistributions[0])
		}},
		{name: "missing PDU reading", edit: func(_ *sdk.ContainerSnapshot, w *mdk.GetContainerHealthResponse) { w.PowerDistributions = nil }},
		{name: "stale meter", edit: func(_ *sdk.ContainerSnapshot, w *mdk.GetContainerHealthResponse) {
			w.Timestamp = timestamppb.New(now.Add(-3 * time.Minute))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := &sdk.ContainerSnapshot{ObservedAt: now, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, Hashboards: []sdk.ContainerHashboard{{Feed: &sdk.ContainerFeedEvidence{PowerFeedID: "pdu-1-outlet-01", ExpectedPowerFeedID: "pdu-1-outlet-01", ConfiguredTank: 1, ConfiguredModule: 1}}}}}}}}
			wire := &mdk.GetContainerHealthResponse{Timestamp: timestamppb.New(now), PowerDistributions: []*mdk.PlantPowerDistribution{{ComponentId: "pdu-1", ActivePowerW: new(1200.0)}}}
			if tc.edit != nil {
				tc.edit(snapshot, wire)
			}
			collectRollups(snapshot, wire, true)
			got := snapshot.Tanks[0].Metrics
			if tc.available {
				require.NotNil(t, got.PowerW)
				require.Equal(t, tc.want, *got.PowerW)
				require.True(t, got.Power.Complete())
			} else {
				require.Nil(t, got.PowerW)
				require.False(t, got.Power.Complete())
			}
		})
	}
}

func TestPollingCorrectionsInstalledBoardsAndSharedPSU(t *testing.T) {
	now := time.Now().UTC()
	boards := []Board{
		{Health: &Health{Timestamp: now, Hashrate: new(10.0), AverageAsicTemperatureC: new(40.0), PSUElectrical: &PSUElectrical{InputPower: new(1200.0)}}},
		{Health: &Health{Timestamp: now, Hashrate: new(20.0), AverageAsicTemperatureC: new(60.0), PSUElectrical: &PSUElectrical{InputPower: new(1200.0)}}},
	}
	got := moduleMetrics(boards, now)
	require.NotNil(t, got.HashrateHS, "two installed boards are sufficient for a complete module reading")
	require.Equal(t, 30e12, *got.HashrateHS)
	require.NotNil(t, got.TemperatureC)
	require.Equal(t, 50.0, *got.TemperatureC)
	require.NotNil(t, got.PowerW)
	require.Equal(t, 1200.0, *got.PowerW, "repeated shared PSU readings must not be summed")
}

func TestPollingCorrectionsContainerUsesBoardHashrate(t *testing.T) {
	now := time.Now().UTC()
	got := collectPollingCorrections(t, now, pollingCorrectionsMap(now), map[string]any{"facilityPowerW": 5000})
	require.NotNil(t, got.HashrateHS)
	require.Equal(t, 100e12, got.HashrateHS.Value, "container hashrate must sum boards, ignoring the mining estimate of 999 TH/s")
	require.Nil(t, got.EfficiencyJH, "a partial container map cannot prove complete hashrate coverage")
}

func TestPollingCorrectionsContainerTemperatureWeightsModulesEqually(t *testing.T) {
	now := time.Now().UTC()
	got := collectPollingCorrections(t, now, pollingCorrectionsMap(now), map[string]any{"facilityPowerW": 5000, "inletTemperatureC": 25})
	require.NotNil(t, got.TempC)
	// Tank 1 contains module means 50 and 80; tank 2 contains module mean 100.
	// Neither equal tank weighting nor equal board weighting produces this mean.
	require.InDelta(t, (50.0+80.0+100.0)/3, got.TempC.Value, 1e-9)
	require.NotNil(t, got.Container.Plant)
	require.NotNil(t, got.Container.Plant.InletTemperatureC)
	require.Equal(t, 25.0, *got.Container.Plant.InletTemperatureC, "coolant inlet remains a separate measurement")
}

func TestPollingCorrectionsFacilityPowerHasNoFallback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant map[string]any
		age   time.Duration
	}{
		{name: "missing facility with plant total", plant: map[string]any{"totalPowerW": 4500}},
		{name: "missing plant"},
		{name: "stale facility", plant: map[string]any{"facilityPowerW": 5000}, age: 3 * time.Minute},
		{name: "invalid facility", plant: map[string]any{"facilityPowerW": -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			got := collectPollingCorrections(t, now.Add(-tc.age), pollingCorrectionsMap(now), tc.plant)
			require.Nil(t, got.PowerW, "plant total, mining power and module PSU power must not substitute for facility power")
			require.Nil(t, got.EfficiencyJH, "efficiency requires valid facility power")
		})
	}
}

func TestPollingCorrectionsExplicitZeroFacilityPowerIsAvailable(t *testing.T) {
	now := time.Now().UTC()
	got := collectPollingCorrections(t, now, pollingCorrectionsCompleteMap(now), map[string]any{"facilityPowerW": 0})
	require.NotNil(t, got.PowerW)
	require.Zero(t, got.PowerW.Value)
	require.NotNil(t, got.EfficiencyJH)
	require.Zero(t, got.EfficiencyJH.Value)
}

func TestPollingCorrectionsCompleteContainerEfficiency(t *testing.T) {
	now := time.Now().UTC()
	got := collectPollingCorrections(t, now, pollingCorrectionsCompleteMap(now), map[string]any{"facilityPowerW": 5000})
	require.NotNil(t, got.HashrateHS)
	require.Equal(t, 100e12, got.HashrateHS.Value)
	require.NotNil(t, got.EfficiencyJH)
	require.InDelta(t, 50.0, got.EfficiencyJH.Value*1e12, 1e-9)
}

func TestPollingCorrectionsAmbiguousZeroBoardHashrateStaysUnavailable(t *testing.T) {
	now := time.Now().UTC()
	wire := map[string]any{"containerMap": map[string]any{"tanks": []any{
		map[string]any{"id": 1, "modules": []any{pollingCorrectionsModule(now, 1, "zero-module", [2]float64{0, 60})}},
	}}}
	got := collectPollingCorrections(t, now, wire, map[string]any{"facilityPowerW": 5000})
	require.Nil(t, got.Container.Tanks[0].Modules[0].Hashboards[0].HashrateHS)
	require.Nil(t, got.HashrateHS, "the mining estimate cannot replace a missing board measurement")
	require.Nil(t, got.EfficiencyJH)
}

func collectPollingCorrections(t *testing.T, plantAt time.Time, wire map[string]any, plant map[string]any) sdk.DeviceMetrics {
	t.Helper()
	client := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/container/map":
			require.NoError(t, json.NewEncoder(w).Encode(wire))
		case "/api/v2/mining/status":
			_, _ = w.Write([]byte(`{"hashrate":999,"power":4000,"mining":true}`))
		case "/api/v2/telemetry/container":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"timestamp": plantAt, "plant": plant}))
		case "/api/v2/power/container/controls":
			_, _ = w.Write([]byte(`{"supported":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	got, err := client.Collect(t.Context(), "controller")
	require.NoError(t, err)
	return got
}

func pollingCorrectionsMap(now time.Time) map[string]any {
	return map[string]any{"containerMap": map[string]any{"tanks": []any{
		map[string]any{"id": 1, "modules": []any{
			pollingCorrectionsModule(now, 1, "module-a", [2]float64{10, 40}, [2]float64{20, 60}),
			pollingCorrectionsModule(now, 2, "module-b", [2]float64{30, 80}),
		}},
		map[string]any{"id": 2, "modules": []any{
			pollingCorrectionsModule(now, 1, "module-c", [2]float64{40, 100}),
		}},
	}}}
}

func pollingCorrectionsCompleteMap(now time.Time) map[string]any {
	tanks := make([]any, 6)
	for ti := range tanks {
		modules := make([]any, 16)
		for mi := range modules {
			id := fmt.Sprintf("tank-%d-module-%d", ti+1, mi+1)
			modules[mi] = pollingCorrectionsModule(now, mi+1, id)
			if ti == 0 && mi == 0 {
				modules[mi] = pollingCorrectionsModule(now, mi+1, id, [2]float64{100, 60})
			}
		}
		tanks[ti] = map[string]any{"id": ti + 1, "modules": modules}
	}
	return map[string]any{"containerMap": map[string]any{"tanks": tanks}}
}

func pollingCorrectionsModule(now time.Time, id int, moduleID string, readings ...[2]float64) map[string]any {
	slots := make([]any, 3)
	for i := range slots {
		slot := map[string]any{"occupied": i < len(readings), "configuredLocation": map[string]any{"hashboardPosition": i + 1}}
		if i < len(readings) {
			slot["hashboard"] = map[string]any{
				"serial": fmt.Sprintf("%s-board-%d", moduleID, i+1), "moduleId": moduleID,
				"location": map[string]any{"hashboardPosition": i + 1}, "miningState": "MINING_STATE_MINING",
				"health": map[string]any{
					"timestamp": now, "hashrate": readings[i][0], "averageAsicTemperatureC": readings[i][1],
					"hashrailOk": true, "refclksOk": true, "psuElectrical": map[string]any{"inputPower": 1200},
				},
			}
		}
		slots[i] = slot
	}
	return map[string]any{"id": id, "slots": slots}
}

func TestConflictingModuleRetainsBoardIdentityClaims(t *testing.T) {
	now := time.Now().UTC()
	wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{
		{ID: 1, Slots: []Slot{
			{Occupied: true, Hashboard: &Board{MACAddress: "AA-BB-CC-DD-EE-01", ModuleID: "module-a"}},
			{Occupied: true, Hashboard: &Board{Serial: " other-board ", ModuleID: "module-b"}},
		}},
		// This distinct raw alias may resolve to the first module's durable board.
		{ID: 2, Slots: []Slot{{Occupied: true, Hashboard: &Board{Serial: "known-serial", ModuleID: "module-c"}}}},
	}}}}}
	got, err := snapshot(wire, now)
	require.NoError(t, err)
	boards := got.Tanks[0].Modules[0].Hashboards
	require.Empty(t, boards[0].UnavailableReason)
	require.Equal(t, "aa:bb:cc:dd:ee:01", boards[0].MACAddress)
	require.Equal(t, "other-board", boards[1].Serial)
	require.Nil(t, boards[0].HashrateHS)
	require.Equal(t, "known-serial", got.Tanks[0].Modules[1].Hashboards[0].Serial)
}

func TestPSUInvalidSamplesRetainSourceTime(t *testing.T) {
	now := time.Now().UTC()
	for _, value := range []*float64{nil, new(-1.0), new(math.NaN()), new(math.Inf(1))} {
		got := moduleMetrics([]Board{{Health: &Health{Timestamp: now, PSUElectrical: &PSUElectrical{InputPower: value, SampleAgeMs: new(2000.0)}}}}, now)
		require.Nil(t, got.PowerW)
		require.Equal(t, now.Add(-2*time.Second), got.Rollup.SourceTimes["module_psu"])
		require.Zero(t, got.Rollup.Power.Reporting)
	}
}

func TestPSUNewestInvalidSampleSupersedesRepeatedValidSamples(t *testing.T) {
	now := time.Now().UTC()
	for _, age := range []float64{0, 10000} {
		valid := Board{Health: &Health{Timestamp: now, PSUElectrical: &PSUElectrical{InputPower: new(1200.0), SampleAgeMs: new(age)}}}
		invalid := Board{Health: &Health{Timestamp: now, PSUElectrical: &PSUElectrical{InputPower: new(-1.0)}}}
		for _, boards := range [][]Board{{valid, invalid}, {invalid, valid}} {
			got := moduleMetrics(boards, now)
			require.Nil(t, got.PowerW)
			require.Equal(t, now, got.Rollup.SourceTimes["module_psu"])
			require.Zero(t, got.Rollup.Power.Reporting)
		}
	}
}

func TestPSUOnlyTrustworthyTimesAdvanceWatermark(t *testing.T) {
	now := time.Now().UTC()
	for _, age := range []float64{-1, math.NaN(), math.Inf(1), 1e30} {
		got := moduleMetrics([]Board{{Health: &Health{Timestamp: now, PSUElectrical: &PSUElectrical{InputPower: new(1200.0), SampleAgeMs: new(age)}}}}, now)
		require.Empty(t, got.Rollup.SourceTimes)
		require.Nil(t, got.PowerW)
	}
	for _, at := range []time.Time{{}, now.Add(time.Minute)} {
		got := moduleMetrics([]Board{{Health: &Health{Timestamp: at, PSUElectrical: &PSUElectrical{InputPower: new(1200.0)}}}}, now)
		require.Empty(t, got.Rollup.SourceTimes)
	}
	stale := moduleMetrics([]Board{{Health: &Health{Timestamp: now.Add(-3 * time.Minute), PSUElectrical: &PSUElectrical{InputPower: new(1200.0)}}}}, now)
	require.Nil(t, stale.PowerW)
	require.Equal(t, now.Add(-3*time.Minute), stale.Rollup.SourceTimes["module_psu"])
}
