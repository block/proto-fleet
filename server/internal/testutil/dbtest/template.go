package dbtest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/fs"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/migrations"
)

// The per-test database harness used to replay every migration into every test
// database. With 140+ migrations and hundreds of DB-backed tests that cost
// ~2s per test and dominated the package runtime (the sqlstores package spent
// ~500s of its 600s Go test timeout inside migrate.Up()).
//
// Instead we migrate once into a template database and let PostgreSQL clone it
// at the filesystem level for each test (CREATE DATABASE ... TEMPLATE), which
// is O(schema size) rather than O(number of migrations) and takes tens of
// milliseconds.
//
// The template is keyed by a fingerprint of the embedded migration set, so
// adding or editing a migration transparently builds a new template and
// abandons the old one. Preparation is guarded by a PostgreSQL advisory lock so
// that the many test binaries `go test ./...` runs in parallel cooperate: the
// first one builds the template, the rest wait and reuse it.
const (
	// templateDBPrefix namespaces template databases so stale ones from earlier
	// migration sets can be identified and dropped.
	templateDBPrefix = "fleet_test_tmpl_"

	// templateBuildPrefix namespaces the staging database a template is built
	// under. Publication is a rename from this name to templateDBPrefix, so the
	// published name never exists in a partial state — see buildTemplateDatabase.
	templateBuildPrefix = "fleet_test_bld_"

	// templateAdvisoryLockNamespace seeds the per-template advisory lock keys, so
	// they cannot collide with advisory locks taken for other purposes.
	templateAdvisoryLockNamespace = "dbtest-template-preparation:7264051893001"

	// templateFingerprintLength is how much of the migration-set hash goes into
	// the template name; 12 hex chars is collision-safe here and keeps the name
	// well inside PostgreSQL's 63-character identifier limit.
	templateFingerprintLength = 12

	// templatePrepareBudget is the total time this process will ever spend trying
	// to prepare a template, across all attempts and all tests. It is a single
	// process-wide budget rather than a per-attempt timeout: N attempts of N
	// minutes each could otherwise add up past Go's 10m package timeout and
	// reproduce the very hang this harness exists to avoid.
	templatePrepareBudget = 3 * time.Minute

	// templatePrepareTimeout caps a single attempt (in practice: waiting for
	// another process's advisory lock). The effective deadline is whichever of
	// this and the remaining budget is smaller.
	templatePrepareTimeout = 90 * time.Second

	// templateLockTimeout bounds how long migrating the template waits on any
	// single lock. db.ConnectAndMigrate is not context-aware (it runs migrations
	// on a background context), so the deadline is enforced inside PostgreSQL
	// instead: it is set on the template database, which every session that
	// migrates it inherits. A migration wedged behind a TimescaleDB catalog lock
	// then fails with lock_not_available, preparation reports an error, and the
	// caller falls back to migrating its own database rather than every DB-backed
	// package hanging on the shared template.
	//
	// Only lock_timeout is set, not statement_timeout: migrations are legitimately
	// slow, and capping their total runtime would fail valid work.
	templateLockTimeout = time.Minute

	// templateMaxAge bounds how long a template may be *reused*. Cloning copies
	// rows a migration seeded with CURRENT_TIMESTAMP — e.g. the
	// curtailment_reconciler_heartbeat row from 000042 — so a clone inherits the
	// template's build time rather than "now", as replaying migrations per test
	// used to give. Rebuilding on a short cadence keeps that skew small without
	// having to enumerate (and keep up with) every time-dependent seed.
	//
	// The cost is one ~1s rebuild per interval per process, so this trades a
	// negligible amount of the speedup for bounded staleness.
	templateMaxAge = 15 * time.Minute

	// templateRebuildWaitTimeout is the total time one test database will wait
	// for other processes' rebuilds of its template, shared across every wait so
	// repeated waits cannot add up. It has to exceed a migration run, since that
	// is what we are waiting for, while staying well inside the package timeout.
	templateRebuildWaitTimeout = 90 * time.Second

	// templateStaleAfter is how old a template must be before another run may
	// sweep it. Cleanup is bounded by age (recorded in the database comment when
	// the template is sealed) rather than "different fingerprint", so two
	// checkouts on different migration sets cannot delete each other's live
	// templates and ping-pong rebuilds.
	templateStaleAfter = 24 * time.Hour

	// templateCommentPrefix marks a database as ours and carries its creation
	// time; anything without it is left alone.
	templateCommentPrefix = "dbtest-template created="

	// templateDetachTimeout bounds how long we wait for sessions to leave the
	// template after terminating them.
	templateDetachTimeout  = 30 * time.Second
	templateDetachInterval = 100 * time.Millisecond

	// templateRebuildWaitInterval is the poll interval while waiting for
	// somebody else's rebuild to publish.
	templateRebuildWaitInterval = 100 * time.Millisecond

	// templateRecoveryMaxAttempts bounds how many times one test database may
	// repair its template situation (rebuild a swept template, wait out a live
	// build, abandon cloning). Separate from the DDL retry budget: recoveries
	// change the situation rather than retrying the same thing.
	templateRecoveryMaxAttempts = 4
)

