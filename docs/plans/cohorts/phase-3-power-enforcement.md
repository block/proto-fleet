---
title: "Cohorts Phase 3 — power enforcement (SDK getter + plugins)"
date: 2026-06-19
status: draft
type: plan
---

# Phase 3 — Power enforcement

> Read [`README.md`](./README.md) first. Deep design: [TDD](../2026-06-19-cohorts-fleet-enforcement-tdd.md)
> §Continuous enforcement reconciler (SDK getter gap).

## Context & prerequisites

**Prerequisite:** Phase 2 merged (the enforcement reconciler drives firmware/pools/cooling). This
phase adds **power/performance mode** as a fourth enforced dimension. It is gated on an SDK change
because, unlike cooling (`GetCoolingMode` already exists), power is currently **set-only**:
`SetPowerTarget` exists but there is no `GetPowerTarget`, so power drift can't be observed today.

This phase spans the SDK and **every plugin** (antminer, virtual, asicrs), so it's a different
skillset/agent than phases 0–2 and touches the plugin build (`proto-regen` + `asicrs-build`).

## Scope

**In:** add a `GetPowerTarget` getter to the device SDK; implement it in all plugins; extend the config
sweep + reconciler to treat power as an enforced dimension.

**Out:** everything else (firmware/pools/cooling already shipped in phase 2).

## Files to create / modify

- `server/sdk/v1/interface.go` — add to `DeviceConfiguration` (near `:399`):
  `GetPowerTarget(ctx context.Context) (PerformanceMode, error)`. Document the contract: a plugin that
  cannot read it back returns `PerformanceModeUnspecified` + a non-nil unsupported error so the
  reconciler treats it as "unverifiable → hold." (`proto-regen` if SDK protos under `server/sdk/v1/pb/`
  change; `asicrs-build` because the Rust plugin consumes `server/sdk/v1/pb/`.)
- Plugin implementations (each must satisfy the interface or fail to compile):
  - `plugin/proto/...` (Proto Rig MDK) — read current performance/power mode.
  - `plugin/antminer/...` — read from miner conf.
  - `plugin/virtual/...` — return the simulated value.
  - `plugin/asicrs/...` (Rust) — implement + rebuild via `just rebuild-plugin asicrs`.
- `server/fake-proto-rig/` and `server/fake-antminer/` — expose the current power/performance mode so
  E2E + local enforcement can exercise it (`fake-rig-fixtures`).
- Phase-2 substrate + reconciler — populate `device_config_state.observed_power_mode` from the sweep
  (now that the getter exists) and add a `power` row to the per-dimension enforcement loop, dispatching
  via `command.Service.SetPowerTarget`.

## Key implementation notes

- **Graceful degradation:** until/unless a given plugin implements a real getter, it returns the
  unsupported error; the reconciler must treat `observed_power_mode = NULL`/unsupported as "hold," never
  as drift — so partially-capable fleets don't get false re-dispatch. Power is "set-on-assignment only"
  on those devices, exactly as it is today.
- **Reuse the phase-2 machinery:** power is just another dimension in the existing per-device state
  machine + cooldown/observe logic. No new reconciler scaffolding.
- **Mind the build coupling:** touching `server/sdk/v1/interface.go` / `server/sdk/v1/pb/` triggers
  `proto-regen` and the asicrs Docker rebuild; the `plugin-contract-tests` suite is the canonical check
  that the plugins still satisfy the driver contract.

## Acceptance criteria

- A cohort's desired power mode is enforced (observed → corrected) on plugins that implement
  `GetPowerTarget`.
- Plugins that don't implement it degrade cleanly to set-on-assignment with **no false drift**.
- Plugin contract tests pass for all drivers.

## Verification

```bash
just gen                          # SDK/proto regen
just rebuild-plugin asicrs        # if the Rust plugin changed
just test-contract                # canonical plugin-driver-contract check
cd server && go test ./internal/domain/cohort/reconciler/...   # power-dimension unit tests
just lint
```

## Open questions

- Which plugins can actually read power/performance mode back from the device, and what each maps to a
  common `PerformanceMode` enum (reduce/balance/maximize). Plugins that can't stay set-only.
- Whether to ship power enforcement behind a flag until coverage across plugins is complete.
