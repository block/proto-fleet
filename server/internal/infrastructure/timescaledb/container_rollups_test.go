package timescaledb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestStoredContainerRollupsPowerWatermarksSurviveAbsence(t *testing.T) {
	r := newContainerRollupRig(t)
	levels := []struct {
		name   string
		source string
		query  string
		watts  float64
	}{
		{name: "facility", source: "facility", watts: 4000, query: `SELECT power_w,efficiency_jh,hashrate_hs,source_times FROM container_latest_metrics WHERE device_id=$1`},
		{name: "PDU", source: "pdu-1", watts: 1500, query: `SELECT h.power_w,h.efficiency_jh,h.hashrate_hs,h.source_times FROM container_tank_latest_metrics h JOIN container_tank t ON t.id=h.tank_id WHERE t.device_id=$1 AND t.position=1`},
		{name: "module PSU", source: "module_psu", watts: 1200, query: `SELECT h.power_w,h.efficiency_jh,h.hashrate_hs,h.source_times FROM container_module_latest_metrics h JOIN container_module m ON m.id=h.module_id JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$1 AND t.position=1 AND m.retired_at IS NULL`},
	}
	check := func(available bool, watermark time.Time) {
		t.Helper()
		for _, level := range levels {
			var power, efficiency, hash sql.NullFloat64
			var raw []byte
			require.NoError(t, r.db.QueryRowContext(t.Context(), level.query, r.databaseID).Scan(&power, &efficiency, &hash, &raw), level.name)
			require.Equal(t, available, power.Valid, level.name)
			require.Equal(t, available, efficiency.Valid, level.name)
			require.True(t, hash.Valid, "a delayed %s sample must not hide fresh board hashrate", level.name)
			if available {
				require.Equal(t, level.watts, power.Float64, level.name)
			}
			var sources map[string]time.Time
			require.NoError(t, json.Unmarshal(raw, &sources))
			require.True(t, watermark.Equal(sources[level.source]), "%s watermark: want %s, got %s", level.name, watermark, sources[level.source])
		}
	}
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	check(true, r.base)
	absent := r.snapshot(time.Second, false, 110e12, 220e12)
	for _, rollup := range []*sdk.ContainerRollup{absent.Metrics, absent.Tanks[0].Metrics, absent.Tanks[0].Modules[0].Metrics.Rollup} {
		rollup.PowerW, rollup.EfficiencyJH = nil, nil
		rollup.Power.Reporting = 0
		delete(rollup.SourceTimes, rollup.PowerSource)
	}
	absent.Tanks[0].Modules[0].Metrics.PowerW = nil
	sdk.CalculateContainerRollups(absent)
	r.store(t, absent)
	check(false, r.base)
	delayed := r.snapshot(2*time.Second, false, 120e12, 240e12)
	for _, rollup := range []*sdk.ContainerRollup{delayed.Metrics, delayed.Tanks[0].Metrics, delayed.Tanks[0].Modules[0].Metrics.Rollup} {
		at := r.base.Add(-time.Second)
		rollup.Power.OldestAt, rollup.Power.NewestAt = at, at
		rollup.SourceTimes[rollup.PowerSource] = at
	}
	r.store(t, delayed)
	check(false, r.base)
	recovered := r.snapshot(3*time.Second, false, 130e12, 260e12)
	for _, rollup := range []*sdk.ContainerRollup{recovered.Metrics, recovered.Tanks[0].Metrics, recovered.Tanks[0].Modules[0].Metrics.Rollup} {
		at := r.base.Add(time.Second)
		rollup.Power.OldestAt, rollup.Power.NewestAt = at, at
		rollup.SourceTimes[rollup.PowerSource] = at
	}
	r.store(t, recovered)
	check(true, r.base.Add(time.Second))
}

