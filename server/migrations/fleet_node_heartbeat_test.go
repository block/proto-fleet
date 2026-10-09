package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
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
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	oldSeen := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	_, err = conn.ExecContext(ctx, "UPDATE fleet_node SET last_seen_at=$1 WHERE id=$2", oldSeen, node)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
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
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	var restored time.Time
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT last_seen_at FROM fleet_node WHERE id=$1", node).Scan(&restored))
	require.Equal(t, latest, restored.UTC())
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
}
