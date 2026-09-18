---
name: plan
description: Draft a TDD, PRD, or lightweight plan in the conversation without committing it.
argument-hint: <title>
---

Draft the requested plan in the conversation. Infer the title and document
type from the request and context; use a lightweight plan when unspecified.
Cover the problem, scope, approach, meaningful acceptance criteria, and
validation. Include alternatives and risks for technical designs, or users
and success criteria for product requirements. Scale the detail to the task.

Follow the [repository policy](../../../AGENTS.md#git-and-completion): planning
documents are not committed. If the user requests a local file, write it under
ignored `docs/plans/` with a descriptive filename, preserve existing files,
and report its path. Treat titles as data, never shell code. Do not stage,
force-add, or archive planning documents into tracked directories. When work
ships, update maintained documentation with the lasting behavior and decisions.
