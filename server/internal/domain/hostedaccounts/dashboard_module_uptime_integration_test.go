package hostedaccounts_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountLegacyModuleUptimeUsesAuthorizedHistoricalState(t *testing.T) {
	for _, tc := range []struct {
		name, state                 string
		sourceOffset, collectOffset time.Duration
		missingTime, wrongSite      bool
		reattributed                bool
		hashing, broken, notHashing int32
	}{
		{name: "active", state: "active", hashing: 1},
		{name: "error despite positive hashrate", state: "error", broken: 1},
		{name: "pool required", state: "needs_mining_pool", broken: 1},
		{name: "updating", state: "updating", broken: 1},
		{name: "reboot required", state: "reboot_required", broken: 1},
		{name: "offline despite positive hashrate", state: "offline", notHashing: 1},
		{name: "inactive", state: "inactive", notHashing: 1},
		{name: "maintenance", state: "maintenance", notHashing: 1},
		{name: "unknown", state: "unknown"},
		{name: "missing observation"},
		{name: "missing source time", state: "active", missingTime: true},
		{name: "stale state", state: "active", sourceOffset: -3 * time.Minute},
		{name: "freshness boundary is exclusive", state: "active", sourceOffset: -2 * time.Minute},
		{name: "allowed clock skew", state: "active", sourceOffset: 5 * time.Second, hashing: 1},
		{name: "future state", state: "active", sourceOffset: 6 * time.Second},
		{name: "previous owner", state: "active", sourceOffset: -10*time.Minute - time.Second, collectOffset: -10*time.Minute + time.Second},
		{name: "allocation end is exclusive", state: "active", sourceOffset: 20 * time.Minute, collectOffset: 20 * time.Minute},
		{name: "other site", state: "active", wrongSite: true},
		{name: "previous attribution", state: "active", collectOffset: 2 * time.Second, reattributed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			module, _ := f.miningModule(t)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
			end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
			start := end.Add(-time.Hour)
			at := start.Add(10 * time.Minute)
			f.legacyModuleHistory(t)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE client_id=$3`, start, start.Add(30*time.Minute), client.ID)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,coverage) VALUES($1,$2,$2,100e12,jsonb_build_object('hashrate',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz)))`, module, at)
			require.NoError(t, err)
			if tc.state != "" {
				var site any
				if tc.wrongSite {
					var id int64
					require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Other site','other-site') RETURNING id`, f.org).Scan(&id))
					site = id
				}
				var attribution any
				if tc.reattributed {
					attribution = at.Add(time.Second)
				}
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,site_id,state,state_observed_at,attribution_since) VALUES($1,$2,$3,$4,CASE WHEN $6 THEN NULL ELSE $5::timestamptz END,$7)`, module, at.Add(tc.collectOffset), site, tc.state, at.Add(tc.sourceOffset), tc.missingTime, attribution)
				require.NoError(t, err)
			}
			request := dashboardRequest(client.AccountID, start, end, nil)
			request.BucketSeconds = 1800
			result, err := f.svc.Dashboard(t.Context(), f.user, f.org, request)
			require.NoError(t, err)
			require.Len(t, result.Readings, 2)
			uptime := result.Readings[0].UptimeStatus
			require.Equal(t, tc.hashing, uptime.HashingCount)
			require.Equal(t, tc.broken, uptime.BrokenCount)
			require.Equal(t, tc.notHashing, uptime.NotHashingCount)
			require.Equal(t, 100e12, result.Readings[0].Summary.GetHashrateHs(), "state classification must not change numeric telemetry")
			require.Zero(t, result.Readings[1].UptimeStatus.GetHashingCount(), "state cannot carry into an unallocated bucket")
		})
	}
}

func TestAccountLegacyModuleUptimeUsesLatestStateWithoutMetricSamples(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	start := end.Add(-time.Hour)
	f.legacyModuleHistory(t)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
	require.NoError(t, err)
	for index, state := range []string{"active", "error", "active", "unknown"} {
		at := start.Add(time.Duration(index+1) * 10 * time.Minute)
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,state,state_observed_at) VALUES($1,$2,$3,$2)`, module, at, state)
		require.NoError(t, err)
	}
	request := dashboardRequest(client.AccountID, start, end, nil)
	request.BucketSeconds = 1800
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, result.Readings, 2)
	require.EqualValues(t, 1, result.Readings[0].UptimeStatus.BrokenCount, "latest error replaces earlier active state")
	require.Zero(t, result.Readings[0].UptimeStatus.HashingCount)
	require.Zero(t, result.Readings[1].UptimeStatus.HashingCount, "latest unknown state must not retain earlier active state")
	require.Zero(t, result.Readings[1].UptimeStatus.NotHashingCount, "unknown is a gap, not an offline observation")

	_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('container_module_diagnostic_observation') c`)
	require.NoError(t, err)
	compressed, err := f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, result.Readings, compressed.Readings, "compressed diagnostics must preserve dashboard history")
}
