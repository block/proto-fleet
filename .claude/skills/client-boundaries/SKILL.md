---
name: client-boundaries
description: Use when editing files under `client/src/shared/`, `client/src/protoOS/`, or `client/src/protoFleet/`. Enforces app-import boundaries (`shared/` cannot import apps; `protoOS` and `protoFleet` cannot import each other) and the no-new-`console.log` rule (`console.error` and the existing build-version logger are fine).
---

# client-boundaries

## What to do

1. For files under `client/src/shared/`, verify imports do not reference
   `@/protoOS`, `@/protoFleet`, or relative paths into either app.
2. For files under `client/src/protoOS/`, verify imports do not reference
   `protoFleet`. Symmetric check for `protoFleet`.
3. Scan the changed files for new `console.log` calls. The existing logger in
   `client/src/shared/utils/version.ts` is intentional; do not treat it as
   precedent for new logs.

## What to avoid

- Do not move app-specific hooks, stores, API clients, or feature components
  into `shared/` unless they are made genuinely app-neutral.
- Do not add eslint disables for `no-console` to suppress the rule.