// templateNamePattern is the exact shape of a template database name. It gates
// both the cleanup query and the identifiers we interpolate into DDL, so a
// catalog row can never steer either.
var templateNamePattern = regexp.MustCompile(`^` + templateDBPrefix + `[0-9a-f]{` +
	strconv.Itoa(templateFingerprintLength) + `}$`)

var (
	templateMu sync.Mutex
	// templateDBName is the prepared template's name, valid only while
	// templatePrepared is true.
	templateDBName string
	// templatePrepared records a *successful* preparation. Failures are not
	// cached, so a later test can retry: a transient blip while the first test
	// happens to run must not force every remaining test in the binary back onto
	// the slow migrate-everything path.
	templatePrepared bool
	// templateFailures counts consecutive failures so a genuinely broken setup
	// stops paying the preparation cost on every test.
	templateFailures int
	// templateBudgetDeadline is when this process stops trying, set on the first
	// attempt. Shared across tests so the total cost is bounded no matter how
	// many tests ask.
	templateBudgetDeadline time.Time
)

// templatePrepareMaxAttempts bounds retries across tests once preparation keeps
// failing; after this we accept the fallback for the rest of the process.
const templatePrepareMaxAttempts = 3

// templateDatabase returns the name of a fully migrated template database that
// test databases can be cloned from, preparing it on first use. It returns ""
// if no template could be prepared, in which case the caller should create an
// empty database and migrate it directly.
func templateDatabase(t *testing.T, adminConfig *db.Config) string {
	t.Helper()

	templateMu.Lock()
	defer templateMu.Unlock()

	if templatePrepared {
		return templateDBName
	}
	if templateFailures >= templatePrepareMaxAttempts {
		return ""
	}

	if templateBudgetDeadline.IsZero() {
		templateBudgetDeadline = time.Now().Add(templatePrepareBudget)
	}

	attemptTimeout, withinBudget := prepareAttemptTimeout(templateBudgetDeadline, time.Now())
	if !withinBudget {
		// Budget spent: stop trying for the rest of the process rather than
		// letting queued tests keep paying for it.
		templateFailures = templatePrepareMaxAttempts
		t.Logf("template preparation budget of %s exhausted; "+
			"migrating test databases directly for the rest of this process", templatePrepareBudget)
		return ""
	}

	name := templateNameFor(adminConfig)

	// Deliberately not t.Context(): the template outlives the test that
	// happens to build it, and cancelling that test must not abandon a
	// half-migrated template for every other test in the binary.
	// nolint: usetesting
	ctx, cancel := context.WithTimeout(context.Background(), attemptTimeout)
	defer cancel()

	// The very first connection of the run can land while the server is still
	// starting or recovering; wait it out the same way the admin DDL path does
	// rather than burning an attempt on it.
	waitForServerReady(ctx, t, adminConfig)

	start := time.Now()
	if err := prepareTemplateDatabase(ctx, adminConfig, name); err != nil {
		if ctx.Err() != nil {
			// A deadline failure means time, not luck, ran out: retrying cannot
			// help, so open the circuit now instead of after N attempts.
			templateFailures = templatePrepareMaxAttempts
			t.Logf("template database %s hit its %s preparation deadline (%v); "+
				"migrating test databases directly for the rest of this process",
				name, attemptTimeout, err)
			return ""
		}

		templateFailures++
		t.Logf("could not prepare template database %s (attempt %d/%d: %v); "+
			"migrating this test database directly",
			name, templateFailures, templatePrepareMaxAttempts, err)
		return ""
	}

	templateDBName = name
	templatePrepared = true
	templateFailures = 0
	t.Logf("template database %s ready in %s", name, time.Since(start))

	return templateDBName
}

