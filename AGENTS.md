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
9. **Client routes use idle-time prefetch.** Adding a route requires
   coordinated edits across `routePrefetch.ts` (factory plus tier) and
   `router.tsx` (`lazy()` wrapper). See the top-of-file runbook in either
   app's `routePrefetch.ts`.

## Git workflow

- Never commit to `main`. Always work on a feature branch. Verify with
  `git branch --show-current` before each commit. Lefthook also rejects
  commits on `main`/`master`/detached HEAD as a safety net (see
  `scripts/lefthook-block-protected-branches.sh`) — don't rely on it as a
  substitute for following the rule.
- After a PR merges, do not reuse that branch for follow-up work — cut a
  fresh branch from the updated `main`.

## PR descriptions

When opening a PR, write the description so a reviewer can judge the
architecture and technical decisions **without reading the low-level code**.
Inspect the actual diff, commits, and changed files first; describe what the
code does, not the decisions made getting there. Structure it as:

1. **Summary** — 2-4 sentences: what this PR delivers and why. Lead with the
   user- or operator-facing capability, not the implementation. For a PR in a
   series (stacked on a parent, has descendant PRs, or marked `N/M`), add a
   short *Stack* note: the full chain with PR links (ancestors, this PR, and any
   PRs stacked on top), that the diff is relative to the immediate base when it
   has ancestors, the load-bearing context from upstream PRs
   a reviewer needs to judge this change, and what is intentionally out of scope
   here and where the remaining work lands (from descendant PRs, the plan docs,
   or tracking issues, since later phases may not be open as PRs yet).
2. **How it works** — the end-to-end mechanism in plain language. Walk the
   primary flow(s): who triggers it, what crosses each boundary, where state
   is persisted, what comes back. Explain workflows, not language syntax.
3. **Diagrams** — mermaid in fenced code blocks labeled `mermaid` so they render on
   GitHub. At least a component/flow diagram of the main path; add a state or
   sequence diagram where lifecycle or ordering matters. Keep syntax
   GitHub-safe: quote labels with special characters, avoid fragile edge styles.
4. **Areas of the code involved** — a table mapping each changed area to its
   role so reviewers know where to focus: `| Area / package / file | What
   changed | Why it matters for review |`. Group by subsystem (`proto/`,
   `server/`, domain logic, migrations, `client/`, `plugin/`); flag generated
   code as "generated — skip".
5. **Key technical decisions & trade-offs** — the choices a reviewer should
   scrutinize (new abstractions, data-model/migration changes, security or
   validation boundaries, backward-compat or rollout concerns). One line each:
   the decision and the alternative it was chosen over.
6. **Testing & validation** — how correctness was verified and what is
   explicitly not covered.

Keep it concise: tables and diagrams over long paragraphs, no filler praise,
no narration of rejected approaches. Claude Code users can generate this with
`/pr-describe`.

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

## Solution docs (institutional learnings)

Documented bugs, fixes, conventions, and architectural learnings live under
`docs/solutions/`, organized by category (`build-errors/`, `database-issues/`,
`best-practices/`, etc.). Each file has YAML frontmatter with `module`,
`problem_type`, `component`, `tags`, and other searchable fields.

**Search before implementing.** Before starting work in a documented area
(debugging an error, making a design decision, touching a fragile subsystem),
grep `docs/solutions/` for prior learnings by frontmatter (`module:`, `tags:`,
`problem_type:`). The knowledge store only compounds value when agents find
it. Use `/ce-compound` to capture new learnings after a fix lands.

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
- `/pr-describe` — write/update a PR description (high-level mechanism, mermaid diagrams, code-area map) per the "PR descriptions" standard above
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
