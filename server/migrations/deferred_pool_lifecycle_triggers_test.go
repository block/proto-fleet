package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestMiningGroupGuardsUpgradePreservesPreseededPermission(t *testing.T) {
	conn, _, _ := miningGroupRollbackFixture(t)
	down, err := migrations.Current.ReadFile("current/001082_internal_ct1_mining_group_guards.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	var permissionID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO permission(key,description)
	 VALUES('miner:deactivate_mining_groups','Preseeded permission') RETURNING id`).Scan(&permissionID))
	up, err := migrations.Current.ReadFile("current/001082_internal_ct1_mining_group_guards.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(up))
	require.NoError(t, err, "upgrade must accept a permission row that already exists")
	var retainedID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM permission WHERE key='miner:deactivate_mining_groups'`).Scan(&retainedID))
	require.Equal(t, permissionID, retainedID)
	var missingGrants int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM role r
	 WHERE r.builtin_key='ADMIN' AND r.deleted_at IS NULL
	 AND NOT EXISTS(SELECT 1 FROM role_permission rp WHERE rp.role_id=r.id AND rp.permission_id=$1)`, permissionID).Scan(&missingGrants))
	require.Zero(t, missingGrants)
}

func TestMiningGroupGuardsDowngradeRestoresLifecycleTriggers(t *testing.T) {
	conn, _, _ := miningGroupRollbackFixture(t)
	var openIndex string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT pg_get_indexdef('idx_pool_lifecycle_transition_open_asset'::regclass)`).Scan(&openIndex))
	require.Contains(t, openIndex, "deferred")
	parent, err := migrations.Current.ReadFile("current/001078_internal_default_pool_lifecycle.up.sql")
	require.NoError(t, err)
	names := []string{
		"enqueue_ownership_pool_transition",
		"enqueue_pairing_pool_transition",
		"retire_absent_board_pool_transition",
		"retire_removed_board_pool_transitions",
		"resume_reinstalled_board_pool_transition",
	}
	current := make(map[string]string, len(names))
	for _, name := range names {
		var body string
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT prosrc FROM pg_proc WHERE oid=($1||'()')::regprocedure`, name).Scan(&body))
		require.Contains(t, body, "deferred")
		current[name] = body
	}
	down, err := migrations.Current.ReadFile("current/001082_internal_ct1_mining_group_guards.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT pg_get_indexdef('idx_pool_lifecycle_transition_open_asset'::regclass)`).Scan(&openIndex))
	require.NotContains(t, openIndex, "deferred", "downgrade must restore the parent index predicate")
	for _, name := range names {
		pattern := regexp.MustCompile(`(?s)CREATE FUNCTION ` + name + `\(\).*?AS \$\$(.*?)\nEND \$\$;`)
		parts := pattern.FindStringSubmatch(string(parent))
		require.Len(t, parts, 2)
		var body string
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT prosrc FROM pg_proc WHERE oid=($1||'()')::regprocedure`, name).Scan(&body))
		require.Equal(t, strings.TrimSpace(parts[1]+"\nEND "), strings.TrimSpace(body), "downgrade must restore migration 1078's function body: %s", name)
	}
	up, err := migrations.Current.ReadFile("current/001082_internal_ct1_mining_group_guards.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	for _, name := range names {
		var body string
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT prosrc FROM pg_proc WHERE oid=($1||'()')::regprocedure`, name).Scan(&body))
		require.Equal(t, current[name], body)
	}
}