// invalidateTemplateDatabase forgets the cached template so the next caller
// rebuilds it. Used when a clone reports the source is gone — a concurrent run
// on a different migration set may have swept it as stale.
func invalidateTemplateDatabase(name string) {
	templateMu.Lock()
	defer templateMu.Unlock()

	if templatePrepared && templateDBName == name {
		templatePrepared = false
		templateDBName = ""
	}
}

// disableTemplateCloning gives up on cloning for the rest of the process. Used
// for failures that retrying cannot fix, such as lacking permission to copy the
// template: without this every remaining test would pay for a clone attempt that
// is guaranteed to fail before falling back.
func disableTemplateCloning() {
	templateMu.Lock()
	defer templateMu.Unlock()

	templatePrepared = false
	templateDBName = ""
	templateFailures = templatePrepareMaxAttempts
}

// prepareTemplateDatabase makes sure a migrated template database exists for the
// current migration set. It is safe to call concurrently from separate
// processes: the advisory lock serialises preparation, and the caller that finds
// a ready template returns immediately.
func prepareTemplateDatabase(ctx context.Context, adminConfig *db.Config, name string) error {
	adminDB, err := db.ConnectToDatabase(adminConfig)
	if err != nil {
		return fmt.Errorf("connect to admin database: %w", err)
	}
	defer adminDB.Close()

	// Advisory locks are session-scoped, so pin a single connection rather than
	// letting the pool hand the unlock to a different session.
	adminConn, err := adminDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("pin admin connection: %w", err)
	}
	defer adminConn.Close()

	lockKey := templateLockKey(name)
	if _, err := adminConn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("acquire template advisory lock: %w", err)
	}
	defer func() {
		_, _ = adminConn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	reusable, err := templateIsReusable(ctx, adminConn, name)
	if err != nil {
		return err
	}
	if reusable {
		return nil
	}

	// Absent, aged out, or left half-built by an interrupted run: build a
	// replacement under a staging name and publish it with a rename.
	if err := buildTemplateDatabase(ctx, adminConn, adminConfig, name); err != nil {
		return err
	}

	dropStaleTemplateDatabases(ctx, adminConn, name)
	return nil
}

