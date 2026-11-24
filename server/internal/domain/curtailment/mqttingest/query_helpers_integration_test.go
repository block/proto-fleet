package mqttingest

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestRigConfigStore_SettingsAndGenerationRollbackTogether(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	source := sqlc.InsertMQTTSourceConfigParams{
		OrganizationID: f.orgID, ServiceUserID: f.userID, SourceName: "original", Topic: "test/curtailment",
		BrokerPrimaryHost: "primary.example", BrokerSecondaryHost: "secondary.example",
		BrokerTransport: "tcp", PayloadFormat: "target_timestamp", Enabled: false,
		MqttUsername: "test", MqttPasswordEnc: "enc:secret",
	}
	created, err := f.queries.InsertMQTTSourceConfig(t.Context(), source)
	require.NoError(t, err)
	// Force the outbox write to fail after the source mutation has executed.
	_, err = f.db.ExecContext(t.Context(), `ALTER TABLE curtailment_rig_config_reconciliation ADD CONSTRAINT test_generation_failure CHECK (desired_generation <= 1)`)
	require.NoError(t, err)
	_, err = f.queries.UpdateMQTTSourceConfig(t.Context(), sqlc.UpdateMQTTSourceConfigParams{
		ID: created.ID, OrganizationID: f.orgID, ServiceUserID: f.userID, SourceName: "changed", Topic: source.Topic,
		BrokerPrimaryHost: source.BrokerPrimaryHost, BrokerSecondaryHost: source.BrokerSecondaryHost,
		BrokerTransport: source.BrokerTransport, PayloadFormat: source.PayloadFormat,
		MqttUsername: source.MqttUsername, MqttPasswordEnc: source.MqttPasswordEnc,
	})
	require.ErrorContains(t, err, "test_generation_failure")
	_, err = f.queries.SetMQTTSourceConfigEnabled(t.Context(), sqlc.SetMQTTSourceConfigEnabledParams{ID: created.ID, OrganizationID: f.orgID, Enabled: true})
	require.ErrorContains(t, err, "test_generation_failure")
	_, err = f.queries.DeleteDisabledMQTTSourceConfigByOrg(t.Context(), sqlc.DeleteDisabledMQTTSourceConfigByOrgParams{ID: created.ID, OrganizationID: f.orgID})
	require.ErrorContains(t, err, "test_generation_failure")
	source.SourceName = "second"
	_, err = f.queries.InsertMQTTSourceConfig(t.Context(), source)
	require.ErrorContains(t, err, "test_generation_failure")
	persisted, err := f.queries.GetMQTTSourceConfigByOrg(t.Context(), sqlc.GetMQTTSourceConfigByOrgParams{ID: created.ID, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.Equal(t, "original", persisted.SourceName)
	require.False(t, persisted.Enabled)
	rows, err := f.queries.ListMQTTSourceConfigsByOrg(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	var desired, full int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT desired_generation, full_reconcile_generation FROM curtailment_rig_config_reconciliation WHERE organization_id = $1`, f.orgID).Scan(&desired, &full))
	require.Equal(t, int64(1), desired)
	require.Equal(t, int64(1), full)
}

func TestRigConfigStore_QueryHelperMigrationRoundTripPreservesWork(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	rig := f.createRig(t, "Proto", "PAIRED")
	f.request(t, rig.identifier)
	for _, direction := range []string{"down", "up"} {
		migration, err := migrations.Current.ReadFile("current/001043_internal_curtailment_query_helpers." + direction + ".sql")
		require.NoError(t, err)
		tx, err := f.db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), string(migration))
		if err != nil {
			_ = tx.Rollback()
		}
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		var generation, targets int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT desired_generation, (SELECT count(*) FROM curtailment_rig_config_target WHERE organization_id=$1) FROM curtailment_rig_config_reconciliation WHERE organization_id=$1`, f.orgID).Scan(&generation, &targets))
		require.Equal(t, int64(1), generation)
		require.Equal(t, int64(1), targets)
	}
	f.request(t, rig.identifier)
	claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), claim.DesiredGeneration)
	require.Equal(t, []string{rig.identifier}, f.targets(t, claim.DesiredGeneration))
}
