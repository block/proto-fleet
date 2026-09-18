# AGENTS.md

Proto Fleet is a monorepo for bitcoin-miner fleet management: Go/Connect/sqlc
server, React/TypeScript clients (ProtoOS and ProtoFleet), Go/Rust/Python
device plugins, and protobuf contracts. Human contributor workflows live in
[CONTRIBUTING.md](CONTRIBUTING.md).

## Task-specific guidance

Read the guidance relevant to the requested change; a small edit does not
require reading the full repo map or every skill.

| When working on | Guidance |
| --- | --- |
| Client features, routes, or E2E | [client/AGENTS.md](client/AGENTS.md), [client README](client/README.md) |
| Server logic, SQL, or monitoring | [server/AGENTS.md](server/AGENTS.md), [server README](server/README.md) |
| Proto contracts, including SDK protos | [proto/AGENTS.md](proto/AGENTS.md) |
| Proto, SQL, or generator configuration changes | [Code generation skill](.agents/skills/code-generation/SKILL.md) |
| Python generator source or bundled `scripts/pip-config.sh` | [Packaging skill](.agents/skills/python-gen-tarball/SKILL.md), [generator README](packages/proto-python-gen/README.md) |
| Miner drivers, fake rigs, or ASIC-rs inputs | Relevant [repo skill](.agents/skills/) for contract tests, fixtures, or ASIC-rs builds |
| Go dependencies or missing tools | [Dependency guidance](docs/development/dependencies.md) |
| PR descriptions or readiness | [PR standard](docs/development/pr-descriptions.md), [readiness skill](.agents/skills/pr-ready/SKILL.md) |

Before debugging or changing a documented fragile subsystem, search
`docs/solutions/` by relevant `module:`, `tags:`, or `problem_type:` and read
matching learnings. Use CONTRIBUTING.md's cross-component workflows for new
API endpoints, schema changes, client features, or server business logic.

## Canonical commands

The root and per-area `justfile`s are the source of truth. `just --list`
shows the full surface; setup options are in CONTRIBUTING.md.

| Goal | Command |
| --- | --- |
| Setup / local app | `just setup` / `just dev` |
| Generate / lint / format | `just gen` / `just lint` / `just format` |
| Check changed areas | `just check-changed` |
| Rebuild a plugin for Docker | `just rebuild-plugin <proto\|antminer\|virtual\|asicrs>` |
| Plugin contracts | `just test-contract` |
| ProtoFleet / ProtoOS E2E | `just test-e2e-fleet` / `just test-e2e-protoos` |
| Python generator tarball | `cd packages/proto-python-gen && just package` |

## Invariants

- Do not hand-edit `**/generated/**`, `*.pb.go`, `*.pb.ts`, or
  `client/src/protoOS/api/generatedApi.ts`. Edit sources and run `just gen`;
  commit source and generated output together.
- Deployed migrations are immutable. Use a new migration with both up and
  down files. See the [migration skill](.agents/skills/migration-immutability/SKILL.md)
  when deciding whether an existing migration can change.
- **Visual snapshot baselines are approval-gated.** Do not run the ProtoFleet
  visual snapshot refresh/overwrite flow unless the developer explicitly
  confirms they understand the checked-in expected screenshots will be
  replaced. After refreshing snapshots, remind the developer to review every
  updated image before committing or pushing.
- Do not add backwards-compatibility shims in unmerged feature branches;
  rebase the source instead.
- Do not introduce new tooling (linters, formatters, build systems) without
  asking. Do not mock the database where real Postgres/Timescale is available
  via docker-compose.

## Git and completion

- Do not commit planning documents (implementation plans, TDDs, PRDs, RFC
  proposals, or task scratchpads). Keep them in the conversation, an issue/PR,
  an external document, or ignored local `docs/plans/` files. Commit maintained
  documentation of current behavior, architecture, and operations instead.
- Work on a feature branch. Never commit on `main`, `master`, or detached
  HEAD; verify `git branch --show-current` before each commit. After a PR
  merges, start a fresh branch from updated `main`.
- Do not bypass hooks with `--no-verify`, `-n`, or local hook overrides.
  Fix the underlying failure; diagnose missing tools via the dependency guide.
- For implementation requests, complete the change and applicable validation.
  Fix failures caused by the change and rerun affected checks without asking
  at each step. Stop when those checks pass or a concrete blocker remains;
  report unrelated failures without expanding scope. Read-only reviews stay
  read-only. Commit, push, and PR creation follow the user's requested scope.
- Choose checks for the affected behavior; broaden only for a relevant risk
  or failure. Report what ran, what passed, and what remains unverified.
- Verify versions and upstream behavior against live sources. For setup
  instructions, read the actual scripts; mark unverifiable claims explicitly.

## Agent tooling

Canonical repository skills live in `.agents/skills/`; their descriptions
identify when to load them. `.claude/skills/` exposes those same definitions to
Claude through per-skill links. Do not copy or fork instructions between
discovery directories. `CLAUDE.md` is a thin entry point. Keep
`.claude/settings.local.json` private. Claude invokes these workflows as
`/regen`, `/pr-ready`, `/pr-describe`, `/triage-pr`, `/release-notes`, and
`/plan`; Codex uses the corresponding `$name`.
