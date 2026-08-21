package telemetry

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
)

// recordingEmitter is a test double that captures every emission.
type recordingEmitter struct {
	mu sync.Mutex

	online        []onlineEvent
	hashing       []hashingEvent
	hashrate      []hashrateEvent
	temperature   []temperatureEvent
	pool          []poolEvent
	telemetryPoll []pollEvent
}

type onlineEvent struct {
	labels metrics.DeviceLabels
	online bool
}
type hashingEvent struct {
	labels metrics.DeviceLabels
	ratio  float64
}
type hashrateEvent struct {
	labels      metrics.DeviceLabels
	observedTHs float64
	expectedTHs float64
}
type temperatureEvent struct {
	labels metrics.DeviceLabels
	kind   string
	maxC   float64
	avgC   float64
}
type poolEvent struct {
	labels    metrics.DeviceLabels
	connected bool
}
type pollEvent struct {
	labels metrics.TelemetryPollLabels
}

func (r *recordingEmitter) EmitDeviceOnline(_ context.Context, labels metrics.DeviceLabels, online bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.online = append(r.online, onlineEvent{labels: labels, online: online})
}
func (r *recordingEmitter) EmitDeviceHashing(_ context.Context, labels metrics.DeviceLabels, ratio float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hashing = append(r.hashing, hashingEvent{labels: labels, ratio: ratio})
}
func (r *recordingEmitter) EmitDeviceHashrate(_ context.Context, labels metrics.DeviceLabels, observedTHs, expectedTHs float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hashrate = append(r.hashrate, hashrateEvent{labels: labels, observedTHs: observedTHs, expectedTHs: expectedTHs})
}
func (r *recordingEmitter) EmitDeviceTemperature(_ context.Context, labels metrics.DeviceLabels, kind string, maxC, avgC float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.temperature = append(r.temperature, temperatureEvent{labels: labels, kind: kind, maxC: maxC, avgC: avgC})
}
func (r *recordingEmitter) EmitDevicePoolConnected(_ context.Context, labels metrics.DeviceLabels, connected bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pool = append(r.pool, poolEvent{labels: labels, connected: connected})
}
func (r *recordingEmitter) EmitTelemetryPoll(_ context.Context, labels metrics.TelemetryPollLabels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.telemetryPoll = append(r.telemetryPoll, pollEvent{labels: labels})
}

func metricVal(v float64) *modelsV2.MetricValue {
	return &modelsV2.MetricValue{Value: v}
}

// metricValWithMax builds a reading whose nameplate (expected) is reported as MetaData.Max.
func metricValWithMax(observedHS, maxHS float64) *modelsV2.MetricValue {
	return &modelsV2.MetricValue{Value: observedHS, MetaData: &modelsV2.MetricValueMetaData{Max: &maxHS}}
}

// TestObserverEmitsHashrateInTerahash converts the H/s reading exposed in DeviceMetrics into TH/s.
func TestObserverEmitsHashrateInTerahash(t *testing.T) {
	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)

	obs.onDeviceMetrics(context.Background(), 7, 42, "antminer", "ant-1", modelsV2.DeviceMetrics{
		DeviceIdentifier: "ant-1",
		HashrateHS:       metricVal(110e12), // 110 TH/s
		Health:           modelsV2.HealthHealthyActive,
	})

	require.Len(t, rec.hashrate, 1)
	require.InDelta(t, 110.0, rec.hashrate[0].observedTHs, 1e-9)
	require.Equal(t, "ant-1", rec.hashrate[0].labels.DeviceID)
	require.Equal(t, "antminer", rec.hashrate[0].labels.Driver)
	require.Equal(t, "7", rec.hashrate[0].labels.OrganizationID)
	require.Equal(t, "42", rec.hashrate[0].labels.SiteID)
}

