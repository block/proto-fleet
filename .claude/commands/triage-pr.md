---
description: Triage a pull request — fetch metadata, check status, summarize failing CI logs, and propose next steps.
argument-hint: <pr-number-or-url>
---

Triage PR `$ARGUMENTS`. Goal: give the user a one-screen status read so
they can decide what to do next without reading the GitHub UI.

## Steps

1. Resolve the PR identifier (number or URL) and fetch metadata:
   `gh pr view $ARGUMENTS --json number,title,state,isDraft,mergeable,mergeStateStatus,headRefName,baseRefName,author,reviewDecision,statusCheckRollup`
2. Fetch check status:
   `gh pr checks $ARGUMENTS`
3. Summarize in this shape:
   - **PR**: number, title, author, branch
   - **State**: open/closed/merged, draft, mergeable status
   - **Reviews**: approval state
   - **CI**: count of pending / failing / passing checks. Name the failing
     ones explicitly.
4. For each failing check, fetch its logs:
   `gh run view --log-failed --job <id>` (resolve the job id from the
   check name via `gh pr checks --json`)
   Identify the root-cause line — usually a test name, lint rule, or
   compile error. Surface that, not the full log.
5. Map failing checks to likely culprit areas using the PR diff:
   `gh pr diff $ARGUMENTS --name-only` — match against the workflow that
   failed (e.g. `protofleet-server-checks.yml` failing with `server/`
   diffs is straightforward; failing without `server/` diffs is suspicious).
6. Propose the next concrete action: "Fix the failing test in X",
   "Rerun CI (probably flaky)", "This needs a rebase against main",
   "Approval is the only blocker", etc.

## Notes

- Do NOT push a fix, comment on the PR, or rerun checks. Triage is
  read-only.
- If the PR is in another repo, pass the URL form to `gh pr view`.
