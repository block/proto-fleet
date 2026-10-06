---
name: proto-fleet-playwright-e2e
description: Add, fix, or review ProtoFleet/ProtoOS Playwright coverage and diagnose failures in those tests.
---

# Proto Fleet Playwright E2E

Complete the requested coverage and verify the affected spec. Use the existing
specs, page objects, setup helpers, and relevant UI as evidence. This skill
does not expand a test request into unrelated product refactoring.

## Boundaries

- Try the current environment after checking its configured target. Read
  [environment guidance](references/environment.md) for setup and target issues.
  Tests can target real miners; do not assume a running environment is disposable.
- Within an authorized local fake/simulator setup, diagnose and fix failures
  caused by the change, then rerun affected checks without repeated approval.
  Stop for a concrete blocker or an action beyond the requested scope.
- Test attributes on real interactive/asserted elements are permitted when
  they match existing patterns. For a coverage-only request, get approval
  before changing other product behavior or shared fake miner behavior;
  existing explicit authorization for that change is sufficient. Shared
  fixture changes require checking affected contract/E2E consumers.
- Follow the root AGENTS.md snapshot replacement approval rule. A failed
  visual comparison does not authorize refreshing expected images.

## Coverage quality

- Specs express user flows and assertions; page objects own selectors,
  responsive differences, and repeated UI interactions. Keep helpers focused.
- Use deterministic setup and locators scoped to the intended entity. Avoid
  `.first()` workarounds, fallback selector chains, and positional guesses.
- Assert visible behavior and request payloads when incorrect targeting is
  the regression risk; a successful HTTP status alone is insufficient.
- Capture and restore persistent state with narrow `afterEach`/`finally`
  cleanup. Tests should rerun independently; prefer round-trip flows over
  state-coupled tests. Do not conceal failed cleanup.
- Use `test.step` for meaningful phases without nesting helpers that already
  define steps. Wait on real state transitions instead of increasing timeouts
  or adding sleeps to hide failures.

## Verification and completion

Use [verification guidance](references/verification.md) for commands and failure
diagnosis. Run touched-file lint and the narrowest meaningful scenario, then
the touched spec. Broaden for relevant shared behavior or unresolved risk.

Report changes, checks, and remaining gaps, distinguishing product, test,
environment, auth, and external-dependency failures. For review feedback,
re-check findings against the current target code and fix supported issues.
Continue through commit/push/PR creation when already requested; otherwise
finish with the verified result rather than a mandatory publishing question.