func TestStoredContainerRollupsVersionTopologyIndependentlyOfMeasurements(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	r.store(t, r.snapshot(time.Second, false, 110e12, 220e12))
	var versions, observations int
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_topology_version WHERE device_id=$1", r.databaseID).Scan(&versions))
	require.Equal(t, 1, versions, "measurement values and source times are not topology")
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_topology_observation WHERE device_id=$1", r.databaseID).Scan(&observations))
	require.Equal(t, 2, observations, "each accepted poll retains its topology observation")
	var distinctVersions int
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(DISTINCT version_id) FROM container_topology_observation WHERE device_id=$1", r.databaseID).Scan(&distinctVersions))
	require.Equal(t, 1, distinctVersions)
	var semantics int
	var savedHash float64
	var observationID sql.NullInt64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT metric_semantics, hash_rate_hs, topology_observation_id FROM device_metrics WHERE device_identifier=$1 AND time=$2", r.deviceID, r.base).Scan(&semantics, &savedHash, &observationID))
	require.Equal(t, 2, semantics)
	require.Equal(t, 300e12, savedHash)
	require.True(t, observationID.Valid)
}

func TestStoredContainerRollupsMovesPreserveSavedTankTotals(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	var firstVersion int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT version_id FROM container_topology_observation WHERE device_id=$1 AND collected_at=$2", r.databaseID, r.base).Scan(&firstVersion))
	r.store(t, r.snapshot(2*time.Second, true, 300e12, 400e12))
	var latestVersion int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT version_id FROM container_topology_observation WHERE device_id=$1 AND collected_at=$2", r.databaseID, r.base.Add(2*time.Second)).Scan(&latestVersion))
	require.NotEqual(t, firstVersion, latestVersion)
	var originalHash, originalPower, originalEfficiency, latestHash float64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT h.hashrate_hs, h.power_w, h.efficiency_jh FROM container_tank_metrics h JOIN container_tank t ON t.id=h.tank_id WHERE t.device_id=$1 AND t.position=1 AND h.time=$2`, r.databaseID, r.base).Scan(&originalHash, &originalPower, &originalEfficiency))
	require.Equal(t, 100e12, originalHash)
	require.Equal(t, 1500.0, originalPower)
	require.InDelta(t, 15e-12, originalEfficiency, 1e-20)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT h.hashrate_hs FROM container_tank_latest_metrics h JOIN container_tank t ON t.id=h.tank_id WHERE t.device_id=$1 AND t.position=1`, r.databaseID).Scan(&latestHash))
	require.Equal(t, 400e12, latestHash)
	var boardTank int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT t.position FROM hashboard_installation i JOIN hashboard_identity b ON b.hashboard_id=i.hashboard_id JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id WHERE b.kind='serial' AND b.value='board-a' AND i.removed_at IS NULL`).Scan(&boardTank))
	require.Equal(t, 2, boardTank)

	// A retry and a delayed poll must not create additional topology or history.
	r.store(t, r.snapshot(2*time.Second, false, 900e12, 900e12))
	r.store(t, r.snapshot(time.Second, false, 900e12, 900e12))
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_topology_observation WHERE device_id=$1", r.databaseID).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_topology_version WHERE device_id=$1", r.databaseID).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_tank_metrics h JOIN container_tank t ON t.id=h.tank_id WHERE t.device_id=$1`, r.databaseID).Scan(&count))
	require.Equal(t, 4, count)
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_module_metrics").Scan(&count))
	require.Equal(t, 4, count, "duplicate and delayed polls cannot append recalculated module history")
}

