---
description: Scaffold a new TDD, PRD, or plan under docs/plans/ with a date-stamped filename and the appropriate template.
argument-hint: <title>
---

Create a new planning doc under `docs/plans/`. The `plan-conventions` skill
covers the lifecycle and archive convention — this command only handles
creation.

## Steps

1. **Title.** If `$ARGUMENTS` is empty, ask for a title.
2. **Type.** Pick a template:
   - `tdd` — technical design (context, design, alternatives, risks, test plan)
   - `prd` — product requirements (problem, users, success criteria, scope)
   - `plan` — lightweight (context, approach, steps, acceptance)

   Infer from the title if it ends in "TDD" or "PRD"; otherwise ask.
3. **Slug.** Derive kebab-case from the title: lowercase, strip punctuation,
   spaces → hyphens. Drop a trailing "tdd"/"prd" token if the user included
   it — it's already the filename suffix.
4. **Write** to `docs/plans/<today>-<slug>-<type>.md` using the matching
   template below. Substitute `{{title}}` and `{{date}}` (today, ISO format).
   Output the resulting path.

## Templates

### TDD

````markdown
---
title: {{title}}
date: {{date}}
status: draft
type: tdd
tracker:
---

# {{title}}

## Context

What problem does this solve? Relevant prior art in the codebase?

## Goals

Outcomes this delivers.

## Non-goals

What's deliberately out of scope.

## Design

The proposed approach: data flow, key components, and touchpoints in the
existing code.

## Alternatives considered

Other approaches and why they were rejected.

## Risks

What could go wrong, mitigations, rollback plan.

## Test plan

How we'll verify this works.
````

### PRD

````markdown
---
title: {{title}}
date: {{date}}
status: draft
type: prd
tracker:
---

# {{title}}

## Problem

The user-facing problem this solves. Observed evidence.

## Users

Who benefits and how. Primary vs secondary personas.

## Success criteria

Measurable outcomes.

## Scope

Concrete deliverables in this round.

## Out of scope

What's deliberately deferred.

## Open questions

Things that need answers before we can build.
````

### Plan

````markdown
---
title: {{title}}
date: {{date}}
status: draft
type: plan
tracker:
---

# {{title}}

## Context

Background; what triggered this.

## Approach

High-level summary of the work.

## Steps

Ordered milestones or workstreams.

## Acceptance

How we know we're done.
````
