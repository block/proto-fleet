---
name: proto-fleet-playwright-e2e
description: Use when adding, fixing, or reviewing Playwright E2E coverage in the block/proto-fleet monorepo, especially after a feature is already implemented. Covers scenario design, spec and page-object changes, local env-aware verification, simulator-safe cleanup, and follow-up on E2E PR comments or CI failures.
---

# Proto Fleet Playwright E2E

Use this skill for Playwright E2E work in `block/proto-fleet` only.

Primary use cases:
- add or fix Playwright E2E coverage for ProtoFleet or ProtoOS features
- run the tests locally until the touched scenario/spec passes
- follow up on PR comments or CI failures that are specifically about those E2E changes

Do not use this skill for:
- general product implementation
- unit/integration tests outside the Playwright E2E suite
- broad refactors unrelated to getting the E2E coverage working

## Default behavior

Assume the feature implementation is already done.

Default to:
1. implementing or adjusting the E2E coverage
2. verifying it locally
3. reporting the results concisely

After local verification succeeds, always ask whether to also commit, push, and open a PR.
Do not silently stop after reporting passing local verification.
Before any git push, rerun the relevant lint check for the touched files.

## First moves

1. Read `AGENTS.md` and follow repo rules.
2. Try the current env first.
3. Inspect before editing:
   - the target spec file, if it exists
   - the corresponding page object
   - nearby spec files that show local style
   - shared helpers/components used by similar tests
   - relevant UI component code if selectors or state are unclear

If the env is blocked or unavailable, ask whether you should set it up yourself or the user will do it.

If setup state is unclear, prefer existing setup specs/helpers over inventing a manual setup flow.

Always state env/setup assumptions explicitly.

## Env behavior

- Do not start by asking setup questions if the current env might already work.
- Try the current env first.
- Only ask once blocked:
  - whether the env should be set up by you or by the user
  - whether setup/onboarding steps have already been completed
- If the user restarts the env, rerun the exact previously blocked targeted test first.
- Do not claim verification if setup prerequisites were skipped.

Remember that for ProtoFleet and ProtoOS, “app running” is not always enough. Many specs require post-setup state.

## Scope and allowed changes

Keep PR scope tight.

Allowed by default:
- add `data-testid` or similar element attributes when they fit existing patterns

Before making other product/source changes, or when touching shared fake miner fixtures under `server/fake-antminer/` or `server/fake-proto-rig/`, stop and ask briefly:
- what you plan to change
- why the test needs it
- what shared surfaces may be affected

If fake miner changes are approved, call out the shared impact and run the relevant follow-up checks, such as `just test-contract`, before calling the work finished.

Do not broaden into product refactors unless the user explicitly asks.

## Test-writing rules

- Specs should read as user flows plus assertions.
- Keep spec files easy to scan.
- Do not accumulate many helper functions, utility types, or setup algorithms at the top of the spec file.
- If logic is not core to reading the scenario, move it to a page object or a nearby helper module.
- Page objects should own:
  - selectors
  - responsive differences
  - repeated UI interaction details
  - UI-specific validation helpers
- Keep helpers small, literal, and easy to read.
- Avoid “god” helpers that navigate, mutate state, and assert several unrelated outcomes.
- If a helper is only used once and does not improve readability, keep the logic inline.
- If a helper is reused or absorbs UI-specific detail, move it into the page object.

Prefer names like:
- `openPowerTargetPopover`
- `clickBlinkLeds`
- `validatePowerTargetWidgetText`

Avoid vague names like:
- `handlePower`
- `processFilters`
- `setupThing`

## Selectors and assertions

- Prefer stable, deterministic locators.
- Prefer `data-testid` when text/class-based selection is likely brittle.
- Put test ids on the real interactive or asserted element, not on unnecessary wrappers.
- Scope locators to the smallest meaningful container before searching by role/text.
- Do not use `.first()` or similar “pick the first match” shortcuts as the main way to make a locator pass.
- Improve the locator itself so it deterministically identifies the correct element.
- Avoid fallback chains, broad XPath/class selectors, and position-based locators unless the UI intentionally guarantees them.
- Keep assertions close to the behavior being verified.
- Prefer a few explicit assertions over generic “page looks good” checks.

If a request triggers an API call, assert:
- the visible user-facing behavior
- the request payload when that is the real regression risk

A `200` alone is often not enough when the real risk is “targeted the wrong entity with valid input”.

