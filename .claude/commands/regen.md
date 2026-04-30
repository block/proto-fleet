---
description: Regenerate all generated code (protos, sqlc, formatted output) and report what changed.
argument-hint: (no arguments)
---

Run the full code-generation pipeline and summarize the result so the user
knows what to stage and commit.

## Steps

1. Run `just gen` from the repo root.
2. Group the resulting diff by language (Go, TypeScript, Python SDK,
   formatting) and surface anything that looks unrelated to the source edit.
3. Remind the user to commit source + generated output in a single commit.

If `just gen` fails, surface the error verbatim and propose a fix rather than
retrying. An empty diff is a meaningful "no-op" signal — say so explicitly.
