---
title: Per-test database migration replay blows the Go test timeout; TimescaleDB worker blocks CREATE DATABASE TEMPLATE
date: 2026-08-21
category: docs/solutions/database-issues
module: server/internal/testutil/dbtest
problem_type: performance
component: testing_framework
symptoms:
  - "panic: test timed out after 10m0s in server/internal/domain/stores/sqlstores"
  - "FAIL github.com/block/proto-fleet/server/internal/domain/stores/sqlstores 600.051s with a named test only 1s in"
  - Dozens of goroutines parked in testing.(*T).Parallel for 9 minutes while one test holds the run
  - "CI logs show hundreds of 'migrations completed duration=~2s' lines, one per test database"
  - "CREATE DATABASE ... TEMPLATE fails with: source database \"fleet_test_tmpl_*\" is being accessed by other users (SQLSTATE 55006)"
  - Template-clone harness is *slower* than full migration replay until background workers are evicted
root_cause: architectural_limitation
resolution_type: refactor
severity: high
related_components:
  - server/migrations
  - timescaledb
  - ci
tags: [postgres, timescaledb, testing, test-performance, go-test-timeout, create-database-template, advisory-lock, golang-migrate, dbtest]
---

# Per-test migration replay blows the Go test timeout, and TimescaleDB blocks the fix

## Problem

`Server Checks / Test` failed on a downstream sync PR with:

```
panic: test timed out after 10m0s
  running tests: TestZoneKeysWildcard_CrossOrgIsolation (1s)
FAIL github.com/block/proto-fleet/server/internal/domain/stores/sqlstores  600.051s
```

The named test is a red herring — it was 1s in when the alarm fired, and 19 other
tests were still parked in `t.Parallel()` after 9 minutes.

The real cause is the shape of the harness. `dbtest.GetTestDB` created a fresh
database per test and replayed the **entire** migration set into it, so cost grew
as O(tests × migrations):

| Signal in the failing job | Value |
| --- | --- |
| Test databases created in `sqlstores` | 259 |
| Migration replays (142 migrations each, ~2s) | 258 |
| Time spent inside `migrate.Up()` alone | ~501s of the 600s budget |

`server/justfile` runs `go test ./... -skip ./generated/...` with no `-timeout`,
so Go's default 600s per-package limit applies. Upstream `main` was already at
`ok … 388.626s` for the same package — every new migration pushed it closer, and
the extra tests in the downstream repo tipped it over.

## Investigation

1. `panic: test timed out` with a 1s-old test name ⇒ the *package*, not one test,
   is over budget. Count the work instead of reading the stack: `grep -c
   "migrations completed"` in the job log gave 258, and summing
   `duration=` gave ~501s.
2. Baseline locally: `sqlstores` took **159s**, full server suite **3m25s**.
3. First attempt at the fix — migrate once into a template database and clone it
   per test with `CREATE DATABASE … TEMPLATE` — made things dramatically *worse*:
   the package went from 159s to **1801s** (timeout).
4. Timing a clone by hand exposed why:

   ```
   ERROR:  source database "fleet_test_tmpl_867fe765862f" is being accessed by other users
   Time: 5289.486 ms
   ```

   ```sql
   SELECT pid, datname, backend_type FROM pg_stat_activity WHERE datname LIKE 'fleet_test%';
   -- 12185 | fleet_test_tmpl_867fe765862f | TimescaleDB Background Worker Scheduler | idle
   ```

   **`CREATE EXTENSION timescaledb` causes the TimescaleDB launcher to attach a
   background-worker scheduler to the database.** That counts as a connected
   session, and `CREATE DATABASE … TEMPLATE` requires *zero* sessions on the
   source. Every clone waited ~5s and then failed.
5. Setting `ALLOW_CONNECTIONS false` on the template is not enough on its own —
   it stops the launcher *reattaching*, but the worker that started when the
   extension was created is already in. After terminating it once, clones landed
   at **21–43ms** and no worker came back.

## Solution

`server/internal/testutil/dbtest/template.go` prepares one migrated template per
migration set; `GetTestDB` clones it instead of replaying migrations.

1. **Key the template by a fingerprint of the embedded migration set** —
   `sha256` over sorted migration filenames plus contents, truncated to 12 hex
   chars, giving `fleet_test_tmpl_<fingerprint>`. Adding or editing a migration
   yields a new name, so a stale schema can never be silently reused. Stale
   templates from other fingerprints are dropped best-effort.
