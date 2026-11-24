package monitoring_test

import (
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReachabilitySummarySeparatesUnknownFromOffline(t *testing.T) {
	db := dbtest.GetTestDB(t)
	_, err := db.Exec(`INSERT INTO organization(id,org_id,name) VALUES(1,'reachability-test','Test');
 INSERT INTO discovered_device(id,org_id,device_identifier,ip_address,port,url_scheme,driver_name,is_active)
 SELECT n,1,'miner-'||n,'127.0.0.1','80','http','proto',true FROM generate_series(1,7) n;
 INSERT INTO device(id,org_id,device_identifier,mac_address,discovered_device_id)
 SELECT n,1,'miner-'||n,'00:00:00:00:00:'||lpad(n::text,2,'0'),n FROM generate_series(1,7) n;
 INSERT INTO device_pairing(device_id,pairing_status,paired_at)
 SELECT n,'PAIRED',now()-interval '1 hour' FROM generate_series(1,7) n;
 INSERT INTO device_status(device_id,status,status_timestamp,telemetry_status_at) VALUES
 (1,'INACTIVE',now(),now()),(2,'OFFLINE',now(),now()),
 (3,'ACTIVE',now()-interval '10 minutes',now()-interval '10 minutes'),
 (4,'UPDATING',now(),NULL),(5,'ACTIVE',now()+interval '1 hour',now()+interval '1 hour');
 UPDATE device_pairing SET paired_at=now() WHERE device_id=6;
 INSERT INTO device_reachability_exclusion(device_id,reason,expires_at) VALUES(7,'maintenance',now()+interval '1 hour');`)
	require.NoError(t, err)
	read := func() []int64 {
		t.Helper()
		v := make([]int64, 6)
		require.NoError(t, db.QueryRow(`SELECT expected_count,reachable_count,unreachable_count,unknown_count,excluded_count,grace_count FROM device_reachability_summary WHERE org_id=1`).Scan(&v[0], &v[1], &v[2], &v[3], &v[4], &v[5]))
		require.Equal(t, v[0], v[1]+v[2]+v[3])
		return v
	}
	require.Equal(t, []int64{5, 1, 1, 3, 1, 1}, read(), "paused is reachable; stale/future/firmware states are unknown")
	_, err = db.Exec(`UPDATE device_reachability_exclusion SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour';
 UPDATE device_status SET status='ACTIVE',status_timestamp=now(),telemetry_status_at=now() WHERE device_id=2`)
	require.NoError(t, err)
	require.Equal(t, []int64{6, 2, 0, 4, 0, 1}, read())
	_, err = db.Exec(`UPDATE device SET deleted_at=now() WHERE id=1`)
	require.NoError(t, err)
	require.Equal(t, []int64{5, 1, 0, 4, 0, 1}, read())
	_, err = db.Exec(`UPDATE device_status SET status='OFFLINE',status_timestamp=telemetry_status_at+interval '1 microsecond' WHERE device_id=2`)
	require.NoError(t, err)
	require.Equal(t, []int64{5, 0, 0, 5, 0, 1}, read(), "evidence for a replaced status does not count")
}

func TestReachabilityMigrationGrantsMonitoringRoleReadAccess(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	tx, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	const path = "current/001112_internal_miner_reachability_monitoring"
	down, err := migrations.Current.ReadFile(path + ".down.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(down))
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile(path + ".up.sql")
	require.NoError(t, err)

	// Simulate HA bootstrap with a role scoped to this test transaction.
	role := "reachability_read_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = tx.ExecContext(ctx, "CREATE ROLE "+role+" NOINHERIT")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "GRANT USAGE ON SCHEMA public TO "+role)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, strings.ReplaceAll(string(up), "grafana_ha_ro", role))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "SET LOCAL ROLE "+role)
	require.NoError(t, err)

	var scopes int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM device_reachability_summary`).Scan(&scopes))
	require.Zero(t, scopes)

	var canReadStatus, canReadExclusions bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT
		has_table_privilege(current_user, 'device_status', 'SELECT'),
		has_table_privilege(current_user, 'device_reachability_exclusion', 'SELECT')
	`).Scan(&canReadStatus, &canReadExclusions))
	require.False(t, canReadStatus)
	require.False(t, canReadExclusions)
}
