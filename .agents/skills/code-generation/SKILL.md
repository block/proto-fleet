---
name: code-generation
description: Regenerate code after proto, SQL schema/query, Fleet CLI or Go generator input, or generator configuration changes.
---

# Code generation

After source edits are complete, select the command below and run it from the
repo root. Keep source and generated output together; never patch generated
files by hand. Inspect the pre-existing diff before generation so user changes
remain identifiable.

| Source changes | Command |
| --- | --- |
| Protobuf contracts in `proto/` or `server/sdk/v1/pb/` | `just gen-protos` |
| SQL schema in `server/migrations/` or queries in `server/sqlc/queries/` | `just gen-db-queries` |
| Fleet CLI manifest (`server/tools/generate-fleet-cli/commands.json`) or templates (`server/tools/generate-fleet-cli/templates/`) | `just gen-fleet-cli` |
| Inputs to the server's `go:generate` directives | `just gen-go` |
| Changes spanning generators, or generator configuration/tool versions (including Buf and sqlc configuration) | `just gen` |

The scoped commands avoid unrelated generators and full client/server
formatting. `just gen-protos` includes SDK protobufs and the protobuf-driven
Fleet CLI. Use full `just gen` when changes cross the scopes above or alter
the generation pipeline.

Read only the reference relevant to the change:

- [Protobuf](references/protobuf.md): proto contracts and generated consumers.
- [SQL](references/sql.md): migrations, queries, and sqlc bindings.
- [Generator upgrades](references/generator-upgrades.md): Buf dependencies
  or generator versions; combine with protobuf guidance when both apply.

Inspect the resulting diff and validate affected consumers. Investigate
unexpected output using source changes, generator configuration, and tool
versions; do not assume it proves the branch was stale. Resolve generation
failures caused by the requested change and rerun; report external blockers.

`just gen` does not rebuild the Python generator distribution. Source changes
there or to bundled `scripts/pip-config.sh` require the
[packaging skill](../python-gen-tarball/SKILL.md).
