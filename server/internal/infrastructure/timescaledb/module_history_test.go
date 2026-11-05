package timescaledb

import (
	"fmt"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestModuleHistoryRawValuesAndAuthority(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	var org, module, siteA, siteB int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT d.org_id,m.id FROM device d JOIN container_tank t ON t.device_id=d.id JOIN container_module m ON m.tank_id=t.id WHERE d.id=$1 AND t.position=1`, r.databaseID).Scan(&org, &module))
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'A','raw-a') RETURNING id`, org).Scan(&siteA))
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'B','raw-b') RETURNING id`, org).Scan(&siteB))
	_, err := r.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, siteB, r.databaseID)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `DELETE FROM container_module_metrics WHERE module_id=$1`, module)
	require.NoError(t, err)
	// Legacy samples can lack topology and coverage. Keep null metrics distinct
	// from zero, and include equal observation times with different collection times.
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,site_id,hashrate_hs,power_w,temperature_c,efficiency_jh,coverage) VALUES
	 ($1,$2::timestamptz-INTERVAL '1 second',$2,$3,999,999,999,999,NULL),
	 ($1,$2,$2::timestamptz+INTERVAL '1 second',$3,100,NULL,50,NULL,'{"hashrate":{"reporting":1,"expected":2,"topology_complete":false}}'),
	 ($1,$2,$2::timestamptz+INTERVAL '2 seconds',$4,NULL,NULL,NULL,NULL,NULL),
	 ($1,$2::timestamptz+INTERVAL '1 second',$2::timestamptz+INTERVAL '3 seconds',NULL,0,0,0,0,'{}'),
	 ($1,$2::timestamptz+INTERVAL '2 seconds',$2::timestamptz+INTERVAL '4 seconds',$4,999,999,999,999,NULL)`, module, r.base, siteA, siteB)
	require.NoError(t, err)
	q := sqlc.New(r.db)
	p := sqlc.ListContainerModuleHistoryParams{ModuleID: module, OrgID: org, StartTime: r.base, EndTime: r.base.Add(2 * time.Second), PageLimit: 1001, Authority: []byte(`[{"org_wide":true}]`)}
	all, err := q.ListContainerModuleHistory(t.Context(), p)
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.True(t, all[0].Time.Equal(r.base))
	require.True(t, all[0].CollectedAt.Equal(r.base.Add(time.Second)))
	require.Equal(t, 100.0, all[0].HashrateHs.Float64)
	require.True(t, all[0].HashrateHs.Valid)
	require.False(t, all[0].PowerW.Valid)
	require.Equal(t, 50.0, all[0].TemperatureC.Float64)
	require.False(t, all[0].EfficiencyJh.Valid)
	require.JSONEq(t, `{"hashrate":{"reporting":1,"expected":2,"topology_complete":false}}`, string(all[0].Coverage.RawMessage))
	require.False(t, all[1].HashrateHs.Valid)
	require.False(t, all[1].PowerW.Valid)
	require.False(t, all[1].TemperatureC.Valid)
	require.False(t, all[1].EfficiencyJh.Valid)
	require.False(t, all[1].Coverage.Valid)
	for _, value := range []struct {
		valid bool
		value float64
	}{{all[2].HashrateHs.Valid, all[2].HashrateHs.Float64}, {all[2].PowerW.Valid, all[2].PowerW.Float64}, {all[2].TemperatureC.Valid, all[2].TemperatureC.Float64}, {all[2].EfficiencyJh.Valid, all[2].EfficiencyJh.Float64}} {
		require.True(t, value.valid)
		require.Zero(t, value.value)
	}
	for _, tc := range []struct {
		name, authority string
		indices         []int
	}{
		{"recorded site", fmt.Sprintf(`[{"sites":[%d]}]`, siteA), []int{0}},
		{"current site", fmt.Sprintf(`[{"sites":[%d]}]`, siteB), []int{1}},
		{"excluded site", fmt.Sprintf(`[{"org_wide":true,"sites":[%d]}]`, siteA), []int{1, 2}},
		{"intersected scopes", fmt.Sprintf(`[{"org_wide":true,"sites":[%d]},{"sites":[%d]}]`, siteB, siteA), []int{0}},
		{"disjoint scopes", fmt.Sprintf(`[{"sites":[%d]},{"sites":[%d]}]`, siteA, siteB), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.Authority = []byte(tc.authority)
			rows, err := q.ListContainerModuleHistory(t.Context(), p)
			require.NoError(t, err)
			require.Len(t, rows, len(tc.indices))
			for i, index := range tc.indices {
				require.Equal(t, all[index], rows[i])
			}
		})
	}
	p.Authority = []byte(`[{"org_wide":true}]`)
	p.PageLimit = 1
	for _, expected := range all {
		rows, err := q.ListContainerModuleHistory(t.Context(), p)
		require.NoError(t, err)
		require.Equal(t, []sqlc.ListContainerModuleHistoryRow{expected}, rows)
		p.HasCursor, p.AfterTime, p.AfterCollectedAt = true, expected.Time, expected.CollectedAt
	}
	rows, err := q.ListContainerModuleHistory(t.Context(), p)
	require.NoError(t, err)
	require.Empty(t, rows)
	p.HasCursor, p.OrgID = false, org+999
	rows, err = q.ListContainerModuleHistory(t.Context(), p)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestModuleHistoryRawPageLimit(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	var org, module int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT d.org_id,m.id FROM device d JOIN container_tank t ON t.device_id=d.id JOIN container_module m ON m.tank_id=t.id WHERE d.id=$1 AND t.position=1`, r.databaseID).Scan(&org, &module))
	_, err := r.db.ExecContext(t.Context(), `DELETE FROM container_module_metrics WHERE module_id=$1`, module)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs)
	 SELECT $1,$2,$2::timestamptz+n*INTERVAL '1 microsecond',n FROM generate_series(1,1002) n`, module, r.base)
	require.NoError(t, err)
	q := sqlc.New(r.db)
	rows, err := q.ListContainerModuleHistory(t.Context(), sqlc.ListContainerModuleHistoryParams{ModuleID: module, OrgID: org, StartTime: r.base, EndTime: r.base.Add(24 * time.Hour), PageLimit: 1001, Authority: []byte(`[{"org_wide":true}]`)})
	require.NoError(t, err)
	require.Len(t, rows, 1001)
	require.Equal(t, 1001.0, rows[1000].HashrateHs.Float64)
	reader := fleetinventory.NewService(q)
	request := &pb.ListModuleHistoryRequest{ModuleId: module, StartTime: timestamppb.New(r.base), EndTime: timestamppb.New(r.base.Add(24 * time.Hour)), PageSize: 1000}
	first, err := reader.History(t.Context(), org, request, []fleetinventory.Scope{{OrgWide: true}})
	require.NoError(t, err)
	require.Len(t, first.Samples, 1000)
	require.NotEmpty(t, first.NextPageToken)
	request.PageToken = first.NextPageToken
	last, err := reader.History(t.Context(), org, request, []fleetinventory.Scope{{OrgWide: true}})
	require.NoError(t, err)
	require.Len(t, last.Samples, 2)
	require.Empty(t, last.NextPageToken)
	require.Equal(t, 1001.0, *last.Samples[0].HashrateHs)
	require.Equal(t, 1002.0, *last.Samples[1].HashrateHs)
}

