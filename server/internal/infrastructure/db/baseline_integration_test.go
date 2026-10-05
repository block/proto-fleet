package db

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/golang-migrate/migrate/v4"
)

func requireBaselineDB(t *testing.T) {
	t.Helper()
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for disposable migration integration tests")
	}
}

func TestBaselineFreshAndUnsafeStates(t *testing.T) {
	requireBaselineDB(t)
	t.Run("fresh and repeated startup", func(t *testing.T) {
		conn, _ := newMigrationBridgeTestDB(t)
		if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err != nil {
			t.Fatal(err)
		}
		if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckBaseline(t.Context(), conn, "target", "", 0); err != nil {
			t.Fatal(err)
		}
		var columns int
		if err := conn.QueryRowContext(t.Context(), `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='schema_migrations'`).Scan(&columns); err != nil {
			t.Fatal(err)
		}
		if columns != 2 {
			t.Fatalf("bookkeeping changed: %d columns", columns)
		}
	})
	t.Run("untracked objects", func(t *testing.T) {
		conn, _ := newMigrationBridgeTestDB(t)
		if _, err := conn.ExecContext(t.Context(), `CREATE TABLE existing_data(id bigint); INSERT INTO existing_data VALUES (7)`); err != nil {
			t.Fatal(err)
		}
		if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err == nil {
			t.Fatal("accepted populated untracked database")
		}
		var tracked bool
		if err := conn.QueryRowContext(t.Context(), `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&tracked); err != nil {
			t.Fatal(err)
		}
		if tracked {
			t.Fatal("refused database was mutated")
		}
	})
}

func seedBaselineIdentity(t *testing.T, conn *sql.DB) {
	t.Helper()
	_, err := conn.ExecContext(t.Context(), `
 INSERT INTO organization(id,org_id,name) VALUES (999999,'fixture-organization','Baseline fixture');
 INSERT INTO "user"(id,user_id,username,password_hash) VALUES (999999,'fixture-user','baseline-user','preserve-this-password-hash');
 INSERT INTO fleet_node(id,org_id,name,identity_pubkey,encryption_pubkey) VALUES (999999,999999,'Existing Node',decode(repeat('01',32),'hex'),decode(repeat('02',32),'hex'));
 INSERT INTO discovered_device(id,org_id,device_identifier,ip_address,port,url_scheme,driver_name) VALUES (999999,999999,'fixture-device','192.0.2.1','80','http','fixture');
 INSERT INTO device(id,org_id,device_identifier,mac_address,discovered_device_id) VALUES (999999,999999,'fixture-device','00:00:00:00:00:01',999999);
 INSERT INTO role(id,name,organization_id) VALUES(999999,'Fixture custom role',999999);
 INSERT INTO permission(id,key,description) VALUES(999999,'fixture:permission','Fixture permission');
 INSERT INTO role_permission(role_id,permission_id) VALUES(999999,999999);
 INSERT INTO user_organization(id,user_id,organization_id,role_id) VALUES(999999,999999,999999,999999);
 INSERT INTO user_organization_role(id,user_id,organization_id,role_id,scope_type) VALUES(999999,999999,999999,999999,'org');
 INSERT INTO api_key(id,key_id,name,prefix,key_hash,user_id,organization_id) VALUES(999999,'fixture-key','Fixture key','fixture','preserve-fixture-hash',999999,999999);
 GRANT SELECT ON role TO PUBLIC;
 GRANT SELECT(id) ON device TO PUBLIC;
 ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO PUBLIC;
 INSERT INTO device_pairing(device_id,pairing_status) VALUES (999999,'PAIRED');
 INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES (999999,999999,999999);
 `)
	if err != nil {
		t.Fatal(err)
	}
}

// roundTripBaselineConstraints reproduces pg_dump/restore reparsing CHECK SQL.
// PostgreSQL can move an array's text cast onto its literals without changing
// the constraint. Keep this DB-backed regression independent of a pg_dump binary.
func roundTripBaselineConstraints(t *testing.T, conn *sql.DB) {
	t.Helper()
	_, err := conn.ExecContext(t.Context(), `DO $$
DECLARE c record;
BEGIN
 FOR c IN SELECT n.nspname, t.relname, x.conname, pg_get_constraintdef(x.oid) AS definition
  FROM pg_constraint x JOIN pg_class t ON t.oid=x.conrelid JOIN pg_namespace n ON n.oid=t.relnamespace
  WHERE n.nspname='public' AND x.contype='c'
 LOOP
  EXECUTE format('ALTER TABLE %I.%I DROP CONSTRAINT %I, ADD CONSTRAINT %I %s',
   c.nspname,c.relname,c.conname,c.conname,c.definition);
 END LOOP;
END $$;`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBaselinePublicReconciliation(t *testing.T) {
	requireBaselineDB(t)
	for _, version := range []uint8{152, 153} {
		t.Run(strconv.Itoa(int(version)), func(t *testing.T) {
			conn, config := newMigrationBridgeTestDB(t)
			migrateTestDBTo(t, conn, config.Name, uint(version))
			seedBaselineIdentity(t, conn)
			if version == 153 {
				roundTripBaselineConstraints(t, conn)
			}
			before, err := protectedBaselineData(t.Context(), conn, true)
			if err != nil {
				t.Fatal(err)
			}
			if err = runCurrentMigrations(t.Context(), conn, migrations.Current); err == nil {
				t.Fatal("startup bypassed reconciliation")
			}
			if err = ApplyBaseline(t.Context(), conn, "public", int(version)); err != nil {
				t.Fatal(err)
			}
			after, err := protectedBaselineData(t.Context(), conn, true)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("credentials or Node pairing changed")
			}
			if err = ApplyBaseline(t.Context(), conn, "public", int(version)); err != nil {
				t.Fatalf("repeat: %v", err)
			}
			roundTripBaselineConstraints(t, conn)
			if _, err = CheckBaseline(t.Context(), conn, "target", "", 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBaselineReconciliationRefusesDirtyAndUnknown(t *testing.T) {
	requireBaselineDB(t)
	for _, mutation := range []string{
		`UPDATE schema_migrations SET dirty=true`,
		`ALTER TABLE device ADD COLUMN unexpected_private integer`,
		`DROP INDEX idx_queue_message_pending_created`,
		`ALTER FUNCTION activity_count_label(bigint,text,text) RENAME TO unexpected_function`,
	} {
		t.Run(mutation, func(t *testing.T) {
			conn, config := newMigrationBridgeTestDB(t)
			migrateTestDBTo(t, conn, config.Name, 152)
			if _, err := conn.ExecContext(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
			if err := ApplyBaseline(t.Context(), conn, "public", 152); err == nil {
				t.Fatal("accepted dirty or unknown source")
			}
			var version int
			if err := conn.QueryRowContext(t.Context(), `SELECT version FROM schema_migrations`).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if version != 152 {
				t.Fatal("refusal changed version")
			}
		})
	}
}

func TestBaselineLegacyRunnerRefusesAdoptedVersion(t *testing.T) {
	requireBaselineDB(t)
	conn, config := newMigrationBridgeTestDB(t)
	if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err != nil {
		t.Fatal(err)
	}
	m, _, err := newMigrator(conn, config)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Up(); err == nil || err == migrate.ErrNoChange {
		t.Fatal("legacy source accepted adopted version")
	}
	if _, err = CheckBaseline(t.Context(), conn, "target", "", 0); err != nil {
		t.Fatal(err)
	}
}

func TestBaselineRefusesMalformedEmptyTrackingTable(t *testing.T) {
	requireBaselineDB(t)
	conn, _ := newMigrationBridgeTestDB(t)
	if _, err := conn.ExecContext(t.Context(), `CREATE TABLE schema_migrations(version bigint PRIMARY KEY, dirty boolean NOT NULL, unexpected text)`); err != nil {
		t.Fatal(err)
	}
	if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err == nil {
		t.Fatal("accepted altered bookkeeping structure")
	}
	var objects int
	if err := conn.QueryRowContext(t.Context(), `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name<>'schema_migrations'`).Scan(&objects); err != nil {
		t.Fatal(err)
	}
	if objects != 0 {
		t.Fatal("refusal ran fresh-install baseline")
	}
}

func TestBaselineLockSerializesAndRecoversAfterCancellation(t *testing.T) {
	requireBaselineDB(t)
	conn, _ := newMigrationBridgeTestDB(t)
	held := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- withBaselineLock(t.Context(), conn, func(*sql.Conn) error {
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case err := <-finished:
		t.Fatalf("initial lock failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	ran := false
	err := withBaselineLock(ctx, conn, func(*sql.Conn) error { ran = true; return nil })
	close(release)
	if firstErr := <-finished; firstErr != nil {
		t.Fatal(firstErr)
	}
	if err == nil || ran {
		t.Fatal("concurrent transition crossed held migration lock")
	}
	if err := withBaselineLock(t.Context(), conn, func(*sql.Conn) error { return nil }); err != nil {
		t.Fatalf("cancelled waiter left lock unusable: %v", err)
	}
}

// Future migrations do not require edits to the frozen baseline assertions.
func TestCheckpointMigrations(t *testing.T) {
	requireBaselineDB(t)
	files := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.Current, "current")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := "current/" + entry.Name()
		body, err := fs.ReadFile(migrations.Current, path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = &fstest.MapFile{Data: body}
	}
	files["current/001002_shared_probe.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE checkpoint_probe(id bigint);")}
	t.Run("ordinary framework upgrade and repeat", func(t *testing.T) {
		conn, _ := newMigrationBridgeTestDB(t)
		if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := runCurrentMigrations(t.Context(), conn, files); err != nil {
				t.Fatal(err)
			}
		}
		status, err := readBaselineStatus(t.Context(), conn)
		if err != nil || status.Version != 1002 || status.Dirty {
			t.Fatalf("status=%+v error=%v", status, err)
		}
		var exists bool
		if err := conn.QueryRowContext(t.Context(), "SELECT to_regclass('checkpoint_probe') IS NOT NULL").Scan(&exists); err != nil || !exists {
			t.Fatalf("migration missing: %v", err)
		}
		if _, err := checkBaselineStartup(t.Context(), conn, migrations.Current); err == nil {
			t.Fatal("older release accepted future database")
		}
	})
}
