See [AGENTS.md](./AGENTS.md) for canonical guidance. Claude-specific tooling
lives under `.claude/`.

## PR review workflow

- When addressing PR review comments, treat each comment as a fresh request.
  Do not assume a comment is a duplicate of a prior round without re-checking
  the current code on disk.
- Before pushing a fix-up commit, run `/pr-ready` (or its underlying steps:
  lint, targeted tests, diff review). For non-trivial diffs, also run the
  `simplify` skill on the changed files.
- Prefer scoped fixes over global refactors. Adding a scoped class is safer
  than changing a global token; touching one component is safer than
  modifying a shared base.
