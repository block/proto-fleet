# Client guidance

- `src/shared/` cannot import from `src/protoOS/` or `src/protoFleet/`;
  the two apps cannot import from each other. Shared hooks, stores, and API
  clients must be app-neutral. Do not disable lint rules to evade boundaries.
- No new `console.log` in production code. The logger in
  `src/shared/utils/version.ts` is intentional; `console.error` is fine.
  Do not suppress `no-console` to introduce logging.
- New routes require coordinated edits to `routePrefetch.ts` (factory and
  tier) and `router.tsx` (`lazy()` wrapper). Follow the top-of-file runbook in
  the relevant app's `routePrefetch.ts`.
- For Playwright coverage, use the
  [E2E skill](../.agents/skills/proto-fleet-playwright-e2e/SKILL.md).
  The root AGENTS.md snapshot replacement approval rule applies to all
  refresh/overwrite commands, including commands suggested by test failures.
- Before completing frontend changes, use the
  [frontend consistency review skill](../.agents/skills/frontend-consistency-review/SKILL.md) to check
  existing design patterns, shared code reuse, and unnecessary UI content.
  Explicit review requests stay read-only.
- Storybook titles use only `Foundation`, `Shared`, `Proto Fleet`, or `Proto OS`
  as their root. Follow [Storybook organization](README.md#storybook-organization)
  for ownership, feature grouping, and Design Drift examples.

Use [README.md](README.md) for client architecture and development commands.
