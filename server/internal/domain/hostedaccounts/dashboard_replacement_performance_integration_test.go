package hostedaccounts_test

import (
	"encoding/json"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountRetainedHistoryWorkDoesNotMultiplyByReplacements(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	controller := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
	start := time.Now().UTC().Truncate(24 * time.Hour).Add(-30 * 24 * time.Hour)
	end := start.Add(24 * time.Hour)
	historyEpoch(t, f, start)
	const boards, replacements = 1000, 10
	// Seed real ownership periods for 1,000 boards and ten replacements, five
	// minutes apart. Read cost must depend on source rows, not global boundaries.
	_, err := f.db.ExecContext(t.Context(), `WITH boards AS (
	 INSERT INTO hashboard(org_id,first_seen_at,last_seen_at)
	 SELECT $1,$2,$2 FROM generate_series(1,$3::int+$4::int) RETURNING id
	), numbered AS (SELECT id,row_number() OVER(ORDER BY id) AS number FROM boards)
	INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,device_identifier,site_id,client_id,started_at,ended_at)
	SELECT $1,7,b.id,d.id,d.device_identifier,d.site_id,$5,
	 $2::timestamptz+GREATEST(0,b.number-$3::int)*interval '5 minutes',
	 CASE WHEN b.number<=$4 THEN $2::timestamptz+b.number*interval '5 minutes' END
	FROM numbered b CROSS JOIN device d WHERE d.id=$6`, f.org, start, boards, replacements, client.ID, controller)
	require.NoError(t, err)
	// Unchanged boards have independently timed metrics in four different
	// slices. Replacement periods each have a sample within their own bounds.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at)
	SELECT sample,i.id,metric,CASE metric WHEN 1 THEN 1e12 WHEN 2 THEN 20 WHEN 3 THEN 55 ELSE 3 END,
	 CASE WHEN metric IN (1,2) THEN true END,sample,sample,5,i.asset_id::text,sample,sample
	FROM hosted_account_asset_interval i CROSS JOIN generate_series(1,4) metric
	CROSS JOIN LATERAL (SELECT i.started_at+interval '1 minute'+
	 CASE WHEN i.started_at=$1 AND i.ended_at IS NULL THEN (metric-1)*interval '5 minutes' ELSE interval '0' END AS sample) at
	WHERE i.client_id=$2`, start, client.ID)
	require.NoError(t, err)
	refreshHostedHistory(t, f, start, end)
	q := sqlc.New(f.db)
	periods, err := q.ListHostedAccountHistoryIntervals(t.Context(), sqlc.ListHostedAccountHistoryIntervalsParams{OrganizationID: f.org, AccountID: client.ID, StartTime: start, EndTime: end})
	require.NoError(t, err)
	intervals := make([]map[string]any, 0, len(periods))
	ids := make([]int64, 0, len(periods))
	for _, period := range periods {
		var ended any
		if period.EndedAt.Valid {
			ended = period.EndedAt.Time
		}
		intervals = append(intervals, map[string]any{"interval_id": period.ID, "kind": period.Kind, "asset_id": period.AssetID, "device_identifier": period.DeviceIdentifier, "site_id": period.SiteID, "started_at": period.StartedAt, "ended_at": ended})
		ids = append(ids, period.ID)
	}
	encoded, err := json.Marshal(intervals)
	require.NoError(t, err)
	for _, hourly := range []bool{true, false} {
		name := "daily"
		if hourly {
			name = "hourly"
		}
		t.Run(name, func(t *testing.T) {
			buckets := []time.Time{start}
			for index := 1; index <= replacements; index++ {
				buckets = append(buckets, start.Add(time.Duration(index)*5*time.Minute))
			}
			if hourly {
				for at := start.Add(time.Hour); at.Before(end); at = at.Add(time.Hour) {
					buckets = append(buckets, at)
				}
			}
			capture := &miningProjectionDB{DB: f.db}
			rows, err := sqlc.New(capture).ListHostedAccountRetainedReadings(t.Context(), sqlc.ListHostedAccountRetainedReadingsParams{
				Intervals: encoded, IntervalIds: ids, StartTime: start, EndTime: end, AvailableFrom: start,
				Hourly: hourly, BucketStarts: buckets, HostAuthority: json.RawMessage("[]"), PolicySites: []int64{},
			})
			require.NoError(t, err)
			require.Len(t, rows, replacements+1)
			var hash, power, temperature, state int64
			for _, row := range rows {
				hash += row.HashrateReportingCount
				power += row.PowerReportingCount
				temperature += row.TemperatureReportingCount
				state += int64(row.StatusHashingCount)
			}
			for _, count := range []int64{hash, power, temperature, state} {
				require.EqualValues(t, boards+replacements, count, "each metric contributes only in its own slice")
			}
			var plans []struct {
				Plan          miningProjectionPlan
				ExecutionTime float64 `json:"Execution Time"`
			}
			require.NoError(t, json.Unmarshal(capture.plan, &plans))
			require.Len(t, plans, 1)
			var checkRows func(miningProjectionPlan)
			checkRows = func(plan miningProjectionPlan) {
				processed := (plan.ActualRows + plan.RowsRemovedByFilter) * plan.ActualLoops
				require.LessOrEqual(t, processed, float64(4*(boards+replacements)), "%s must not expand source rows across unrelated replacement boundaries", plan.NodeType)
				for _, child := range plan.Plans {
					checkRows(child)
				}
			}
			checkRows(plans[0].Plan)
			require.Less(t, plans[0].ExecutionTime, 5000.0, "retained query must fit the mining transaction deadline")
			t.Logf("%d boards, %d staggered replacements, %d returned rows, execution %.2fms", boards, replacements, len(rows), plans[0].ExecutionTime)
		})
	}
}
