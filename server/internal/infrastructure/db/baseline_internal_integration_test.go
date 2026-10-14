package db

import (
	"database/sql"
	"strconv"
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
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

func TestBaselineInternalConvertsCleanPublicBaseline(t *testing.T) {
	requireBaselineDB(t)
	conn, _ := newMigrationBridgeTestDB(t)
	// Apply the real shared baseline with the stock runner, stopping before the
	// private baseline. The connection remains owned by the test cleanup.
	source, err := iofs.New(migrations.Current, "current")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := postgres.WithInstance(conn, &postgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("public-baseline", source, "", driver)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(1000); err != nil {
		t.Fatal(err)
	}
	seedBaselineIdentity(t, conn)
	if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err == nil {
		t.Fatal("startup ran private baseline against existing shared data")
	}
	roundTripBaselineConstraints(t, conn)
	if err := ApplyBaseline(t.Context(), conn, "public", 1000); err != nil {
		t.Fatal(err)
	}
	if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err != nil {
		t.Fatal(err)
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