// buildTemplateDatabase migrates a fresh template under a staging name and only
// then renames it to the published name.
//
// The rename is what makes publication atomic. Building in place would leave the
// published name pointing at a database that is empty (before the migration
// connects) or dirty (after an interrupted build), and PostgreSQL happily clones
// either of those whenever no session happens to be attached: an empty clone
// silently replays every migration, and a clone of a golang-migrate "dirty"
// database fails outright. With a staging name, the published name only ever
// exists fully migrated and sealed.
func buildTemplateDatabase(
	ctx context.Context,
	adminConn *sql.Conn,
	adminConfig *db.Config,
	name string,
) error {
	staging := templateBuildPrefix + strings.TrimPrefix(name, templateDBPrefix)

	// Any leftover staging database belongs to a dead build of this same
	// template, whose lock we now hold, so it is ours to discard.
	if err := tryDropTestDatabase(ctx, adminConfig, staging); err != nil {
		return fmt.Errorf("drop stale template build: %w", err)
	}
	if _, err := adminConn.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(staging)); err != nil {
		return fmt.Errorf("create template build database: %w", err)
	}

	// Bound the migrating session inside PostgreSQL, since we cannot cancel it
	// from Go. These are the only guarantees that do not depend on the watchdog
	// below succeeding: whatever happens, the server itself gives up eventually.
	// Milliseconds, not Duration.String(): PostgreSQL rejects Go's "1m0s" form.
	for setting, limit := range map[string]time.Duration{
		"lock_timeout": templateLockTimeout,
		// Derived from the per-attempt deadline. Migrations run against an empty
		// database and take milliseconds each, so a single statement outliving
		// this is wedged, not slow.
		"statement_timeout": templatePrepareTimeout,
	} {
		if _, err := adminConn.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE %s SET %s = %s",
			quoteIdentifier(staging), setting,
			quoteLiteral(fmt.Sprintf("%dms", limit.Milliseconds())))); err != nil {
			abandonTemplate(adminConfig, staging)
			return fmt.Errorf("set template build %s: %w", setting, err)
		}
	}

	if err := migrateTemplateDatabase(ctx, adminConfig, staging); err != nil {
		// Leave nothing half-migrated behind for the next run to trust.
		abandonTemplate(adminConfig, staging)
		return err
	}

	// Blocking connections is both a correctness guard and the readiness
	// marker: CREATE DATABASE ... TEMPLATE fails while any session is connected
	// to the source, and a template is only ever published in this state.
	if _, err := adminConn.ExecContext(ctx,
		"ALTER DATABASE "+quoteIdentifier(staging)+" WITH ALLOW_CONNECTIONS false"); err != nil {
		abandonTemplate(adminConfig, staging)
		return fmt.Errorf("seal template database: %w", err)
	}

	// Creating the extension started a TimescaleDB background worker scheduler
	// inside the database, and that counts as a connected session: every clone
	// would fail with "source database is being accessed by other users", and
	// the rename below would fail too. Evict it now that ALLOW_CONNECTIONS false
	// stops the launcher reattaching.
	if err := detachTemplateSessions(ctx, adminConn, staging); err != nil {
		abandonTemplate(adminConfig, staging)
		return err
	}

	// Ownership metadata: marks the database as ours and records when it was
	// built, which is what makes age-bounded cleanup possible. Comments follow
	// the object, so this survives the rename.
	if _, err := adminConn.ExecContext(ctx, fmt.Sprintf("COMMENT ON DATABASE %s IS %s",
		quoteIdentifier(staging),
		quoteLiteral(templateCommentPrefix+time.Now().UTC().Format(time.RFC3339Nano)))); err != nil {
		abandonTemplate(adminConfig, staging)
		return fmt.Errorf("record template metadata: %w", err)
	}

	// Publish. Dropping the old name is what a concurrent clone may notice (it
	// gets "database does not exist" and rebuilds or waits, both handled),
	// whereas a partially built database under the published name would be
	// cloned silently. The rename itself is atomic.
	if err := tryDropTestDatabase(ctx, adminConfig, name); err != nil {
		abandonTemplate(adminConfig, staging)
		return fmt.Errorf("drop superseded template: %w", err)
	}
	if _, err := adminConn.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE %s RENAME TO %s",
		quoteIdentifier(staging), quoteIdentifier(name))); err != nil {
		abandonTemplate(adminConfig, staging)
		return fmt.Errorf("publish template database: %w", err)
	}

	return nil
}

// templateIsReusable reports whether an existing template can be cloned as-is.
// Two conditions must hold:
//
//   - connections are disabled, which only happens after migrations succeed, so
//     a database that still allows them is incomplete and must be rebuilt;
//   - it was built within templateMaxAge, so clones do not inherit badly stale
//     CURRENT_TIMESTAMP seed data;
//   - it is owned by the role we are connecting as, since PostgreSQL refuses to
//     copy another role's database unless we are a superuser.
//
// A template with no recognisable creation stamp (an older format, or one built
// before stamping existed) is rebuilt rather than trusted.
func templateIsReusable(ctx context.Context, adminConn *sql.Conn, name string) (bool, error) {
	var allowConnections bool
	var ownedByCurrentRole bool
	var comment sql.NullString
	err := adminConn.QueryRowContext(ctx, `
		SELECT datallowconn,
		       pg_catalog.pg_get_userbyid(datdba) = current_user,
		       shobj_description(oid, 'pg_database')
		FROM pg_database
		WHERE datname = $1
	`, name).Scan(&allowConnections, &ownedByCurrentRole, &comment)
	switch {
	case err == nil:
		if allowConnections || !ownedByCurrentRole {
			return false, nil
		}
		created, ok := templateCreatedAt(comment)
		return ok && time.Since(created) <= templateMaxAge, nil
	case strings.Contains(err.Error(), sql.ErrNoRows.Error()):
		return false, nil
	default:
		return false, fmt.Errorf("inspect template database: %w", err)
	}
}

