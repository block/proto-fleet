package hostedaccounts_test

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedHistoryCaptureMalformedCoverageDoesNotRejectTelemetry(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
	historyEpoch(t, f, day)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=6 AND asset_id=$2`, day, module)
	require.NoError(t, err)
	for index, tc := range []struct {
		name, field string
		value       any
	}{
		{"missing source", "newest_at", nil},
		{"invalid timestamp", "newest_at", "not-a-timestamp"},
		{"invalid calendar date", "oldest_at", "2026-02-30T00:00:00Z"},
		{"nonfinite timestamp", "newest_at", "infinity"},
		{"invalid boolean", "topology_complete", "perhaps"},
		{"invalid count", "reporting", []int{1}},
		{"count overflow", "expected", int64(1) << 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := day.Add(time.Hour + time.Duration(index)*time.Minute)
			coverage := map[string]any{"oldest_at": at, "newest_at": at, "reporting": 1, "expected": 1, "topology_complete": true}
			coverage[tc.field] = tc.value
			encoded, err := json.Marshal(coverage)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage)
    VALUES($1,$2,$2,100e12,2000,55,jsonb_build_object('hashrate',$3::jsonb,
    'power',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',1,'expected',1,'topology_complete',true),
    'temperature',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz)))`, module, at, encoded)
			require.NoError(t, err, "invalid capture evidence must not reject the raw measurement")
			var raw, hash, power, temperature int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_module_metrics WHERE module_id=$1 AND collected_at=$2`, module, at).Scan(&raw))
			require.Equal(t, 1, raw)
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(value) FILTER(WHERE metric=1),count(value) FILTER(WHERE metric=2),count(value) FILTER(WHERE metric=3) FROM hosted_account_history_sample WHERE origin_key=$1 AND origin_collected_at=$2`, strconv.FormatInt(module, 10), at).Scan(&hash, &power, &temperature))
			require.Zero(t, hash)
			require.Equal(t, 1, power, "another metric's valid source stays usable")
			require.Equal(t, 1, temperature)
			// UPDATE and DELETE must also tolerate the old malformed evidence.
			_, err = f.db.ExecContext(t.Context(), `UPDATE container_module_metrics SET hashrate_hs=200e12 WHERE module_id=$1 AND collected_at=$2`, module, at)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `DELETE FROM container_module_metrics WHERE module_id=$1 AND collected_at=$2`, module, at)
			require.NoError(t, err)
			var remaining int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample WHERE origin_key=$1 AND origin_collected_at=$2`, strconv.FormatInt(module, 10), at).Scan(&remaining))
			require.Zero(t, remaining)
		})
	}
}

func TestHostedHistoryCaptureNonfiniteMinerValues(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
	historyEpoch(t, f, day)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, day, miner)
	require.NoError(t, err)
	for index, value := range []string{"NaN", "Infinity", "-Infinity"} {
		at := day.Add(time.Hour + time.Duration(index)*time.Minute)
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c) SELECT $1,device_identifier,$2::float8,$2::float8,$2::float8 FROM device WHERE id=$3`, at, value, miner)
		require.NoError(t, err)
		var available, complete int
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(value),count(*) FILTER(WHERE complete) FROM hosted_account_history_sample WHERE origin_time=$1`, at).Scan(&available, &complete))
		require.Zero(t, available)
		require.Zero(t, complete)
	}
}

func TestHostedHistoryCaptureDatabaseErrorsStillRejectTelemetry(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	_, err := f.db.ExecContext(t.Context(), `ALTER TABLE hosted_account_history_sample ADD CONSTRAINT capture_test_failure CHECK(value IS NULL) NOT VALID`)
	require.NoError(t, err)
	at := time.Now().UTC()
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, at, miner)
	require.ErrorContains(t, err, "capture_test_failure")
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM device_metrics WHERE time=$1`, at).Scan(&count))
	require.Zero(t, count, "an unexpected database failure must remain visible and atomic")
}

func TestHostedHistoryCustomPlanningDoesNotEscapeDashboardTransaction(t *testing.T) {
	f := setup(t)
	f.db.SetMaxOpenConns(1)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-120 * 24 * time.Hour)
	historyEpoch(t, f, day)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, day, miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, day.Add(time.Minute), miner)
	require.NoError(t, err)
	refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
	_, err = f.db.ExecContext(t.Context(), `SET plan_cache_mode=force_generic_plan`)
	require.NoError(t, err)
	for range 6 {
		req := dashboardRequest(client.AccountID, day, day.Add(24*time.Hour), nil)
		req.BucketSeconds = 86400
		result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
		require.NoError(t, err)
		require.Equal(t, 100e12, result.Readings[0].Summary.GetHashrateHs())
	}
	var generic, custom int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COALESCE(sum(generic_plans),0),COALESCE(sum(custom_plans),0) FROM pg_prepared_statements WHERE statement LIKE '-- name: ListHostedAccountRetainedReadings :many%'`).Scan(&generic, &custom))
	require.Zero(t, generic, "the retained read must avoid the measured generic-plan regression")
	require.GreaterOrEqual(t, custom, 6)
	var mode string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SHOW plan_cache_mode`).Scan(&mode))
	require.Equal(t, "force_generic_plan", mode, "dashboard planning must not change the pooled session setting")
}
