package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	sqldriver "database/sql/driver"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"

	"github.com/block/proto-fleet/server/migrations"
)

// These are release-owned assertions and fixed reconciliation recipes, never
// a record of what an installation has applied. schema_migrations owns that.
//
//go:embed baseline/*
var baselineFiles embed.FS

// Catalogs admit only verified migration-created and dump/restored forms.
// PostgreSQL can reparse equivalent CHECK casts differently during restore.
type baselineRecipe struct {
	Catalogs []string `json:"catalogs"`
	SQL      []string `json:"sql"`
}

type baselineAssertions struct {
	AddedColumns map[string][]string       `json:"added_columns,omitempty"`
	Preserve     []string                  `json:"preserve"`
	Target       int                       `json:"target"`
	Sources      map[string]baselineRecipe `json:"sources"`
	Targets      []string                  `json:"targets"`
}

// BaselineStatus intentionally contains no credentials, connection strings, or
// data digests. Cloud maintenance checks the exact runtime on both DB members.
type BaselineStatus struct {
	Version int  `json:"version"`
	Dirty   bool `json:"dirty"`
	Runtime struct {
		Postgres    string `json:"postgres"`
		TimescaleDB string `json:"timescaledb"`
		Toolkit     string `json:"timescaledb_toolkit"`
	} `json:"runtime"`
}

