package hostedaccounts_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountHistoryRawRetainedParity(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	start := time.Now().UTC().Truncate(24 * time.Hour).Add(-2 * 24 * time.Hour)
	end := start.Add(24 * time.Hour)
	historyEpoch(t, f, start)
	var firstSite, secondSite int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'First','first') RETURNING id`, f.org).Scan(&firstSite))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Second','second') RETURNING id`, f.org).Scan(&secondSite))
	for index, temp := range []float64{-5, 0, 69, 70, 89, 90, 55} {
		miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		site := firstSite
		if index%2 == 1 {
			site = secondSite
		}
		if index == 6 {
			site = 0
		}
		_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=NULLIF($1,0) WHERE id=$2`, site, miner)
		require.NoError(t, err)
		require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
		_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, start, miner)
		require.NoError(t, err)
		for sample, offset := range []time.Duration{5 * time.Minute, 35 * time.Minute} {
			hash := 100e12
			if index == 1 || index == 0 && sample == 0 {
				hash = 0
			}
			var power, temperature any = 2000.0, temp
			if index == 6 {
				power, temperature = nil, nil
			}
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,site_id,hash_rate_hs,power_w,temp_c)
 SELECT $1,device_identifier,site_id,$2,$3,$4 FROM device WHERE id=$5`, start.Add(offset), hash, power, temperature, miner)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO miner_state_snapshots(time,org_id,device_identifier,site_id,state)
 SELECT $1,org_id,device_identifier,site_id,$2 FROM device WHERE id=$3`, start.Add(offset), index%4, miner)
			require.NoError(t, err)
		}
	}
	for _, complete := range []bool{true, false} {
		module, controller := f.miningModule(t)
		_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, secondSite, controller)
		require.NoError(t, err)
		require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
		_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=6 AND asset_id=$2`, start, module)
		require.NoError(t, err)
		for _, offset := range []time.Duration{10 * time.Minute, 40 * time.Minute} {
			at := start.Add(offset)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,site_id,hashrate_hs,power_w,temperature_c,coverage)
 VALUES($1,$2,$2,$3,200e12,4000,55,jsonb_build_object('hashrate',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',1,'expected',1,'topology_complete',$4::boolean),
 'power',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',1,'expected',1,'topology_complete',$4::boolean),
 'temperature',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz)))`, module, at, secondSite, complete)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,site_id,state,state_observed_at) VALUES($1,$2,$3,'maintenance',$2)`, module, at, secondSite)
			require.NoError(t, err)
		}
	}
	q := sqlc.New(f.db)
	periods, err := q.ListHostedAccountHistoryIntervals(t.Context(), sqlc.ListHostedAccountHistoryIntervalsParams{OrganizationID: f.org, AccountID: client.ID, StartTime: start, EndTime: end})
	require.NoError(t, err)
	intervals := []map[string]any{}
	intervalIDs := []int64{}
	for _, period := range periods {
		intervals = append(intervals, map[string]any{"interval_id": period.ID, "kind": period.Kind, "asset_id": period.AssetID, "device_identifier": period.DeviceIdentifier, "site_id": period.SiteID, "started_at": period.StartedAt})
		intervalIDs = append(intervalIDs, period.ID)
	}
	encoded, err := json.Marshal(intervals)
	require.NoError(t, err)
	refreshHostedHistory(t, f, start, end)
	for _, hourly := range []bool{true, false} {
		bucket := int32(24 * time.Hour / time.Second)
		if hourly {
			bucket = int32(time.Hour / time.Second)
		}
		for _, scope := range []struct {
			name       string
			site       int64
			restricted bool
			sites      []int64
			authority  string
			expected   int64
		}{
			{name: "all", authority: "[]", expected: 9},
			{name: "site filter", site: firstSite, authority: "[]", expected: 3},
			{name: "policy permits", restricted: true, sites: []int64{firstSite}, authority: "[]", expected: 3},
			{name: "policy denies", restricted: true, sites: []int64{}, authority: "[]"},
			{name: "host permits", authority: fmt.Sprintf(`[{"org_wide":false,"sites":[%d]}]`, secondSite), expected: 5},
			{name: "host excludes", authority: fmt.Sprintf(`[{"org_wide":true,"sites":[%d]}]`, firstSite), expected: 6},
			{name: "filter and policy intersect", site: firstSite, restricted: true, sites: []int64{secondSite}, authority: "[]"},
			{name: "host empty scope denies", authority: `[{"org_wide":false,"sites":[]}]`},
			{name: "host org wide includes unassigned", authority: `[{"org_wide":true,"sites":[]}]`, expected: 9},
			{name: "host intersection denies", authority: fmt.Sprintf(`[{"org_wide":false,"sites":[%d]},{"org_wide":false,"sites":[%d]}]`, firstSite, secondSite)},
		} {
			t.Run(fmt.Sprintf("hourly=%t/%s", hourly, scope.name), func(t *testing.T) {
				raw, err := q.ListHostedAccountHistoricalReadings(t.Context(), sqlc.ListHostedAccountHistoricalReadingsParams{OrganizationID: f.org, Intervals: encoded, StartTime: start, EndTime: end, BucketSeconds: bucket, SiteFilter: scope.site, PolicyRestricted: scope.restricted, PolicySites: scope.sites, HostAuthority: json.RawMessage(scope.authority)})
				require.NoError(t, err)
				var reporting int64
				for _, row := range raw {
					reporting += row.HashrateReportingCount
				}
				require.Equal(t, scope.expected, reporting, "permission expectations are independent of query parity")
				if scope.name == "all" {
					require.Len(t, raw, 1)
					require.Positive(t, raw[0].TemperatureColdCount)
					require.Positive(t, raw[0].TemperatureOkCount)
					require.Positive(t, raw[0].TemperatureHotCount)
					require.Positive(t, raw[0].TemperatureCriticalCount)
					require.Positive(t, raw[0].StatusHashingCount)
					require.Positive(t, raw[0].StatusBrokenCount)
					require.Positive(t, raw[0].StatusNotHashingCount)
					require.Positive(t, raw[0].OfflineCount)
					require.EqualValues(t, 1, raw[0].PartialModuleCount)
					require.False(t, raw[0].Complete)
				}
				for _, direct := range []bool{true, false} {
					retained, err := q.ListHostedAccountRetainedReadings(t.Context(), sqlc.ListHostedAccountRetainedReadingsParams{AvailableFrom: start, Intervals: encoded, IntervalIds: intervalIDs, StartTime: start, EndTime: end, Hourly: hourly, DirectBuckets: direct, BucketSeconds: bucket, SiteFilter: scope.site, PolicyRestricted: scope.restricted, PolicySites: scope.sites, HostAuthority: json.RawMessage(scope.authority)})
					require.NoError(t, err)
					require.Len(t, retained, len(raw))
					for index, row := range retained {
						require.Equal(t, raw[index], sqlc.ListHostedAccountHistoricalReadingsRow(row), "all fields must match, direct=%t", direct)
					}
				}
			})
		}
	}
}
