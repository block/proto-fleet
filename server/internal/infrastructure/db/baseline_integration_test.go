package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
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

// The public sequence is the shared subset of legacy files. The two repositories
// deliberately retain their original numbering; this mapping is test-only.
func baselinePublicFixture(t *testing.T, conn *sql.DB, version int) {
	t.Helper()
	if version < 0 {
		t.Fatal("public fixture version must be nonnegative")
	}
	entries, err := fs.ReadDir(migrations.Migrations, ".")
	if err != nil {
		t.Fatal(err)
	}
	_, internalErr := fs.Stat(migrations.Current, "current/001001_internal_baseline.up.sql")
	isInternal := internalErr == nil
	sourceFiles := fstest.MapFS{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		n, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			t.Fatal(err)
		}
		publicVersion := n
		if isInternal && n >= 150 {
			mapped, ok := map[int]int{151: 150, 155: 151, 156: 152, 157: 153}[n]
			if !ok {
				continue
			}
			publicVersion = mapped
		}
		data, err := fs.ReadFile(migrations.Migrations, name)
		if err != nil {
			t.Fatal(err)
		}
		mapped := fmt.Sprintf("%06d_%s", publicVersion, strings.SplitN(name, "_", 2)[1])
		sourceFiles[mapped] = &fstest.MapFile{Data: data}
	}
	source, err := iofs.New(sourceFiles, ".")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := postgres.WithInstance(conn, &postgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("public-fixture", source, "", driver)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(uint(version)); err != nil {
		t.Fatal(err)
	}
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

func TestBaselinePublicReconciliation(t *testing.T) {
	requireBaselineDB(t)
	for _, version := range []int{152, 153} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			conn, _ := newMigrationBridgeTestDB(t)
			baselinePublicFixture(t, conn, version)
			seedBaselineIdentity(t, conn)
			before, err := protectedBaselineData(t.Context(), conn, true)
			if err != nil {
				t.Fatal(err)
			}
			if err = runCurrentMigrations(t.Context(), conn, migrations.Current); err == nil {
				t.Fatal("startup bypassed reconciliation")
			}
			if err = ApplyBaseline(t.Context(), conn, "public", version); err != nil {
				t.Fatal(err)
			}
			after, err := protectedBaselineData(t.Context(), conn, true)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("credentials or Node pairing changed")
			}
			if err = ApplyBaseline(t.Context(), conn, "public", version); err != nil {
				t.Fatalf("repeat: %v", err)
			}
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
			conn, _ := newMigrationBridgeTestDB(t)
			baselinePublicFixture(t, conn, 152)
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

func TestBaselineInterruptedReconciliationRollsBack(t *testing.T) {
	requireBaselineDB(t)
	if _, err := fs.Stat(migrations.Current, "current/001001_internal_baseline.up.sql"); err != nil {
		t.Skip("private transition interruption exercised internally")
	}
	conn, _ := newMigrationBridgeTestDB(t)
	baselinePublicFixture(t, conn, 152)
	// Block the event table after the shared ALTER has executed. Cancellation
	// must roll back both that shared work and the untouched migration row.
	blocker, err := conn.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = blocker.ExecContext(t.Context(), `LOCK TABLE release_channel IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if err = ApplyBaseline(ctx, conn, "public", 152); err == nil {
		t.Fatal("interrupted transition succeeded")
	}
	if err = blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = CheckBaseline(t.Context(), conn, "source", "public", 152); err != nil {
		t.Fatalf("source not restored: %v", err)
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
	files["current/001003_internal_checkpoint.up.sql"] = &fstest.MapFile{Data: []byte("-- requires-shared: 1002\nSELECT 1;")}
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
		if err != nil || status.Version != 1003 || status.Dirty {
			t.Fatalf("status=%+v error=%v", status, err)
		}
		var exists bool
		if err := conn.QueryRowContext(t.Context(), "SELECT to_regclass('checkpoint_probe') IS NOT NULL").Scan(&exists); err != nil || !exists {
			t.Fatalf("migration missing: %v", err)
		}
		if _, err := checkBaselineStartup(t.Context(), conn, migrations.Current); err == nil {
			t.Fatal("older release accepted future database")
		}
		publicFiles := fstest.MapFS{}
		for path, file := range files {
			if !strings.Contains(path, "_internal_") {
				publicFiles[path] = file
			}
		}
		if err := runCurrentMigrations(t.Context(), conn, publicFiles); err == nil {
			t.Fatal("public application accepted private checkpoint")
		}
	})
	t.Run("interrupted shared step cannot impersonate public", func(t *testing.T) {
		conn, _ := newMigrationBridgeTestDB(t)
		if err := runCurrentMigrations(t.Context(), conn, migrations.Current); err != nil {
			t.Fatal(err)
		}
		checkpoints, _, err := migrationCheckpoints(files)
		if err != nil {
			t.Fatal(err)
		}
		driver, err := postgres.WithInstance(conn, &postgres.Config{})
		if err != nil {
			t.Fatal(err)
		}
		source, err := iofs.New(files, "current")
		if err != nil {
			t.Fatal(err)
		}
		m, err := migrate.NewWithInstance("interruption", source, "", stopAfterSharedCheckpoint{checkpointDriver{driver, checkpoints}})
		if err != nil {
			t.Fatal(err)
		}
		if err = m.Up(); err == nil {
			t.Fatal("did not interrupt after shared step")
		}
		status, err := readBaselineStatus(t.Context(), conn)
		if err != nil || status.Version != 1002 || !status.Dirty {
			t.Fatalf("unsafe interrupted state: %+v %v", status, err)
		}
		if err := runCurrentMigrations(t.Context(), conn, files); err == nil {
			t.Fatal("internal restart accepted dirty checkpoint")
		}
		// Public files contain the same shared number, but must still reject the
		// interrupted internal database before changing it.
		delete(files, "current/001001_internal_baseline.up.sql")
		delete(files, "current/001001_internal_baseline.down.sql")
		delete(files, "current/001003_internal_checkpoint.up.sql")
		if err := runCurrentMigrations(t.Context(), conn, files); err == nil {
			t.Fatal("public switch accepted dirty shared step")
		}
	})
}

type stopAfterSharedCheckpoint struct{ migratedatabase.Driver }

func (d stopAfterSharedCheckpoint) SetVersion(version int, dirty bool) error {
	if err := d.Driver.SetVersion(version, dirty); err != nil {
		return err
	}
	if version == 1002 && !dirty {
		return fmt.Errorf("simulated process exit between migrations")
	}
	return nil
}
