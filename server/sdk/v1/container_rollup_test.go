package sdk

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestContainerRollupsCountInstalledBoardsAndKeepUnknownCoverage(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		lastSlot ContainerHashboard
		expected int32
		complete bool
	}{
		{name: "confirmed empty", lastSlot: ContainerHashboard{Position: 3, Occupancy: "empty"}, expected: 2, complete: true},
		{name: "unknown", lastSlot: UnavailableHashboard(3, ""), expected: 3},
		{name: "identity conflict", lastSlot: UnavailableHashboard(3, HashboardIdentityConflict), expected: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := rollupTestModule(now, 1, [2]float64{10e12, 40}, [2]float64{20e12, 60})
			module.Hashboards[2] = tc.lastSlot
			snapshot := rollupTestSnapshot(now, module)
			CalculateContainerRollups(snapshot)
			for _, r := range []*ContainerRollup{snapshot.Metrics, snapshot.Tanks[0].Metrics, snapshot.Tanks[0].Modules[0].Metrics.Rollup} {
				require.Equal(t, int32(2), r.Hashrate.Reporting)
				require.Equal(t, tc.expected, r.Hashrate.Expected)
				require.Equal(t, tc.complete, r.Hashrate.Complete())
				require.Equal(t, tc.complete, r.Hashrate.TopologyComplete)
				require.NotNil(t, r.HashrateHS)
				require.Equal(t, 30e12, *r.HashrateHS)
			}
			if tc.complete {
				require.Equal(t, 50.0, *snapshot.Metrics.TemperatureC)
				require.True(t, snapshot.Metrics.Temperature.Complete())
			} else {
				require.Nil(t, snapshot.Metrics.TemperatureC, "an incomplete module mean must not become a container temperature")
				require.False(t, snapshot.Metrics.Temperature.Complete())
			}
		})
	}
}

func TestContainerRollupsWeightEveryDescendantModuleEqually(t *testing.T) {
	now := time.Now().UTC()
	snapshot := rollupTestSnapshot(now,
		rollupTestModule(now, 1, [2]float64{10e12, 40}, [2]float64{20e12, 60}),
		rollupTestModule(now, 2, [2]float64{30e12, 80}),
	)
	snapshot.Tanks = append(snapshot.Tanks, ContainerTank{Position: 2, TopologyComplete: true, Modules: []ContainerModule{
		rollupTestModule(now, 3, [2]float64{40e12, 100}),
	}})
	CalculateContainerRollups(snapshot)
	require.Equal(t, 65.0, *snapshot.Tanks[0].Metrics.TemperatureC)
	require.Equal(t, 50.0, *snapshot.Tanks[0].Metrics.MinTemperatureC)
	require.Equal(t, 80.0, *snapshot.Tanks[0].Metrics.MaxTemperatureC)
	require.InDelta(t, (50.0+80.0+100.0)/3, *snapshot.Metrics.TemperatureC, 1e-9)
	require.Equal(t, 50.0, *snapshot.Metrics.MinTemperatureC)
	require.Equal(t, 100.0, *snapshot.Metrics.MaxTemperatureC)
	require.Equal(t, int32(3), snapshot.Metrics.Temperature.Reporting)
	require.Equal(t, int32(3), snapshot.Metrics.Temperature.Expected)
	require.Equal(t, int32(4), snapshot.Metrics.Hashrate.Reporting)
	require.Equal(t, 100e12, *snapshot.Metrics.HashrateHS)
}

func TestContainerRollupsRejectUnavailableBoardReadings(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name string
		edit func(*ContainerHashboard)
	}{
		{name: "missing readings", edit: func(b *ContainerHashboard) { b.HashrateHS, b.TemperatureC = nil, nil }},
		{name: "stale", edit: func(b *ContainerHashboard) { b.ObservedAt = now.Add(-2 * time.Minute) }},
		{name: "future", edit: func(b *ContainerHashboard) { b.ObservedAt = now.Add(6 * time.Second) }},
		{name: "missing source time", edit: func(b *ContainerHashboard) { b.ObservedAt = time.Time{} }},
		{name: "not a number", edit: func(b *ContainerHashboard) { b.HashrateHS, b.TemperatureC = new(math.NaN()), new(math.NaN()) }},
		{name: "infinite", edit: func(b *ContainerHashboard) { b.HashrateHS, b.TemperatureC = new(math.Inf(1)), new(math.Inf(1)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := rollupTestModule(now, 1, [2]float64{10e12, 40}, [2]float64{20e12, 60})
			tc.edit(&module.Hashboards[1])
			snapshot := rollupTestSnapshot(now, module)
			CalculateContainerRollups(snapshot)
			require.Equal(t, 10e12, *snapshot.Metrics.HashrateHS)
			require.Equal(t, int32(1), snapshot.Metrics.Hashrate.Reporting)
			require.Equal(t, int32(2), snapshot.Metrics.Hashrate.Expected)
			require.False(t, snapshot.Metrics.Hashrate.Complete())
			require.Nil(t, snapshot.Metrics.TemperatureC)
			require.Nil(t, snapshot.Metrics.EfficiencyJH)
		})
	}
}

