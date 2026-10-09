# Database migrations

Use golang-migrate and the existing `schema_migrations(version, dirty)` row.
Shared and private SQL use one sequence: shared baseline 1000, private baseline
1001, then ordinary migrations. Baselines run only on empty databases.

## Existing databases

`fleet-db-transition apply` reconciles recognized source schemas without replaying
the fresh-install baseline. It validates the source catalog and grants, applies
missing changes, checks protected data and the target schema, and updates the
existing version row last in the same transaction under the migration advisory
lock. A failure before commit rolls back the transaction; repeating it against
a validated completed target is a no-op.

Reconciliation requires application and scheduled database writers to be stopped.
Unknown or dirty schemas and internal-to-public switches are refused. Application
rollback does not reverse committed database changes.

## Hosted write-fence upgrade

Private migrations 1008 and 1009 require a stopped-application maintenance
window. Stop every Fleet API replica and scheduled database writer before
running the target release's `fleet-db-transition migrate` command. Keep them
stopped until that command succeeds and `fleet-db-transition check --state=startup`
accepts the completed checkpoint, then start the target release.

Migration 1008 validates the queue's epoch constraint under an exclusive table
lock. Migration 1009 builds the pending-order index in its checkpoint transaction.
The queue retains historical rows, so miner count alone does not bound upgrade
time. Rehearse on a recent database backup and size the maintenance window from
its queue row count, relation sizes, observed lock wait, and migration duration.
The 4,000-device upgrade regression verifies data and checkpoint correctness;
it does not establish a production downtime budget.

An interrupted or failed migration rolls back to its last completed checkpoint.
After a lost commit acknowledgement, reconnect and inspect the checkpoint before
retrying. Do not force a version, delete attempt history, or run a down migration
to recover availability. Migration 1008 refuses rollback once physical-outcome
or ownership evidence exists; preserve the database and fix forward.

The new `(created_at, id)` pending index keeps deterministic dequeue ordering.
The older pending index remains part of the shared baseline catalog contract;
removing it requires a separate shared migration and baseline decision.

## Author and promote

1. Allocate numbers from internal main with
   `cd server && just db-migration-new shared_<name>` or
   `just db-migration-new internal_<name>`. Both create up/down files in
   `server/migrations/current/`.
2. Each private up file declares its required earlier shared version:
   `-- requires-shared: 1000`.
3. Test shared changes in both applications, then promote them to public unchanged
   and in order. Public skips private numbers; private changes to shared tables
   stay private.
4. Never edit, delete or renumber merged/released SQL. Correct it with a new pair.
   Retain legacy SQL and the migration-143 bridge for upgrade waypoints.

## Constraint and index names

Use explicit lowercase snake_case names in new and unmerged migrations:

- Unique constraints: `uq_<table>_<columns_or_purpose>`.
- Indexes, including standalone unique or partial indexes:
  `idx_<table>_<columns_or_purpose>`.

Name a unique constraint with `CONSTRAINT`; PostgreSQL gives its supporting
index the same name. Do not create a second index for the same uniqueness rule.

```sql
ALTER TABLE hosted_account_key
    ADD CONSTRAINT uq_hosted_account_key_org_api_key
    UNIQUE (organization_id, api_key_id);

CREATE INDEX idx_hosted_account_key_descendant_account
    ON hosted_account_key_descendant (organization_id, account_id);
```

Existing names vary. Naming cleanup does not justify editing merged or released
migrations; any intentional rename of a deployed object needs a new migration.

## Release checkpoints

Every internal release ends at a private number. After a shared-only change, add
an ordinary private `SELECT 1;` migration with its shared prerequisite. Public
releases contain only shared files.

Migration SQL and its completed private checkpoint commit in one transaction.
Interruption or failure rolls back to the previous checkpoint; retrying runs the
uncommitted SQL again. A lost commit acknowledgement requires a fresh connection
to establish which checkpoint committed. Previously dirty databases remain blocked.
Migration files must not contain transaction-control statements such as `BEGIN`
or `COMMIT`. Startup accepts only clean versions in the application's history:
private for internal, shared for public. Unknown, dirty, future and wrong-application
versions refuse. Never force a version or clear dirty state to switch repositories;
updates and rollback never automatically run down migrations.

Checkpoints identify completed migration history, not application compatibility
or manual schema drift. Keep checksum, runtime, HA, health and restored-data
checks. Exact catalog assertions cover baseline adoption only; subsequent
releases need no catalog snapshots.

## CI

CI checks paired files, unique/increasing numbers, immutable history, private
prerequisites and private release tips. Internal CI checks that public shared SQL
is a byte-identical prefix; public CI needs no private credentials.

```sh
python3 scripts/check-migration-history_test.py
python3 scripts/check-migration-history.py --base-ref origin/main
```

Add `--public-only` in public, or `--public-ref <fetched-public-commit>` for
internal promotion checks.