// templateCreatedAt parses the creation time we stamp into a template's comment.
func templateCreatedAt(comment sql.NullString) (time.Time, bool) {
	if !comment.Valid || !strings.HasPrefix(comment.String, templateCommentPrefix) {
		return time.Time{}, false
	}

	created, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(comment.String, templateCommentPrefix))
	if err != nil {
		return time.Time{}, false
	}
	return created, true
}

// abandonTemplate drops a template that failed to build. It deliberately does
// not reuse the preparation context: the most common reason to get here is that
// very context expiring, and a cleanup on a dead context silently leaves a
// half-built template behind. Leaving one is not a correctness problem (an
// unsealed template is treated as incomplete and rebuilt), but it wastes disk
// until the next run.
func abandonTemplate(adminConfig *db.Config, name string) {
	// nolint: usetesting
	ctx, cancel := context.WithTimeout(context.Background(), templateDetachTimeout)
	defer cancel()

	_ = tryDropTestDatabase(ctx, adminConfig, name)
}

// prepareAttemptTimeout returns how long the next preparation attempt may take:
// the smaller of the per-attempt cap and what is left of the process-wide
// budget. The bool reports whether any budget remains at all.
func prepareAttemptTimeout(deadline time.Time, now time.Time) (time.Duration, bool) {
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return 0, false
	}
	if remaining > templatePrepareTimeout {
		return templatePrepareTimeout, true
	}
	return remaining, true
}

// migrateTemplateDatabase runs the full migration set against the template and
// closes the connection, leaving no session attached to it.
//
// db.ConnectAndMigrate is not context-aware, so ctx is enforced out-of-band: a
// watchdog terminates the migrating backend if the deadline passes. Together
// with the template's lock_timeout that bounds both lock waits and any other
// stall, and it is what lets preparation fail (and callers fall back) instead of
// hanging until Go's package timeout.
func migrateTemplateDatabase(ctx context.Context, adminConfig *db.Config, name string) error {
	templateConfig := *adminConfig
	templateConfig.Name = name

	migrationDone := make(chan struct{})
	var watchdog sync.WaitGroup
	var watchdogErr error
	watchdog.Add(1)

	go func() {
		defer watchdog.Done()
		select {
		case <-migrationDone:
		case <-ctx.Done():
			// The parent context is spent, so give the termination its own.
			// nolint: usetesting
			killCtx, cancel := context.WithTimeout(context.Background(), templateDetachTimeout)
			defer cancel()
			// Not evictTemplateSessions: that one deliberately spares unsealed
			// templates, and the template is unsealed for the whole of this
			// migration, so it would never match the backend we need to stop.
			watchdogErr = terminateTemplateBuildSessions(killCtx, adminConfig, name)
		}
	}()

	conn, err := db.ConnectAndMigrate(&templateConfig)
	close(migrationDone)
	// Let a firing watchdog finish before the caller drops the template, so the
	// termination cannot land on an unrelated database that reuses the name.
	watchdog.Wait()

	if err != nil {
		if ctx.Err() != nil {
			if watchdogErr != nil {
				// Worth surfacing: if termination failed, the statement and lock
				// timeouts set on the template are what ended the migration.
				return fmt.Errorf("migrate template database: deadline exceeded "+
					"(terminating the migration backend also failed: %v): %w", watchdogErr, err)
			}
			return fmt.Errorf("migrate template database: deadline exceeded, migration backend terminated: %w", err)
		}
		return fmt.Errorf("migrate template database: %w", err)
	}

	if watchdogErr != nil {
		// The deadline passed and we failed to stop the migration, yet it finished
		// anyway. Treat the template as untrustworthy rather than publishing it.
		return fmt.Errorf("migrate template database: deadline passed and terminating the "+
			"migration backend failed: %w", watchdogErr)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("close template connection: %w", err)
	}
	return nil
}

