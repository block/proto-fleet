# ProtoOS browser tests

## Rig suite

The default `playwright.config.ts` targets an MDK v1 rig. Read `config/test.config.defaults.ts` and any ignored `config/test.config.local.ts` override before running it: settings/onboarding tests can change the target device. From this directory:

```bash
npx playwright test --project=desktop
```

The existing `containerHome`, `containerPools`, and `containerTanks` specs are opt-in via `CT1_BASE_URL`. They expect a separately started protoOS server backed by fake-ct1; tank-health coverage requires the full demo topology. Do not point the rig suite at CT1 or run state-changing tests against a shared controller without authorization.

## CT1 production standalone

`playwright.ct1.config.ts` is a separate, self-contained suite for the on-device production contract. It uses the built `client/dist/protoOS` directly—not Vite dev/preview or an API proxy. Its web server starts an isolated fake-ct1 on `http://127.0.0.1:18081` with two tanks, two modules per tank, and demo states disabled. Browser entry, assets, probe, reads, and scoped writes all share that origin. A second isolated simulator at `http://127.0.0.1:18082` supplies the complete six-tank, one-module-per-tank map for local firmware tests. Both use ephemeral native gRPC ports.

Activate Hermit from the repository root before project commands. From `client/`:

```bash
npx playwright install chromium
npm run test:e2e:ct1

# Build and run only one project/scenario
npm run test:e2e:ct1 -- --project=desktop --grep "scoped tank"
```

The npm pretest always builds protoOS first. To reuse an already built bundle while debugging, from `client/` run:

```bash
npx playwright test --config e2eTests/protoOS/playwright.ct1.config.ts --project=desktop
```

The suite refuses to reuse a running server, so an occupied port is a setup error, not permission to test a different miner. It does not use `CT1_BASE_URL` or the rig's local configuration. Tests assert:

- CT1 probe resolution and successful production JavaScript, stylesheets, and body/monospace font loads from the same server;
- container Home's chart-free schematic Overview and live stat labels, plus successful aggregate history and plotted data on Hashrate; tank/module deep-link refreshes without login/onboarding, including successful selected-board ASIC reads and populated temperatures;
- diagram Fit, zoom below Fit, bounded pan, keyboard navigation, native two-finger pinch, and equipment clicks after dragging, without issuing control writes;
- fresh-storage boot separately from returning users with persisted synthetic v1 bearer credentials;
- completed entry assets for each document and current-document assets; optional prefetches from replaced documents need not finish, but all observed HTTP errors and failures other than old-document navigation cancellations still fail the test;
- adapter reads and writes without `Authorization`, with `X-Frigate-Actor: proto-os`, even with a persisted synthetic v1 bearer session; only the first system-info resolver probe per document load is undecorated;
- confirmed scoped tank stop/start, exact module coordinates, visible observed state, and unchanged boards in the neighboring tank;
- schematic pump-speed and whole-cooler manual-fan edits, native MDKv2 payloads and actor headers, persisted values on reopening, and restoration of the touched setpoints and cooler mode;
- F1 metadata rejection, a two-chunk upload accepted by the simulator's size and digest check, exact board/build/force targeting, lost Start response and reload recovery without another Start, per-board completion/failure, cancellation, a refused Start left untracked, and an update no controller knows kept across a reload until the operator stops tracking; same-version uploads preserve the firmware baseline and cleanup cancels created jobs and restores isolation;
- narrow restoration of the touched scope in a fixture with its own 30-second timeout and independent API context, even after a browser test timeout.

Reports go to `playwright-report/ct1/`, within the existing lint/git artifact exclusions; traces and failure screenshots go to `test-results/ct1/`. The ProtoOS E2E workflow runs this suite on desktop and mobile alongside the independent rig job. Simulator unit tests run once in Server Checks' Fake CT1 job. No visual baselines are created or replaced.

This validates the current client against a simulator with MDK v2 host-compatible routing, not the host's artifact verification, package installation, or a physical control host. See [the client deployment contract](../../README.md#ct1-on-device-standalone) for artifact ownership and rollout verification.
