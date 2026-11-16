package migrations_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMiningGroupPoolWorkGuardsAvoidRetainedTargetHistory(t *testing.T) {
	conn, org, device := miningGroupRollbackFixture(t)
	ctx := t.Context()
	config, revision := uuid.NewString(), uuid.NewString()
	_, err := conn.ExecContext(ctx, `INSERT INTO pool_configuration(configuration_id,organization_id,name) VALUES($1,$2,'Plan fixture')`, config, org)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_revision(revision_id,organization_id,configuration_id,number,slots)
 VALUES($1,$2,$3,1,'[{"id":"plan-slot","url":"stratum+tcp://pool.example","username":"worker","worker_template":"","password_ciphertext":"encrypted","has_password":false}]')`, revision, org, config)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_operation(operation_id,organization_id,configuration_id,revision_id,actor_id)
 SELECT gen_random_uuid()::text,$1,$2,$3,(SELECT user_id FROM user_organization WHERE organization_id=$1 LIMIT 1) FROM generate_series(1,20001)`, org, config, revision)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,operation_id)
 SELECT $1,'device',$2,$2,$3,$4,1,operation_id FROM pool_configuration_operation WHERE organization_id=$1 LIMIT 1`, org, device, config, revision)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_operation_target(organization_id,operation_id,target_kind,target_id,assignment_version,state)
 SELECT $1,operation_id,'device',$2,1,'verified' FROM pool_configuration_operation WHERE organization_id=$1`, org, device)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET state='uncertain' WHERE operation_id=(SELECT operation_id FROM pool_configuration_assignment WHERE organization_id=$1 LIMIT 1)`, org)
	require.NoError(t, err)
	// One current board exercises the device-target branch of the board list.
	var tank, module, board int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, device).Scan(&tank))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,1,'plan-module',now(),now()) RETURNING id`, tank).Scan(&module))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, org).Scan(&board))
	_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,1,now(),now())`, board, module)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `ANALYZE pool_configuration_operation_target; ANALYZE pool_configuration_assignment`)
	require.NoError(t, err)
	for _, tc := range []struct{ file, name string }{
		{"mining_group_default_return.sql", "ListDefaultReturnBoards"},
		{"mining_group_default_return.sql", "HasPendingDefaultReturnPoolPolicy"},
		{"mining_group_guards.sql", "GetMiningGroupDeactivationBlocker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := os.ReadFile("../sqlc/queries/" + tc.file)
			require.NoError(t, err)
			_, query, ok := strings.Cut(string(source), "-- name: "+tc.name+" :")
			require.True(t, ok)
			_, query, ok = strings.Cut(query, "\n")
			require.True(t, ok)
			query, _, _ = strings.Cut(query, "-- name:")
			query = strings.NewReplacer("sqlc.arg(organization_id)", fmt.Sprint(org), "sqlc.arg(device_id)", fmt.Sprint(device), "sqlc.arg(ignore_deferred)", "false", "sqlc.arg(device_identifier)", "''", "sqlc.arg(hashboard_ids)", "'{}'").Replace(query)
			var plan string
			require.NoError(t, conn.QueryRowContext(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+query).Scan(&plan))
			require.Contains(t, plan, "idx_pool_configuration_operation_target_open", "guards must use the small open-work index over 20000 retained terminal targets")
			t.Log(plan)
		})
	}
}
