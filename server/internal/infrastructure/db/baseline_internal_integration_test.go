package db

import (
	"database/sql"
	"io/fs"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/require"
)

func TestBaselineInternalReconciliation(t *testing.T) {
	requireBaselineDB(t)
	// Cover both bounded legacy paths, the snapshot boundary and both direct entry points.
	for _, version := range []uint8{150, 152, 153, 154, 158} {
		t.Run(strconv.Itoa(int(version)), func(t *testing.T) {
			conn, config := newLegacyMigrationTestDB(t, uint(version))
			seedBaselineIdentity(t, conn)
			if version == 152 || version == 153 {
				if _, err := conn.ExecContext(t.Context(), `INSERT INTO container_snapshot(device_id,observed_at,snapshot) VALUES(999999,now(),'{"keep":true}')`); err != nil {
					t.Fatal(err)
				}
			}
			if version == 158 {
				if _, err := conn.ExecContext(t.Context(), `INSERT INTO container_fan_observation(device_id,component_id,observed_at,speed_percent,power_w) VALUES(999999,'fan-1','2026-01-01',51,120); INSERT INTO container_controls_observation(device_id,observed_at,supported) VALUES(999999,'2026-01-01',true); INSERT INTO container_control_observation(device_id,name,enabled) VALUES(999999,'fixture-control',true); INSERT INTO container_setpoint_observation(device_id,name,value,unit) VALUES(999999,'fixture-setpoint',42,'C')`); err != nil {
					t.Fatal(err)
				}
			}
			sourceVersion := version
			if version != 153 && version != 158 {
				if err := ApplyBaseline(t.Context(), conn, "internal", int(sourceVersion)); err == nil {
					t.Fatal("non-entry-point source was accepted directly")
				}
				// The stock legacy runner reaches one of two entry points. Never
				// cross destructive migration 154 from a database retaining snapshots.
				sourceVersion = 158
				if version < 153 {
					sourceVersion = 153
				}
				migrateTestDBTo(t, conn, config.Name, uint(sourceVersion))
			}
			if version == 153 || version == 158 {
				roundTripBaselineConstraints(t, conn)
			}
			before, err := protectedBaselineData(t.Context(), conn, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = ApplyBaseline(t.Context(), conn, "internal", int(sourceVersion)); err != nil {
				t.Fatal(err)
			}
			after, err := protectedBaselineData(t.Context(), conn, false)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("protected data changed")
			}
			if err = ApplyBaseline(t.Context(), conn, "internal", int(sourceVersion)); err != nil {
				t.Fatal(err)
			}
			if version == 158 {
				var preserved bool
				if err := conn.QueryRowContext(t.Context(), `SELECT
                    (SELECT speed_percent=51 AND power_w=120 AND observed_at='2026-01-01' FROM container_fan_observation WHERE device_id=999999 AND component_id='fan-1') AND
                    (SELECT supported AND observed_at='2026-01-01' FROM container_controls_observation WHERE device_id=999999) AND
                    (SELECT enabled FROM container_control_observation WHERE device_id=999999 AND name='fixture-control') AND
                    (SELECT value=42 AND unit='C' FROM container_setpoint_observation WHERE device_id=999999 AND name='fixture-setpoint')`).Scan(&preserved); err != nil {
					t.Fatal(err)
				}
				if !preserved {
					t.Fatal("equipment observations changed")
				}
			}
			if err = runCurrentMigrations(t.Context(), conn, migrations.Current); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBaselineInternalRefusesSkippedPrivateMigration(t *testing.T) {
	requireBaselineDB(t)
	conn, _ := newBaselinePublicFixture(t, 153)
	if _, err := conn.ExecContext(t.Context(), `UPDATE schema_migrations SET version=1001`); err != nil {
		t.Fatal(err)
	}
	if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err == nil {
		t.Fatal("same version admitted a shared-only schema")
	}
	if err := ApplyBaseline(t.Context(), conn, "public", 1001); err == nil {
		t.Fatal("unqualified equal-version switch was accepted")
	}
	var privateColumn bool
	if err := conn.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='discovered_device' AND column_name='device_class')`).Scan(&privateColumn); err != nil {
		t.Fatal(err)
	}
	if privateColumn {
		t.Fatal("refusal applied private SQL")
	}
}

func TestConsolidationUpgradesPublicBaseline(t *testing.T) {
	requireBaselineDB(t)
	fresh, _ := newMigrationBridgeTestDB(t)
	require.NoError(t, runCurrentMigrations(t.Context(), fresh, migrations.Current))
	// Reparse the final CHECK constraints on each database so equivalent casts compare equally.
	roundTripBaselineConstraints(t, fresh)
	freshCatalog, err := readBaselineCatalog(t.Context(), fresh)
	require.NoError(t, err)

	firstSample := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	secondSample := firstSample.Add(time.Hour)
	for _, test := range []struct {
		name      string
		reconcile bool
	}{
		{name: "direct upgrade"},
		{name: "offline reconciliation", reconcile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn, config := newMigrationBridgeTestDB(t)
			baseline := fstest.MapFS{}
			for _, direction := range []string{"up", "down"} {
				name := "001000_shared_baseline." + direction + ".sql"
				body, err := fs.ReadFile(migrations.Active, "current/"+name)
				require.NoError(t, err)
				baseline[name] = &fstest.MapFile{Data: body}
			}
			require.NoError(t, migrateTestDBSourceTo(t.Context(), conn, config.Name, baseline, 1000))
			seedBaselineIdentity(t, conn)
			_, err := conn.ExecContext(t.Context(), `
				INSERT INTO device_metrics (time, device_identifier, hash_rate_hs, power_w)
				VALUES ($1, 'fixture-device', 100000000000000, 3000),
				       ($2, 'fixture-device', 110000000000000, 3100)
			`, firstSample, secondSample)
			require.NoError(t, err)
			roundTripBaselineConstraints(t, conn)

			if test.reconcile {
				require.NoError(t, ApplyBaseline(t.Context(), conn, "public", 1000))
				status, err := readBaselineStatus(t.Context(), conn)
				require.NoError(t, err)
				require.Equal(t, 1001, status.Version)
				require.False(t, status.Dirty)
			}
			for range 2 {
				require.NoError(t, runCurrentMigrations(t.Context(), conn, migrations.Current))
			}
			status, err := CheckBaseline(t.Context(), conn, "target", "", 0)
			require.NoError(t, err)
			require.GreaterOrEqual(t, status.Version, migrations.ConsolidationVersion)
			roundTripBaselineConstraints(t, conn)
			catalog, err := readBaselineCatalog(t.Context(), conn)
			require.NoError(t, err)
			require.Equal(t, freshCatalog, catalog, "upgraded schema must match a fresh install")

			var preserved bool
			require.NoError(t, conn.QueryRowContext(t.Context(), `
				SELECT (SELECT password_hash='preserve-this-password-hash' FROM "user" WHERE id=999999)
				   AND (SELECT key_hash='preserve-fixture-hash' FROM api_key WHERE id=999999)
				   AND (SELECT device_identifier='fixture-device' FROM device WHERE id=999999)
				   AND has_column_privilege('public', 'device', 'id', 'SELECT')
			`).Scan(&preserved))
			require.True(t, preserved)

			var paired bool
			require.NoError(t, conn.QueryRowContext(t.Context(), `
				SELECT EXISTS (
				    SELECT 1 FROM device_pairing p JOIN fleet_node_device n USING (device_id)
				    WHERE p.device_id=999999 AND p.pairing_status='PAIRED'
				      AND n.fleet_node_id=999999 AND n.org_id=999999
				)
			`).Scan(&paired))
			require.True(t, paired, "device pairing and Node assignment must survive upgrade")

			var samples int
			require.NoError(t, conn.QueryRowContext(t.Context(), `
				SELECT count(*) FROM device_metrics WHERE device_identifier='fixture-device'
				  AND ((time=$1 AND hash_rate_hs=100000000000000 AND power_w=3000)
				    OR (time=$2 AND hash_rate_hs=110000000000000 AND power_w=3100))
			`, firstSample, secondSample).Scan(&samples))
			require.Equal(t, 2, samples, "both telemetry samples must survive upgrade")
		})
	}
}

func TestConsolidationResumesInternalCheckpoint(t *testing.T) {
	requireBaselineDB(t)
	for _, checkpoint := range []int{1113, 1114} {
		t.Run(strconv.Itoa(checkpoint), func(t *testing.T) {
			previous := fstest.MapFS{}
			paths, err := fs.Glob(migrations.Current, "current/*.sql")
			require.NoError(t, err)
			for _, path := range paths {
				version, err := strconv.Atoi(path[len("current/") : len("current/")+6])
				require.NoError(t, err)
				if version <= checkpoint {
					sql, err := fs.ReadFile(migrations.Current, path)
					require.NoError(t, err)
					previous[path] = &fstest.MapFile{Data: sql}
				}
			}
			conn, _ := newMigrationBridgeTestDB(t)
			require.NoError(t, runCurrentMigrations(t.Context(), conn, previous))
			status, err := readBaselineStatus(t.Context(), conn)
			require.NoError(t, err)
			require.Equal(t, checkpoint, status.Version)
			seedBaselineIdentity(t, conn)

			require.NoError(t, runCurrentMigrations(t.Context(), conn, migrations.Current))
			status, err = CheckBaseline(t.Context(), conn, "target", "", 0)
			require.NoError(t, err)
			require.GreaterOrEqual(t, status.Version, migrations.ConsolidationVersion)
			var passwordHash string
			require.NoError(t, conn.QueryRowContext(t.Context(),
				`SELECT password_hash FROM "user" WHERE id=999999`).Scan(&passwordHash))
			require.Equal(t, "preserve-this-password-hash", passwordHash)
		})
	}
}

func TestInternalModuleIssuesUpgrade(t *testing.T) {
	requireBaselineDB(t)
	for _, source := range []string{"internal baseline", "public legacy transition"} {
		t.Run(source, func(t *testing.T) {
			var conn *sql.DB
			if source == "public legacy transition" {
				conn, _ = newBaselinePublicFixture(t, 153)
				seedBaselineIdentity(t, conn)
				if err := ApplyBaseline(t.Context(), conn, "public", 153); err != nil {
					t.Fatal(err)
				}
			} else {
				conn, _ = newMigrationBridgeTestDB(t)
				files, err := iofs.New(migrations.Current, "current")
				if err != nil {
					t.Fatal(err)
				}
				driver, err := postgres.WithInstance(conn, &postgres.Config{})
				if err != nil {
					t.Fatal(err)
				}
				m, err := migrate.NewWithInstance("internal-baseline", files, "", driver)
				if err != nil {
					t.Fatal(err)
				}
				if err := m.Migrate(1001); err != nil {
					t.Fatal(err)
				}
				seedBaselineIdentity(t, conn)
			}
			if _, err := conn.ExecContext(t.Context(), `
INSERT INTO container_inventory(device_id, observed_at) VALUES (999999, now());
INSERT INTO container_tank(id, device_id, position, first_seen_at, last_seen_at) VALUES (999999, 999999, 1, now(), now());
INSERT INTO container_module(id, tank_id, position, reported_id, first_seen_at, last_seen_at) VALUES (999999, 999999, 1, 'preserved-module', now(), now());`); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err != nil {
					t.Fatal(err)
				}
			}
			var preserved bool
			if err := conn.QueryRowContext(t.Context(), `SELECT reported_id='preserved-module'
AND issues IS NULL AND issues_observed_at IS NULL AND issues_source_times='{}'::jsonb
FROM container_module WHERE id=999999`).Scan(&preserved); err != nil {
				t.Fatal(err)
			}
			if !preserved {
				t.Fatal("upgrade changed existing module identity or initial issue evidence")
			}
		})
	}
}
