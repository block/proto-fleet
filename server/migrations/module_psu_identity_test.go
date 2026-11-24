package migrations_test

import (
	"database/sql"
	"testing"
	"time"

	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestModulePSUIdentityMigrationPreservesLegacyRecordsAndGuardsRollback(t *testing.T) {
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, config)
	firstUser := database.CreateSuperAdminUser()
	secondUser := database.CreateSuperAdminUser2()
	firstDevice := database.CreateDevice(firstUser.OrganizationID, "proto")
	secondDevice := database.CreateDevice(secondUser.OrganizationID, "proto")
	conn, ctx := database.DB, t.Context()
	rollbackEmptyHashboardAllocations(t, conn)
	rollbackEmptyPoolConfigurations(t, conn)

	down, err := migrations.Current.ReadFile("current/001038_internal_module_psu_identity.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001038_internal_module_psu_identity.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err, "the empty fixture schema must roll back before legacy data is staged")

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	for _, device := range []struct {
		id    int64
		orgID int64
	}{
		{firstDevice.DatabaseID, firstUser.OrganizationID},
		{secondDevice.DatabaseID, secondUser.OrganizationID},
	} {
		_, err = conn.ExecContext(ctx, `
			INSERT INTO container_inventory(device_id,org_id,observed_at)
			VALUES($1,$2,$3)`, device.id, device.orgID, base)
		require.NoError(t, err)
	}
	var firstTank, secondTank int64
	require.NoError(t, conn.QueryRowContext(ctx, `
		INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at)
		VALUES($1,1,$2,$2) RETURNING id`, firstDevice.DatabaseID, base,
	).Scan(&firstTank))
	require.NoError(t, conn.QueryRowContext(ctx, `
		INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at)
		VALUES($1,1,$2,$2) RETURNING id`, secondDevice.DatabaseID, base,
	).Scan(&secondTank))

	insertModule := func(tankID int64, position int, reportedID string, retiredAt *time.Time) int64 {
		t.Helper()
		var id int64
		var retired sql.NullTime
		if retiredAt != nil {
			retired = sql.NullTime{Time: *retiredAt, Valid: true}
		}
		require.NoError(t, conn.QueryRowContext(ctx, `
			INSERT INTO container_module(
				tank_id,position,reported_id,identity_confirmed,
				first_seen_at,last_seen_at,retired_at,state
			) VALUES($1,$2,$3,true,$4,$5,$6,'active') RETURNING id`,
			tankID, position, reportedID, base, base.Add(10*time.Minute), retired,
		).Scan(&id))
		return id
	}
	retiredAt := base.Add(20 * time.Minute)
	duplicateRetired := insertModule(firstTank, 1, " duplicate-psu ", &retiredAt)
	duplicateCurrent := insertModule(firstTank, 1, "duplicate-psu", nil)
	sharedFirst := insertModule(firstTank, 2, "shared-psu", nil)
	unique := insertModule(firstTank, 3, "unique-psu", nil)
	blank := insertModule(firstTank, 4, "", nil)
	whitespace := insertModule(firstTank, 5, "   ", nil)
	sharedSecond := insertModule(secondTank, 1, "shared-psu", nil)

	_, err = conn.ExecContext(ctx, `
		INSERT INTO container_module_metrics(
			module_id,time,collected_at,hashrate_hs,power_w,temperature_c
		) VALUES
			($1,$3,$3,101,1001,51),
			($1,$4,$4,102,1002,52),
			($2,$5,$5,201,2001,61),
			($2,$6,$6,202,2002,62)`,
		duplicateRetired, duplicateCurrent,
		base.Add(time.Minute), base.Add(2*time.Minute),
		base.Add(3*time.Minute), base.Add(4*time.Minute),
	)
	require.NoError(t, err)

	snapshot := func(query string, args ...any) string {
		t.Helper()
		var value string
		require.NoError(t, conn.QueryRowContext(ctx, query, args...).Scan(&value))
		return value
	}
	moduleIDsBefore := snapshot(`SELECT jsonb_agg(id ORDER BY id)::text FROM container_module`)
	metricsBefore := snapshot(`
		SELECT jsonb_agg(to_jsonb(m) ORDER BY module_id,time,collected_at)::text
		FROM container_module_metrics m WHERE module_id IN ($1,$2)`, duplicateRetired, duplicateCurrent)

	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	require.Equal(t, moduleIDsBefore, snapshot(`SELECT jsonb_agg(id ORDER BY id)::text FROM container_module`))
	require.Equal(t, metricsBefore, snapshot(`
		SELECT jsonb_agg(to_jsonb(m) ORDER BY module_id,time,collected_at)::text
		FROM container_module_metrics m WHERE module_id IN ($1,$2)`, duplicateRetired, duplicateCurrent))

	var mapped sql.NullInt64
	var exactLegacyIDs bool
	require.NoError(t, conn.QueryRowContext(ctx, `
		SELECT module_id,legacy_module_ids=ARRAY[$3,$4]::bigint[]
		FROM container_module_identity WHERE org_id=$1 AND reported_module_id=$2`,
		firstUser.OrganizationID, "duplicate-psu", duplicateRetired, duplicateCurrent,
	).Scan(&mapped, &exactLegacyIDs))
	require.False(t, mapped.Valid, "ambiguous active and retired claims must remain unresolved")
	require.True(t, exactLegacyIDs, "the mapping must enumerate legacy rows without merging them")

	assertMapping := func(orgID int64, psuID string, expected int64) {
		t.Helper()
		var moduleID int64
		var singleton bool
		require.NoError(t, conn.QueryRowContext(ctx, `
			SELECT module_id,legacy_module_ids=ARRAY[$3]::bigint[]
			FROM container_module_identity WHERE org_id=$1 AND reported_module_id=$2`,
			orgID, psuID, expected,
		).Scan(&moduleID, &singleton))
		require.Equal(t, expected, moduleID)
		require.True(t, singleton)
	}
	assertMapping(firstUser.OrganizationID, "shared-psu", sharedFirst)
	assertMapping(secondUser.OrganizationID, "shared-psu", sharedSecond)
	assertMapping(firstUser.OrganizationID, "unique-psu", unique)
	require.NotEqual(t, sharedFirst, sharedSecond)
	_, err = conn.ExecContext(ctx, `INSERT INTO container_module_identity(org_id,reported_module_id,module_id) VALUES($1,'foreign-claim',$2)`, secondUser.OrganizationID, blank)
	require.ErrorContains(t, err, "module identity organization mismatch")
	_, err = conn.ExecContext(ctx, `UPDATE container_module SET tank_id=$1,position=99 WHERE id=$2`, secondTank, unique)
	require.ErrorContains(t, err, "module placement organization mismatch")

	var mappings, blankMappings int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM container_module_identity`).Scan(&mappings))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM container_module_identity WHERE btrim(reported_module_id)=''`).Scan(&blankMappings))
	require.Equal(t, 4, mappings)
	require.Zero(t, blankMappings)
	for _, id := range []int64{blank, whitespace} {
		var exists bool
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM container_module WHERE id=$1)`, id).Scan(&exists))
		require.True(t, exists, "blank legacy modules must remain even though they receive no mapping")
	}

	// The assertions above exercise the historical migration in isolation.
	// Current telemetry also reads pool assignments, so restore that schema
	// before exercising the upgraded runtime on the preserved legacy records.
	restoreEmptyPoolConfigurations(t, conn)

	// Anonymous polls after the upgrade must reuse both empty and space-only
	// legacy rows without inventing another active module at either position.
	_, err = conn.ExecContext(ctx, "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", firstDevice.DatabaseID)
	require.NoError(t, err)
	store, err := timescaledb.NewTelemetryStore(conn, timescaledb.DefaultConfig())
	require.NoError(t, err)
	observedAt := time.Now().UTC().Add(100 * time.Millisecond).Truncate(time.Microsecond)
	for _, offset := range []time.Duration{0, time.Millisecond} {
		at := observedAt.Add(offset)
		require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: firstDevice.ID, Timestamp: at, Container: &sdk.ContainerSnapshot{
			ObservedAt: at, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{
				{Position: 4, State: sdk.MinerStatusUnknown},
				{Position: 5, ReportedID: "   ", State: sdk.MinerStatusUnknown},
			}}},
		}}))
	}
	for position, expectedID := range map[int]int64{4: blank, 5: whitespace} {
		var id int64
		var reportedID string
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT id,reported_id FROM container_module WHERE tank_id=$1 AND position=$2 AND retired_at IS NULL", firstTank, position).Scan(&id, &reportedID))
		require.Equal(t, expectedID, id)
		require.Empty(t, reportedID)
	}

	_, err = conn.ExecContext(ctx, `UPDATE container_module SET position=99,last_seen_at=$2 WHERE id=$1`, unique, base.Add(30*time.Minute))
	require.NoError(t, err)
	var history int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM container_module_placement WHERE module_id=$1 AND ended_at IS NOT NULL`, unique).Scan(&history))
	require.Equal(t, 1, history)

	_, err = conn.ExecContext(ctx, string(down))
	require.ErrorContains(t, err, "module placement history exists; preserve evidence and fix forward")
	for _, table := range []string{"container_module_identity", "container_module_placement"} {
		var exists bool
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists))
		require.True(t, exists, "refused rollback must preserve "+table)
	}
}
