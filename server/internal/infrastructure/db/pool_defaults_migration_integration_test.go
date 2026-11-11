package db

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDefaultPoolLifecycleUpgradeFromPopulated1072(t *testing.T) {
	requireBaselineDB(t)
	conn, _ := newMigrationBridgeTestDB(t)
	ctx := t.Context()
	previous := fstest.MapFS{}
	paths, err := fs.Glob(migrations.Current, "current/*.sql")
	require.NoError(t, err)
	for _, path := range paths {
		if path < "current/001073" {
			body, err := fs.ReadFile(migrations.Current, path)
			require.NoError(t, err)
			previous[path] = &fstest.MapFile{Data: body}
		}
	}
	require.NoError(t, runCurrentMigrations(ctx, conn, previous))
	var version int
	var dirty bool
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty))
	require.Equal(t, 1072, version)
	require.False(t, dirty)
	var org, client, subclient int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pool upgrade') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT $1,$2,id,1,'Client',31 FROM hosted_account WHERE organization_id=$2 AND depth=0 RETURNING id`, uuid.NewString(), org).Scan(&client))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,2,'Subclient',31) RETURNING id`, uuid.NewString(), org, client).Scan(&subclient))
	type library struct {
		owner                      int64
		oldest, assigned, revision string
	}
	libraries := []library{{owner: 0}, {owner: client}, {owner: subclient}}
	const slots = `[{"id":"primary","url":"stratum+tcp://pool.example","username":"existing","worker_template":"","password_ciphertext":"retained-encrypted-secret","has_password":true}]`
	for i := range libraries {
		for position := range 3 {
			config, revision := uuid.NewString(), uuid.NewString()
			_, err := conn.ExecContext(ctx, `INSERT INTO pool_configuration(configuration_id,organization_id,account_id,name,archived,created_at) VALUES($1,$2,NULLIF($3::bigint,0),'Existing',$4,'2020-01-01'::timestamptz+$5::int*interval '1 day')`, config, org, libraries[i].owner, position == 0, position)
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_revision(revision_id,configuration_id,organization_id,number,slots) VALUES($1,$2,$3,1,$4)`, revision, config, org, slots)
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, `UPDATE pool_configuration SET current_revision_id=$1 WHERE configuration_id=$2`, revision, config)
			require.NoError(t, err)
			if position == 1 {
				libraries[i].oldest = config
			}
			if position == 2 {
				libraries[i].assigned = config
				libraries[i].revision = revision
			}
		}
	}
	var device int64
	require.NoError(t, conn.QueryRowContext(ctx, `WITH discovery AS (
 INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class)
 VALUES($1,$2,'192.0.2.1','80','http','proto',1) RETURNING id)
 INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id)
 SELECT $1,$2,'02:00:00:00:00:01',id FROM discovery RETURNING id`, org, uuid.NewString()).Scan(&device))
	_, err = conn.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'AUTHENTICATION_NEEDED')`, device)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,adopted_at) VALUES($1,'device',$2,$2,$3,$4,1,now())`, org, device, libraries[0].assigned, libraries[0].revision)
	require.NoError(t, err)
	var assignmentBefore, revisionsBefore string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_jsonb(a)::text FROM pool_configuration_assignment a WHERE device_id=$1`, device).Scan(&assignmentBefore))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(r) ORDER BY revision_id)::text FROM pool_configuration_revision r`).Scan(&revisionsBefore))

	require.NoError(t, runCurrentMigrations(ctx, conn, migrations.Current))
	require.NoError(t, runCurrentMigrations(ctx, conn, migrations.Current), "repeated startup must not recreate or reapply defaults")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty))
	require.Equal(t, 1073, version)
	require.False(t, dirty)
	for _, library := range libraries {
		var chosen string
		var count int
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*),min(configuration_id) FROM pool_configuration WHERE organization_id=$1 AND COALESCE(account_id,0)=$2 AND is_default`, org, library.owner).Scan(&count, &chosen))
		require.Equal(t, 1, count)
		require.Equal(t, library.oldest, chosen, "choose oldest active configuration independently for each owner")
	}
	var assignmentAfter, revisionsAfter string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_jsonb(a)::text FROM pool_configuration_assignment a WHERE device_id=$1`, device).Scan(&assignmentAfter))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(r) ORDER BY revision_id)::text FROM pool_configuration_revision r`).Scan(&revisionsAfter))
	require.JSONEq(t, assignmentBefore, assignmentAfter, "choosing a default must not reassign existing hardware")
	require.JSONEq(t, revisionsBefore, revisionsAfter, "upgrade preserves existing revisions and encrypted credentials")
	var events int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_lifecycle_transition`).Scan(&events))
	require.Zero(t, events, "upgrade must not enqueue hardware changes")
	_, err = conn.ExecContext(ctx, `UPDATE device_pairing SET pairing_status='PAIRED' WHERE device_id=$1`, device)
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_lifecycle_transition`).Scan(&events))
	require.Zero(t, events, "repairing pre-upgrade authentication preserves the manual assignment")
}