// detachTemplateSessions terminates every backend attached to the template and
// waits for them to disappear, so the first clone does not race the shutdown.
func detachTemplateSessions(ctx context.Context, adminConn *sql.Conn, name string) error {
	deadline := time.Now().Add(templateDetachTimeout)
	for {
		if _, err := adminConn.ExecContext(ctx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, name); err != nil {
			return fmt.Errorf("terminate template sessions: %w", err)
		}

		var attached int
		if err := adminConn.QueryRowContext(ctx,
			"SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			name).Scan(&attached); err != nil {
			return fmt.Errorf("count template sessions: %w", err)
		}
		if attached == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%d session(s) still attached to template %s after %s",
				attached, name, templateDetachTimeout)
		}

		time.Sleep(templateDetachInterval)
	}
}

// evictTemplateSessions is the best-effort recovery used when a clone loses the
// race to a session that attached to the template anyway (a background worker
// started before ALLOW_CONNECTIONS false took effect, say). Errors are ignored:
// the caller is already in a retry loop and the CREATE DATABASE it retries
// reports the real failure.
//
// It only ever evicts from a *sealed* template. An unsealed one is being built
// right now — by this process or another — and the session attached to it is
// that build's migration. Terminating it would abort a legitimate rebuild,
// which is how two binaries could otherwise take turns killing each other's
// migrations. The datallowconn check is part of the same statement so there is
// no window between deciding and acting.
func evictTemplateSessions(ctx context.Context, adminConfig *db.Config, template string) {
	if template == "" {
		return
	}

	conn, err := db.ConnectToDatabase(adminConfig)
	if err != nil {
		return
	}
	defer conn.Close()

	_, _ = conn.ExecContext(ctx, `
		SELECT pg_terminate_backend(activity.pid)
		FROM pg_stat_activity activity
		JOIN pg_database database ON database.datname = activity.datname
		WHERE activity.datname = $1
		  AND NOT database.datallowconn
		  AND activity.pid <> pg_backend_pid()
	`, template)
}

// terminateTemplateBuildSessions unconditionally disconnects everything attached
// to the template, including an unsealed (in-progress) build.
//
// This is only safe for the process holding the preparation advisory lock, which
// makes the build in question its own: the lock guarantees no other process is
// building this template, and clones never connect to their source. It exists
// solely so the deadline watchdog can stop a migration that Go cannot cancel,
// db.ConnectAndMigrate not being context-aware.
// It reports failures rather than swallowing them: if the migration cannot be
// stopped, the template must not be published, and the caller needs to say so.
// The statement and lock timeouts set on the template are the backstop that does
// not depend on this succeeding.
func terminateTemplateBuildSessions(ctx context.Context, adminConfig *db.Config, name string) error {
	if name == "" {
		return nil
	}

	conn, err := db.ConnectToDatabase(adminConfig)
	if err != nil {
		return fmt.Errorf("connect to admin database to terminate template build: %w", err)
	}
	defer conn.Close()

	var lastErr error
	for attempt := 1; attempt <= migrationMaxRetries; attempt++ {
		var remaining int
		lastErr = conn.QueryRowContext(ctx, `
			WITH terminated AS (
				SELECT pg_terminate_backend(pid) AS stopped
				FROM pg_stat_activity
				WHERE datname = $1 AND pid <> pg_backend_pid()
			)
			SELECT count(*) FILTER (WHERE NOT stopped) FROM terminated
		`, name).Scan(&remaining)
		if lastErr == nil && remaining == 0 {
			return nil
		}
		if ctx.Err() != nil {
			break
		}
		time.Sleep(templateDetachInterval)
	}

	if lastErr != nil {
		return fmt.Errorf("terminate template build sessions: %w", lastErr)
	}
	return fmt.Errorf("terminate template build sessions: sessions still attached to %s", name)
}

// templateBuildState describes what a clone is up against when its source is
// unusable.
type templateBuildState int

