# AGENTS.md

Guidance for AI coding agents (Claude Code, Codex, Copilot, etc.) working in this repo.
Human contributors should read [CONTRIBUTING.md](./CONTRIBUTING.md) instead — it covers
the same material in more depth.

## What this is

Proto Fleet is a monorepo for bitcoin-miner fleet management. Main areas:

| Area | Stack | Path |
| --- | --- | --- |
| Server | Go, Connect RPC, sqlc, Postgres/TimescaleDB | `server/` |
| Clients (ProtoOS, ProtoFleet) | TypeScript, React, Vite, Tailwind | `client/` |
| Plugins (device drivers) | Go, Rust, Python | `plugin/` |
| Proto definitions | Protobuf + buf | `proto/` |
| Python proto generator | Python, packaged as tarball | `packages/proto-python-gen/` |

Per-area details live in `server/README.md`, `client/README.md`, and
`packages/proto-python-gen/README.md` — prefer reading those over re-deriving from code.

## Canonical commands

The `justfile` is the source of truth for build/test/lint commands. Prefer these
over inventing your own.

| Goal | Command |
| --- | --- |
| Install everything | `just setup` |
| Run app locally | `just dev` |
| Regenerate all generated code | `just gen` |
| Lint everything | `just lint` |
| Format everything | `just format` |
| Rebuild a single plugin in Docker | `just rebuild-plugin <proto\|antminer\|virtual\|asicrs>` |
| Plugin contract tests | `just test-contract` |
| ProtoFleet E2E | `just test-e2e-fleet` |
| ProtoOS E2E | `just test-e2e-protoos` |
| Build Python generator tarball | `cd packages/proto-python-gen && just package` |

Run `just --list` for the full surface.

## Rules that matter

These are the rules that recur in code review and that contributors most often miss.

1. **Generated code is generated.** Do not hand-edit anything under a
   `**/generated/**` path, or any `*.pb.go` / `*.pb.ts` file. Change the
   source (`.proto`, `sqlc/queries/`, `migrations/`) and run `just gen`.
2. **Commit proto + generated code together.** Never split a `.proto` change
   from its regenerated output across commits.
3. **Migrations are immutable after deploy.** Add a new migration; never edit
   one that has shipped. Both up and down migrations are required.
4. **Python plugin generator changes require a tarball rebuild.** If you touch
   any source file under `packages/proto-python-gen/`, run `just package` from
   that directory and commit the regenerated
   `proto-python-gen-<version>.tar.gz`.
5. **Component boundaries in `client/`.** `shared/` cannot import from
   `protoOS/` or `protoFleet/`; `protoOS/` and `protoFleet/` cannot import
   from each other.
6. **No new `console.log` in production client code.** The existing build-version
   logger is intentional; `console.error` is fine.
7. **Server: prepared statements only.** All DB access goes through sqlc.
8. **Go workspace.** Run `go work sync` after touching dependencies in any
   module under `server/` or `plugin/`.

## Git workflow

- Never commit to `main`. Always work on a feature branch. Verify with
  `git branch --show-current` before each commit. Lefthook also rejects
  commits on `main`/`master`/detached HEAD as a safety net (see
  `scripts/lefthook-block-protected-branches.sh`) — don't rely on it as a
  substitute for following the rule.
- After a PR merges, do not reuse that branch for follow-up work — cut a
  fresh branch from the updated `main`.

## Verification

- Don't pin versions, package versions, or upstream behaviors from training
  data. Verify against the live source — package registries (npm, PSGallery,
  PyPI, crates.io), upstream source code, or actual API responses — before
  stating specifics.
- When documenting install or setup flows, read the actual scripts (e.g.
  `dev.sh`, `bin/activate-hermit`, `deployment-files/**`); don't infer
  prerequisites from package names.
- If you can't verify a claim, mark it explicitly rather than extrapolating.

## Planning docs

TDDs, PRDs, and lightweight plans live under `docs/plans/`. Filename
pattern: `YYYY-MM-DD-<slug>-<type>.md` where `<type>` is `tdd`, `prd`, or
`plan`. Frontmatter carries `status:` (`draft → proposed → accepted →
implementing → completed | cancelled`) and `type:`. When status flips to
`completed` or `cancelled`, move the file to `docs/plans/archive/` via
`git mv`.

Use `/plan <title>` to scaffold a new doc with the right template.

## Common cross-component workflows

These map directly to sections in [CONTRIBUTING.md](./CONTRIBUTING.md):

- Adding a new API endpoint → CONTRIBUTING.md "Adding a New API Endpoint"
- Database schema change → CONTRIBUTING.md "Making Database Schema Changes"
- New client feature → CONTRIBUTING.md "Adding Features to the Client"
- New server domain logic → CONTRIBUTING.md "Adding Business Logic to the Server"

## For Claude Code users

Slash commands and auto-triggering skills live in `.claude/`. The commands and
skills are repo tooling and should be committed; `.claude/settings.local.json`
stays private.

- `/regen` — run `just gen`, surface what changed, flag generated files to commit
- `/pr-ready` — lint + targeted tests + diff summary suitable for a PR description
- `/release-notes <version>` — draft release notes from commits since the previous tag
- `/triage-pr <#>` — fetch PR status, summarize failing CI, propose next action
- `/plan <title>` — scaffold a new TDD, PRD, or plan under `docs/plans/`

Skills auto-fire based on what's being edited; see `.claude/skills/` for the
catalog. Their descriptions document their own triggers.

## Things to avoid

- Don't run `--no-verify` on commits to bypass `lefthook` hooks. Fix the underlying issue.
- Don't add backwards-compatibility shims in unmerged feature branches — rebase the source instead.
- Don't introduce new tooling (linters, formatters, build systems) without asking. The toolchain is intentionally tight.
- Don't mock the database in tests where a real Postgres/Timescale is available via docker-compose.
