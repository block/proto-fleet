---
name: code-generation
description: Regenerate code after proto, SQL schema/query, sqlc, or Buf configuration changes.
---

# Code generation

Run `just gen` from the repo root after source edits are complete. Keep source
and generated output together; never patch generated files by hand. Inspect
the pre-existing diff before generation so user changes remain identifiable.

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