func TestStoredContainerRollupsPartialMapKeepsUnreportedHardware(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	partial := r.snapshot(time.Second, false, 110e12, 220e12)
	partial.TopologyComplete = false
	partial.Tanks = partial.Tanks[:1]
	sdk.CalculateContainerRollups(partial)
	r.store(t, partial)
	var present bool
	var retired sql.NullTime
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT t.present,m.retired_at FROM container_tank t JOIN container_module m ON m.tank_id=t.id WHERE t.device_id=$1 AND t.position=2`, r.databaseID).Scan(&present, &retired))
	require.True(t, present)
	require.False(t, retired.Valid)
	var installations int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_installation i JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$1 AND i.removed_at IS NULL`, r.databaseID).Scan(&installations))
	require.Equal(t, 2, installations)
	var complete bool
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT complete FROM container_topology_observation WHERE device_id=$1 AND collected_at=$2`, r.databaseID, partial.ObservedAt).Scan(&complete))
	require.False(t, complete)
}

func TestStoredContainerRollupsPreserveTypedBoardPowerIncludingZero(t *testing.T) {
	r := newContainerRollupRig(t)
	first := r.snapshot(0, false, 100e12, 200e12)
	first.Tanks[0].Modules[0].Hashboards[0].PowerW = new(0.0)
	first.Tanks[0].Modules[0].Hashboards[0].EfficiencyJH = new(0.0)
	r.store(t, first)
	var power, efficiency sql.NullFloat64
	for _, query := range []string{
		`SELECT h.power_w,h.efficiency_jh FROM hashboard_latest_metrics h JOIN hashboard_identity i ON i.hashboard_id=h.hashboard_id WHERE i.kind='serial' AND i.value='board-a'`,
		`SELECT h.power_w,h.efficiency_jh FROM hashboard_metrics h JOIN hashboard_identity i ON i.hashboard_id=h.hashboard_id WHERE i.kind='serial' AND i.value='board-a'`,
	} {
		require.NoError(t, r.db.QueryRowContext(t.Context(), query).Scan(&power, &efficiency))
		require.True(t, power.Valid)
		require.Zero(t, power.Float64)
		require.True(t, efficiency.Valid)
		require.Zero(t, efficiency.Float64)
	}
	second := r.snapshot(time.Second, false, 100e12, 200e12)
	second.Tanks[0].Modules[0].Hashboards[0].PowerW = nil
	second.Tanks[0].Modules[0].Hashboards[0].EfficiencyJH = nil
	r.store(t, second)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT h.power_w,h.efficiency_jh FROM hashboard_latest_metrics h JOIN hashboard_identity i ON i.hashboard_id=h.hashboard_id WHERE i.kind='serial' AND i.value='board-a'`).Scan(&power, &efficiency))
	require.False(t, power.Valid, "PSU and facility measurements cannot fill missing board power")
	require.False(t, efficiency.Valid)
}

func TestStoredContainerRollupsExcludeOutOfRangeHashboards(t *testing.T) {
	for _, position := range []int32{0, sdk.CT1HashboardsPerModule + 1} {
		t.Run(fmt.Sprintf("position_%d", position), func(t *testing.T) {
			r := newContainerRollupRig(t)
			snapshot := r.snapshot(0, false, 100e12, 200e12)
			snapshot.Tanks[0].Modules[0].Hashboards[0].Position = position
			require.NotNil(t, snapshot.Metrics, "exercise persisted rollup recalculation")
			r.store(t, snapshot)

			for _, query := range []string{
				`SELECT h.hashrate_hs FROM container_module_metrics h JOIN container_module m ON m.id=h.module_id JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$1 AND t.position=1`,
				`SELECT h.hashrate_hs FROM container_module_latest_metrics h JOIN container_module m ON m.id=h.module_id JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$1 AND t.position=1`,
				`SELECT h.hashrate_hs FROM container_tank_metrics h JOIN container_tank t ON t.id=h.tank_id WHERE t.device_id=$1 AND t.position=1`,
				`SELECT h.hashrate_hs FROM container_tank_latest_metrics h JOIN container_tank t ON t.id=h.tank_id WHERE t.device_id=$1 AND t.position=1`,
			} {
				var hash sql.NullFloat64
				require.NoError(t, r.db.QueryRowContext(t.Context(), query, r.databaseID).Scan(&hash))
				require.False(t, hash.Valid, "rejected board must not contribute to %s", query)
			}
			for _, query := range []string{
				`SELECT h.hash_rate_hs FROM device_metrics h JOIN device d ON d.device_identifier=h.device_identifier WHERE d.id=$1`,
				`SELECT hashrate_hs FROM container_latest_metrics WHERE device_id=$1`,
			} {
				var hash float64
				require.NoError(t, r.db.QueryRowContext(t.Context(), query, r.databaseID).Scan(&hash))
				require.Equal(t, 200e12, hash, "only the healthy module contributes to %s", query)
			}
			require.Len(t, snapshot.Tanks[0].Modules[0].Hashboards, sdk.CT1HashboardsPerModule)
			for _, board := range snapshot.Tanks[0].Modules[0].Hashboards {
				require.GreaterOrEqual(t, board.Position, int32(1))
				require.LessOrEqual(t, board.Position, int32(sdk.CT1HashboardsPerModule))
				require.Equal(t, "unknown", board.Occupancy)
			}
		})
	}
}

type containerRollupRig struct {
	db         *sql.DB
	telemetry  *TimescaleTelemetryStore
	deviceID   string
	databaseID int64
	base       time.Time
}