func TestContainerRollupsEfficiencyRequiresCompleteHashrateAndDedicatedPower(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name string
		edit func(*ContainerSnapshot)
		want *float64
	}{
		{name: "complete readings", want: new(50e-12)},
		{name: "explicit zero power", edit: func(s *ContainerSnapshot) { s.Metrics.PowerW = new(0.0) }, want: new(0.0)},
		{name: "missing power", edit: func(s *ContainerSnapshot) { s.Metrics.PowerW = nil }},
		{name: "incomplete power coverage", edit: func(s *ContainerSnapshot) { s.Metrics.Power.Reporting = 0 }},
		{name: "unknown power topology", edit: func(s *ContainerSnapshot) { s.Metrics.Power.TopologyComplete = false }},
		{name: "negative power", edit: func(s *ContainerSnapshot) { s.Metrics.PowerW = new(-1.0) }},
		{name: "infinite power", edit: func(s *ContainerSnapshot) { s.Metrics.PowerW = new(math.Inf(1)) }},
		{name: "stale power", edit: func(s *ContainerSnapshot) {
			s.Metrics.Power.OldestAt, s.Metrics.Power.NewestAt = now.Add(-3*time.Minute), now.Add(-3*time.Minute)
			s.Metrics.SourceTimes["facility"] = now.Add(-3 * time.Minute)
		}},
		{name: "zero hashrate", edit: func(s *ContainerSnapshot) { s.Tanks[0].Modules[0].Hashboards[0].HashrateHS = new(0.0) }},
		{name: "negative hashrate", edit: func(s *ContainerSnapshot) { s.Tanks[0].Modules[0].Hashboards[0].HashrateHS = new(-1.0) }},
		{name: "missing board hashrate", edit: func(s *ContainerSnapshot) { s.Tanks[0].Modules[0].Hashboards[0].HashrateHS = nil }},
		{name: "unknown board slot", edit: func(s *ContainerSnapshot) { s.Tanks[0].Modules[0].Hashboards[1] = UnavailableHashboard(2, "") }},
		{name: "incomplete container topology", edit: func(s *ContainerSnapshot) { s.TopologyComplete = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := rollupTestSnapshot(now, rollupTestModule(now, 1, [2]float64{100e12, 60}))
			snapshot.Metrics = rollupTestPower(now, 5000, "facility")
			if tc.edit != nil {
				tc.edit(snapshot)
			}
			CalculateContainerRollups(snapshot)
			if tc.want == nil {
				require.Nil(t, snapshot.Metrics.EfficiencyJH)
			} else {
				require.NotNil(t, snapshot.Metrics.EfficiencyJH)
				require.InDelta(t, *tc.want, *snapshot.Metrics.EfficiencyJH, 1e-20)
			}
		})
	}
}

func TestContainerRollupsDoNotSubstituteOtherLevelsPower(t *testing.T) {
	now := time.Now().UTC()
	snapshot := rollupTestSnapshot(now, rollupTestModule(now, 1, [2]float64{100e12, 60}))
	snapshot.Plant = &ContainerPlant{ObservedAt: now, TotalPowerW: new(5000.0)}
	snapshot.Tanks[0].Modules[0].Hashboards[0].PowerW = new(1000.0)
	snapshot.Tanks[0].Modules[0].Metrics = &ContainerModuleMetrics{ObservedAt: now, PowerW: new(1200.0), Rollup: rollupTestPower(now, 1200, "module_psu")}
	CalculateContainerRollups(snapshot)
	require.Nil(t, snapshot.Metrics.PowerW)
	require.Nil(t, snapshot.Metrics.EfficiencyJH)
	require.Nil(t, snapshot.Tanks[0].Metrics.PowerW)
	require.Nil(t, snapshot.Tanks[0].Metrics.EfficiencyJH)
	require.InDelta(t, 12e-12, *snapshot.Tanks[0].Modules[0].Metrics.EfficiencyJH, 1e-20)
}

func TestContainerRollupsTransportPreservesPowerCoverageAndFeed(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 123000, time.UTC)
	snapshot := rollupTestSnapshot(now, rollupTestModule(now, 1, [2]float64{100e12, 0}))
	snapshot.Metrics = rollupTestPower(now, 0, "facility")
	snapshot.Tanks[0].Metrics = rollupTestPower(now, 0, "pdu")
	snapshot.Tanks[0].Metrics.Attribution = "name"
	module := &snapshot.Tanks[0].Modules[0]
	module.Metrics = &ContainerModuleMetrics{ObservedAt: now, PowerW: new(0.0), Rollup: rollupTestPower(now, 0, "module_psu")}
	board := &module.Hashboards[0]
	board.PowerW, board.EfficiencyJH = new(0.0), new(0.0)
	board.Feed = &ContainerFeedEvidence{PowerFeedID: "pdu-1-outlet-07", ExpectedPowerFeedID: "pdu-1-outlet-07", WiringStatus: "configured", WiringReasons: []string{"configured_feed"}, ConfiguredTank: 1, ConfiguredModule: 1}
	CalculateContainerRollups(snapshot)
	wire := deviceMetricsToProto(DeviceMetrics{DeviceID: "controller", Timestamp: now, Container: snapshot})
	encoded, err := proto.Marshal(wire)
	require.NoError(t, err)
	// Use the existing telemetry adapter after an actual protobuf encoding round trip.
	proto.Reset(wire)
	require.NoError(t, proto.Unmarshal(encoded, wire))
	got := deviceMetricsFromProto(wire).Container
	require.Equal(t, snapshot, got)
	require.NotNil(t, got.Metrics.PowerW)
	require.Zero(t, *got.Metrics.PowerW)
	require.NotNil(t, got.Metrics.EfficiencyJH)
	require.Zero(t, *got.Metrics.EfficiencyJH)
	require.Nil(t, got.Tanks[0].Modules[0].Hashboards[1].PowerW)
}