func baselineCatalogDigest(catalog map[string]json.RawMessage) string {
	// encoding/json orders map keys. Canonicalize each JSON value as well, so
	// PostgreSQL's JSON whitespace does not become a compatibility decision.
	canonical := make(map[string]any, len(catalog))
	for key, value := range catalog {
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return ""
		}
		canonical[key] = decoded
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func loadBaselineAssertions() (baselineAssertions, error) {
	var assertions baselineAssertions
	data, err := baselineFiles.ReadFile("baseline/assertions.json")
	if err != nil {
		return assertions, fmt.Errorf("read baseline assertions: %w", err)
	}
	if err = json.Unmarshal(data, &assertions); err != nil {
		return assertions, fmt.Errorf("decode baseline assertions: %w", err)
	}
	return assertions, nil
}

type baselineQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readBaselineStatus(ctx context.Context, q baselineQuerier) (BaselineStatus, error) {
	var status BaselineStatus
	var schema string
	if err := q.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		return status, fmt.Errorf("inspect current schema: %w", err)
	}
	if schema != "public" {
		return status, fmt.Errorf("baseline reconciliation requires the qualified public schema, got %q", schema)
	}
	if err := checkMigrationTableShape(ctx, q); err != nil {
		return status, err
	}
	if err := q.QueryRowContext(ctx, `SELECT version, dirty FROM public.schema_migrations`).Scan(&status.Version, &status.Dirty); err != nil {
		return status, fmt.Errorf("read existing migration row: %w", err)
	}
	var rows int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM public.schema_migrations`).Scan(&rows); err != nil {
		return status, fmt.Errorf("count migration rows: %w", err)
	}
	if rows != 1 {
		return status, fmt.Errorf("expected exactly one migration row, got %d", rows)
	}
	if err := q.QueryRowContext(ctx, `SELECT split_part(current_setting('server_version'),' ',1),
		(SELECT extversion FROM pg_extension WHERE extname='timescaledb'),
		(SELECT extversion FROM pg_extension WHERE extname='timescaledb_toolkit')`).Scan(&status.Runtime.Postgres, &status.Runtime.TimescaleDB, &status.Runtime.Toolkit); err != nil {
		return status, fmt.Errorf("inspect database runtime: %w", err)
	}
	return status, nil
}

func checkMigrationTableShape(ctx context.Context, q baselineQuerier) error {
	var columns int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='schema_migrations' AND ((column_name='version' AND data_type='bigint' AND is_nullable='NO') OR (column_name='dirty' AND data_type='boolean' AND is_nullable='NO'))`).Scan(&columns); err != nil {
		return fmt.Errorf("inspect migration column types: %w", err)
	}
	var totalColumns int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='schema_migrations'`).Scan(&totalColumns); err != nil {
		return fmt.Errorf("count migration columns: %w", err)
	}
	if columns != 2 || totalColumns != 2 {
		return fmt.Errorf("unexpected schema_migrations structure; refusing baseline operation")
	}
	return nil
}

func readBaselineCatalog(ctx context.Context, q baselineQuerier) (string, error) {
	query, err := baselineFiles.ReadFile("baseline/catalog.sql")
	if err != nil {
		return "", fmt.Errorf("read catalog query: %w", err)
	}
	var raw []byte
	if err = q.QueryRowContext(ctx, string(query)).Scan(&raw); err != nil {
		return "", fmt.Errorf("inspect application schema: %w", err)
	}
	var catalog map[string]json.RawMessage
	if err = json.Unmarshal(raw, &catalog); err != nil {
		return "", fmt.Errorf("decode application catalog: %w", err)
	}
	if err := checkBaselineGrants(ctx, q); err != nil {
		return "", err
	}
	digest := baselineCatalogDigest(catalog)
	if digest == "" {
		return "", fmt.Errorf("invalid application catalog")
	}
	return digest, nil
}

func checkBaselineState(ctx context.Context, q baselineQuerier, state, source string, version int) (BaselineStatus, error) {
	if state == "startup" {
		return checkBaselineStartup(ctx, q, migrations.Current)
	}
	status, err := readBaselineStatus(ctx, q)
	if err != nil {
		return status, err
	}
	if status.Dirty {
		return status, fmt.Errorf("database migration %d is dirty; refusing baseline operation", status.Version)
	}
	assertions, err := loadBaselineAssertions()
	if err != nil {
		return status, err
	}
	digest, err := readBaselineCatalog(ctx, q)
	if err != nil {
		return status, err
	}
	switch state {
	case "source":
		if status.Version != version {
			return status, fmt.Errorf("expected source version %d, found %d", version, status.Version)
		}
		recipe, ok := assertions.Sources[source+"-"+strconv.Itoa(version)]
		if !ok || !slices.Contains(recipe.Catalogs, digest) {
			return status, fmt.Errorf("source schema is not the qualified %s/%d schema; refusing reconciliation", source, version)
		}
	case "target":
		if status.Version != assertions.Target {
			return status, fmt.Errorf("expected target version %d, found %d", assertions.Target, status.Version)
		}
		if slices.Contains(assertions.Targets, digest) {
			return status, nil
		}
		return status, fmt.Errorf("target schema does not match this release; refusing startup")
	default:
		return status, fmt.Errorf("state must be source or target")
	}
	return status, nil
}

// CheckBaseline is read-only and works on the writer or its standby. It does
// not construct a migration driver, create tracking tables, or run SQL files.
func CheckBaseline(ctx context.Context, conn *sql.DB, state, source string, version int) (BaselineStatus, error) {
	//nolint:forbidigo // Catalog/version inspection must use one consistent snapshot.
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return BaselineStatus{}, fmt.Errorf("begin baseline inspection: %w", err)
	}
	defer tx.Rollback()
	if state == "target" {
		status, err := checkBaselineStartup(ctx, tx, migrations.Current)
		if err != nil {
			return status, err
		}
		_, latest, err := migrationCheckpoints(migrations.Current)
		if err != nil {
			return status, err
		}
		if status.Version != latest {
			return status, fmt.Errorf("expected release migration %d, found %d", latest, status.Version)
		}
		return status, nil
	}
	return checkBaselineState(ctx, tx, state, source, version)
}

// withBaselineLock uses the stock driver's exact session advisory lock key.
// Its nested stock-driver acquisition is reentrant on this same connection.
func withBaselineLock(ctx context.Context, pool *sql.DB, run func(*sql.Conn) error) (err error) {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer func() {
		if err != nil {
			_ = conn.Raw(func(any) error { return sqldriver.ErrBadConn })
		}
		_ = conn.Close()
	}()
	var name, schema string
	var standby bool
	if err = conn.QueryRowContext(ctx, `SELECT current_database(),current_schema(),pg_is_in_recovery()`).Scan(&name, &schema, &standby); err != nil {
		return fmt.Errorf("inspect migration connection: %w", err)
	}
	if standby {
		return fmt.Errorf("migration requires the database writer")
	}
	if schema != "public" {
		return fmt.Errorf("baseline migration requires the qualified public schema")
	}
	key, err := migratedatabase.GenerateAdvisoryLockId(name, schema, "schema_migrations")
	if err != nil {
		return fmt.Errorf("derive migration lock key: %w", err)
	}
	lockContext, cancel := context.WithTimeout(ctx, migrate.DefaultLockTimeout)
	defer cancel()
	if _, err = conn.ExecContext(lockContext, `SELECT pg_advisory_lock($1)`, key); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// A cancelled operation still releases its session lock before the
		// connection returns to the application pool.
		var unlocked bool
		unlockContext, cancelUnlock := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelUnlock()
		unlockErr := conn.QueryRowContext(unlockContext, `SELECT pg_advisory_unlock($1)`, key).Scan(&unlocked)
		if unlockErr == nil && !unlocked {
			unlockErr = fmt.Errorf("migration advisory lock was not held")
		}
		if unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("release migration lock: %w", unlockErr))
		}
	}()
	return run(conn)
}

// ApplyBaseline reconciles only enumerated source schemas. Every schema/data
// assertion and the final UPDATE share a single transaction. The original
// migration row remains intact when SQL or validation fails.
func ApplyBaseline(ctx context.Context, pool *sql.DB, source string, version int) error {
	return withBaselineLock(ctx, pool, func(conn *sql.Conn) error {
		//nolint:forbidigo // One-time DDL, validation and version update are atomic.
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{})
		if err != nil {
			return fmt.Errorf("begin baseline reconciliation: %w", err)
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `LOCK TABLE public.schema_migrations IN EXCLUSIVE MODE`); err != nil {
			return fmt.Errorf("lock migration row: %w", err)
		}
		// Repeating an acknowledged or uncertain completed transition is safe
		// only after the complete target is revalidated.
		if _, targetErr := checkBaselineState(ctx, tx, "target", "", 0); targetErr == nil {
			return nil
		}
		if _, err = checkBaselineState(ctx, tx, "source", source, version); err != nil {
			return err
		}
		assertions, err := loadBaselineAssertions()
		if err != nil {
			return err
		}
		recipe := assertions.Sources[source+"-"+strconv.Itoa(version)]
		tables := make([]string, 0, len(assertions.Preserve))
		for _, table := range assertions.Preserve {
			tables = append(tables, `public."`+strings.ReplaceAll(table, `"`, `""`)+`"`)
		}
		sort.Strings(tables)
		if _, err = tx.ExecContext(ctx, `LOCK TABLE `+strings.Join(tables, ",")+` IN SHARE MODE`); err != nil {
			return fmt.Errorf("lock protected application tables: %w", err)
		}
		settings, err := baselineSettings(ctx, tx)
		if err != nil {
			return err
		}
		before, err := protectedBaselineData(ctx, tx, source == "public")
		if err != nil {
			return err
		}
		for _, path := range recipe.SQL {
			var data []byte
			if strings.HasPrefix(path, "baseline/") {
				data, err = baselineFiles.ReadFile(path)
			} else {
				data, err = fs.ReadFile(migrations.Migrations, path)
			}
			if err != nil {
				return fmt.Errorf("read reconciliation SQL %s: %w", path, err)
			}
			if _, err = tx.ExecContext(ctx, string(data)); err != nil {
				return fmt.Errorf("reconcile %s: %w", path, err)
			}
		}
		after, err := protectedBaselineData(ctx, tx, source == "public")
		if err != nil {
			return err
		}
		afterSettings, err := baselineSettings(ctx, tx)
		if err != nil {
			return err
		}
		for key, value := range settings {
			if afterSettings[key] != value {
				return fmt.Errorf("existing database grants or job schedule changed; reconciliation rolled back")
			}
		}
		if before != after {
			return fmt.Errorf("protected identities or credentials changed; reconciliation rolled back")
		}
		digest, err := readBaselineCatalog(ctx, tx)
		if err != nil {
			return err
		}
		if !slices.Contains(assertions.Targets, digest) {
			return fmt.Errorf("reconciled schema does not match target; rolling back")
		}
		result, err := tx.ExecContext(ctx, `UPDATE public.schema_migrations SET version=$1,dirty=false WHERE version=$2 AND NOT dirty`, assertions.Target, version)
		if err != nil {
			return fmt.Errorf("update migration version: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("verify migration row update: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("source version changed during reconciliation")
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit acknowledgement failed; keep apps stopped, prove this command ended, then check source/target before retrying: %w", err)
		}
		return nil
	})
}

