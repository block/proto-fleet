---
name: db-generation-hygiene
description: Use when editing `server/sqlc/queries/**/*.sql`, `server/sqlc.yaml`, or creating a new `server/migrations/*.sql` file. sqlc query and schema source changes require running `just gen` so `server/generated/sqlc/` stays in sync. Edits to *existing* migration files are owned by the `migration-immutability` skill instead.
---

# db-generation-hygiene

Server DB access goes through sqlc-generated bindings. Source of truth is
split between migrations (`server/migrations/`) and queries
(`server/sqlc/queries/`); generated Go lives under `server/generated/sqlc/`.

## What to do

1. After the SQL edit is complete, run `just gen` from the repo root.
2. Confirm the expected diff lands under `server/generated/sqlc/`. If
   unrelated generated files change, the branch was already stale before
   this edit — call that out.
3. Commit SQL source and generated output together.

## What to avoid

- Do not hand-edit `server/generated/sqlc/`.