// TestObserverEmitsHashingGauge checks fleet_device_hashing: the observed/expected ratio while a device is expected to hash, a non-alerting 1.0 once it is not, and nothing for a still-expected device with a missing/invalid reading.
func TestObserverEmitsHashingGauge(t *testing.T) {
	cases := []struct {
		name      string
		health    modelsV2.HealthStatus
		hashrate  *modelsV2.MetricValue
		wantEmit  bool
		wantRatio float64
	}{
		// Expected to hash + valid reading: emit the ratio (the rule applies the threshold).
		{"no nameplate, hashing", modelsV2.HealthHealthyActive, metricVal(110e12), true, 1.0},
		{"no nameplate, zero hashrate", modelsV2.HealthHealthyActive, metricVal(0), true, 0.0},
		{"at expected", modelsV2.HealthHealthyActive, metricValWithMax(100e12, 100e12), true, 1.0},
		{"above expected", modelsV2.HealthHealthyActive, metricValWithMax(120e12, 100e12), true, 1.2},
		{"degraded", modelsV2.HealthHealthyActive, metricValWithMax(50e12, 100e12), true, 0.5},
		{"warning degraded", modelsV2.HealthWarning, metricValWithMax(50e12, 100e12), true, 0.5},
		{"critical zero hashrate", modelsV2.HealthCritical, metricValWithMax(0, 100e12), true, 0.0},
		// Not expected to hash: clear with a non-alerting 1.0 regardless of reading.
		{"intentionally inactive", modelsV2.HealthHealthyInactive, metricValWithMax(0, 100e12), true, 1.0},
		{"unknown health", modelsV2.HealthUnknown, metricVal(110e12), true, 1.0},
		// Expected to hash but no usable reading: emit nothing so a gap/garbage can't clear a real low.
		{"no hashrate reading", modelsV2.HealthHealthyActive, nil, false, 0},
		{"invalid hashrate", modelsV2.HealthHealthyActive, metricVal(math.NaN()), false, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingEmitter{}
			obs := newMetricsObserver(rec)
			obs.onDeviceMetrics(context.Background(), 7, 42, "antminer", "ant-1", modelsV2.DeviceMetrics{
				DeviceIdentifier: "ant-1",
				HashrateHS:       tc.hashrate,
				Health:           tc.health,
			})

			if !tc.wantEmit {
				require.Empty(t, rec.hashing, "must not emit fleet_device_hashing")
				return
			}
			require.Len(t, rec.hashing, 1)
			require.InDelta(t, tc.wantRatio, rec.hashing[0].ratio, 1e-9)
			require.Equal(t, "ant-1", rec.hashing[0].labels.DeviceID)
			require.Equal(t, "7", rec.hashing[0].labels.OrganizationID)
		})
	}
}

// A low hashing sample must be cleared when a miner is paused or goes offline, but never by a telemetry gap, an invalid reading, or a still-reporting critical device.
func TestObserverHashingClearSemantics(t *testing.T) {
	sample := func(h modelsV2.HealthStatus, hr *modelsV2.MetricValue) modelsV2.DeviceMetrics {
		return modelsV2.DeviceMetrics{DeviceIdentifier: "ant-1", Health: h, HashrateHS: hr}
	}
	low := func() *modelsV2.MetricValue { return metricValWithMax(50e12, 100e12) }

	t.Run("paused clears", func(t *testing.T) {
		rec := &recordingEmitter{}
		obs := newMetricsObserver(rec)
		obs.onDeviceMetrics(context.Background(), 7, 0, "antminer", "ant-1", sample(modelsV2.HealthHealthyActive, low()))
		obs.onDeviceMetrics(context.Background(), 7, 0, "antminer", "ant-1", sample(modelsV2.HealthHealthyInactive, metricValWithMax(0, 100e12)))
		require.Len(t, rec.hashing, 2)
		require.InDelta(t, 0.5, rec.hashing[0].ratio, 1e-9)
		require.InDelta(t, 1.0, rec.hashing[1].ratio, 1e-9)
	})

	t.Run("offline status clears", func(t *testing.T) {
		rec := &recordingEmitter{}
		obs := newMetricsObserver(rec)
		obs.onDeviceMetrics(context.Background(), 7, 0, "antminer", "ant-1", sample(modelsV2.HealthHealthyActive, low()))
		obs.onDeviceStatus(context.Background(), 7, 0, "antminer", "ant-1", mm.MinerStatusOffline)
		require.Len(t, rec.hashing, 2)
		require.InDelta(t, 1.0, rec.hashing[1].ratio, 1e-9)
	})

	t.Run("invalid reading does not clear", func(t *testing.T) {
		rec := &recordingEmitter{}
		obs := newMetricsObserver(rec)
		obs.onDeviceMetrics(context.Background(), 7, 0, "antminer", "ant-1", sample(modelsV2.HealthHealthyActive, low()))
		obs.onDeviceMetrics(context.Background(), 7, 0, "antminer", "ant-1", sample(modelsV2.HealthHealthyActive, metricVal(math.NaN())))
		require.Len(t, rec.hashing, 1)
		require.InDelta(t, 0.5, rec.hashing[0].ratio, 1e-9)
	})

	t.Run("error status does not clear critical", func(t *testing.T) {
		rec := &recordingEmitter{}
		obs := newMetricsObserver(rec)
		obs.onDeviceMetrics(context.Background(), 7, 0, "antminer", "ant-1", sample(modelsV2.HealthCritical, metricValWithMax(0, 100e12)))
		obs.onDeviceStatus(context.Background(), 7, 0, "antminer", "ant-1", mm.MinerStatusError)
		require.Len(t, rec.hashing, 1)
		require.InDelta(t, 0.0, rec.hashing[0].ratio, 1e-9)
	})
}

