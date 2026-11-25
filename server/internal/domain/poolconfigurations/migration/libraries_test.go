package migration

import (
	"encoding/base64"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLibrariesConsolidatesOperatorAndHostSnapshotsWithoutChangingSources(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	secrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	var org, root, account int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pools') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',15) RETURNING id`, uuid.NewString(), org, root).Scan(&account))
	_, err = conn.ExecContext(ctx, `INSERT INTO pool(org_id,pool_name,url,username,password_enc) VALUES($1,'Operator','stratum+tcp://pool.example','payout.worker',''),($1,'Automatic worker','stratum+tcp://backup.example','payout','')`, org)
	require.NoError(t, err)
	encoded, err := poolconfigurations.EncryptSlots(secrets, []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout.worker"}}, nil)
	require.NoError(t, err)
	for _, owner := range []int64{root, account} {
		id := uuid.NewString()
		_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, id, org, owner)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots) VALUES($1,$2,$3,$4,1,'Saved stack',$5)`, uuid.NewString(), org, owner, id, encoded)
		require.NoError(t, err)
	}
	run := func() poolconfigurations.ImportReport {
		t.Helper()
		report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
			return Libraries(ctx, q, secrets, org)
		})
		require.NoError(t, err)
		return report
	}
	first := run()
	require.Empty(t, first.Unresolved)
	require.Equal(t, 3, first.CreatedConfigurations)
	require.Len(t, first.Imported, 4)
	second := run()
	require.Zero(t, second.CreatedConfigurations)
	require.Equal(t, first.Imported, second.Imported)
	var legacy, hosted int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool WHERE org_id=$1`, org).Scan(&legacy))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_pool_revision WHERE organization_id=$1`, org).Scan(&hosted))
	require.Equal(t, 2, legacy)
	require.Equal(t, 2, hosted)
	var worker, ciphertext string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT r.slots->0->>'worker_template',r.slots->0->>'password_ciphertext' FROM pool_configuration c JOIN pool_configuration_revision r ON r.revision_id=c.current_revision_id WHERE c.organization_id=$1 AND c.name='Automatic worker'`, org).Scan(&worker, &ciphertext))
	require.Equal(t, "{miner_name}", worker)
	password, err := secrets.Decrypt(ciphertext)
	require.NoError(t, err)
	require.Empty(t, password, "legacy omitted passwords are known empty")

	// Renames advance the legacy timestamp or hosted revision identity, while
	// the collector no longer supplies the older, unpinned source versions.
	_, err = conn.ExecContext(ctx, `UPDATE pool SET pool_name=pool_name || ' renamed', updated_at=updated_at + interval '1 second' WHERE org_id=$1`, org)
	require.NoError(t, err)
	for _, owner := range []int64{root, account} {
		_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots)
			SELECT $1,organization_id,account_id,pool_id,2,'Renamed stack',slots FROM hosted_account_pool_revision
			WHERE organization_id=$2 AND account_id=$3 AND number=1`, uuid.NewString(), org, owner)
		require.NoError(t, err)
	}
	renamed := run()
	require.Empty(t, renamed.Unresolved)
	require.Zero(t, renamed.CreatedConfigurations)
	require.Len(t, renamed.Imported, 4)
	wantRevisions := map[string]string{}
	for _, imported := range first.Imported {
		wantRevisions[imported.ConfigurationID] = imported.RevisionID
	}
	for _, imported := range renamed.Imported {
		require.Equal(t, wantRevisions[imported.ConfigurationID], imported.RevisionID)
	}
	require.NotEqual(t, first.Imported, renamed.Imported, "new source versions receive their own mappings")
}

func TestLibrariesPreservesPinnedOlderProfileRevision(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	secrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	var org, root int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pools') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	poolID, pinnedID := uuid.NewString(), uuid.NewString()
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, poolID, org, root)
	require.NoError(t, err)
	for number, id := range []string{pinnedID, uuid.NewString()} {
		encoded, err := poolconfigurations.EncryptSlots(secrets, []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: id}}, nil)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots) VALUES($1,$2,$3,$4,$5,'Saved stack',$6)`, id, org, root, poolID, number+1, encoded)
		require.NoError(t, err)
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_profile(organization_id,account_id,revision_id) VALUES($1,$2,$3)`, org, root, pinnedID)
	require.NoError(t, err)
	report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
		return Libraries(ctx, q, secrets, org)
	})
	require.NoError(t, err)
	require.Empty(t, report.Unresolved)
	require.Len(t, report.Imported, 2, "both the current and explicitly selected older revision need source mappings")
	var mapped int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_import WHERE organization_id=$1 AND source_id=$2`, org, pinnedID).Scan(&mapped))
	require.Equal(t, 1, mapped)
}