func newContainerRollupRig(t *testing.T) *containerRollupRig {
	t.Helper()
	dbSvc := testutil.NewDatabaseService(t, nil)
	user := dbSvc.CreateSuperAdminUser()
	device := dbSvc.CreateDevice(user.OrganizationID, "proto")
	_, err := dbSvc.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
	require.NoError(t, err)
	telemetry, err := NewTelemetryStore(dbSvc.DB, DefaultConfig())
	require.NoError(t, err)
	return &containerRollupRig{db: dbSvc.DB, telemetry: telemetry, deviceID: device.ID, databaseID: device.DatabaseID, base: time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)}
}

func (r *containerRollupRig) store(t *testing.T, snapshot *sdk.ContainerSnapshot) {
	t.Helper()
	require.NoError(t, r.telemetry.StoreDeviceMetrics(t.Context(), modelsV2.DeviceMetrics{DeviceIdentifier: r.deviceID, Timestamp: snapshot.ObservedAt, Container: snapshot, Health: modelsV2.HealthHealthyActive}))
}

func (r *containerRollupRig) snapshot(offset time.Duration, moved bool, hashA, hashB float64) *sdk.ContainerSnapshot {
	at := r.base.Add(offset)
	power := func(watts float64, source string) *sdk.ContainerRollup {
		return &sdk.ContainerRollup{PowerW: new(watts), PowerSource: source, Power: sdk.MetricCoverage{Reporting: 1, Expected: 1, TopologyComplete: true, OldestAt: at, NewestAt: at}, SourceTimes: map[string]time.Time{source: at}}
	}
	boards := []sdk.ContainerHashboard{
		{Position: 1, Occupancy: "occupied", Serial: "board-a", State: sdk.ModuleHashing, ObservedAt: at, StateObservedAt: at, HashrateHS: new(hashA), TemperatureC: new(50.0), PowerW: new(1000.0), EfficiencyJH: new(1000 / hashA)},
		{Position: 1, Occupancy: "occupied", Serial: "board-b", State: sdk.ModuleHashing, ObservedAt: at, StateObservedAt: at, HashrateHS: new(hashB), TemperatureC: new(70.0), PowerW: new(2000.0), EfficiencyJH: new(2000 / hashB)},
	}
	if moved {
		boards[0], boards[1] = boards[1], boards[0]
	}
	snapshot := &sdk.ContainerSnapshot{ObservedAt: at, TopologyComplete: true, Metrics: power(4000, "facility")}
	for i, board := range boards {
		position := int32(i + 1) //nolint:gosec // fixture contains two tanks
		snapshot.Tanks = append(snapshot.Tanks, sdk.ContainerTank{Position: position, TopologyComplete: true, Metrics: power(1500+float64(i)*1000, fmt.Sprintf("pdu-%d", position)), Modules: []sdk.ContainerModule{{
			Position: 1, ReportedID: fmt.Sprintf("module-%d", position), State: sdk.ModuleHashing, ObservedAt: at,
			Metrics:    &sdk.ContainerModuleMetrics{ObservedAt: at, PowerW: new(1200.0), Rollup: power(1200, "module_psu")},
			Hashboards: []sdk.ContainerHashboard{board, {Position: 2, Occupancy: "empty", State: sdk.ModuleUnknown}, {Position: 3, Occupancy: "empty", State: sdk.ModuleUnknown}},
		}}})
	}
	sdk.CalculateContainerRollups(snapshot)
	return snapshot
}

