# Server guidance

- Prepared statements only: all database access goes through sqlc. Schema
  and query edits require `just gen` from the repo root; see the
  [generation skill](../.agents/skills/code-generation/SKILL.md).
- Before editing existing migrations, use the
  [migration skill](../.agents/skills/migration-immutability/SKILL.md).
- SDK proto changes follow [proto contract guidance](../proto/AGENTS.md).
- Every `uid:` under `monitoring/grafana/` must be at most 40 characters.
  One overlong UID aborts provisioning of all alerting files. Keep each
  `proto_fleet_rule_uid` label equal to its rule's UID.

Use [README.md](README.md) for architecture and the local `justfile` for
server commands. Go dependency changes follow the root dependency guide.
