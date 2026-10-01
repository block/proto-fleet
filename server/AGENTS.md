# Server guidance

- Prepared statements only: all database access goes through sqlc. After schema
  and query edits, run the appropriate command from the
  [generation skill](../.agents/skills/code-generation/SKILL.md) at the repo root.
- Before editing existing migrations, use the
  [migration skill](../.agents/skills/migration-immutability/SKILL.md).
- SDK proto changes follow [proto contract guidance](../proto/AGENTS.md).
- Every `uid:` under `monitoring/grafana/` must be at most 40 characters.
  One overlong UID aborts provisioning of all alerting files. Keep each
  `proto_fleet_rule_uid` label equal to its rule's UID.

Use [README.md](README.md) for architecture and the local `justfile` for
server commands. Go dependency changes follow the root dependency guide.