// Five chips at varied temps must collapse to one (chip, max=85, avg=80) emission.
func TestObserverAggregatesPerSensorKindMaxAvg(t *testing.T) {
	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)

	hb := modelsV2.HashBoardMetrics{
		ComponentInfo: modelsV2.ComponentInfo{Index: 0, Name: "board-0"},
		TempC:         metricVal(72),
		ASICs: []modelsV2.ASICMetrics{
			{ComponentInfo: modelsV2.ComponentInfo{Index: 0}, TempC: metricVal(75)},
			{ComponentInfo: modelsV2.ComponentInfo{Index: 1}, TempC: metricVal(80)},
			{ComponentInfo: modelsV2.ComponentInfo{Index: 2}, TempC: metricVal(85)},
			{ComponentInfo: modelsV2.ComponentInfo{Index: 3}, TempC: metricVal(78)},
			{ComponentInfo: modelsV2.ComponentInfo{Index: 4}, TempC: metricVal(82)},
		},
	}

	obs.onDeviceMetrics(context.Background(), 1, 0, "proto", "proto-1", modelsV2.DeviceMetrics{
		DeviceIdentifier: "proto-1",
		HashBoards:       []modelsV2.HashBoardMetrics{hb},
		Health:           modelsV2.HealthHealthyActive,
	})

	byKind := map[string]temperatureEvent{}
	for _, e := range rec.temperature {
		byKind[e.kind] = e
	}

	chip := byKind[metrics.SensorKindChip]
	require.Equal(t, metrics.SensorKindChip, chip.kind)
	require.InDelta(t, 85.0, chip.maxC, 1e-9)
	require.InDelta(t, 80.0, chip.avgC, 1e-9) // (75+80+85+78+82)/5

	board := byKind[metrics.SensorKindBoard]
	require.Equal(t, metrics.SensorKindBoard, board.kind)
	require.InDelta(t, 72.0, board.maxC, 1e-9)
	require.InDelta(t, 72.0, board.avgC, 1e-9)
}

// The observer must not infer pool connectivity from overall device health.
// fleet_device_pool_connected is reserved until plugins surface an explicit
// pool-state signal; emitting it from health produced false positives for
// intentionally inactive devices and missed real pool disconnects.
func TestObserverDoesNotEmitPoolGaugeFromHealth(t *testing.T) {
	healths := []modelsV2.HealthStatus{
		modelsV2.HealthHealthyActive,
		modelsV2.HealthWarning,
		modelsV2.HealthHealthyInactive,
		modelsV2.HealthCritical,
		modelsV2.HealthUnknown,
	}

	for _, h := range healths {
		rec := &recordingEmitter{}
		obs := newMetricsObserver(rec)
		obs.onDeviceMetrics(context.Background(), 1, 0, "virtual", "v-1", modelsV2.DeviceMetrics{
			DeviceIdentifier: "v-1",
			Health:           h,
		})
		require.Empty(t, rec.pool, "pool gauge must not be emitted, health=%v", h)
	}
}

// only a truly-unreachable device (MinerStatusOffline) emits fleet_device_online=0; error/unknown devices are still reachable and stay online=1.
func TestObserverEmitsExplicitZeroOnOffline(t *testing.T) {
	cases := []struct {
		status mm.MinerStatus
		online bool
	}{
		{mm.MinerStatusOffline, false},
		{mm.MinerStatusError, true},
		{mm.MinerStatusUnknown, true},
		{mm.MinerStatusActive, true},
	}
	for _, tc := range cases {
		t.Run(tc.status.String(), func(t *testing.T) {
			rec := &recordingEmitter{}
			obs := newMetricsObserver(rec)
			obs.onDeviceStatus(context.Background(), 1, 0, "virtual", "v-1", tc.status)
			require.Len(t, rec.online, 1)
			require.Equal(t, tc.online, rec.online[0].online)
		})
	}
}

