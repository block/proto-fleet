package hostedaccounts_test

import (
	"encoding/json"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestAccountHistoryQueryReturnsOnlyChartBuckets(t *testing.T) {
	f := setup(t)
	start := time.Now().UTC().Truncate(time.Second).Add(-7 * 24 * time.Hour)
	end := start.Add(7 * 24 * time.Hour)
	intervals := make([]map[string]any, 0, 100)
	for range 100 {
		id := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		var identifier string
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT device_identifier FROM device WHERE id=$1`, id).Scan(&identifier))
		intervals = append(intervals, map[string]any{"kind": 5, "asset_id": id, "device_identifier": identifier, "site_id": 0, "started_at": start})
	}
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c) SELECT sample,d.device_identifier,1e12,20,55 FROM device d CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-interval '15 minutes',interval '15 minutes') sample WHERE d.org_id=$3`, start, end, f.org)
	require.NoError(t, err)
	encoded, err := json.Marshal(intervals)
	require.NoError(t, err)
	capture := &miningProjectionDB{DB: f.db}
	rows, err := sqlc.New(capture).ListHostedAccountHistoricalReadings(t.Context(), sqlc.ListHostedAccountHistoricalReadingsParams{StartTime: start, EndTime: end, BucketSeconds: 900, Intervals: encoded, HostAuthority: json.RawMessage("[]"), PolicySites: []int64{}})
	require.NoError(t, err)
	require.Len(t, rows, 672, "database result must be bounded by chart buckets, independent of asset count")
	for _, row := range rows {
		require.EqualValues(t, 100, row.HashrateReportingCount)
		require.EqualValues(t, 100, row.PairedReportingCount)
		require.Equal(t, 100e12, row.HashrateHs)
	}
	var plans []struct {
		Plan          miningProjectionPlan
		ExecutionTime float64 `json:"Execution Time"`
	}
	require.NoError(t, json.Unmarshal(capture.plan, &plans))
	require.Len(t, plans, 1)
	require.EqualValues(t, 672, plans[0].Plan.ActualRows)
	t.Logf("source-authorized history: 100 assets, 67,200 samples over 7 days, 672 returned rows, database execution %.2fms", plans[0].ExecutionTime)
}
