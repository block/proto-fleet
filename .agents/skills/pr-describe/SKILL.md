---
name: pr-describe
description: Write or update a PR description that lets reviewers judge the architecture and technical decisions without reading low-level code — high-level mechanism, mermaid diagrams, and a code-area map.
argument-hint: "(optional: PR number/URL; defaults to current branch PR or draft body)"
---

Write (or update) the description for this PR so a reviewer can understand what
it does and judge the architecture and technical decisions **without reading the
low-level code**. Inspect the actual diff, commits, and changed files first;
describe what the code does, not the decisions made getting there.

Save the optional PR number or URL supplied with the skill invocation as
`pr_ref`. If none was supplied, leave `pr_ref` empty; that selects the
current-branch path below.

## Steps

1. Determine the target and pick the path. Decide once, here, and use the same
   path for every command below — never mix PR-derived refs with the local
   checkout.

   - **Numbered-PR path** — `pr_ref` is a PR number or URL. The target is
     that PR, which may live in a different repository and on a branch you do
     not have checked out. Resolve its refs from metadata, not from local HEAD:
     `gh pr view "$pr_ref" --json number,url,headRefName,baseRefName,title`.
     Capture `number` and parse `owner`/`repo` from the `url` field (which is
     `https://github.com/<owner>/<repo>/pull/<number>`). The `url` is gh's
     output, so it is safe to parse. You need `owner`/`repo` because a bare
     `number` resolves in the *current* repo — a `pr_ref` URL pointing at
     another repo would otherwise read one PR and edit a same-numbered PR here.
     Pass `-R <owner>/<repo> <number>` to every `gh pr` call on this path.
   - **Current-branch path** — `pr_ref` is empty or absent. The target is the
     current branch. `gh pr view --json number,url,headRefName,baseRefName,title`
     tells you whether a PR already exists; if none does, you will draft the
     body for the PR the user is about to open from this branch.

   After resolving refs, check whether the target is part of a **series**
   (stacked or multi-part). Any one of these signals counts: `baseRefName` is
   not the repository's default branch (it is stacked on a parent PR); it has
   descendant PRs (others are stacked on it); or its title carries an `N/M` or
   `part N` marker. A foundation PR that targets the default branch but has
   descendants still counts, so do not gate on the base ref alone. Walk the
   chain both ways. Upward: find the parent PR whose head is this PR's base
   (`gh pr list --head "<baseRefName>" --state all --json
   number,title,url,baseRefName`, adding `-R <owner>/<repo>` on the numbered-PR
   path), repeating on the parent's base until you reach the default branch.
   Downward: find child PRs whose base is this PR's head (`gh pr list --base
   "<headRefName>" --state open --json number,title,url,baseRefName,headRefName`, adding `-R`),
   repeating on each child's head. Record both ancestors and descendants (each
   one's number, title, url) for steps 2 and 3.

2. Read the change using the path chosen in step 1 — do not fall back to local
   `git` on the numbered-PR path, since local HEAD may be an unrelated branch:

   - **Numbered-PR path:** using the `owner`/`repo`/`number` from step 1,
     `gh pr diff <number> -R <owner>/<repo>` for the full diff and
     `gh pr diff <number> -R <owner>/<repo> --name-only` for the file list.
     Pull the commit list from
     `gh pr view <number> -R <owner>/<repo> --json commits`. All of these read
     the PR head in its own repo, regardless of what is checked out locally.
   - **Current-branch path:** fetch the base first (`git fetch origin "<base>"`)
     and diff against `origin/<base>`, never the bare local ref, so a parent
     branch that has moved (common in a stack) can't produce a stale diff:
     `git diff "origin/<base>...HEAD"` (full diff),
     `git diff "origin/<base>...HEAD" --stat`, and
     `git log "origin/<base>..HEAD" --oneline`, where `<base>` is the
     `baseRefName` from step 1 (default `main`).

   From the file list, identify which subsystems are touched (`server/`,
   `client/`, `plugin/`, `proto/`, `migrations/`, `packages/proto-python-gen/`).

   Compute reviewable counts using the exclusions and rename/binary rules in
   the [PR standard](../../../docs/development/pr-descriptions.md#counting-the-reviewable-diff).
   For a numbered PR, save its aggregate `gh pr diff <number> -R <owner>/<repo>`
   output and use `git apply --numstat` on that diff. Do not use `--patch`,
   which supplies per-commit patches and can count the same edit repeatedly.
   For local work, use `git diff "origin/<base>...HEAD" --numstat`.
   Inspect renamed paths explicitly so either side's exclusion applies.

   If the target is part of a series (step 1), also read each ancestor PR's description
   (`gh pr view <number> --json title,body,url`, adding `-R` on the numbered-PR
   path) and extract the load-bearing context this PR depends on: the contracts,
   abstractions, schema, or decisions established upstream that a reviewer must
   understand to judge this change. For each descendant, capture a one-line
   scope from its title (skim its body only if the title is opaque) so you can
   tell the reviewer what is deferred to later PRs and where it lands.

   Remaining work is often not open as a PR yet, so do not stop at descendant
   PRs. Also draw on linked issues, PR discussions, and external design
   documents for the phasing and explicit out-of-scope items, and, when running
   interactively, on this conversation, which may name deferred scope and tracking
   issues/PRs it lands in. Record those deferred items with their tracking
   references even when no PR exists for them yet (state facts about scope, not
   the back-and-forth of how the work was planned).

3. Draft the description using the complete
   [PR description standard](../../../docs/development/pr-descriptions.md).
   Use the same resolved target, diff, reviewable counts, and stack context
   throughout. Include validation evidence and explicit gaps.

4. Apply the result against the target resolved in step 1:
   - For draft-only or audit-only requests, output the proposed body without
     editing GitHub. Otherwise, when updating the description is requested
     and a PR exists, update **that** PR by its `number`, scoped to its repo:
     `gh pr edit <number> -R <owner>/<repo> --body-file <tmp>` (write the body
     to a temp file to preserve mermaid fences and tables). The `-R` is what
     keeps a cross-repo URL target from editing a same-numbered PR in the local
     repo. On the current-branch path `-R` is unnecessary (the PR is local).
   - If no PR exists yet (current-branch path only), output the body for the
     user to use when opening it.

## Rules

- Mechanism and architecture over line-by-line detail. If a reviewer needs to
  open a file to understand the shape of the change, the description has failed.
- Don't narrate the back-and-forth or rejected approaches — describe the final
  state.
- No filler praise. Be concise; prefer tables and diagrams over long paragraphs.
- Always quote JSON-derived branch refs (`headRefName`, `baseRefName`) when
  passing them to a shell command. Branch names may contain shell
  metacharacters (a branch literally named `feature;rm -rf foo` is valid on
  GitHub), so an unquoted ref can run unintended commands instead of just
  querying `gh`/`git`.
