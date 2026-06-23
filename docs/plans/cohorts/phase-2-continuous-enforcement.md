---
title: "Cohorts Phase 2 — continuous enforcement (substrate + reconciler)"
date: 2026-06-19
status: draft
type: plan
---

# Phase 2 — Continuous enforcement

> Read [`README.md`](./README.md) first. Deep design: [TDD](../2026-06-19-cohorts-fleet-enforcement-tdd.md)
> §Observability substrate, §Continuous enforcement reconciler.

## Context & prerequisites

**Prerequisite:** Phase 1 merged (cohorts can hold a desired firmware/config; lease + release work).
This phase makes that desired state *enforced*: a background reconciler observes each device's current
firmware/config, compares it to its cohort's desired state, and corrects drift — and because
release/expiry moves a device to the default cohort, **reset-on-release "just works" via convergence**
(no separate reset code).

This is the novel part of the project. The reconciler is a near-verbatim clone of
`server/internal/domain/curtailment/reconciler/reconciler.go`; the genuinely new work is the
**observability substrate** that makes "current firmware/config per device" queryable.

## Scope

**In:** the substrate (firmware/config observed-state shadows, the firmware variant registry, the
per-device enforcement-state table, the worker-name normalization helper); the enforcement reconciler
for **firmware, pools, and cooling**; wiring into `fleetd`.

**Out:** power enforcement (phase 3 — needs a new SDK getter); rollout/canary (deferred).

## Files to create / modify

### Substrate (migrations + sqlc + small writers)
- `server/migrations/0000NN_create_cohort_enforcement_substrate.{up,down}.sql` — `device_firmware_state`
  (PK `device_identifier`, `firmware_version`, **`observed_at`**, `org_id`); `device_config_state`
  (observed pools/cooling, `observed_at`); `firmware_release` (`(org_id, channel, model, manufacturer)
  → firmware_file_id, target_version`, active partial-unique index); `device_enforcement_state`
  (PK `(device_identifier, dimension)`: `state`, `retry_count`, `last_batch_uuid`,
  `last_dispatched_at`, `last_error`); a cohort reconciler heartbeat (`CHECK(id=1)`, *clone-from*
  `migrations/000042:186`). Confirm the next free number before writing.
- `server/sqlc/queries/cohort_enforcement.sql` + generated.
- **Firmware shadow write:** extend `persistFirmwareVersionIfChanged`
  (`server/internal/domain/telemetry/service.go:871`) to also upsert `device_firmware_state` with
  `observed_at = now`, bumping `observed_at` on every non-empty observation (debounced), threading
  `orgID`. Do **not** add firmware to `device_metrics` (Timescale hypertable; wrong read shape).
- **Config sweep:** a slow loop (e.g. piggyback telemetry, or a dedicated ticker) calling
  `GetMiningPools`/`GetCoolingMode` (`server/sdk/v1/interface.go:405,398`) into `device_config_state`.
  Decouples per-device RPC fanout from the fast drift tick.
- **`configdrift` helper** — worker-name-aware pool comparison. The executor suffixes pool usernames
  (`appendMinerNameToPoolUsername`); strip/reconstruct via `workername.FromPoolUsername`
  (`server/internal/domain/workername/workername.go`). One shared helper used by both the sweep and the
  drift check so they agree.

### Reconciler
- `server/internal/domain/cohort/reconciler/reconciler.go` — clone curtailment's structure: singleton
  30s tick, `Start`/`Stop` + watchdog, per-tick + per-device panic isolation, heartbeat upsert,
  optimistic-concurrency state writes. Resolve desired state by **device → cohort (or default)**;
  resolve `desired_firmware_channel` → file+version via `firmware_release` per the device's model
  (best-effort: no match ⇒ skip that device). Run a **per-dimension** state machine
  (`firmware`/`pools`/`cooling` independent): `pending→dispatching→dispatched→confirmed`,
  `confirmed→drifted→(re-dispatch)`, terminal `failed` at `MaxRetries`.
