package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func rollbackEmptyPoolConfigurations(t *testing.T, conn *sql.DB) {
	t.Helper()
	for _, path := range []string{
		"current/001078_internal_default_pool_lifecycle.down.sql",
		"current/001070_internal_activation_pool_handoff.down.sql",
		"current/001068_internal_pool_removal_bindings.down.sql",
		"current/001067_shared_pool_removal.down.sql",
		"current/001063_internal_pool_review_authorization_checkpoint.down.sql",
		"current/001062_shared_pool_review_authorization.down.sql",
		"current/001056_internal_pool_configuration_cutover.down.sql",
		"current/001054_internal_pool_equipment_integration.down.sql",
		"current/001053_shared_pool_equipment_state.down.sql",
		"current/001047_internal_pool_operation_bindings.down.sql",
		"current/001046_internal_pool_configuration_constraints.down.sql",
		"current/001045_shared_pool_configurations.down.sql",
	} {
		down, err := migrations.Current.ReadFile(path)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(down))
		require.NoError(t, err)
	}
}

// Restore dependencies before a historical migration fixture invokes current
// application code. The pool schema is empty in these migration tests.
func restoreEmptyPoolConfigurations(t *testing.T, conn *sql.DB) {
	t.Helper()
	for _, path := range []string{
		"current/001045_shared_pool_configurations.up.sql",
		"current/001046_internal_pool_configuration_constraints.up.sql",
		"current/001047_internal_pool_operation_bindings.up.sql",
		"current/001053_shared_pool_equipment_state.up.sql",
		"current/001054_internal_pool_equipment_integration.up.sql",
		"current/001056_internal_pool_configuration_cutover.up.sql",
		"current/001062_shared_pool_review_authorization.up.sql",
		"current/001063_internal_pool_review_authorization_checkpoint.up.sql",
		"current/001067_shared_pool_removal.up.sql",
		"current/001068_internal_pool_removal_bindings.up.sql",
		"current/001070_internal_activation_pool_handoff.up.sql",
		"current/001078_internal_default_pool_lifecycle.up.sql",
	} {
		up, err := migrations.Current.ReadFile(path)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(up))
		require.NoError(t, err)
	}
}

func TestEmptyPoolConfigurationMigrationRoundTrip(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	rollbackEmptyPoolConfigurations(t, conn)
	var remaining int
	require.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT count(*) FROM pg_tables
		WHERE schemaname = 'public' AND tablename LIKE 'pool_configuration%'`).Scan(&remaining))
	require.Zero(t, remaining)
	restoreEmptyPoolConfigurations(t, conn)
	// A second rollback also checks that no functions or constraints were left
	// behind by the first one or omitted when recreating the schema.
	rollbackEmptyPoolConfigurations(t, conn)
}

func TestPoolConfigurationScopeRevisionAndSlotConstraints(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, otherOrg int64
	for _, id := range []*int64{&org, &otherOrg} {
		require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pools') RETURNING id`, uuid.NewString()).Scan(id))
	}
	config, revision := uuid.NewString(), uuid.NewString()
	_, err := conn.ExecContext(ctx, `INSERT INTO pool_configuration(configuration_id,organization_id,name) VALUES($1,$2,'Production')`, config, org)
	require.NoError(t, err)
	slots := `[{"id":"primary","url":"stratum+tcp://pool.example","username":"payout","worker_template":"","password_ciphertext":"encrypted","has_password":true}]`
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_revision(revision_id,configuration_id,organization_id,number,slots) VALUES($1,$2,$3,1,$4)`, revision, config, otherOrg, slots)
	require.Error(t, err, "revisions cannot cross organization boundaries")
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_revision(revision_id,configuration_id,organization_id,number,slots) VALUES($1,$2,$3,1,$4)`, revision, config, org, slots)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration SET current_revision_id=$1 WHERE configuration_id=$2`, revision, config)
	require.NoError(t, err)
	for _, query := range []string{
		`UPDATE pool_configuration_revision SET number=2 WHERE revision_id=$1`,
		`DELETE FROM pool_configuration_revision WHERE revision_id=$1`,
	} {
		_, err = conn.ExecContext(ctx, query, revision)
		require.Error(t, err, "saved revisions cannot be rewritten or deleted")
	}
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration SET organization_id=$1 WHERE configuration_id=$2`, otherOrg, config)
	require.Error(t, err, "configuration ownership cannot be changed")
	otherConfig := uuid.NewString()
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration(configuration_id,organization_id,name,current_revision_id) VALUES($1,$2,'Other',$3)`, otherConfig, org, revision)
	require.Error(t, err, "current revision must belong to its configuration")
	for _, invalid := range []string{
		`[]`,
		`null`,
		`[{"url":"stratum+tcp://pool.example","username":"payout","worker_template":"","password_ciphertext":"encrypted","has_password":true}]`,
		`[{"id":"primary","url":"stratum+tcp://pool.example","username":"payout","worker_template":"","password_ciphertext":"","has_password":true}]`,
		slots[:len(slots)-1] + "," + slots[1:],
	} {
		_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_revision(revision_id,configuration_id,organization_id,number,slots) VALUES($1,$2,$3,2,$4)`, uuid.NewString(), config, org, invalid)
		require.Error(t, err, "invalid or repeated slot identities must be rejected")
	}
	var foreignAccount int64
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, otherOrg).Scan(&foreignAccount))
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration(configuration_id,organization_id,account_id,name) VALUES($1,$2,$3,'Private')`, uuid.NewString(), org, foreignAccount)
	require.Error(t, err, "hosted ownership must belong to the same organization")
	down, err := migrations.Current.ReadFile("current/001045_shared_pool_configurations.down.sql")
	require.NoError(t, err)
	statement, err := conn.PrepareContext(ctx, string(down))
	require.NoError(t, err, "rollback refusal and DDL must be one atomic statement")
	defer statement.Close()
	_, err = statement.ExecContext(ctx)
	require.ErrorContains(t, err, "cannot roll back with pool configuration data")
	var retained string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT slots->0->>'password_ciphertext' FROM pool_configuration_revision WHERE revision_id=$1`, revision).Scan(&retained))
	require.Equal(t, "encrypted", retained)
}
