# SQL generation

Schema sources are in `server/migrations/`; query sources are in
`server/sqlc/queries/`; configuration is `server/sqlc.yaml`. Root `just gen`
regenerates `server/generated/sqlc/` and the other configured outputs.

Use the [migration skill](../../migration-immutability/SKILL.md) before
modifying an existing migration. New migrations require both up and down
files. Regenerate after schema or query changes, including schema changes
that do not edit a query file. Commit SQL source and generated output together.
