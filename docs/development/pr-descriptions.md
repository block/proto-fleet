# PR descriptions

When opening a PR, write the description so a reviewer can judge the
architecture and technical decisions **without reading the low-level code**.
Inspect the actual diff, commits, and changed files first; describe what the
code does, not the decisions made getting there. Structure it as:

1. `Reviewable diff: +<additions>/-<deletions> across <files> files (excludes generated, test, and story files).`
   The line count comes from the PR diff after excluding generated files
   (`**/generated/**`, `*.pb.go`, `*.pb.ts`,
   `client/src/protoOS/api/generatedApi.ts`), test files (`*_test.go`,
   `*.test.*`, `*.spec.*`, `__tests__/**`, `tests/**`, `test/**`), and story
   files (`*.stories.*`, `*.story.*`, `stories/**`). Put this line first, above
   Summary.
2. **Summary** — 2-4 sentences: what this PR delivers and why. Lead with the
   user- or operator-facing capability, not the implementation. For a PR in a
   series (stacked on a parent, has descendant PRs, or marked `N/M`), add a
   short *Stack* note: the full chain with PR links (ancestors, this PR, and any
   PRs stacked on top), that the diff is relative to the immediate base when it
   has ancestors, the load-bearing context from upstream PRs
   a reviewer needs to judge this change, and what is intentionally out of scope
   here and where the remaining work lands (from descendant PRs, the plan docs,
   or tracking issues, since later phases may not be open as PRs yet).
3. **How it works** — the end-to-end mechanism in plain language. Walk the
   primary flow(s): who triggers it, what crosses each boundary, where state
   is persisted, what comes back. Explain workflows, not language syntax.
4. **Diagrams** — mermaid in fenced code blocks labeled `mermaid` so they render on
   GitHub. At least a component/flow diagram of the main path; add a state or
   sequence diagram where lifecycle or ordering matters. Keep syntax
   GitHub-safe: quote labels with special characters, avoid fragile edge styles,
   and use explicit node IDs with bracketed labels (`A["Label"] --> B["Other"]`).
   Do not use bare quoted string nodes (`"Label" --> "Other"`); GitHub's Mermaid
   parser rejects that form in flowcharts.
5. **Areas of the code involved** — a table mapping each changed area to its
   role so reviewers know where to focus: `| Area / package / file | What
   changed | Why it matters for review |`. Group by subsystem (`proto/`,
   `server/`, domain logic, migrations, `client/`, `plugin/`); flag generated
   code as "generated — skip".
6. **Key technical decisions & trade-offs** — the choices a reviewer should
   scrutinize (new abstractions, data-model/migration changes, security or
   validation boundaries, backward-compat or rollout concerns). One line each:
   the decision and the alternative it was chosen over.
7. **Testing & validation** — how correctness was verified and what is
   explicitly not covered.

Keep it concise: tables and diagrams over long paragraphs, no filler praise,
no narration of rejected approaches. Claude Code users can generate this with
`/pr-describe`.


## Counting the reviewable diff

Count the final base-to-head diff, not the sum of per-commit patches. For a
published PR use its aggregate diff; for local work use the merge-base diff
against its verified immediate base. Exclude a renamed file when either its
old or new path matches an excluded pattern. Count non-excluded binary files
in the file total, omit their nonnumeric line counts, and append
`; <N> binary files have no line count` when applicable.
