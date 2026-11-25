package migrations_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Explain the source queries rather than maintaining simplified copies that can
// lose a recovery branch. SQL generation checks keep these and the worker in sync.
func poolLifecycleQuery(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile("../sqlc/queries/pool_lifecycle.sql")
	require.NoError(t, err)
	_, query, ok := strings.Cut(string(source), "-- name: "+name+" :")
	require.True(t, ok)
	_, query, ok = strings.Cut(query, "\n")
	require.True(t, ok)
	query, _, _ = strings.Cut(query, "-- name:")
	return query
}

func explainPoolLifecycle(t *testing.T, conn *sql.DB, query string, args ...any) {
	t.Helper()
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	if len(args) > 0 {
		// PL/pgSQL can reuse a generic plan after repeated allocations. Exercise
		// that path too: NEW.kind cannot be constant-folded for each board.
		_, err = tx.ExecContext(t.Context(), "SET LOCAL plan_cache_mode=force_generic_plan")
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), "PREPARE lifecycle_allocation_plan(bigint,integer,bigint,bigint) AS "+query)
		require.NoError(t, err)
		defer func() {
			_, err := tx.ExecContext(t.Context(), "DEALLOCATE lifecycle_allocation_plan")
			require.NoError(t, err)
		}()
		query = fmt.Sprintf("EXECUTE lifecycle_allocation_plan(%d,%d,%d,%d)", args...)
		args = nil
	}
	var raw string
	require.NoError(t, tx.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw))
	t.Log(raw)
	var plans []struct{ Plan map[string]any }
	require.NoError(t, json.Unmarshal([]byte(raw), &plans))
	var inspect func(map[string]any)
	inspect = func(node map[string]any) {
		if node["Relation Name"] == "pool_lifecycle_transition" {
			require.NotEqual(t, "Seq Scan", node["Node Type"], "lifecycle access must not scan retained history")
			if rows, ok := node["Actual Rows"].(float64); ok {
				loops, ok := node["Actual Loops"].(float64)
				require.True(t, ok)
				require.Less(t, rows*loops, float64(100), "index access must stay bounded to relevant work")
			}
			if removed, ok := node["Rows Removed by Filter"].(float64); ok {
				require.Less(t, removed, float64(100), "index access must not filter the organization's retained history")
			}
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				plan, ok := child.(map[string]any)
				require.True(t, ok)
				inspect(plan)
			}
		}
	}
	inspect(plans[0].Plan)
}

func TestPoolLifecycleQueriesAvoidRetainedHistory(t *testing.T) {
	conn, org, device := miningGroupRollbackFixture(t)
	_, err := conn.ExecContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,state)
 SELECT $1,$2,7,n,'ownership',CASE WHEN n>20000 THEN 'pending' ELSE (ARRAY['completed','verified','superseded','failed'])[1+n%4] END
 FROM generate_series(1,20020) n`, org, device)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,pairing_cycle_id)
 VALUES($1,$2,5,$2,'ownership',NULL),($1,$2,0,$2,'pairing',gen_random_uuid())`, org, device)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `ANALYZE pool_lifecycle_transition`)
	require.NoError(t, err)
	t.Run("polling", func(t *testing.T) {
		explainPoolLifecycle(t, conn, poolLifecycleQuery(t, "ListPendingPoolLifecycleTransitions"))
	})
	t.Run("reconciliation", func(t *testing.T) {
		explainPoolLifecycle(t, conn, poolLifecycleQuery(t, "ReconcilePoolLifecycleOperations"))
	})
	// Allocation must locate the selected asset even when the same organization
	// also has substantial unfinished work for other assets.
	_, err = conn.ExecContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason)
 SELECT $1,$2,7,n,'ownership' FROM generate_series(30000,32000) n`, org, device)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `ANALYZE pool_lifecycle_transition`)
	require.NoError(t, err)
	// Use the installed trigger's actual update, including its miner/container OR
	// branch. Explaining the outer allocation UPDATE would hide the trigger plan.
	var function string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT pg_get_functiondef('enqueue_ownership_pool_transition()'::regprocedure)`).Scan(&function))
	_, update, ok := strings.Cut(function, " UPDATE pool_lifecycle_transition")
	require.True(t, ok)
	update, _, ok = strings.Cut(update, ";")
	require.True(t, ok)
	update = "UPDATE pool_lifecycle_transition" + update
	update = strings.NewReplacer("NEW.organization_id", "$1", "NEW.kind", "$2", "NEW.asset_id", "$3", "NEW.device_id", "$4").Replace(update)
	for _, kind := range []int{7, 5} {
		name := "board allocation"
		asset := int64(20001)
		if kind == 5 {
			name = "miner allocation"
			asset = device
		}
		t.Run(name, func(t *testing.T) { explainPoolLifecycle(t, conn, update, org, kind, asset, device) })
	}
}
