---
description: Run the repository generation pipeline and report source and generated changes.
argument-hint: (no arguments)
---

Use the [generation skill](../skills/code-generation/SKILL.md) to run
`just gen` and inspect the resulting diff. Resolve failures caused by the
requested change and rerun affected generation; report external blockers.
Summarize output by consumer/language, explain unexpected changes with
evidence, and identify artifacts that belong with the source commit.
An empty diff is a valid no-op result. Do not commit unless requested.
