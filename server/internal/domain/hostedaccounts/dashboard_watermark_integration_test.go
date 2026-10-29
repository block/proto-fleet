package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccountDashboardRetainedBudgetsActualWatermarks(t *testing.T) {
	for _, daily := range []bool{false, true} {
		name := "hourly"
		if daily {
			name = "daily"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			end := time.Now().UTC().Truncate(24 * time.Hour)
			start := end.Add(-7 * 24 * time.Hour)
			if daily {
				start = end.Add(-365 * 24 * time.Hour)
			}
			const assets = 140
			selections := make([]*pb.MiningAssetSelection, 0, assets)
			for range assets {
				selections = append(selections, selected(5, f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG), 0))
			}
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selections...))
			historyEpoch(t, f, start)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
			require.NoError(t, err)
			q := sqlc.New(f.db)
			watermarks, err := q.GetHostedAccountHistoryWatermarks(t.Context(), start)
			require.NoError(t, err)
			require.True(t, watermarks.Hourly.Equal(start), "uninitialized hourly watermark charges all available history")
			require.True(t, watermarks.Daily.Equal(start), "uninitialized daily watermark charges all available history")
			req := dashboardRequest(client.AccountID, start, end, nil)
			req.BucketSeconds = int32(time.Hour / time.Second)
			if daily {
				req.BucketSeconds = int32(24 * time.Hour / time.Second)
			}
			_, err = f.svc.Dashboard(t.Context(), f.user, f.org, req)
			require.True(t, fleeterror.IsUnavailableError(err), "uninitialized retained history must enforce its actual work budget: %v", err)
			require.ErrorContains(t, err, "refresh is behind")

			watermark := end.Add(-4 * 24 * time.Hour)
			if daily {
				watermark = end
			}
			insert := func(at time.Time) {
				t.Helper()
				_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w)
 SELECT $1,device_identifier,100e12,2000 FROM device WHERE id=$2`, at, selections[0].Id)
				require.NoError(t, err)
			}
			insert(watermark.Add(-30 * time.Minute))
			_, err = f.db.ExecContext(t.Context(), `CALL refresh_continuous_aggregate('hosted_account_history_hourly',$1::timestamptz,$2::timestamptz)`, start, watermark)
			require.NoError(t, err)
			watermarks, err = q.GetHostedAccountHistoryWatermarks(t.Context(), start)
			require.NoError(t, err)
			require.True(t, watermarks.Hourly.Equal(watermark))
			require.True(t, watermarks.Daily.Equal(start), "hourly materialization must not imply daily materialization")
			_, err = f.svc.Dashboard(t.Context(), f.user, f.org, req)
			require.True(t, fleeterror.IsUnavailableError(err), "lagging %s must reject oversized work before the retained scan: %v", name, err)
			require.ErrorContains(t, err, "refresh is behind")

			if !daily {
				// Even a caught-up policy must scan the future part of a request.
				// Its source buckets fit, but the normal raw-tail budget does not.
				oversized := dashboardRequest(client.AccountID, start, end.Add(3*24*time.Hour), nil)
				oversized.BucketSeconds = int32(time.Hour / time.Second)
				_, err = f.svc.Dashboard(t.Context(), f.user, f.org, oversized)
				require.True(t, fleeterror.IsInvalidArgumentError(err), "inherently oversized work must not be reported as refresh lag: %v", err)

				historical := dashboardRequest(client.AccountID, start, start.Add(2*24*time.Hour), nil)
				historical.BucketSeconds = int32(time.Hour / time.Second)
				_, err = f.svc.Dashboard(t.Context(), f.user, f.org, historical)
				require.NoError(t, err, "history below the watermark has no raw tail")
				_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, end.Add(-time.Hour), client.ID)
				require.NoError(t, err)
				_, err = f.svc.Dashboard(t.Context(), f.user, f.org, req)
				require.NoError(t, err, "recent assignments only pay for their overlap with the lagging tail")
				_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
				require.NoError(t, err)
				insert(end.Add(-30 * time.Minute))
			}
			if daily {
				// A daily policy does not materialize the last source day.
				insert(end.Add(-48*time.Hour + 30*time.Minute))
				_, err = f.db.ExecContext(t.Context(), `CALL refresh_continuous_aggregate('hosted_account_history_hourly',$1::timestamptz,$2::timestamptz)`, start, end)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `CALL refresh_continuous_aggregate('hosted_account_history_daily',$1::timestamptz,$2::timestamptz)`, start, end.Add(-24*time.Hour))
				require.NoError(t, err)
				watermarks, err = q.GetHostedAccountHistoryWatermarks(t.Context(), start)
				require.NoError(t, err)
				require.True(t, watermarks.Daily.Equal(end.Add(-24*time.Hour)))
			} else {
				refreshHostedHistory(t, f, start, end)
			}
			result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
			require.NoError(t, err, "the same request succeeds after refresh catches up")
			require.Equal(t, 100e12, result.Readings[len(result.Readings)-1].Summary.GetHashrateHs())
		})
	}
}

func TestAccountDashboardRetainedBudgetsWholeEndBucket(t *testing.T) {
	for _, daily := range []bool{false, true} {
		name := "hourly"
		if daily {
			name = "daily"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			boundary := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
			start := boundary.Add(-7 * 24 * time.Hour)
			watermark := boundary.Add(-23 * time.Hour)
			bucket := int32(time.Hour / time.Second)
			if daily {
				start = boundary.Add(-30 * 24 * time.Hour)
				watermark = boundary
				bucket = int32(24 * time.Hour / time.Second)
			}
			selections := make([]*pb.MiningAssetSelection, 301)
			for i := range selections {
				selections[i] = selected(5, f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG), 0)
			}
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selections...))
			historyEpoch(t, f, start)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w)
 SELECT $1,device_identifier,100e12,2000 FROM device WHERE id=$2`, watermark.Add(-30*time.Minute), selections[0].Id)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `CALL refresh_continuous_aggregate('hosted_account_history_hourly',$1::timestamptz,$2::timestamptz)`, start, watermark)
			require.NoError(t, err)
			if daily {
				_, err = f.db.ExecContext(t.Context(), `CALL refresh_continuous_aggregate('hosted_account_history_daily',$1::timestamptz,$2::timestamptz)`, start, watermark)
				require.NoError(t, err)
			}
			request := dashboardRequest(client.AccountID, start, boundary, nil)
			request.BucketSeconds = bucket
			_, err = f.svc.Dashboard(t.Context(), f.user, f.org, request)
			require.NoError(t, err, "an aligned end excludes the following source bucket")
			request.EndTime = timestamppb.New(boundary.Add(time.Second))
			_, err = f.svc.Dashboard(t.Context(), f.user, f.org, request)
			require.True(t, fleeterror.IsUnavailableError(err), "the additional source bucket takes 301 allocations above the 300 asset-day tail budget: %v", err)
		})
	}
}