// exercises the success/failure branch of the telemetry-poll counter.
func TestObserverPollResultIsClosedEnum(t *testing.T) {
	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)

	obs.onPollResult(context.Background(), 1, 0, "v-1", true)
	obs.onPollResult(context.Background(), 1, 0, "v-1", false)

	require.Len(t, rec.telemetryPoll, 2)
	require.Equal(t, metrics.ResultSuccess, rec.telemetryPoll[0].labels.Result)
	require.Equal(t, metrics.ResultFailure, rec.telemetryPoll[1].labels.Result)
}

// plugins (Antminer, virtual) that report only the device-level TempC field.
func TestObserverFallsBackToAggregatedTempWhenNoComponents(t *testing.T) {
	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)
	obs.onDeviceMetrics(context.Background(), 1, 0, "antminer", "ant-2", modelsV2.DeviceMetrics{
		DeviceIdentifier: "ant-2",
		TempC:            metricVal(68.5),
		Health:           modelsV2.HealthHealthyActive,
	})
	require.Len(t, rec.temperature, 1)
	require.Equal(t, metrics.SensorKindBoard, rec.temperature[0].kind)
	require.InDelta(t, 68.5, rec.temperature[0].maxC, 1e-9)
}

// tests Antminer, Proto, asicrs, and virtual plugins
func TestObserverHandlesAllKnownDriversInFixture(t *testing.T) {
	cases := []struct {
		name   string
		driver string
		dm     modelsV2.DeviceMetrics
	}{
		{
			name:   "antminer",
			driver: "antminer",
			dm: modelsV2.DeviceMetrics{
				DeviceIdentifier: "ant-1",
				HashrateHS:       metricVal(120e12),
				TempC:            metricVal(72),
				Health:           modelsV2.HealthHealthyActive,
			},
		},
		{
			name:   "proto",
			driver: "proto",
			dm: modelsV2.DeviceMetrics{
				DeviceIdentifier: "proto-1",
				HashrateHS:       metricVal(140e12),
				HashBoards: []modelsV2.HashBoardMetrics{{
					TempC: metricVal(75),
					ASICs: []modelsV2.ASICMetrics{{TempC: metricVal(78)}},
				}},
				Health: modelsV2.HealthWarning,
			},
		},
		{
			name:   "asicrs",
			driver: "asicrs",
			dm: modelsV2.DeviceMetrics{
				DeviceIdentifier: "asicrs-1",
				HashrateHS:       metricVal(100e12),
				PSUMetrics: []modelsV2.PSUMetrics{{
					HotSpotTempC: metricVal(60),
				}},
				Health: modelsV2.HealthHealthyActive,
			},
		},
		{
			name:   "virtual",
			driver: "virtual",
			dm: modelsV2.DeviceMetrics{
				DeviceIdentifier: "virt-1",
				HashrateHS:       metricVal(0),
				TempC:            metricVal(40),
				Health:           modelsV2.HealthHealthyInactive,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingEmitter{}
			obs := newMetricsObserver(rec)
			obs.onDeviceMetrics(context.Background(), 1, 0, tc.driver, models.DeviceIdentifier(tc.dm.DeviceIdentifier), tc.dm)

			require.GreaterOrEqual(t, len(rec.hashrate), 1, "hashrate gauge required")
			require.Empty(t, rec.pool, "pool gauge must not be emitted until plugins surface real pool state")
			require.Equal(t, tc.driver, rec.hashrate[0].labels.Driver)
		})
	}
}

// asserts that orgIDToLabel returns an empty string for an unknown org rather than "0"
func TestOrgIDLabelDropsZero(t *testing.T) {
	require.Equal(t, "", metrics.OrgIDToLabel(0))
	require.Equal(t, "1", metrics.OrgIDToLabel(1))
	require.Equal(t, "9999", metrics.OrgIDToLabel(9999))
}

// protects the spelling table from drift
func TestSensorKindFromTypeAcceptsCommonAliases(t *testing.T) {
	cases := map[string]string{
		"ambient":  metrics.SensorKindAmbient,
		"intake":   metrics.SensorKindAmbient,
		"inlet":    metrics.SensorKindInlet,
		"outlet":   metrics.SensorKindOutlet,
		"exhaust":  metrics.SensorKindOutlet,
		"board":    metrics.SensorKindBoard,
		"chip":     metrics.SensorKindChip,
		"asic":     metrics.SensorKindChip,
		"hotspot":  metrics.SensorKindHotspot,
		"hot_spot": metrics.SensorKindHotspot,
	}
	for input, want := range cases {
		require.Equal(t, want, sensorKindFromType(input), "type=%s", input)
	}
	require.Equal(t, "", sensorKindFromType("nonsense"))
}

