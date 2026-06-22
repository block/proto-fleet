---
title: GitHub-safe Mermaid diagrams in PR descriptions
date: 2026-06-22
category: docs/solutions/best-practices
module: documentation
problem_type: pr_description_rendering
component: pr_descriptions
symptoms:
  - "GitHub PR description shows 'Unable to render rich display'"
  - "Mermaid parse error near a quoted flowchart node label"
root_cause: mermaid_syntax_incompatibility
resolution_type: documentation_guardrail
severity: medium
related_components:
  - AGENTS.md
  - .claude/commands/pr-describe.md
tags: [github, mermaid, pr-description, diagrams, agent-guidance]
---

# GitHub-safe Mermaid diagrams in PR descriptions

## Problem

GitHub's Mermaid renderer rejects flowcharts that use bare quoted strings as
node identifiers:

```mermaid
flowchart TD
  "Release artifact build" --> "Server tarball includes virtual-plugin and config"
```

That form can produce a PR description error like:

```text
Unable to render rich display
Parse error ... got 'STR'
```

## Solution

Use stable node IDs with bracketed quoted labels:

```mermaid
flowchart TD
  A["Release artifact build"] --> B["Server tarball includes virtual-plugin and config"]
```

For labels with punctuation, paths, environment variables, or spaces, keep the
text inside the bracketed label and keep the node ID simple:

```mermaid
flowchart TD
  Start["ENABLE_VIRTUAL_MINERS=true"] --> Copy["Copy /app/optional-plugins into /app/plugins"]
  Copy --> Loader["Fleet API plugin loader"]
```

## Prevention

- In PR descriptions, never write flowchart edges between bare quoted strings
  like `"Label" --> "Other"`.
- Always use explicit node IDs plus bracketed labels:
  `A["Label"] --> B["Other"]`.
- Keep IDs alphanumeric and short (`A`, `Build`, `PluginLoader`); put all
  reviewer-facing text in the label.
- If editing `.claude/commands/pr-describe.md` or `AGENTS.md`, preserve this
  rule in the PR-description diagram guidance.

