package db

import (
	"strconv"
	"testing"

	"github.com/block/proto-fleet/server/migrations"
)

func TestBaselineInternalReconciliation(t *testing.T) {
	requireBaselineDB(t)
	// Cover both bounded legacy paths, the snapshot boundary and both direct entry points.
	for _, version := range []uint8{150, 152, 153, 154, 158} {
		t.Run(strconv.Itoa(int(version)), func(t *testing.T) {
			conn, config := newMigrationBridgeTestDB(t)
			migrateTestDBTo(t, conn, config.Name, uint(version))
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
	conn, _ := newMigrationBridgeTestDB(t)
	baselinePublicFixture(t, conn, 153)
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
	baselinePublicFixture(t, conn, 153)
	if _, err := conn.ExecContext(t.Context(), `UPDATE schema_migrations SET version=1000`); err != nil {
		t.Fatal(err)
	}
	if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err == nil {
		t.Fatal("startup ran private baseline against existing shared data")
	}
	if err := ApplyBaseline(t.Context(), conn, "public", 1000); err != nil {
		t.Fatal(err)
	}
	if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err != nil {
		t.Fatal(err)
	}
}