func TestStoredContainerRollupsRejectEveryCachedPollAfterSiteMove(t *testing.T) {
	r := newContainerRollupRig(t)
	ctx := t.Context()
	var org, siteA, siteB, tank int64
	require.NoError(t, r.db.QueryRowContext(ctx, "SELECT org_id FROM device WHERE id=$1", r.databaseID).Scan(&org))
	require.NoError(t, r.db.QueryRowContext(ctx, "INSERT INTO site(org_id,name,slug) VALUES ($1,'A','a') RETURNING id", org).Scan(&siteA))
	require.NoError(t, r.db.QueryRowContext(ctx, "INSERT INTO site(org_id,name,slug) VALUES ($1,'B','b') RETURNING id", org).Scan(&siteB))
	move := func(site int64) {
		_, err := r.db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE id=$2", site, r.databaseID)
		require.NoError(t, err)
	}
	cached := func(offset time.Duration) *sdk.ContainerSnapshot {
		s := r.snapshot(offset, false, 100e12, 200e12)
		for ti := range s.Tanks {
			b := &s.Tanks[ti].Modules[0].Hashboards[0]
			b.ObservedAt, b.StateObservedAt = r.base.Add(-time.Second), r.base.Add(-time.Second)
		}
		sdk.CalculateContainerRollups(s)
		return s
	}
	move(siteA)
	// Initial discovery may use asynchronous readings, including later cached polls.
	r.store(t, cached(0))
	r.store(t, cached(time.Second))
	var hash sql.NullFloat64
	require.NoError(t, r.db.QueryRowContext(ctx, "SELECT hashrate_hs FROM container_latest_metrics WHERE device_id=$1", r.databaseID).Scan(&hash))
	require.True(t, hash.Valid)
	require.NoError(t, r.db.QueryRowContext(ctx, "SELECT id FROM container_tank WHERE device_id=$1 AND position=1", r.databaseID).Scan(&tank))
	move(siteB)
	for _, offset := range []time.Duration{2 * time.Second, 3 * time.Second} {
		r.store(t, cached(offset))
		for _, query := range []string{
			"SELECT hashrate_hs,temperature_c,efficiency_jh FROM container_latest_metrics WHERE device_id=$1",
			"SELECT h.hashrate_hs,h.temperature_c,h.efficiency_jh FROM container_tank_latest_metrics h JOIN container_tank t ON t.id=h.tank_id WHERE t.device_id=$1 AND t.position=1",
			"SELECT h.hashrate_hs,h.temperature_c,h.efficiency_jh FROM container_module_latest_metrics h JOIN container_module m ON m.id=h.module_id JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$1 AND t.position=1",
		} {
			var temperature, efficiency sql.NullFloat64
			require.NoError(t, r.db.QueryRowContext(ctx, query, r.databaseID).Scan(&hash, &temperature, &efficiency))
			require.False(t, hash.Valid, "cached poll %s", offset)
			require.False(t, temperature.Valid)
			require.False(t, efficiency.Valid)
		}
		authority, err := json.Marshal([]map[string]any{{"org_wide": false, "sites": []int64{siteB}}})
		require.NoError(t, err)
		rows, err := sqlc.New(r.db).GetContainerTankHistory(ctx, sqlc.GetContainerTankHistoryParams{TankID: tank, ContainerIdentifier: r.deviceID, OrgID: org, Authority: authority, StartTime: r.base.Add(-time.Minute), EndTime: r.base.Add(time.Minute), GranularitySeconds: 1})
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		for _, row := range rows {
			if row.MeasurementType != 3 {
				require.False(t, row.HasValues, "site B must not see prior-site hash, temperature or efficiency")
			}
		}
	}
	r.store(t, r.snapshot(4*time.Second, false, 110e12, 220e12))
	require.NoError(t, r.db.QueryRowContext(ctx, "SELECT hashrate_hs FROM container_latest_metrics WHERE device_id=$1", r.databaseID).Scan(&hash))
	require.True(t, hash.Valid)
	require.Equal(t, 330e12, hash.Float64)
	var original float64
	require.NoError(t, r.db.QueryRowContext(ctx, "SELECT hashrate_hs FROM container_tank_metrics WHERE tank_id=$1 AND time=$2 AND site_id=$3", tank, r.base, siteA).Scan(&original))
	require.Equal(t, 100e12, original)
}