func protectedBaselineData(ctx context.Context, q baselineQuerier, omitAddedColumns bool) (string, error) {
	// These tables are not rewritten by any supported reconciliation recipe.
	// Hashes stay in process memory and are never returned or logged.
	assertions, err := loadBaselineAssertions()
	if err != nil {
		return "", err
	}
	tables := assertions.Preserve
	sort.Strings(tables)
	digest := sha256.New()
	for _, table := range tables {
		var exists bool
		if err := q.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+strconv.Quote(table)).Scan(&exists); err != nil {
			return "", fmt.Errorf("inspect protected table %s: %w", table, err)
		}
		if !exists {
			continue
		}
		var value string
		query := `SELECT md5(COALESCE(string_agg(row_value, '' ORDER BY row_value),'')) FROM (SELECT (to_jsonb(t)-$1::text[])::text AS row_value FROM public."` + table + `" t) rows`
		columns := []string{}
		if omitAddedColumns {
			columns = append(columns, assertions.AddedColumns[table]...)
		}
		if err := q.QueryRowContext(ctx, query, columns).Scan(&value); err != nil {
			return "", fmt.Errorf("verify protected table %s: %w", table, err)
		}
		_, _ = digest.Write([]byte(table + ":" + value))
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func checkBaselineStartup(ctx context.Context, q baselineQuerier, files fs.FS) (BaselineStatus, error) {
	var status BaselineStatus
	var tracked bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&tracked); err != nil {
		return status, fmt.Errorf("inspect migration tracking table: %w", err)
	}
	var rows int
	if tracked {
		if err := checkMigrationTableShape(ctx, q); err != nil {
			return status, err
		}
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM public.schema_migrations`).Scan(&rows); err != nil {
			return status, fmt.Errorf("count startup migration rows: %w", err)
		}
	}
	if rows == 0 {
		var count int
		if err := q.QueryRowContext(ctx, `SELECT
   (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname<>'schema_migrations' AND c.relkind IN ('r','p','v','m','S') AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.deptype='e')) +
   (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid='pg_proc'::regclass AND d.objid=p.oid AND d.deptype='e')) +
   (SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public' AND t.typtype IN ('e','d') AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid='pg_type'::regclass AND d.objid=t.oid AND d.deptype='e'))`).Scan(&count); err != nil {
			return status, fmt.Errorf("inspect untracked application objects: %w", err)
		}
		if count != 0 {
			return status, fmt.Errorf("untracked database contains application objects; refusing fresh baseline")
		}
		status.Version = -1
		return status, nil
	}
	status, err := readBaselineStatus(ctx, q)
	if err != nil {
		return status, err
	}
	if status.Dirty {
		return status, fmt.Errorf("database migration %d is dirty", status.Version)
	}
	checkpoints, _, err := migrationCheckpoints(files)
	if err != nil {
		return status, err
	}
	if !checkpoints[status.Version] {
		return status, fmt.Errorf("version %d is not a checkpoint owned by this application; use the offline transition procedure", status.Version)
	}
	// Exact catalog validation belongs to baseline adoption only. Future
	// migrations use the stock runner and repository-owned checkpoints.
	if status.Version <= 1001 {
		return checkBaselineState(ctx, q, "target", "", 0)
	}
	return status, nil
}