const (
	// templateStatePublished covers a sealed template (or one that is missing
	// entirely): nothing about the build is in the way.
	templateStatePublished templateBuildState = iota
	// templateStateBuilding means a process holds the preparation lock and is
	// migrating the template right now. Worth waiting for.
	templateStateBuilding
	// templateStateAbandoned means the template is unsealed but nobody is
	// building it — a build died mid-flight, leaving a partially migrated
	// database that will stay that way forever. Must be rebuilt; waiting would
	// burn the clone's whole budget for nothing.
	templateStateAbandoned
)

// inspectTemplateBuildState distinguishes a live build from an abandoned one.
// Being unsealed is not sufficient evidence of a build: an active builder also
// holds the preparation advisory lock.
func inspectTemplateBuildState(ctx context.Context, adminConfig *db.Config, template string) templateBuildState {
	if template == "" {
		return templateStatePublished
	}

	conn, err := db.ConnectToDatabase(adminConfig)
	if err != nil {
		return templateStatePublished
	}
	defer conn.Close()

	var allowConnections bool
	if err := conn.QueryRowContext(ctx,
		"SELECT datallowconn FROM pg_database WHERE datname = $1", template).Scan(&allowConnections); err != nil {
		return templateStatePublished
	}
	if !allowConnections {
		return templateStatePublished
	}

	if preparationLockHeld(ctx, conn, template) {
		return templateStateBuilding
	}
	return templateStateAbandoned
}

// templateRebuildInProgress reports whether some process is actively rebuilding
// the template, meaning it is worth waiting for.
func templateRebuildInProgress(ctx context.Context, adminConfig *db.Config, template string) bool {
	return inspectTemplateBuildState(ctx, adminConfig, template) == templateStateBuilding
}

