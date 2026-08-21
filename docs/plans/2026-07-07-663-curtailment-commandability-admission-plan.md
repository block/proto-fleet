---
title: "Curtailment: admit reachable pool-less miners (commandability admission)"
date: 2026-07-07
status: implementing
type: plan
tracker: https://github.com/block/proto-fleet/issues/663
---

# Curtailment: Admit Reachable Pool-less Miners

## Problem

Both curtailment admission classifiers exclude `NEEDS_MINING_POOL` miners even
though they are reachable, authenticated, have a driver, and draw idle power —
a sleep command would land. On a 14-miner all-paired full-shutdown dev-fleet
event, 10 miners were held `unavailable`, several of them reachable pool-less
proto miners whose idle draw persisted through the curtailment.

Curtailment's job is to stop power draw; whether the miner is productively
mining is irrelevant. Admission should converge on **commandability** (can we
deliver a sleep command?), not mining status.

## Scope

- `NEEDS_MINING_POOL` becomes targetable in both classifiers:
  - Normal selection (`classifyCandidates`): admitted, still behind the
    existing telemetry-freshness gates. Fixed-kW's dual-signal filter still
    excludes zero-hash miners, so kW-sized selection stays sound; the
    practical admission change lands in full-fleet selection, plus more
    accurate skip reasons in fixed-kW.
  - All-paired policy (`AllPairedPolicyTargetState`): initial state `pending`,
    dispatched normally instead of parked `unavailable`.
- `INACTIVE` stays excluded **by design**: codebase-wide it means "sleeping"
  (CSV export, dashboard sleeping counts). Admitting it would produce no-op
  curtails and let restore wake miners an operator deliberately put to sleep.
- `ERROR`/`UNKNOWN` stay parked in the all-paired path (that path has no
  telemetry gates); documented in code comments.
- Genuinely unreachable states unchanged: `OFFLINE`, `UPDATING`,
  `REBOOT_REQUIRED`, missing status, missing driver, `AUTHENTICATION_NEEDED`.
- Baseline persistence fixed for never-hashing miners (see below).
- Server-only. No UI change: the skip-reason vocabulary is unchanged and the
  live rollup already surfaces pending/confirmed transitions.

## Key finding: baseline/restore gap for never-hashing miners

`shouldPersistBaselinePowerW` rejects FullFleet baselines below
`candidate_min_power_w` (default 1500 W). Pool-less idle draw is typically
below that, so no baseline would be persisted and confirmation degrades to the
hash-only fallback — which is provably broken for a never-hashing miner:

- `isCurtailed` hash-only fallback confirms instantly (hash is already 0) —
  a false positive before the sleep command takes effect.
- `isRestored` hash-only fallback requires hash > 0, which never happens —
  restore ages out to `restore_failed`.

Fix: bypass the FullFleet baseline floor when the candidate is **not
currently hashing** (hash missing or <= 0). The floor exists to keep garbage
low-power readings from becoming baselines for hashing miners, where the
hash-only fallback works; for non-hashing miners the fallback cannot work, so
a real power baseline is strictly better. This also fixes the same latent bug
for ERROR/UNKNOWN phantom loads admitted into full-fleet selection today.

## Implementation units

### U1. Admit NEEDS_MINING_POOL in normal selection

`server/internal/domain/curtailment/service.go`: move `NEEDS_MINING_POOL` out
of the skip arm into a fall-through arm (commandability admission); keep
`INACTIVE` on `SkipNonActionableStatus` with an exclusion-by-design comment.
Update `TestDeviceStatusClassifierMatrix` and
`TestService_Preview_FiltersByPairingDeviceStatusAndStaleness`; add coverage
for fresh-telemetry admission, stale-telemetry skip, and the dual-signal
interaction in fixed-kW vs full-fleet modes.

### U2. All-paired policy dispatches pool-less miners

`server/internal/domain/curtailment/selector.go`: remove `NEEDS_MINING_POOL`
from the unavailable arm of `AllPairedPolicyTargetState` so it falls through
to `TargetStatePending`. Update both classifiers' sync comments. Update
`BuildAllPairedPolicyPlan` tests (pool-less pending, idle power counted in the
estimate) and add reconciler tests: pool-less target dispatches and confirms;
a parked `unavailable` pool-less target on an in-flight event is promoted and
dispatched on the next tick (existing events self-heal after deploy — no
migration needed).

### U3. Baseline persistence at idle draw

Thread a hashing signal into baseline persistence:
`SelectedDevice.HashRateHS`, `shouldPersistBaselinePowerW(..., hashing)`,
`BuildInsertTargetParams`, `AllPairedPromotionBaselinePowerW`. Non-hashing
candidates persist any positive finite power baseline. Tests: idle-baseline
curtail confirm requires power to actually drop; restore confirms when power
returns; hashing miners below the floor keep today's behavior.

### U4. Docs and verification

This plan doc; `just lint`; full curtailment package + reconciler test suites.

## Risks

- Low-baseline flap: if a miner's sleep draw exceeds half its idle draw,
  drift/confirm could oscillate. Mitigated by the configurable
  `DriftThresholdFactor` and the drift path's preserve-on-missing semantics.
- The classifier matrix test intentionally breaks unless both switches move
  together; U1/U2 land as one logical change.
