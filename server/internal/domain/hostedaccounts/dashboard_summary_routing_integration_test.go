package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountDashboardRetainedFallbackPreservesSampleWeights(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		age, span, boundary, later time.Duration
		bucketSeconds              int32
		reassign                   bool
	}{
		{"hourly reassignment", 20 * 24 * time.Hour, time.Hour, 30 * time.Minute, 35 * time.Minute, int32(time.Hour / time.Second), true},
		{"daily reassignment", 120 * 24 * time.Hour, 24 * time.Hour, 12 * time.Hour, 13 * time.Hour, int32(24 * time.Hour / time.Second), true},
		{"two source hours", 20 * 24 * time.Hour, 2 * time.Hour, 0, 65 * time.Minute, int32(2 * time.Hour / time.Second), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			assign := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, assign, selected(5, miner, 0)))
			day := time.Now().UTC().Truncate(24 * time.Hour).Add(-tc.age)
			historyEpoch(t, f, day)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, day, miner)
			require.NoError(t, err)
			if tc.reassign {
				require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
				_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET ended_at=$1 WHERE kind=5 AND asset_id=$2`, day.Add(tc.boundary), miner)
				require.NoError(t, err)
				require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, assign, selected(5, miner, 2)))
				_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2 AND ended_at IS NULL`, day.Add(tc.boundary), miner)
				require.NoError(t, err)
			}
			for index, offset := range []time.Duration{5 * time.Minute, 10 * time.Minute, 15 * time.Minute, tc.later} {
				hash, power := 100e12, 2000.0
				if index == 3 {
					hash, power = 900e12, 18000
				}
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,$2,$3 FROM device WHERE id=$4`, day.Add(offset), hash, power, miner)
				require.NoError(t, err)
			}
			refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
			_, err = f.db.ExecContext(t.Context(), `SELECT drop_chunks('hosted_account_history_sample',older_than=>$1::timestamptz)`, day.Add(24*time.Hour))
			require.NoError(t, err)
			req := dashboardRequest(client.AccountID, day, day.Add(tc.span), nil)
			req.BucketSeconds = tc.bucketSeconds
			result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
			require.NoError(t, err)
			require.Len(t, result.Readings, 1)
			summary := result.Readings[0].Summary
			require.Equal(t, 300e12, summary.GetHashrateHs(), "combine sample sums/counts across intervals or source periods before summing assets")
			require.Equal(t, 6000.0, summary.GetPowerW())
			require.Equal(t, 20.0, summary.GetEfficiencyJTh())
			require.EqualValues(t, 1, summary.AssetCount, "reassignment must not count the same asset twice")
		})
	}
}
