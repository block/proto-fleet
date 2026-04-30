---
description: Run pre-PR checks (lint, targeted tests, diff review), draft a PR description, and optionally open the PR if the user asked.
argument-hint: (no arguments; pass any text mentioning "open" or "create" to also run gh pr create)
---

Sweep the current branch for issues before opening a PR. The goal is to catch
the things CI will flag, surface anything risky, and give the user a draft
PR description they can paste — or open the PR directly if asked.

## Steps

1. Run `git status` and `git diff main...HEAD --stat` to see the scope of
   changes. Note which areas are touched: `server/`, `client/`, `plugin/`,
   `proto/`, `packages/proto-python-gen/`, etc.
2. Run `just lint`. Report any failures verbatim and stop if it fails — the
   user should fix lint before continuing.
3. Run targeted tests based on what was touched (do not run everything):
   - `server/` changes → `cd server && just test` (or a narrower `go test`
     scope if the diff is small)
   - `client/` changes → `cd client && npm test -- --run` for affected files
   - `plugin/` or `.proto` changes → `just test-contract`
   - **`plugin/asicrs/`, `sdk/rust/`, or `server/sdk/v1/pb/` changes →
     `just rebuild-plugin asicrs` then `just test-contract`.** The contract
     suite's freshness check can skip the ASIC-rs rebuild after a clean
     checkout or branch switch — force the rebuild explicitly so tests
     don't run against a stale binary.
   - **`server/fake-antminer/` or `server/fake-proto-rig/` changes →
     `just test-contract`** (fake rigs are consumed by both contract and
     E2E tests; mention E2E as a manual follow-up since they're slow).
   - Python generator changes → `cd packages/proto-python-gen && just test`
   - Python SDK changes → `cd server/sdk/v1/python && just test`
4. Verify ALL path-triggered skills (not just the generated-code subset)
   have nothing outstanding for the touched paths. The skills auto-fire
   during edits, but this is the terminal check — surface anything that
   slipped through. Common gaps to look for: stale generated files
   (`proto-regen`, `db-generation-hygiene`), missing tarball rebuild
   (`python-gen-tarball`), missing `go work sync` (`go-work-sync`), stale
   ASIC-rs binary (`asicrs-build`), fake-rig changes without contract
   coverage (`fake-rig-fixtures`), new `console.log` or app-boundary
   imports (`client-boundaries`), edits to deployed migrations
   (`migration-immutability`), `--no-verify` slipping in
   (`lefthook-bypass-guard`).
5. Draft a PR description following the format in CONTRIBUTING.md:
   - **Summary** (1–3 bullets, focus on the *why*)
   - **Test Plan** (what was run, how to verify manually)
6. **Open the PR only if the user's invocation explicitly asked you to**
   (e.g. "open it", "create the PR", "ship it"). Otherwise stop after
   presenting the draft so the user can edit it before running
   `gh pr create` themselves.

   When opening the PR:
   1. Confirm `git branch --show-current` is not `main` or `master`. If it
      is, stop — there's nothing to PR.
   2. Push the branch with `git push -u origin <branch>` if it doesn't yet
      track a remote.
   3. Run `gh pr create --title "<title>" --body "<drafted description>"`,
      passing the body via a heredoc to preserve formatting.
   4. Output the resulting PR URL.

## Notes

- Do not run E2E tests by default — they're slow and require docker-compose.
  Mention them as a manual follow-up if the diff suggests UI/API behavior
  changes worth verifying end-to-end.
- If lint, tests, or the hygiene check (steps 2–4) fail, do not proceed to
  step 6 even if the user asked for the PR to be opened. Surface the
  failures and ask.