// A plugin that reports a different DeviceIdentifier than the one we asked
// it to poll must not be able to forge time series for another device.
func TestObserverDropsSampleWithMismatchedPluginDeviceID(t *testing.T) {
	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)

	obs.onDeviceMetrics(context.Background(), 1, 0, "antminer", "ant-1", modelsV2.DeviceMetrics{
		DeviceIdentifier: "ant-spoofed",
		HashrateHS:       metricVal(110e12),
		TempC:            metricVal(70),
		Health:           modelsV2.HealthHealthyActive,
	})

	require.Empty(t, rec.hashrate, "hashrate must not be emitted on mismatch")
	require.Empty(t, rec.temperature, "temperature must not be emitted on mismatch")
	require.Empty(t, rec.pool, "pool gauge must not be emitted on mismatch")
}

// Plugins that don't populate DeviceIdentifier should still produce metrics,
// labelled with the trusted requested device ID.
func TestObserverAllowsEmptyPluginDeviceID(t *testing.T) {
	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)

	obs.onDeviceMetrics(context.Background(), 1, 0, "virtual", "v-1", modelsV2.DeviceMetrics{
		// DeviceIdentifier intentionally omitted.
		HashrateHS: metricVal(50e12),
		Health:     modelsV2.HealthHealthyActive,
	})

	require.Len(t, rec.hashrate, 1)
	require.Equal(t, "v-1", rec.hashrate[0].labels.DeviceID, "label must come from trusted requested ID")
}

// A buggy or hostile plugin returning huge component arrays must not be able
// to tie up the metrics writer. The aggregator caps iteration at a fixed
// upper bound per component kind.
func TestAggregateTemperaturesTruncatesUnboundedComponentArrays(t *testing.T) {
	const huge = 10_000

	hashBoards := make([]modelsV2.HashBoardMetrics, huge)
	for i := range hashBoards {
		hashBoards[i] = modelsV2.HashBoardMetrics{TempC: metricVal(70)}
	}
	// Also stuff one hashboard with absurdly many ASICs.
	manyASICs := make([]modelsV2.ASICMetrics, huge)
	for i := range manyASICs {
		manyASICs[i] = modelsV2.ASICMetrics{TempC: metricVal(75)}
	}
	hashBoards[0].ASICs = manyASICs

	psus := make([]modelsV2.PSUMetrics, huge)
	for i := range psus {
		psus[i] = modelsV2.PSUMetrics{HotSpotTempC: metricVal(60)}
	}
	fans := make([]modelsV2.FanMetrics, huge)
	for i := range fans {
		fans[i] = modelsV2.FanMetrics{TempC: metricVal(30)}
	}
	sensors := make([]modelsV2.SensorMetrics, huge)
	for i := range sensors {
		sensors[i] = modelsV2.SensorMetrics{Type: "ambient", Value: metricVal(25)}
	}

	dm := modelsV2.DeviceMetrics{
		DeviceIdentifier: "hostile-1",
		HashBoards:       hashBoards,
		PSUMetrics:       psus,
		FanMetrics:       fans,
		SensorMetrics:    sensors,
	}

	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)
	obs.onDeviceMetrics(context.Background(), 1, 0, "virtual", "hostile-1", dm)

	// The exact aggregate counts depend on the caps; what matters is the
	// aggregator returns within bounded work — bounded by the per-kind caps
	// rather than the size of the input arrays. We assert "at most one
	// emission per sensor kind", which is the contract the rest of the
	// pipeline relies on.
	byKind := map[string]int{}
	for _, e := range rec.temperature {
		byKind[e.kind]++
	}
	for kind, n := range byKind {
		require.Equal(t, 1, n, "expected exactly one aggregate per kind, got %d for kind=%s", n, kind)
	}
	// And the produced max values come from real samples (70/75/60/25),
	// not from runaway iteration.
	for _, e := range rec.temperature {
		require.GreaterOrEqual(t, e.maxC, 25.0)
		require.LessOrEqual(t, e.maxC, 100.0)
	}
}

