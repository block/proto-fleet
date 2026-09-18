---
name: migration-immutability
description: Determine whether an existing migration can change and implement corrective migrations safely.
---

# Migration immutability

Applied migrations cannot be rewritten: golang-migrate skips applied versions.
Treat migrations merged to main as immutable, even when deployment status is
unknown. Check the creation commit's reachability from `origin/main` (or
`main` when unavailable), not merely whether the file has local commit history.
Merged history is a conservative boundary, not proof of deployment. Missing
or shallow history is inconclusive; resolve it before declaring a file editable.

A new, unmerged migration can be revised in its feature branch unless it is
known to have been deployed. Do not delete or renumber immutable migrations.

For an immutable migration, implement the requested correction in a new pair
using `cd server && just db-migration-new <name>`. Include both up and down
files and run root `just gen` after schema/query changes. This is the normal
way to complete a schema fix, not a reason to stop at proposing one.
