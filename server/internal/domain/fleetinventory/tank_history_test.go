package fleetinventory_test

import (
	"strings"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTankHistory(t *testing.T) {
	infra := testutil.InitializeDBServiceInfrastructure(t)
	org := infra.DatabaseService.CreateSuperAdminUser().OrganizationID
	ctx := t.Context()
	db := infra.ServiceProvider.DB
	identifier := infra.DatabaseService.CreateTestMiners(org, 1, "https://192.0.2.1:80")[0]
	_, err := db.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, identifier)
	require.NoError(t, err)
	var deviceID, tankID, emptyTankID, first, second, retired, siteID int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id FROM device WHERE device_identifier=$1`, identifier).Scan(&deviceID))
	_, err = db.ExecContext(ctx, `INSERT INTO container_inventory(device_id,observed_at,org_id) SELECT id,now(),org_id FROM device WHERE id=$1`, deviceID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO container_tank (device_id,position,first_seen_at,last_seen_at) VALUES ($1,1,now(),now()) RETURNING id`, deviceID).Scan(&tankID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO container_tank (device_id,position,first_seen_at,last_seen_at) VALUES ($1,2,now(),now()) RETURNING id`, deviceID).Scan(&emptyTankID))
	for i, id := range []*int64{&first, &second, &retired} {
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO container_module (tank_id,position,first_seen_at,last_seen_at) VALUES ($1,$2,now(),now()) RETURNING id`, tankID, i+1).Scan(id))
	}
	_, err = db.ExecContext(ctx, `UPDATE container_module SET retired_at=now() WHERE id=$1`, retired)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO site (org_id,name,slug) VALUES ($1,'Tank site','tank-site') RETURNING id`, org).Scan(&siteID))
	start := time.Now().UTC().Truncate(time.Hour).Add(-24 * time.Hour)
	service := fleetinventory.NewService(sqlc.New(db))
	scopes := []fleetinventory.Scope{{OrgWide: true}}
	request := &pb.GetTankHistoryRequest{ContainerIdentifier: identifier, TankId: tankID, StartTime: timestamppb.New(start), EndTime: timestamppb.New(start.Add(time.Hour)), GranularitySeconds: 90}
	var versionID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO container_topology_version (device_id,fingerprint,topology) VALUES ($1,'fixture','{}') RETURNING id`, deviceID).Scan(&versionID))
	observation := func(at time.Time) int64 {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO container_topology_observation (device_id,version_id,collected_at,complete) VALUES ($1,$2,$3,true) ON CONFLICT (device_id,collected_at) DO UPDATE SET complete=EXCLUDED.complete RETURNING id`, deviceID, versionID, at).Scan(&id))
		return id
	}
	coverage := `{"hashrate":{"reporting":2,"expected":2,"topology_complete":true},"power":{"reporting":1,"expected":1,"topology_complete":true},"temperature":{"reporting":2,"expected":2,"topology_complete":true}}`
	insert := func(tank int64, offset int, hash, power, efficiency, temp, low, high any) {
		at := start.Add(time.Duration(offset) * time.Second)
		_, err := db.ExecContext(ctx, `INSERT INTO container_tank_metrics (tank_id,time,topology_observation_id,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage,topology_version_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, tank, at, observation(at), hash, power, efficiency, temp, low, high, coverage, versionID)
		require.NoError(t, err)
	}
	// Every row is an immutable tank observation calculated when the poll arrived.
	insert(tankID, 0, 300e12, 6000, 20e-12, 65, 60, 70)
	insert(tankID, 30, 600e12, 9000, 15e-12, 67, 62, 72)
	insert(tankID, 90, 300e12, nil, nil, 65, 60, 70)
	// Separate partial polls remain separate, never reconstructed using present membership.
	insert(tankID, 180, 100e12, 2000, 20e-12, 60, 60, 60)
	insert(tankID, 181, 200e12, 4000, 20e-12, 70, 70, 70)
	insert(tankID, 270, 0, 0, nil, 65, 60, 70)
	insert(tankID, 360, 300e12, 6000, 20e-12, 65, 60, 70)
	insert(tankID, 361, 100e12, 2000, nil, 60, 60, 60)
	// Non-finite stored values cannot poison another valid sample in the bucket.
	insert(tankID, 450, "NaN", "Infinity", "NaN", "-Infinity", "-Infinity", "Infinity")
	insert(tankID, 451, 200e12, 4000, 20e-12, 70, 70, 70)
	insert(tankID, 540, nil, nil, nil, nil, nil, nil)
	insert(tankID, 630, 100e12, 4000, nil, 65, 60, 70)
	insert(tankID, 3600, 300e12, 6000, 20e-12, 65, 60, 70)
	// Even contradictory current module history cannot alter saved tank readings.
	_, err = db.ExecContext(ctx, `INSERT INTO container_module_metrics (module_id,time,collected_at,hashrate_hs,power_w,temperature_c) VALUES ($1,$2,$2,999e12,99999,99)`, retired, start)
	require.NoError(t, err)

	t.Run("aggregates available observations in SQL", func(t *testing.T) {
		response, err := service.TankHistory(ctx, org, request, scopes)
		require.NoError(t, err)
		require.Len(t, response.Metrics, 32)
		metric := func(offset int, kind telemetrypb.MeasurementType) *telemetrypb.Metric {
			for _, m := range response.Metrics {
				if m.OpenTime.AsTime().Equal(start.Add(time.Duration(offset)*time.Second)) && m.MeasurementType == kind {
					return m
				}
			}
			t.Fatalf("missing metric %v at %d", kind, offset)
			return nil
		}
		values := func(offset int, kind telemetrypb.MeasurementType) []float64 {
			result := []float64{}
			for _, v := range metric(offset, kind).AggregatedValues {
				result = append(result, v.Value)
			}
			return result
		}
		require.Equal(t, []float64{450, 300, 600}, values(0, telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE))
		require.Equal(t, []float64{7.5, 6, 9}, values(0, telemetrypb.MeasurementType_MEASUREMENT_TYPE_POWER))
		require.InDelta(t, 17.5, values(0, telemetrypb.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY)[0], 1e-9)
		require.Equal(t, []float64{66, 60, 72}, values(0, telemetrypb.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE))
		require.EqualValues(t, 1, metric(0, telemetrypb.MeasurementType_MEASUREMENT_TYPE_POWER).DeviceCount)
		require.EqualValues(t, 2, metric(0, telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE).GetReportingCount())
		require.Equal(t, []float64{300, 300, 300}, values(90, telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE))
		require.Empty(t, values(90, telemetrypb.MeasurementType_MEASUREMENT_TYPE_POWER))
		require.Zero(t, metric(90, telemetrypb.MeasurementType_MEASUREMENT_TYPE_POWER).DeviceCount)
		require.Empty(t, values(90, telemetrypb.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY))
		require.Equal(t, []float64{0, 0, 0}, values(270, telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE))
		require.Empty(t, values(270, telemetrypb.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY))
		require.Equal(t, []float64{150, 100, 200}, values(180, telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE))
		require.Equal(t, []float64{200, 100, 300}, values(360, telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE))
		require.Equal(t, []float64{200, 200, 200}, values(450, telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE))
		require.Equal(t, []float64{4, 4, 4}, values(450, telemetrypb.MeasurementType_MEASUREMENT_TYPE_POWER))
		require.Equal(t, []float64{70, 70, 70}, values(450, telemetrypb.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE))
		for _, kind := range []telemetrypb.MeasurementType{1, 2, 3, 4} {
			require.Empty(t, values(540, kind))
			require.Zero(t, metric(540, kind).DeviceCount)
		}
		require.Equal(t, []float64{100, 100, 100}, values(630, telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE))
		require.Equal(t, []float64{4, 4, 4}, values(630, telemetrypb.MeasurementType_MEASUREMENT_TYPE_POWER))
		require.Empty(t, values(630, telemetrypb.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY))

	})

	t.Run("current tank totals use saved readings independently of module metrics", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE container_module SET state='hashing',state_observed_at=now(),last_seen_at=now() WHERE tank_id=$1`, tankID)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO container_module_latest_metrics (module_id,time,collected_at,hashrate_hs,power_w,temperature_c) VALUES ($1,now(),now(),100e12,NULL,60),($2,now(),now(),200e12,4000,NULL),($3,now(),now(),999e12,99999,99)`, first, second, retired)
		require.NoError(t, err)
		at := time.Now().UTC().Truncate(time.Microsecond)
		_, err = db.ExecContext(ctx, `INSERT INTO container_tank_latest_metrics (tank_id,time,topology_observation_id,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage) VALUES ($1,$2,$3,300e12,4500,15e-12,65,60,70,$4)`, tankID, at, observation(at), strings.ReplaceAll(coverage, `"topology_complete":true`, `"topology_complete":true,"oldest_at":"`+at.Format(time.RFC3339Nano)+`","newest_at":"`+at.Format(time.RFC3339Nano)+`"`))
		require.NoError(t, err)

		list := func() *pb.InventoryItem {
			response, err := service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Kinds: []pb.EntityKind{pb.EntityKind_ENTITY_KIND_TANK}}, scopes)
			require.NoError(t, err)
			for _, item := range response.Items {
				if item.Ref.Id == tankID {
					return item
				}
			}
			t.Fatal("missing tank")
			return nil
		}
		item := list()
		require.EqualValues(t, 2, item.AssetCount)
		require.EqualValues(t, 2, item.HashrateReportingCount)
		require.EqualValues(t, 1, item.PowerReportingCount)
		require.EqualValues(t, 2, item.ListStats.TemperatureReportingCount)
		require.EqualValues(t, 2, item.ListStats.EfficiencyReportingCount)
		require.NotNil(t, item.HashrateThs)
		require.Equal(t, 300.0, *item.HashrateThs)
		require.NotNil(t, item.PowerKw)
		require.Equal(t, 4.5, *item.PowerKw)
		require.NotNil(t, item.EfficiencyJth)
		require.Equal(t, 15.0, *item.EfficiencyJth)
		require.NotNil(t, item.MinTemperatureC)
		require.Equal(t, 60.0, *item.MinTemperatureC)
		_, err = db.ExecContext(ctx, `UPDATE container_tank_latest_metrics SET hashrate_hs=NULL,power_w=NULL,efficiency_jh=NULL,temperature_c=NULL,min_temperature_c=NULL,max_temperature_c=NULL WHERE tank_id=$1`, tankID)
		require.NoError(t, err)
		item = list()
		require.Nil(t, item.HashrateThs)
		require.Nil(t, item.PowerKw)
		require.Nil(t, item.EfficiencyJth)
		require.Nil(t, item.MinTemperatureC)
	})
	t.Run("module inventory uses saved efficiency without recomputing a ratio", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `INSERT INTO container_module_latest_metrics (module_id,time,collected_at,hashrate_hs,power_w,efficiency_jh,temperature_c) VALUES ($1,now(),now(),200e12,4000,17e-12,70) ON CONFLICT (module_id) DO UPDATE SET time=EXCLUDED.time,collected_at=EXCLUDED.collected_at,hashrate_hs=EXCLUDED.hashrate_hs,power_w=EXCLUDED.power_w,efficiency_jh=EXCLUDED.efficiency_jh,temperature_c=EXCLUDED.temperature_c`, second)
		require.NoError(t, err)
		read := func() *pb.InventoryItem {
			response, err := service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Kinds: []pb.EntityKind{pb.EntityKind_ENTITY_KIND_MODULE}}, scopes)
			require.NoError(t, err)
			for _, item := range response.Items {
				if item.Ref.Id == second {
					return item
				}
			}
			t.Fatal("missing module")
			return nil
		}
		item := read()
		require.NotNil(t, item.EfficiencyJth)
		require.InDelta(t, 17.0, *item.EfficiencyJth, 1e-9, "saved efficiency differs from the 20 J/TH power/hashrate ratio")
		_, err = db.ExecContext(ctx, `UPDATE container_module_latest_metrics SET efficiency_jh=NULL WHERE module_id=$1`, second)
		require.NoError(t, err)
		require.Nil(t, read().EfficiencyJth, "missing saved efficiency must not be reconstructed")
	})

	t.Run("supports every fleet duration with bounded output", func(t *testing.T) {
		for _, window := range []struct {
			hours   int
			seconds int32
		}{{1, 90}, {24, 90}, {168, 900}, {720, 2700}, {2160, 8100}, {8760, 32850}} {
			req, ok := proto.Clone(request).(*pb.GetTankHistoryRequest)
			require.True(t, ok)
			req.EndTime = timestamppb.New(start.Add(time.Duration(window.hours) * time.Hour))
			req.GranularitySeconds = window.seconds
			response, err := service.TankHistory(ctx, org, req, scopes)
			require.NoError(t, err)
			require.NotEmpty(t, response.Metrics)
			require.LessOrEqual(t, len(response.Metrics), 4000)
		}
	})

	t.Run("includes history older than 24 hours at the requested granularity", func(t *testing.T) {
		// Exercise an already-materialized summary, as when the bootstrap job has run.
		_, err := db.ExecContext(ctx, `CALL refresh_continuous_aggregate('container_tank_metrics_hourly',NULL,NULL)`)
		require.NoError(t, err)
		insert(tankID, -3*86400, 700e12, 14000, 20e-12, 65, 60, 70)
		// Backdated fixture rows need an explicit refresh behind the summary watermark.
		_, err = db.ExecContext(ctx, `CALL refresh_continuous_aggregate('container_tank_metrics_hourly',NULL,NULL)`)
		require.NoError(t, err)
		req, ok := proto.Clone(request).(*pb.GetTankHistoryRequest)
		require.True(t, ok)
		req.StartTime = timestamppb.New(start.Add(-7 * 24 * time.Hour))
		req.GranularitySeconds = 900
		response, err := service.TankHistory(ctx, org, req, scopes)
		require.NoError(t, err)
		require.NotEmpty(t, response.Metrics)
		require.Equal(t, start.Add(-3*24*time.Hour), response.Metrics[0].OpenTime.AsTime())
		for _, m := range response.Metrics {
			if m.MeasurementType == telemetrypb.MeasurementType_MEASUREMENT_TYPE_HASHRATE && m.OpenTime.AsTime().Before(start) {
				require.Equal(t, 700.0, m.AggregatedValues[0].Value)
				return
			}
		}
		t.Fatal("missing historical tank total")
	})
	t.Run("empty tank and missing history remain empty", func(t *testing.T) {
		req, ok := proto.Clone(request).(*pb.GetTankHistoryRequest)
		require.True(t, ok)
		req.TankId = emptyTankID
		response, err := service.TankHistory(ctx, org, req, scopes)
		require.NoError(t, err)
		require.Empty(t, response.Metrics)
		req.TankId = tankID
		req.StartTime = timestamppb.New(start.Add(-time.Hour))
		req.EndTime = timestamppb.New(start)
		response, err = service.TankHistory(ctx, org, req, scopes)
		require.NoError(t, err)
		require.Empty(t, response.Metrics)
	})

	t.Run("saved totals survive changes to current module membership", func(t *testing.T) {
		before, err := service.TankHistory(ctx, org, request, scopes)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `UPDATE container_module SET retired_at=now() WHERE tank_id=$1`, tankID)
		require.NoError(t, err)
		after, err := service.TankHistory(ctx, org, request, scopes)
		require.NoError(t, err)
		require.Equal(t, before, after)
	})

	t.Run("rejects invalid and unbounded ranges", func(t *testing.T) {
		for _, change := range []func(*pb.GetTankHistoryRequest){
			func(r *pb.GetTankHistoryRequest) { r.StartTime = nil },
			func(r *pb.GetTankHistoryRequest) { r.EndTime = r.StartTime },
			func(r *pb.GetTankHistoryRequest) { r.GranularitySeconds = 0 },
			func(r *pb.GetTankHistoryRequest) { r.GranularitySeconds = 86401 },
			func(r *pb.GetTankHistoryRequest) { r.GranularitySeconds = 1 },
			func(r *pb.GetTankHistoryRequest) {
				r.EndTime = timestamppb.New(start.Add(367 * 24 * time.Hour))
				r.GranularitySeconds = 86400
			},
			func(r *pb.GetTankHistoryRequest) { r.TankId = 0 },
			func(r *pb.GetTankHistoryRequest) { r.ContainerIdentifier = "" },
		} {
			req, ok := proto.Clone(request).(*pb.GetTankHistoryRequest)
			require.True(t, ok)
			change(req)
			_, err := service.TankHistory(ctx, org, req, scopes)
			require.True(t, fleeterror.IsInvalidArgumentError(err), "got %v", err)
		}
	})
	t.Run("enforces container identity organization and all site scopes", func(t *testing.T) {
		req, ok := proto.Clone(request).(*pb.GetTankHistoryRequest)
		require.True(t, ok)
		req.ContainerIdentifier = "another-container"
		_, err := service.TankHistory(ctx, org, req, scopes)
		require.True(t, fleeterror.IsNotFoundError(err))
		_, err = service.TankHistory(ctx, org+1, request, scopes)
		require.True(t, fleeterror.IsNotFoundError(err))
		for _, denied := range [][]fleetinventory.Scope{nil, {{Sites: []int64{siteID}}}, {{OrgWide: true}, {Sites: []int64{siteID}}}} {
			_, err = service.TankHistory(ctx, org, request, denied)
			require.True(t, fleeterror.IsForbiddenError(err), "got %v", err)
		}
		_, err = db.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE id=$2`, siteID, deviceID)
		require.NoError(t, err)
		response, err := service.TankHistory(ctx, org, request, []fleetinventory.Scope{{Sites: []int64{siteID}}})
		require.NoError(t, err)
		require.Empty(t, response.Metrics, "moving a container cannot transfer its previous site history")
		insert(tankID, 720, 100e12, 2000, 20e-12, 60, 60, 60)
		_, err = db.ExecContext(ctx, `UPDATE container_tank_metrics SET site_id=$1 WHERE tank_id=$2 AND time=$3`, siteID, tankID, start.Add(720*time.Second))
		require.NoError(t, err)
		response, err = service.TankHistory(ctx, org, request, []fleetinventory.Scope{{Sites: []int64{siteID}}})
		require.NoError(t, err)
		require.Len(t, response.Metrics, 4)
		for _, metric := range response.Metrics {
			require.Equal(t, start.Add(720*time.Second), metric.OpenTime.AsTime())
		}
		_, err = service.TankHistory(ctx, org, request, []fleetinventory.Scope{{OrgWide: true, Sites: []int64{siteID}}})
		require.True(t, fleeterror.IsForbiddenError(err))
		// The SQL statement independently enforces the scope even after an earlier scope read.
		rows, err := sqlc.New(db).GetContainerTankHistory(ctx, sqlc.GetContainerTankHistoryParams{TankID: tankID, ContainerIdentifier: identifier, OrgID: org, Authority: []byte(`[{"org_wide":false,"sites":[]}]`), StartTime: start, EndTime: start.Add(time.Hour), GranularitySeconds: 90})
		require.NoError(t, err)
		require.Empty(t, rows)
		_, err = db.ExecContext(ctx, `UPDATE device SET deleted_at=now() WHERE id=$1`, deviceID)
		require.NoError(t, err)
		_, err = service.TankHistory(ctx, org, request, scopes)
		require.True(t, fleeterror.IsNotFoundError(err))
	})
}