// Plugins reporting NaN / +Inf / -Inf temperature samples must have those
// samples dropped rather than poisoning the aggregate.
func TestAggregateTemperaturesDropsNonFiniteSamples(t *testing.T) {
	dm := modelsV2.DeviceMetrics{
		DeviceIdentifier: "bad-1",
		HashBoards: []modelsV2.HashBoardMetrics{{
			TempC: metricVal(math.NaN()),
			ASICs: []modelsV2.ASICMetrics{
				{TempC: metricVal(math.Inf(1))},
				{TempC: metricVal(math.Inf(-1))},
				{TempC: metricVal(70)}, // the one good reading
			},
		}},
	}

	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)
	obs.onDeviceMetrics(context.Background(), 1, 0, "virtual", "bad-1", dm)

	byKind := map[string]temperatureEvent{}
	for _, e := range rec.temperature {
		byKind[e.kind] = e
	}
	_, hasBoard := byKind[metrics.SensorKindBoard]
	require.False(t, hasBoard, "NaN board temp must not produce an emission")

	chip, hasChip := byKind[metrics.SensorKindChip]
	require.True(t, hasChip, "the one finite chip temp must still produce an emission")
	require.InDelta(t, 70.0, chip.maxC, 1e-9)
	require.InDelta(t, 70.0, chip.avgC, 1e-9)
}

// Readings outside the plausibility window (e.g. a stuck sensor reporting
// 9999 °C) must be dropped so they don't trigger spurious temperature
// alerts.
func TestAggregateTemperaturesClampsOutOfRangeReadings(t *testing.T) {
	dm := modelsV2.DeviceMetrics{
		DeviceIdentifier: "stuck-1",
		HashBoards: []modelsV2.HashBoardMetrics{{
			ASICs: []modelsV2.ASICMetrics{
				{TempC: metricVal(9999)},  // implausible
				{TempC: metricVal(-1000)}, // implausible
				{TempC: metricVal(82)},    // good
				{TempC: metricVal(80)},    // good
			},
		}},
	}

	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)
	obs.onDeviceMetrics(context.Background(), 1, 0, "virtual", "stuck-1", dm)

	require.Len(t, rec.temperature, 1)
	require.Equal(t, metrics.SensorKindChip, rec.temperature[0].kind)
	require.InDelta(t, 82.0, rec.temperature[0].maxC, 1e-9)
	require.InDelta(t, 81.0, rec.temperature[0].avgC, 1e-9) // (82+80)/2
}

// Non-finite hashrates must be dropped before they reach the emitter.
func TestObserverDropsNonFiniteHashrate(t *testing.T) {
	cases := []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1}
	for _, v := range cases {
		rec := &recordingEmitter{}
		obs := newMetricsObserver(rec)
		obs.onDeviceMetrics(context.Background(), 1, 0, "virtual", "v-1", modelsV2.DeviceMetrics{
			DeviceIdentifier: "v-1",
			HashrateHS:       metricVal(v),
		})
		require.Empty(t, rec.hashrate, "hashrate=%v must be dropped", v)
	}
}

// A non-finite nameplate must not block emission of a valid observed
// hashrate — expectedTHs falls through as zero so the Hashrate template
// degrades gracefully.
func TestObserverEmitsObservedWhenNameplateIsNonFinite(t *testing.T) {
	rec := &recordingEmitter{}
	obs := newMetricsObserver(rec)
	obs.onDeviceMetrics(context.Background(), 1, 0, "virtual", "v-1", modelsV2.DeviceMetrics{
		DeviceIdentifier: "v-1",
		HashrateHS:       metricValWithMax(110e12, math.NaN()),
	})
	require.Len(t, rec.hashrate, 1)
	require.InDelta(t, 110.0, rec.hashrate[0].observedTHs, 1e-9)
	require.InDelta(t, 0.0, rec.hashrate[0].expectedTHs, 1e-9)
}

// NoMetrics emitter must not panic on any call.
func TestObserverHandlesNilEmitter(t *testing.T) {
	obs := newMetricsObserver(nil) // installs NoMetrics()
	require.NotNil(t, obs)
	obs.onDeviceMetrics(context.Background(), 1, 0, "virtual", "v-1", modelsV2.DeviceMetrics{
		DeviceIdentifier: "v-1",
		HashrateHS:       metricVal(50e12),
		Health:           modelsV2.HealthHealthyActive,
	})
	obs.onDeviceStatus(context.Background(), 1, 0, "virtual", "v-1", mm.MinerStatusActive)
	obs.onPollResult(context.Background(), 1, 0, "v-1", true)
}