func checkBaselineGrants(ctx context.Context, q baselineQuerier) error {
	var exists bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='grafana_ha_ro')`).Scan(&exists); err != nil {
		return fmt.Errorf("inspect Grafana database role: %w", err)
	}
	if !exists {
		return nil
	}
	var valid bool
	if err := q.QueryRowContext(ctx, `SELECT has_table_privilege('grafana_ha_ro','public.notification_metric_sample','SELECT') AND has_table_privilege('grafana_ha_ro','public.fleet_active_organization','SELECT') AND (SELECT bool_and(has_column_privilege('grafana_ha_ro','public.fleet_node',name,'SELECT')) FROM unnest(ARRAY['org_id','id','last_seen_at','enrollment_status','deleted_at']) AS columns(name))`).Scan(&valid); err != nil {
		return fmt.Errorf("inspect Grafana database grants: %w", err)
	}
	if !valid {
		return fmt.Errorf("migration-owned Grafana grants are missing; refusing baseline operation")
	}
	return nil
}

func baselineSettings(ctx context.Context, q baselineQuerier) (map[string]string, error) {
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT COALESCE(jsonb_object_agg(key,value),'{}'::jsonb) FROM (
 SELECT 'job/'||job_id::text key,jsonb_build_array(schedule_interval,max_runtime,max_retries,retry_period,scheduled)::text value FROM timescaledb_information.jobs
 UNION ALL SELECT 'acl/'||c.oid::text,COALESCE(c.relacl::text,'') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'
 UNION ALL SELECT 'column-acl/'||c.oid::text||'/'||a.attnum::text,COALESCE(a.attacl::text,'') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid WHERE n.nspname='public' AND a.attnum>0 AND NOT a.attisdropped
 UNION ALL SELECT 'default-acl/'||oid::text,defaclacl::text FROM pg_default_acl
 ) settings`).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("inspect database grants and job schedules: %w", err)
	}
	var settings map[string]string
	if err = json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("decode database grants and job schedules: %w", err)
	}
	return settings, nil
}