func TestRejectedModuleClaimsPreventAlternateAliasMove(t *testing.T) {
	r := newContainerRollupRig(t)
	first := r.snapshot(0, false, 100e12, 200e12)
	first.Tanks[0].Modules[0].Hashboards[0].MACAddress = "aa:bb:cc:dd:ee:01"
	r.store(t, first)
	conflicting := r.snapshot(time.Second, false, 100e12, 200e12)
	// Collector rejects inconsistent module IDs but retains normalized claims.
	rejected := sdk.UnavailableHashboard(1, sdk.HashboardIdentityConflict)
	rejected.IdentityClaims = []sdk.HashboardIdentityClaim{{MACAddress: "aa:bb:cc:dd:ee:01"}}
	conflicting.Tanks[0].Modules[0].Hashboards[0] = rejected
	conflicting.Tanks[1].Modules[0].Hashboards[0].Serial = "board-a"
	r.store(t, conflicting)
	var tank, installations int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT t.position FROM hashboard_installation i JOIN hashboard_identity a ON a.hashboard_id=i.hashboard_id JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id WHERE a.kind='serial' AND a.value='board-a' AND i.removed_at IS NULL`).Scan(&tank))
	require.Equal(t, 1, tank, "alternate alias cannot move the board out of the conflicting module")
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_installation i JOIN hashboard_identity a ON a.hashboard_id=i.hashboard_id WHERE a.kind='serial' AND a.value='board-a'`).Scan(&installations))
	require.Equal(t, 1, installations)
	for _, level := range conflicting.Tanks {
		require.Equal(t, sdk.HashboardIdentityConflict, level.Modules[0].Hashboards[0].UnavailableReason)
		require.Nil(t, level.Metrics.HashrateHS)
	}
}