2. **Serialise preparation with a PostgreSQL advisory lock** on a pinned
   connection (`sql.DB.Conn`, because advisory locks are session-scoped). Many
   test binaries run concurrently under `go test ./...`; the first builds the
   template and the rest wait and reuse it. Verified: a cold database plus a full
   parallel suite produces exactly **one** template.
3. **Publish the template by disabling connections** —
   `ALTER DATABASE … ALLOW_CONNECTIONS false` is both the guard (nothing can
   attach and break clones) and the readiness marker. A template that exists but
   still allows connections is treated as half-built and rebuilt, so an
   interrupted run cannot leave a partially migrated template behind.
4. **Evict the TimescaleDB scheduler after sealing** and poll `pg_stat_activity`
   until the template has no backends, so the first clone does not race the
   shutdown.
5. **Retry clones on SQLSTATE 55006** (`isRetryableCreateError`, plus the
   "is being accessed by other users" text) and evict template sessions before
   each retry. **Rebuild on SQLSTATE 3D000**: a concurrent checkout on another
   migration set may have swept the template between clones.
6. **Keep `ConnectAndMigrate` on the clone.** It is a no-op version check on an
   already-current database (a couple of queries), and it self-heals if a clone
   is ever behind — which is what makes the whole change safe: if a template is
   stale or unavailable, the harness falls back to the old migrate-everything
   path instead of failing.

## Results

Measured with `-count=1` on both sides, same machine and same TimescaleDB container:

| Scope | Before | After |
| --- | --- | --- |
| `internal/domain/stores/sqlstores` | 159.4s | 33.4s (32.1s warm) |
| Full server suite | 3m25s | 1m09s |
| Per test database setup | ~2s (142 migrations) | ~20–40ms (clone) |
| Template build (once per fingerprint) | — | ~0.6–1.0s |

CI's `sqlstores` package drops from 388s (65% of the 600s budget) to well under
a third of it, and the budget no longer grows with each new migration.

## Prevention / gotchas

- **Any TimescaleDB database has a background worker attached.** Anything
  requiring an exclusive source database (`CREATE DATABASE … TEMPLATE`,
  `DROP DATABASE`) must disable connections *and* terminate existing backends.
  `ALLOW_CONNECTIONS false` alone does not evict what is already connected.
- **`pg_advisory_lock` on a `*sql.DB` is a bug.** Pool routing can send the
  unlock to a different session. Pin with `db.Conn(ctx)`.
- **Don't derive template readiness from "the database exists."** Use a state
  that is only reachable after migrations succeed (here: connections disabled).
- **Per-test DB cost is O(tests × migrations) by default.** If a DB-backed
  package creeps toward the 10m Go timeout, count migration replays in the log
  before suspecting a specific test.
- **`LIKE 'prefix_%'` is not a prefix match.** Every `_` is a single-character
  wildcard, so `fleet_test_tmpl_%` also matches `fleetXtestYtmplZanything`. Any
  query that feeds `DROP DATABASE` must use an anchored regex (`datname ~
  '^fleet_test_tmpl_[0-9a-f]{12}$'`), re-validate in Go, and quote the
  identifier — database names cannot be bound as DDL parameters.
- **Content-addressed templates need a recovery path, not just a lock.** Two
  checkouts on different migration sets sharing one PostgreSQL server will sweep
  each other's templates between clones (a sealed template has no sessions, so
  the drop succeeds). The clone then fails with SQLSTATE **3D000**, which is
  *not* in the transient-retry set — handle it by rebuilding the template.
- **Don't cache preparation failures in `sync.Once`.** A single blip while the
  first test runs would push every remaining test in the binary back onto the
  slow path — exactly the timeout this change removes. Cache only success (mutex
  plus a `prepared` flag), bound the retries, and wait for server readiness
  before the first attempt.
- `CREATE DATABASE … TEMPLATE` does **not** copy `datallowconn` or per-database
  GUCs to the clone; clones are normal, connectable databases.
- Prefer the default `STRATEGY = WAL_LOG` for a template this size (16MB);
  `FILE_COPY` was only ~2x faster per clone and forces two checkpoints, which
  hurts under hundreds of concurrent clones.