## test.step usage

- Use `test.step()` for meaningful user-flow phases.
- Keep steps lean.
- Do not wrap helpers that already define their own `test.step()` inside another `test.step()`.
- Split bulky steps into action and validation when that improves readability.

## Determinism and branching

- Prefer deterministic tests.
- Avoid `if` statements in the spec body when possible.
- Do not branch the user flow in the test just to handle multiple UI states if the scenario can be made deterministic through setup, page-object logic, or helper preparation.
- If branching is unavoidable, keep it out of the main scenario flow and isolate it in helper/setup logic.

## State and cleanup

Strongly prefer restoring persistent state after tests whenever possible.

- Capture original state before mutation when product state persists beyond the test.
- Use `afterEach` or `finally` for cleanup when tests mutate persistent state.
- Prefer independent tests that can run repeatedly in any order.
- If a flow naturally transitions both ways, prefer one round-trip scenario over two state-coupled tests.
- Keep cleanup deterministic and narrow.

## Verification workflow

After editing:
1. run ESLint on touched files only
2. run the narrowest meaningful Playwright verification first
3. if the targeted scenario passes, run the whole touched spec file

Prefer:
- single spec or `--grep` first
- whole touched spec file second
- broader runs only if clearly needed

Run targeted `npx eslint ...` commands from `client/`.
Run targeted Playwright commands from the suite directory, such as `client/e2eTests/protoFleet` or `client/e2eTests/protoOS`, and include an explicit project like `--project=desktop` unless you intentionally need another one.
From the repo root, prefer the canonical broader commands from `justfile`, such as `just test-e2e-fleet` and `just test-e2e-protoos`.

Repo-specific note:
- In this repo, E2E TypeScript files can still fail CI typecheck on unused imports/locals even when touched-file ESLint does not fail. Do not ignore ESLint warnings about unused symbols in `e2eTests/**`. If the change adds new imports/helpers or the file shape changed meaningfully, consider running the client typecheck before calling the work finished.

## Flake handling

Do not start by increasing timeouts.

Use short loops:
1. inspect the failure
2. identify the most likely root cause
3. make one focused change
4. run the narrowest meaningful verification
5. repeat only if needed

Prefer:
- waiting for real state transitions
- waiting for loading to finish
- waiting for stable UI state
- existing wait helpers

Avoid:
- broad sleeps
- infinite polling
- brittle DOM/class soup selectors
- multiple unrelated speculative fixes at once

## Failure classification

Classify failures explicitly as one of:
1. product bug
2. test bug
3. environment/startup problem
4. auth/permission issue
5. external dependency/network flake

If CI never reached the spec, do not talk as if the spec failed.

## Review comments and CI follow-up

When working on PR comments or failing checks related to these E2E changes:
- fix clearly valid comments
- call out questionable or low-value comments instead of looping on minor bot feedback
- keep each fix traceable to the comment/problem it addresses
- rerun the narrowest directly affected local verification after each fix

## ProtoFleet / ProtoOS specifics

- Prefer simulator-friendly scenarios first.
- Avoid exact telemetry assertions unless the env guarantees them.
- On freshly booted envs, prefer shell/state-transition validation over exact chart values.
- Avoid overfitted assumptions about exact network topology unless the fake env guarantees them.
- For auth-gated ProtoOS scenarios, avoid intrusive harnessing like exposing stores or globals.
- If the realistic auth flow cannot be tested cleanly without intrusive hooks, reduce scope and cover only the clean flows.

## Commands and tooling

Prefer small, readable commands.

Common commands:
- `rg`
- `sed -n`
- `git status --short --branch`
- `git diff -- <file>`
- `cd client && npx eslint <touched files>`
- `cd client/e2eTests/protoFleet && npx playwright test spec/<spec>.ts --project=desktop --grep "scenario"`
- `cd client/e2eTests/protoOS && npx playwright test spec/<spec>.ts --project=desktop --grep "scenario"`
- `just test-e2e-fleet`
- `just test-e2e-protoos`

Use `apply_patch` for edits.
Do not create git worktrees.
Stay in the user’s current checkout.

## Reporting

Keep user-facing output concise.

For code changes, include:
- what changed
- what you verified
- any remaining risk or unverified parts

For PR/comment work, include branch, commit, or PR link if relevant.

For analysis-only work:
- findings first
- recommendations second