- Dispatch surfaces: firmware → `command.Service.FirmwareUpdate(ctx, selector, firmwareFileID)`
  (`command/service.go:1357`); pools → credential-free actor-gated reapply (reuse
  `execution_service.go:483` stored-worker-name reapply — the synthetic ctx can't pass
  `verifyUserCredentials`); cooling → `command.Service.SetCoolingMode`.
- `server/cmd/fleetd/main.go` — construct + `Start`/`defer Stop` the reconciler next to the
  curtailment block (~`:474`), with `cohortStore` + a command dispatcher + `ActorCohort` context.

## Key implementation notes

- **Stale ⇒ hold, never drift.** If `observed_at` is older than a staleness window, treat the device
  as unverifiable and hold — don't re-dispatch. Mirror curtailment's missing-evidence asymmetry
  (`reconciler.go` `checkDrift`/`confirmOneDispatched`), so a flaky sensor can't cause a reflash storm.
- **Mandatory firmware open-batch guard** (firmware is *not* idempotent mid-install): before
  re-dispatching firmware, skip if the device's last firmware batch isn't finished
  (`queue.IsBatchFinished` / `GetBatchStatusAndDeviceCounts`); treat plugin install states
  `installing`/`confirming` as "in progress, hold."
- **Anti-storm debounce:** a long `FirmwareReDispatchCooldown` (30–60 min) + require N consecutive
  fresh drift observations before re-dispatch. Config/cooling can use a shorter cooldown.
- **Per-dimension independence:** never reflash firmware to fix a pool drift; each dimension has its
  own row in `device_enforcement_state`.
- **Curtailment wins:** a device under active curtailment will have cohort dispatches filter-skipped;
  treat that skip as "hold, don't burn retry budget" (special-case the `curtailment_active` skip reason).
- **Reset = convergence:** no reset-specific code. A released device is in the default cohort; if the
  default has desired firmware/config, the same loop drives it there; if not, it's left as-is.
- **Multi-instance caution:** the reconciler is a singleton with a heartbeat but no leader election.
  Because firmware isn't idempotent, guard the firmware dispatch path with a `pg_advisory_xact_lock`
  (cf. authz reconcile) or `SELECT … FOR UPDATE`, beyond the optimistic-concurrency state writes.

## Acceptance criteria

- A cohort with a desired firmware drives its members to that version, and **re-applies** if a device
  drifts off it; a cohort with desired pools/cooling corrects config drift.
- Releasing/expiring a cohort converges its devices to the default cohort's desired state (or leaves
  them if the default has none) — with no reset-specific code path.
- Stale observations do not trigger re-dispatch; no reflash storms (cooldown + open-batch guard hold).
- A device with no `firmware_release` match for a channel-based desired firmware is left unmanaged
  (surfaced as `unmanaged`/`unresolvable`, not silently no-op'd).

## Verification

```bash
# Reconciler logic — fakes, deterministic time
cd server && go test ./internal/domain/cohort/reconciler/...   # fakeStore + fakeDispatcher, override now()
# Substrate writers / queries — real DB
cd server && DB_PASSWORD=fleet go test ./internal/domain/telemetry/... ./internal/domain/cohort/...
# Local end-to-end against a fake rig
just dev            # set a cohort's desired firmware; watch fake-proto-rig receive firmware-update;
                    # then change the fake rig's reported version and watch the reconciler re-dispatch
just lint
```

## Open questions

- **Config sweep cadence** vs drift-detection latency (and whether to piggyback telemetry polling).
- **Offline-mid-membership:** does a device that goes offline hold indefinitely, or fail a dimension
  after a timeout (cf. curtailment's restore-dispatch timeout)?
- **Reboot-required vs drift:** model a distinct `pending_reboot` state and possibly issue a `Reboot`
  rather than counting a post-flash old-version reading as drift.
- **Credential-bypass pool reapply** needs a security review before merge.
