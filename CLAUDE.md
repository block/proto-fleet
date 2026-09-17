See [AGENTS.md](./AGENTS.md) for canonical guidance and task-specific references.
Repository skills live under `.claude/skills/` and are exposed unchanged to
other agents through `.agents/skills/`.

When addressing review feedback, re-check each finding against the current
target code before treating it as already addressed. Complete scoped fixes
and applicable validation under `/pr-ready`; avoid unrelated refactors.