func TestModuleHistoryRawScopedServicePagination(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	var org, module, excluded, readable int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT d.org_id,m.id FROM device d JOIN container_tank t ON t.device_id=d.id JOIN container_module m ON m.tank_id=t.id WHERE d.id=$1 AND t.position=1`, r.databaseID).Scan(&org, &module))
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Excluded','excluded') RETURNING id`, org).Scan(&excluded))
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Readable','readable') RETURNING id`, org).Scan(&readable))
	_, err := r.db.ExecContext(t.Context(), `DELETE FROM container_module_metrics WHERE module_id=$1`, module)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,site_id,hashrate_hs)
 SELECT $1,$2,$2::timestamptz+n*INTERVAL '1 microsecond',CASE WHEN n%2=0 THEN $3::bigint ELSE $4::bigint END,n FROM generate_series(1,5) n`, module, r.base, readable, excluded)
	require.NoError(t, err)
	reader := fleetinventory.NewService(sqlc.New(r.db))
	request := &pb.ListModuleHistoryRequest{ModuleId: module, StartTime: timestamppb.New(r.base), EndTime: timestamppb.New(r.base.Add(time.Hour)), PageSize: 1}
	scopes := []fleetinventory.Scope{{Sites: []int64{readable}}}
	first, err := reader.History(t.Context(), org, request, scopes)
	require.NoError(t, err)
	require.Len(t, first.Samples, 1)
	require.Equal(t, 2.0, *first.Samples[0].HashrateHs)
	require.NotEmpty(t, first.NextPageToken)
	request.PageToken = first.NextPageToken
	last, err := reader.History(t.Context(), org, request, scopes)
	require.NoError(t, err)
	require.Len(t, last.Samples, 1)
	require.Equal(t, 4.0, *last.Samples[0].HashrateHs)
	require.Empty(t, last.NextPageToken)
}
