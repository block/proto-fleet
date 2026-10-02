# Database migrations

The application uses one ordered SQL history and the existing
`schema_migrations(version, dirty)` row. Shared and private describe file
ownership, not separate database version counters.

## Creating and promoting migrations

Allocate every new number in the internal repository, from an up-to-date main
branch. Run `cd server && just db-migration-new shared_<name>` for a shared
change or `just db-migration-new internal_<name>` for a private change. Resolve
concurrent allocations before merge; never insert a migration below the highest
number already merged or released. An unmerged migration may be renumbered only
if it has never been applied or released.

Both directions belong in `server/migrations/current/`. Files use six-digit
numbers and names such as `001002_shared_add_column.up.sql` and the matching
`.down.sql`. The shared baseline is version 1000; the private baseline is 1001.
Every private up migration declares its required shared version on its own line:

```sql
-- requires-shared: 1000
```

The dependency must name an earlier shared migration included in the artifact.
Private changes to shared tables still belong in private migrations. Test shared
changes against both public-only and internal databases.

Every new migration also updates
`server/internal/infrastructure/db/baseline/assertions.json`. Set `target` to
the highest active migration number. Capture destination catalog digests from
disposable fresh-install and previous-release upgrade fixtures using the
read-only `fleet-db-transition catalog` command. Compare both results and verify
the expected schema and preserved data before updating `targets` and the
corresponding `admissions` entry. Retain the previous release's target digests
under its version in `admissions` so its database can enter the new migration.
Never learn or accept an assertion from an unknown live database. Updating the
version alone can otherwise let a migration commit before startup rejects the
destination catalog.

Promote shared files to public unchanged, in their original order and with their
original numbers. Public numbering intentionally has gaps for private files.
Public shared files must form a byte-identical prefix of the internal shared
sequence; only a suffix may await promotion. Never publish private SQL, including
changes to shared tables. Do not create placeholder public migrations for private
numbers. Reviewers verify that shared changes originated internally.

Merged or released SQL is immutable: no edits, deletions, moves or renumbering.
Use new migration pairs for corrections. Retain the original legacy SQL and
compatibility bridges; `current/` is the fresh-install source, not permission to
delete upgrade history.

## Validation

Migration Hygiene checks every PR's merge result. It validates paired filenames,
unique numbers, private prerequisites, immutability against the current base and
no insertion below its high-water mark. It also checks that schema assertions
name the highest active version and retain the previous release's catalog
admissions. Catalog equivalence still requires fresh-install and upgrade tests;
the policy checker does not infer schema correctness from JSON metadata.
Internal CI additionally fetches public
main and logs the exact commit used for shared parity. Public CI uses only its
own repository and does not need private credentials.

Run the same checks locally from the repository root after activating Hermit:

```sh
python3 scripts/check-migration-history_test.py
python3 scripts/check-migration-history.py --base-ref origin/main
```

For public validation, add `--public-only`. For internal promotion validation,
fetch the desired public revision and add `--public-ref <exact-commit>`. Missing
revisions fail validation; the checker never substitutes an empty history.

## Existing databases and repository switches

The baseline SQL is for empty databases only. Existing installations need the
[qualified reconciliation procedure](baseline-upgrade.md); do not replay a baseline or
force a migration version to bypass validation. A matching integer alone does
not establish repository or application compatibility. For example, a public
database at shared version 1002 may lack private version 1001; the ordinary
migration runner would skip that private change. Switching repositories therefore
requires explicit admission and reconciliation, not merely a new image.

Keep existing on-prem upgrade paths and the migration-143 compatibility bridge.
Never automatically run down migrations or delete private data during a switch.
After a migration, reverting the application does not revert the database; use
qualified forward recovery or a coordinated restore of database and local state.
