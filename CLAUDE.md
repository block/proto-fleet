See [AGENTS.md](./AGENTS.md) for canonical guidance and task-specific references.
Canonical repository skills live under `.agents/skills/`; Claude discovers
them through the per-skill links in `.claude/skills/`.

When addressing review feedback, re-check each finding against the current
target code before treating it as already addressed. Complete scoped fixes
and applicable validation under `/pr-ready`; avoid unrelated refactors.