func TestStoredPSUWatermarkSurvivesNewerInvalidSample(t *testing.T) {
	r := newContainerRollupRig(t)
	for _, step := range []struct {
		poll, source     time.Duration
		valid, available bool
		watermark        time.Duration
	}{
		{0, 0, true, true, 0},
		{20 * time.Second, 20 * time.Second, false, false, 20 * time.Second},
		{21 * time.Second, 10 * time.Second, true, false, 20 * time.Second},
		{30 * time.Second, 30 * time.Second, true, true, 30 * time.Second},
	} {
		s := r.snapshot(step.poll, false, 100e12, 200e12)
		for ti := range s.Tanks {
			m := s.Tanks[ti].Modules[0].Metrics
			at := r.base.Add(step.source)
			m.Rollup.SourceTimes["module_psu"] = at
			m.Rollup.Power.OldestAt, m.Rollup.Power.NewestAt = at, at
			if !step.valid {
				m.PowerW, m.Rollup.PowerW = nil, nil
				m.Rollup.Power.Reporting = 0
			}
		}
		r.store(t, s)
		var power, efficiency sql.NullFloat64
		var raw []byte
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT h.power_w,h.efficiency_jh,h.source_times FROM container_module_latest_metrics h JOIN container_module m ON m.id=h.module_id JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$1 AND t.position=1`, r.databaseID).Scan(&power, &efficiency, &raw))
		require.Equal(t, step.available, power.Valid, "poll %s", step.poll)
		require.Equal(t, step.available, efficiency.Valid)
		var sources map[string]time.Time
		require.NoError(t, json.Unmarshal(raw, &sources))
		require.True(t, r.base.Add(step.watermark).Equal(sources["module_psu"]))
	}
}

func TestContainerPowerTimesBoundedByServerClock(t *testing.T) {
	received := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	for _, offset := range []time.Duration{5 * time.Second, 5*time.Second + time.Nanosecond} {
		t.Run(offset.String(), func(t *testing.T) {
			at := received.Add(offset)
			r := &sdk.ContainerRollup{
				PowerW: new(1000.0), PowerSource: "facility",
				Power:       sdk.MetricCoverage{Reporting: 1, Expected: 1, TopologyComplete: true, OldestAt: at, NewestAt: at},
				SourceTimes: map[string]time.Time{"facility": at},
			}
			// A slightly fast collection clock cannot extend the server's allowance.
			s := &sdk.ContainerSnapshot{ObservedAt: received.Add(4 * time.Second), Metrics: r}
			normalizeContainerPowerTimes(s, received)
			if offset == 5*time.Second {
				require.NotNil(t, r.PowerW)
				require.Equal(t, at, r.SourceTimes["facility"])
			} else {
				require.Nil(t, r.PowerW)
				require.NotContains(t, r.SourceTimes, "facility")
			}
		})
	}
}

func TestStoredContainerRollupsRejectInvalidPowerTimestamps(t *testing.T) {
	for _, tc := range []struct {
		name      string
		edit      func(*sdk.ContainerRollup, time.Time)
		available bool
		watermark time.Duration
	}{
		{name: "far future", edit: func(r *sdk.ContainerRollup, at time.Time) {
			future := at.AddDate(1, 0, 0)
			r.SourceTimes[r.PowerSource] = future
			r.Power.OldestAt, r.Power.NewestAt = future, future
		}},
		{name: "future watermark hidden by fresh coverage", edit: func(r *sdk.ContainerRollup, at time.Time) {
			r.SourceTimes[r.PowerSource] = at.AddDate(1, 0, 0)
		}},
		{name: "beyond collection skew allowance", edit: func(r *sdk.ContainerRollup, at time.Time) {
			future := at.Add(6 * time.Second)
			r.SourceTimes[r.PowerSource] = future
			r.Power.OldestAt, r.Power.NewestAt = future, future
		}},
		{name: "missing source", edit: func(r *sdk.ContainerRollup, _ time.Time) {
			delete(r.SourceTimes, r.PowerSource)
		}},
		{name: "zero source", edit: func(r *sdk.ContainerRollup, _ time.Time) {
			r.SourceTimes[r.PowerSource] = time.Time{}
		}},
		{name: "coverage disagrees with source", edit: func(r *sdk.ContainerRollup, at time.Time) {
			r.Power.NewestAt = at.Add(-time.Second)
		}},
		{name: "unavailable value retains trustworthy time", watermark: time.Second, edit: func(r *sdk.ContainerRollup, _ time.Time) {
			r.PowerW = nil
			r.Power.Reporting = 0
			r.Power.OldestAt, r.Power.NewestAt = time.Time{}, time.Time{}
		}},
		{name: "invalid value retains trustworthy time", watermark: time.Second, edit: func(r *sdk.ContainerRollup, _ time.Time) {
			r.PowerW = new(-1.0)
		}},
		{name: "five second allowance", available: true, watermark: 6 * time.Second, edit: func(r *sdk.ContainerRollup, at time.Time) {
			future := at.Add(5 * time.Second)
			r.SourceTimes[r.PowerSource] = future
			r.Power.OldestAt, r.Power.NewestAt = future, future
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newContainerRollupRig(t)
			check := func(available bool, watermark time.Time) {
				t.Helper()
				for _, level := range []struct{ source, query string }{
					{"facility", `SELECT power_w,efficiency_jh,hashrate_hs,source_times FROM container_latest_metrics WHERE device_id=$1`},
					{"pdu-1", `SELECT h.power_w,h.efficiency_jh,h.hashrate_hs,h.source_times FROM container_tank_latest_metrics h JOIN container_tank t ON t.id=h.tank_id WHERE t.device_id=$1 AND t.position=1`},
					{"module_psu", `SELECT h.power_w,h.efficiency_jh,h.hashrate_hs,h.source_times FROM container_module_latest_metrics h JOIN container_module m ON m.id=h.module_id JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$1 AND t.position=1 AND m.retired_at IS NULL`},
				} {
					var power, efficiency, hash sql.NullFloat64
					var raw []byte
					require.NoError(t, r.db.QueryRowContext(t.Context(), level.query, r.databaseID).Scan(&power, &efficiency, &hash, &raw))
					require.Equal(t, available, power.Valid, level.source)
					require.Equal(t, available, efficiency.Valid, level.source)
					require.True(t, hash.Valid, "invalid meter evidence must not discard board hashrate")
					var sources map[string]time.Time
					require.NoError(t, json.Unmarshal(raw, &sources))
					require.True(t, watermark.Equal(sources[level.source]), "%s: expected %s, got %s", level.source, watermark, sources[level.source])
					require.NotContains(t, sources, "unexpected-future")
					require.NotContains(t, sources, "unexpected-zero")
				}
			}
			r.store(t, r.snapshot(0, false, 100e12, 200e12))
			check(true, r.base)
			bad := r.snapshot(time.Second, false, 100e12, 200e12)
			for _, rollup := range []*sdk.ContainerRollup{bad.Metrics, bad.Tanks[0].Metrics, bad.Tanks[0].Modules[0].Metrics.Rollup} {
				tc.edit(rollup, bad.ObservedAt)
				rollup.SourceTimes["unexpected-future"] = bad.ObservedAt.AddDate(1, 0, 0)
				rollup.SourceTimes["unexpected-zero"] = time.Time{}
			}
			// Submit plugin-supplied data without running the trusted collector again.
			r.store(t, bad)
			check(tc.available, r.base.Add(tc.watermark))
			r.store(t, r.snapshot(10*time.Second, false, 100e12, 200e12))
			check(true, r.base.Add(10*time.Second))
		})
	}
}
