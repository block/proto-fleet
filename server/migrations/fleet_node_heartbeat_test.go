package migrations_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFleetNodeHeartbeatMigrationPreservesUpgradeAndRollbackLiveness(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, node int64
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO organization(org_id,name) VALUES($1,'heartbeat-host') RETURNING id", uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,'heartbeat-node',$2,$3,'CONFIRMED') RETURNING id", org, []byte(uuid.NewString()), make([]byte, 32)).Scan(&node))
	down, err := migrations.Current.ReadFile("current/001013_internal_fleet_node_heartbeat.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001013_internal_fleet_node_heartbeat.up.sql")
	require.NoError(t, err)
	// Exercise the existing-role grant with a test-specific cluster role.
	role := "heartbeat_read_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = conn.ExecContext(ctx, "CREATE ROLE "+role)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := conn.ExecContext(context.Background(), "DROP OWNED BY "+role)
		require.NoError(t, err)
		_, err = conn.ExecContext(context.Background(), "DROP ROLE "+role)
		require.NoError(t, err)
	})
	up = []byte(strings.ReplaceAll(string(up), "grafana_ha_ro", role))
	applyFleetNodeDetailsMigration(t, conn, "down")
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	oldSeen := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	_, err = conn.ExecContext(ctx, "UPDATE fleet_node SET last_seen_at=$1 WHERE id=$2", oldSeen, node)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	applyFleetNodeDetailsMigration(t, conn, "up")
	q := sqlc.New(conn)
	upgraded, err := q.GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: node, OrgID: org})
	require.NoError(t, err)
	require.Equal(t, oldSeen, upgraded.LastSeenAt.Time.UTC())
	latest := time.Now().UTC().Truncate(time.Microsecond)
	_, err = q.UpdateFleetNodeLastSeenAt(ctx, sqlc.UpdateFleetNodeLastSeenAtParams{ID: node, OrgID: org, LastSeenAt: latest})
	require.NoError(t, err)
	// An older heartbeat cannot move the authoritative timestamp backwards.
	_, err = q.UpdateFleetNodeLastSeenAt(ctx, sqlc.UpdateFleetNodeLastSeenAtParams{ID: node, OrgID: org, LastSeenAt: oldSeen})
	require.NoError(t, err)
	var granted, keysGranted bool
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT has_column_privilege($1,'fleet_node_live','last_seen_at','SELECT'),has_column_privilege($1,'fleet_node_live','identity_pubkey','SELECT')", role).Scan(&granted, &keysGranted))
	require.True(t, granted)
	require.False(t, keysGranted, "alert reader must not gain access to node credentials")
	applyFleetNodeDetailsMigration(t, conn, "down")
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	var restored time.Time
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT last_seen_at FROM fleet_node WHERE id=$1", node).Scan(&restored))
	require.Equal(t, latest, restored.UTC())
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	applyFleetNodeDetailsMigration(t, conn, "up")
}

