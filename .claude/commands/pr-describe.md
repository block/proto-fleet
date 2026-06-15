---
description: Write or update a PR description that lets reviewers judge the architecture and technical decisions without reading low-level code — high-level mechanism, mermaid diagrams, and a code-area map.
argument-hint: (optional: PR number/URL; defaults to current branch PR or draft body)
---

Write (or update) the description for this PR so a reviewer can understand what
it does and judge the architecture and technical decisions **without reading the
low-level code**. Inspect the actual diff, commits, and changed files first;
describe what the code does, not the decisions made getting there.

## Steps

1. Determine the target. If `$ARGUMENTS` is a PR number or URL, use that PR.
   Otherwise describe the current branch's PR (`gh pr view --json
   number,url,baseRefName,headRefName`); if none exists yet, draft the body for
   the PR the user is about to open.
2. Read the change: `git diff <base>...HEAD --stat`, `gh pr diff` (or
   `git diff <base>...HEAD`), and `git log <base>..HEAD --oneline`. Identify
   which subsystems are touched (`server/`, `client/`, `plugin/`, `proto/`,
   `migrations/`, `packages/proto-python-gen/`).
3. Draft the description in this structure:

   1. **Summary** — 2-4 sentences: what this PR delivers and why it exists.
      Lead with the user- or operator-facing capability, not the implementation.
   2. **How it works** — the end-to-end mechanism in plain language. Walk the
      primary flow(s) step by step (who triggers it, what crosses each boundary,
      where state is persisted, what comes back). Assume the reader does not
      know Go/TS idioms; explain workflows and mechanisms, not syntax.
   3. **Diagrams** — include mermaid diagrams in ```mermaid fenced blocks so
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

4. If a PR exists, update it with `gh pr edit <number> --body-file <tmp>`
   (write the body to a temp file to preserve mermaid fences and tables).
   If no PR exists yet, output the body for the user to use when opening it.

## Rules

- Mechanism and architecture over line-by-line detail. If a reviewer needs to
  open a file to understand the shape of the change, the description has failed.
- Don't narrate the back-and-forth or rejected approaches — describe the final
  state.
- No filler praise. Be concise; prefer tables and diagrams over long paragraphs.
