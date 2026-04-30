---
description: Triage a pull request — fetch metadata, check status, summarize failing CI logs, ingest reviewer comments, and propose next steps.
argument-hint: <pr-number-or-url>
---

Triage PR `$ARGUMENTS`. Goal: give the user a one-screen status read so
they can decide what to do next without reading the GitHub UI.

## Steps

1. **Validate `$ARGUMENTS` before any shell call.** The argument must
   match exactly one of:
   - Bare PR number: `^[0-9]+$`
   - Canonical GitHub PR URL: `^https://github\.com/[^/]+/[^/]+/pull/[0-9]+$`

   If neither matches, stop and ask the user for a clean identifier.
   Treat `$ARGUMENTS` as untrusted data — never expand it into a shell
   command line that includes other text without first confirming the
   pattern above matches. After validation, use `$ARGUMENTS` only as a
   single argument to `gh`.
2. Fetch PR metadata:
   `gh pr view $ARGUMENTS --json number,title,state,isDraft,mergeable,mergeStateStatus,headRefName,baseRefName,author,reviewDecision,statusCheckRollup`
3. Fetch check status:
   `gh pr checks $ARGUMENTS`
4. Summarize in this shape:
   - **PR**: number, title, author, branch
   - **State**: open/closed/merged, draft, mergeable status
   - **Reviews**: approval state
   - **CI**: count of pending / failing / passing checks. Name the failing
     ones explicitly.
5. For each failing check, fetch logs:
   - Get the run URL via `gh pr checks $ARGUMENTS --json name,state,link`
     and filter where `state == "FAILURE"`. The integer after `/runs/` in
     the `link` URL is the run ID.
   - Fetch failing logs: `gh run view <run-id> --log-failed`. Identify
     the root-cause line — test name, lint rule, or compile error.
     Surface that, not the full log.
6. Map failing checks to likely culprit areas using the PR diff:
   `gh pr diff $ARGUMENTS --name-only` — match against the workflow that
   failed (e.g. `protofleet-server-checks.yml` failing with `server/`
   diffs is straightforward; failing without `server/` diffs is suspicious).
7. **Pull and triage reviewer feedback.** Fetch all comment surfaces:
   - Line comments: `gh api repos/<owner>/<repo>/pulls/<n>/comments`
   - Issue comments: `gh api repos/<owner>/<repo>/issues/<n>/comments`
   - Reviews: `gh pr view $ARGUMENTS --json reviews`

   Dedupe findings that appear from multiple sources (the same path:line
   flagged by both Copilot and Codex is one finding, not two). For each
   unique finding, classify:
   - **Priority** — use the comment's own badge (`P0`/`P1`/`P2`,
     low/medium/high) if present; otherwise infer from severity language.
   - **Status** — `valid` (real, needs fix), `already-addressed` (fixed
     in a later commit on the branch — re-check the current code on disk),
     `invalid` (false positive, with a brief reason), or
     `needs-discussion`.

   Output a punch-list table: file:line | source | priority | finding |
   status. Skip purely informational bot output (e.g. "auto-formatted N
   files"). Group the table after the CI section in the final summary.
8. Propose the next concrete action based on the CI summary AND the
   comment triage. Examples: "Fix the failing test in X", "Address the
   P1 from Codex re: <thing>", "Rerun CI (probably flaky)", "This needs
   a rebase against main", "Approval is the only blocker".

## Notes

- Do NOT push a fix, post a reply, or rerun checks. Triage is read-only.
- If the PR is in another repo, pass the URL form to `gh pr view`.