func TestFleetMiningAssetUsesLiveHeartbeatAcrossMigration(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	ctx := t.Context()
	var node int64
	require.NoError(t, database.DB.QueryRowContext(ctx, `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,'asset-node',$2,$3,'CONFIRMED') RETURNING id`, user.OrganizationID, []byte(uuid.NewString()), make([]byte, 32)).Scan(&node))
	_, err = database.DB.ExecContext(ctx, `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)`, node, device.DatabaseID, user.OrganizationID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, device.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, `INSERT INTO device_status(device_id,status) VALUES($1,'ACTIVE')`, device.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c) VALUES(statement_timestamp(),$1,123000000000000,3200,65)`, device.ID)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001013_internal_fleet_node_heartbeat.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001013_internal_fleet_node_heartbeat.up.sql")
	require.NoError(t, err)
	applyFleetNodeDetailsMigration(t, database.DB, "down")
	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err)
	stale := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	_, err = database.DB.ExecContext(ctx, `UPDATE fleet_node SET last_seen_at=$1 WHERE id=$2`, stale, node)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, string(up))
	require.NoError(t, err)
	applyFleetNodeDetailsMigration(t, database.DB, "up")
	assertAsset := func(expectedState string, hasMetrics bool) {
		t.Helper()
		var state string
		var hash, power, temperature sql.NullFloat64
		require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT state,hashrate_hs,power_w,temperature_c FROM fleet_mining_asset WHERE kind=5 AND id=$1`, device.DatabaseID).Scan(&state, &hash, &power, &temperature))
		require.Equal(t, expectedState, state)
		require.Equal(t, hasMetrics, hash.Valid)
		require.Equal(t, hasMetrics, power.Valid)
		require.Equal(t, hasMetrics, temperature.Valid)
		if hasMetrics {
			require.Equal(t, float64(123000000000000), hash.Float64)
			require.Equal(t, float64(3200), power.Float64)
			require.Equal(t, float64(65), temperature.Float64)
		}
	}
	assertAsset("offline", false)
	fresh := time.Now().UTC().Truncate(time.Microsecond)
	_, err = sqlc.New(database.DB).UpdateFleetNodeLastSeenAt(ctx, sqlc.UpdateFleetNodeLastSeenAtParams{ID: node, OrgID: user.OrganizationID, LastSeenAt: fresh})
	require.NoError(t, err)
	var snapshot time.Time
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT last_seen_at FROM fleet_node WHERE id=$1`, node).Scan(&snapshot))
	require.Equal(t, stale, snapshot.UTC(), "the baseline timestamp remains a migration snapshot")
	assertAsset("hashing", true)
	applyFleetNodeDetailsMigration(t, database.DB, "down")
	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err, "rollback must restore the asset dependency before dropping fleet_node_live")
	assertAsset("hashing", true)
	_, err = database.DB.ExecContext(ctx, `UPDATE fleet_node SET last_seen_at=$1 WHERE id=$2`, stale, node)
	require.NoError(t, err)
	assertAsset("offline", false)
}

func applyFleetNodeDetailsMigration(t *testing.T, conn *sql.DB, direction string) {
	t.Helper()
	migration, err := migrations.Current.ReadFile("current/001026_internal_fleet_node_details." + direction + ".sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(migration))
	require.NoError(t, err)
}

func TestFleetNodeDetailsMigrationPreservesExistingHeartbeats(t *testing.T) {
	// Arrange
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, node int64
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO organization(org_id,name) VALUES($1,'details-host') RETURNING id", uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,'details-node',$2,$3,'CONFIRMED') RETURNING id", org, []byte(uuid.NewString()), make([]byte, 32)).Scan(&node))
	applyFleetNodeDetailsMigration(t, conn, "down")
	seen := time.Now().UTC().Truncate(time.Microsecond)
	_, err := conn.ExecContext(ctx, "INSERT INTO fleet_node_heartbeat(fleet_node_id,org_id,last_seen_at) VALUES($1,$2,$3)", node, org, seen)
	require.NoError(t, err)

	// Act
	applyFleetNodeDetailsMigration(t, conn, "up")
	q := sqlc.New(conn)
	nodes, err := q.ListFleetNodesForOrganization(ctx, org)
	require.NoError(t, err)

	// Assert
	require.Len(t, nodes, 1)
	require.Equal(t, seen, nodes[0].LastSeenAt.Time.UTC())
	require.Empty(t, nodes[0].IpAddresses)
	require.Empty(t, nodes[0].Version)
	require.Empty(t, nodes[0].EnrolledBy)

	// Act
	_, err = q.UpdateFleetNodeLastSeenAt(ctx, sqlc.UpdateFleetNodeLastSeenAtParams{ID: node, OrgID: org, LastSeenAt: seen.Add(time.Minute), IpAddresses: []string{"10.0.0.7"}, Version: "v1.0.0"})
	require.NoError(t, err)
	_, err = q.UpdateFleetNodeLastSeenAt(ctx, sqlc.UpdateFleetNodeLastSeenAtParams{ID: node, OrgID: org, LastSeenAt: seen, IpAddresses: []string{"10.0.0.8"}, Version: "v0.9.0"})
	require.NoError(t, err)
	nodes, err = q.ListFleetNodesForOrganization(ctx, org)
	require.NoError(t, err)

	// Assert
	require.Equal(t, []string{"10.0.0.7"}, nodes[0].IpAddresses)
	require.Equal(t, "v1.0.0", nodes[0].Version)

	// Act
	applyFleetNodeDetailsMigration(t, conn, "down")
	var preserved time.Time
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT last_seen_at FROM fleet_node_heartbeat WHERE fleet_node_id=$1", node).Scan(&preserved))

	// Assert
	require.Equal(t, seen.Add(time.Minute), preserved.UTC())
}