func rollupTestSnapshot(now time.Time, modules ...ContainerModule) *ContainerSnapshot {
	return &ContainerSnapshot{ObservedAt: now, TopologyComplete: true, Tanks: []ContainerTank{{Position: 1, TopologyComplete: true, Modules: modules}}}
}

func rollupTestModule(now time.Time, position int32, readings ...[2]float64) ContainerModule {
	m := ContainerModule{Position: position, ObservedAt: now, Hashboards: []ContainerHashboard{
		{Position: 1, Occupancy: "empty"}, {Position: 2, Occupancy: "empty"}, {Position: 3, Occupancy: "empty"},
	}}
	for i, r := range readings {
		m.Hashboards[i] = ContainerHashboard{Position: m.Hashboards[i].Position, Occupancy: "occupied", Serial: fmt.Sprintf("module-%d-board-%d", position, i+1), ObservedAt: now, HashrateHS: new(r[0]), TemperatureC: new(r[1])}
	}
	return m
}

func rollupTestPower(at time.Time, power float64, source string) *ContainerRollup {
	return &ContainerRollup{PowerW: new(power), PowerSource: source, Power: MetricCoverage{Reporting: 1, Expected: 1, TopologyComplete: true, OldestAt: at, NewestAt: at}, SourceTimes: map[string]time.Time{source: at}}
}

func TestContainerRollupsRetainUnlocatedInventory(t *testing.T) {
	now := time.Now().UTC()
	snapshot := rollupTestSnapshot(now, rollupTestModule(now, 1, [2]float64{100e12, 60}))
	snapshot.TopologyComplete = false
	snapshot.Metrics = rollupTestPower(now, 3000, "facility")
	snapshot.UnlocatedHashboards = make([]ContainerHashboard, 6)
	for range 2 { // Collection and server reconciliation both recalculate rollups.
		CalculateContainerRollups(snapshot)
		require.EqualValues(t, 6, snapshot.Metrics.UnlocatedHashboards)
		require.False(t, snapshot.Metrics.Hashrate.Complete())
		require.Nil(t, snapshot.Metrics.EfficiencyJH)
	}
	snapshot.UnlocatedHashboards = nil
	snapshot.TopologyComplete = true
	CalculateContainerRollups(snapshot)
	require.Zero(t, snapshot.Metrics.UnlocatedHashboards)
	require.True(t, snapshot.Metrics.Hashrate.Complete())
	require.NotNil(t, snapshot.Metrics.EfficiencyJH)
}

func TestUnlocatedReadingsOnlyContributeToContainer(t *testing.T) {
	now := time.Now().UTC()
	for _, bad := range []string{"", "stale", "future", "conflict", "negative", "missing", "position"} {
		t.Run(bad, func(t *testing.T) {
			b := ContainerHashboard{Occupancy: "occupied", Serial: "unlocated", ObservedAt: now, HashrateHS: new(50e12), TemperatureC: new(99.0)}
			switch bad {
			case "stale":
				b.ObservedAt = now.Add(-3 * time.Minute)
			case "future":
				b.ObservedAt = now.Add(time.Minute)
			case "conflict":
				b = UnavailableHashboard(0, HashboardIdentityConflict)
			case "negative":
				b.HashrateHS = new(-1.0)
			case "missing":
				b.HashrateHS = nil
			case "position":
				b.Position = 1
			}
			s := rollupTestSnapshot(now, rollupTestModule(now, 1, [2]float64{100e12, 60}))
			s.TopologyComplete = false
			s.UnlocatedHashboards = []ContainerHashboard{b}
			for range 2 {
				CalculateContainerRollups(s)
				want := 100e12
				if bad == "" {
					want += 50e12
				}
				require.Equal(t, want, *s.Metrics.HashrateHS)
				require.Equal(t, 100e12, *s.Tanks[0].Metrics.HashrateHS)
				require.Equal(t, 100e12, *s.Tanks[0].Modules[0].Metrics.HashrateHS)
				require.EqualValues(t, 1, s.Metrics.UnlocatedHashboards)
				require.False(t, s.Metrics.Hashrate.Complete())
				require.Nil(t, s.Metrics.EfficiencyJH)
				require.Equal(t, 60.0, *s.Metrics.TemperatureC)
			}
		})
	}
}
