package migrations_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

func TestDelegatedRolloutEventsMigrationDownAndUpPreservesLegacyHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	f := newReleaseChannelFixture(t, db)
	downSQL, err := migrations.Migrations.ReadFile("000153_delegated_rollout_events.down.sql")
	require.NoError(t, err)
	upSQL, err := migrations.Migrations.ReadFile("000153_delegated_rollout_events.up.sql")
	require.NoError(t, err)
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&n))
		return n
	}
	requireSchema := func(present bool) {
		t.Helper()
		require.Equal(t, present, count(`SELECT count(*) FROM information_schema.columns
            WHERE table_schema='public' AND table_name='firmware_rollout' AND column_name='controller_waiting_since'`) == 1)
		for _, name := range []string{"firmware_rollout_event", "firmware_rollout_event_org_id", "firmware_rollout_event_rollout_id", "firmware_rollout_event_channel_id"} {
			require.Equal(t, present, count(`SELECT count(*) FROM pg_class WHERE relname=$1 AND relkind IN ('r','i')`, name) == 1, name)
		}
		require.Equal(t, present, count(`SELECT count(*) FROM pg_proc WHERE proname='activity_display_label_v150'`) == 1)
	}
	functionDefinition := func() string {
		t.Helper()
		var definition string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT pg_get_functiondef('activity_display_label(text,text,text,jsonb,text)'::regprocedure)`).Scan(&definition))
		return definition
	}

	requireSchema(true)
	f.exec(string(downSQL))
	requireSchema(false)
	originalLabels := functionDefinition()
	// Insert history against exactly the pre-153 schema. Upgrading may add a
	// nullable clock, but must never fabricate a wait or rewrite old evidence.
	channel := f.channel("legacy rollout history")
	device := f.device("legacy-device", 0)
	rollout := f.rollout(channel, "Bitmain", "S19")
	f.exec(`INSERT INTO firmware_rollout_device(rollout_id,device_id,attempts) VALUES($1,$2,2)`, rollout, device.id)
	f.exec(`UPDATE firmware_rollout SET status='completed_with_failures',finished_at=clock_timestamp() WHERE id=$1`, rollout)
	originalRevision := f.revision(rollout)
	var originalUpdatedAt time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT updated_at FROM firmware_rollout WHERE id=$1`, rollout).Scan(&originalUpdatedAt))

	for range 2 {
		f.exec(string(upSQL))
		requireSchema(true)
		var waiting sql.NullTime
		var status, checksum, version string
		var updatedAt time.Time
		require.NoError(t, db.QueryRowContext(ctx, `SELECT controller_waiting_since,status,firmware_checksum,firmware_version,updated_at
            FROM firmware_rollout WHERE id=$1`, rollout).Scan(&waiting, &status, &checksum, &version, &updatedAt))
		require.False(t, waiting.Valid)
		require.Equal(t, "completed_with_failures", status)
		require.Equal(t, "sum", checksum)
		require.Equal(t, "v1", version)
		require.Equal(t, originalUpdatedAt, updatedAt)
		require.Equal(t, originalRevision, f.revision(rollout))
		require.Equal(t, 1, count(`SELECT count(*) FROM firmware_rollout_device WHERE rollout_id=$1 AND device_id=$2 AND attempts=2`, rollout, device.id))
		require.Zero(t, count(`SELECT count(*) FROM firmware_rollout_event`), "migration does not invent events for legacy history")
		f.exec(`INSERT INTO firmware_rollout_event(org_id,rollout_id,channel_id,type,actor_type,actor_id,actor_name,rollout_revision,device_identifiers)
            VALUES($1,$2,$3,'rollout_device_failed','system',0,'',$4,ARRAY['legacy-device'])`, f.org, rollout, channel, originalRevision)
		require.Equal(t, 1, count(`SELECT count(*) FROM firmware_rollout_event WHERE rollout_id=$1`, rollout))
		f.exec(string(downSQL))
		requireSchema(false)
		require.Equal(t, originalLabels, functionDefinition(), "downgrade restores the original label implementation")
		require.Equal(t, originalRevision, f.revision(rollout))
	}
	f.exec(string(upSQL))
	requireSchema(true)
}

func TestDelegatedRolloutActivityLabelsPreserveEarlierLabels(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	labels := map[string]string{
		"rollout_advanced":                "Advanced firmware update",
		"rollout_devices_skipped":         "Skipped firmware update targets",
		"rollout_device_failed":           "Firmware update target failed",
		"rollout_controller_timed_out":    "Firmware update controller timed out",
		"rollout_completed_with_failures": "Completed firmware update with failures",
	}
	for eventType, want := range labels {
		var label string
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT activity_display_label($1,NULL::text,NULL::text,
            '{"channel_name":"Canary","manufacturer":"Proto","model":"Rig","firmware_version":"2.0.0"}'::jsonb,'')`, eventType).Scan(&label))
		require.Equal(t, want+": Canary Proto Rig → 2.0.0", label, eventType)
	}
	var label string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT activity_display_label('maintenance.ticket_created',NULL::text,NULL::text,'{}'::jsonb,'')`).Scan(&label))
	require.Equal(t, "Created repair ticket", label)
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT activity_display_label('cli_reset_password',NULL::text,NULL::text,'{"target_username":"owner"}'::jsonb,'')`).Scan(&label))
	require.Equal(t, "Break-glass password reset for owner", label)
}