// preparationLockHeld reports whether any session is building this specific
// template. PostgreSQL splits a bigint advisory key across pg_locks.classid
// (high 32 bits) and objid (low 32 bits).
func preparationLockHeld(ctx context.Context, conn *sql.DB, template string) bool {
	var held bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_locks
			WHERE locktype = 'advisory'
			  AND granted
			  AND ((classid::bigint << 32) | objid::bigint) = $1
		)
	`, templateLockKey(template)).Scan(&held); err != nil {
		return false
	}
	return held
}

// templateLockKey derives the advisory lock key for one template name.
//
// The key is per template rather than global for two reasons: a global key makes
// every concurrent build serialise even when they are building *different*
// templates (different roles, or different migration sets across checkouts), and
// — worse — it makes "somebody holds the lock" useless as evidence that *this*
// template is being built, which is what tells a live build apart from one that
// died and left the database unsealed.
func templateLockKey(name string) int64 {
	sum := sha256.Sum256([]byte(templateAdvisoryLockNamespace + name))
	// Mask off the sign bit first: the pg_locks reconstruction treats the key as
	// a positive bigint built from two unsigned halves, and masking before the
	// conversion keeps it in int64 range by construction.
	const maxInt64 = uint64(math.MaxInt64)
	// nolint: gosec // masked to 63 bits on the line above, so it always fits.
	return int64(binary.BigEndian.Uint64(sum[:8]) & maxInt64)
}

// waitForTemplateRebuild blocks until the template is sealed (the rebuild
// published it), disappears (the rebuild failed and dropped it), or the shared
// deadline passes. Cloning cannot proceed while a rebuild holds the source, and
// the clone retry budget is far shorter than a full migration run, so waiting
// here is what keeps a concurrent rebuild from failing tests.
//
// The deadline is supplied by the caller and shared across every wait for one
// test database, so repeated waits cannot add up past it.
func waitForTemplateRebuild(ctx context.Context, adminConfig *db.Config, template string, deadline time.Time) {
	for time.Now().Before(deadline) {
		if !templateRebuildInProgress(ctx, adminConfig, template) {
			return
		}
		if ctx.Err() != nil {
			return
		}
		time.Sleep(templateRebuildWaitInterval)
	}
}

// dropStaleTemplateDatabases removes *old* templates built for other migration
// sets (typically left by an earlier branch or an older checkout) so they do not
// accumulate on long-lived local databases.
//
// Three constraints shape this:
//
// Matching is by anchored regex rather than LIKE, because every underscore in
// `fleet_test_tmpl_%` is a single-character wildcard and LIKE would also match
// unrelated names like `fleetXtestYtmplZsomething`. Each name is then
// re-validated in Go and quoted before it reaches DDL, so a catalog row can
// never steer the statement.
//
// Deletion is gated on age, not on "has a different fingerprint". Cloning does
// not hold the preparation lock, so two checkouts on different migration sets
// would otherwise alternate between rebuilding their own template and deleting
// the other's live one, turning a one-off build into a rebuild storm. A template
// younger than templateStaleAfter therefore belongs to somebody's current run
// and is left alone.
//
// Only databases carrying our own creation-time comment are considered; anything
// unrecognised is never touched.
func dropStaleTemplateDatabases(ctx context.Context, adminConn *sql.Conn, keep string) {
	rows, err := adminConn.QueryContext(ctx, `
		SELECT datname, shobj_description(oid, 'pg_database')
		FROM pg_database
		WHERE datname ~ $1 AND datname <> $2
	`, templateNamePattern.String(), keep)
	if err != nil {
		return
	}
	defer rows.Close()

	var stale []string
	for rows.Next() {
		var name string
		var comment sql.NullString
		if err := rows.Scan(&name, &comment); err != nil {
			return
		}
		// Defence in depth: never interpolate a name the pattern does not own,
		// even though the query already filtered on it.
		if !templateNamePattern.MatchString(name) || name == keep {
			continue
		}
		if !templateIsStale(comment) {
			continue
		}
		stale = append(stale, name)
	}
	if rows.Err() != nil {
		return
	}

	for _, name := range stale {
		// Sealed templates have no sessions to terminate, so a plain DROP is
		// enough; failures mean someone else is mid-clone and we leave it.
		_, _ = adminConn.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(name))
	}
}

// templateIsStale reports whether a template is old enough for another run to
// sweep. Only our own comment format counts as evidence: a template with no
// comment, a foreign comment, or an unparseable timestamp is left in place
// rather than guessed about.
func templateIsStale(comment sql.NullString) bool {
	created, ok := templateCreatedAt(comment)
	if !ok {
		return false
	}
	return time.Since(created) > templateStaleAfter
}

// quoteIdentifier renders a PostgreSQL identifier safely. Database names cannot
// be bound as parameters in DDL, so anything interpolated into a statement goes
// through here.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteLiteral renders a PostgreSQL string literal safely, for the DDL
// statements (COMMENT, ALTER DATABASE SET) that cannot take bind parameters.
func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

// templateNameFor returns the template name this configuration may use.
//
// The connecting role is part of the identity, not just the migration set:
// PostgreSQL only lets a role copy a database it owns (or any, for superusers),
// so two checkouts on one cluster using different roles must not compete for one
// template name. Without this the second role gets "permission denied to copy
// database" on every clone.
func templateNameFor(config *db.Config) string {
	return templateDBPrefix + migrationSetFingerprint(config.Username)
}

// migrationSetFingerprint hashes the embedded migration files (names and
// contents), plus the owning role, so that any change to the schema — or a
// different role — yields a new template database rather than silently reusing
// one that does not match.
func migrationSetFingerprint(role string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(role))
	_, _ = hash.Write([]byte{0})

	names, err := migrationFileNames()
	if err != nil {
		// A read failure here would make every test share an unkeyed template;
		// fall back to a unique fingerprint so we build a fresh one instead.
		return fmt.Sprintf("%x", time.Now().UnixNano())[:templateFingerprintLength]
	}

	for _, name := range names {
		contents, err := fs.ReadFile(migrations.Migrations, name)
		if err != nil {
			return fmt.Sprintf("%x", time.Now().UnixNano())[:templateFingerprintLength]
		}
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write(contents)
	}

	return hex.EncodeToString(hash.Sum(nil))[:templateFingerprintLength]
}

// migrationFileNames lists the embedded migration files in a stable order.
func migrationFileNames() ([]string, error) {
	entries, err := fs.ReadDir(migrations.Migrations, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}
