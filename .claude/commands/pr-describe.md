---
description: Write or update a PR description that lets reviewers judge the architecture and technical decisions without reading low-level code — high-level mechanism, mermaid diagrams, and a code-area map.
argument-hint: "(optional: PR number/URL; defaults to current branch PR or draft body)"
---

Write (or update) the description for this PR so a reviewer can understand what
it does and judge the architecture and technical decisions **without reading the
low-level code**. Inspect the actual diff, commits, and changed files first;
describe what the code does, not the decisions made getting there.

## Steps

1. Determine the target and pick the path. Decide once, here, and use the same
   path for every command below — never mix PR-derived refs with the local
   checkout.

   - **Numbered-PR path** — `$ARGUMENTS` is a PR number or URL. The target is
     that PR, which may live in a different repository and on a branch you do
     not have checked out. Resolve its refs from metadata, not from local HEAD:
     `gh pr view "$ARGUMENTS" --json number,url,headRefName,baseRefName,title`.
     Capture `number` and parse `owner`/`repo` from the `url` field (which is
     `https://github.com/<owner>/<repo>/pull/<number>`). The `url` is gh's
     output, so it is safe to parse. You need `owner`/`repo` because a bare
     `number` resolves in the *current* repo — a `$ARGUMENTS` URL pointing at
     another repo would otherwise read one PR and edit a same-numbered PR here.
     Pass `-R <owner>/<repo> <number>` to every `gh pr` call on this path.
   - **Current-branch path** — no `$ARGUMENTS`. The target is the current
     branch. `gh pr view --json number,url,headRefName,baseRefName` tells you
     whether a PR already exists; if none does, you will draft the body for the
     PR the user is about to open from this branch.

2. Read the change using the path chosen in step 1 — do not fall back to local
   `git` on the numbered-PR path, since local HEAD may be an unrelated branch:

   - **Numbered-PR path:** using the `owner`/`repo`/`number` from step 1,
     `gh pr diff <number> -R <owner>/<repo>` for the full diff and
     `gh pr diff <number> -R <owner>/<repo> --name-only` for the file list.
     Pull the commit list from
     `gh pr view <number> -R <owner>/<repo> --json commits`. All of these read
     the PR head in its own repo, regardless of what is checked out locally.
   - **Current-branch path:** `git diff <base>...HEAD` (full diff),
     `git diff <base>...HEAD --stat`, and `git log <base>..HEAD --oneline`,
     where `<base>` is the `baseRefName` from step 1 (default `main`).

   From the file list, identify which subsystems are touched (`server/`,
   `client/`, `plugin/`, `proto/`, `migrations/`, `packages/proto-python-gen/`).

3. Draft the description in this structure:

   1. **Summary** — 2-4 sentences: what this PR delivers and why it exists.
      Lead with the user- or operator-facing capability, not the implementation.
   2. **How it works** — the end-to-end mechanism in plain language. Walk the
      primary flow(s) step by step (who triggers it, what crosses each boundary,
      where state is persisted, what comes back). Assume the reader does not
      know Go/TS idioms; explain workflows and mechanisms, not syntax.
   3. **Diagrams** — include mermaid diagrams in fenced code blocks labeled `mermaid` so
      they render on GitHub. At minimum a component/flow diagram of the main
      path; add a state or sequence diagram where lifecycle or ordering matters.
      Keep syntax GitHub-safe: quote labels containing special characters, avoid
      fragile edge styles (e.g. dotted/labelled edges that GitHub mis-renders).
   4. **Areas of the code involved** — a table so reviewers know where to focus:
      `| Area / package / file | What changed | Why it matters for review |`.
      Group by subsystem. Call out new vs. modified files, and flag generated
      code (`**/generated/**`, `*.pb.go`, `*.pb.ts`) as "generated — skip".
   5. **Key technical decisions & trade-offs** — bullet the choices a reviewer
      should scrutinize: new abstractions, data-model/migration changes,
      security or validation boundaries, backward-compat or rollout concerns.
      One line each: the decision and the alternative it was chosen over.
   6. **Testing & validation** — how correctness was verified (tests added,
      manual checks, migrations run) and what is explicitly NOT covered.

4. Apply the result against the target resolved in step 1:
   - If a PR exists, update **that** PR by its `number`, scoped to its repo:
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
