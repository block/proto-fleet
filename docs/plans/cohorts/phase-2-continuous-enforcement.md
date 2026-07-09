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

**Current implementation baseline:** this branch already has cohort CRUD, lease/membership moves,
expiry sweep, the command membership filter, UI routes, `fleetcli` cohort commands, `ActorCohort`, and
per-manufacturer/model firmware targets via `cohort_firmware_target`. It does **not** have any phase-2
substrate tables, sqlc queries, config-state writers, or cohort reconciler package yet. Treat this
phase as adding those pieces on top of migrations `000104`-`000107`.

## Scope

**In:** the substrate (firmware/config observed-state shadows, the firmware variant registry if
needed, the per-device enforcement-state table, shared pool comparison helpers); the enforcement
reconciler for **firmware, pools, and cooling**; wiring into `fleetd`.

**Out:** power enforcement (phase 3 — needs a new SDK getter); rollout/canary (deferred).

## Files to create / modify

### Substrate (migrations + sqlc + small writers)
- `server/migrations/000108_create_cohort_enforcement_substrate.{up,down}.sql` — `device_firmware_state`
  (PK `org_id, device_identifier`, `firmware_version`, **`observed_at`**); `device_config_state`
  (observed pools/cooling, `observed_at`); `device_enforcement_state` (PK
  `(device_identifier, dimension)`: `state`, `retry_count`, `last_batch_uuid`,
  `last_dispatched_at`, `last_error`); a cohort reconciler heartbeat (`CHECK(id=1)`, clone from the
  curtailment heartbeat pattern). Reconfirm `000108` is still the next free migration immediately
  before writing.
- `server/sqlc/queries/cohort_enforcement.sql` + generated.
- **Firmware shadow write:** extend `persistFirmwareVersionIfChanged`
  (`server/internal/domain/telemetry/service.go:871`) to also upsert `device_firmware_state` with
  `observed_at = now`, bumping `observed_at` on every non-empty observation (debounced), threading
  `orgID`. Do **not** add firmware to `device_metrics` (Timescale hypertable; wrong read shape).
- **Firmware file metadata:** uploaded firmware metadata carries the expected reported
  `firmware_version`. Cohort firmware targets stay file-only (`firmware_file_id`); the reconciler
  resolves the expected version from the referenced firmware file metadata and skips old files that
  do not have version metadata.
- **Config sweep:** a slow loop (e.g. piggyback telemetry, or a dedicated ticker) calling
  `GetMiningPools`/`GetCoolingMode` (`server/sdk/v1/interface.go:405,398`) into `device_config_state`.
  Decouples per-device RPC fanout from the fast drift tick.
- **Desired config shape:** `cohort.desired_config_jsonb` is currently opaque. Before enforcing pools or
  cooling, define a typed domain shape for desired pools/cooling and validate it in the handler/service
  so the reconciler does not parse ad-hoc JSON.
- **`configdrift` helper** — worker-name-aware pool comparison. The executor already suffixes pool
  usernames (`appendMinerNameToPoolUsername`) and exposes `ReapplyCurrentPoolsWithWorkerNames`; strip
  worker suffixes via `workername.FromPoolUsername` (`server/internal/domain/workername/workername.go`).
  One shared helper should be used by both the sweep and the drift check so they agree.

### Reconciler
- `server/internal/domain/cohort/reconciler/reconciler.go` — clone curtailment's structure: singleton
  30s tick, `Start`/`Stop` + watchdog, per-tick + per-device panic isolation, heartbeat upsert,
  optimistic-concurrency state writes. Resolve desired state by **device → cohort (or default)**; for
  firmware, match the device's observed/registered manufacturer+model to that cohort's
  `cohort_firmware_target` row, then resolve the target version from the referenced firmware file
  metadata (fall back to legacy `desired_firmware_file_id` only where existing APIs still mirror it);
  for config, read the typed desired pools/cooling shape. Run a
  **per-dimension** state machine
  (`firmware`/`pools`/`cooling` independent): `pending→dispatching→dispatched→confirmed`,
  `confirmed→drifted→(re-dispatch)`, terminal `failed` at `MaxRetries`.
- Dispatch surfaces: firmware → `command.Service.FirmwareUpdate(ctx, selector, firmwareFileID)`
  (`command/service.go`); pools → either `command.Service.UpdateMiningPools` with an internal actor
  credential bypass or `ReapplyCurrentPoolsWithWorkerNames` when the desired action is only worker-name
  normalization; cooling → `command.Service.SetCoolingMode`.
- `server/cmd/fleetd/main.go` — construct + `Start`/`defer Stop` the reconciler next to the
  curtailment block, with `cohortStore` + a command dispatcher + `ActorCohort` context. `cohortStore`,
  `cohortSvc`, `SetFirmwareMetadataProvider`, the expiry sweeper, and `CohortMembershipFilter` are
  already wired; do not duplicate those.

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
- **Per-type firmware targets:** default cohorts may carry more than one `cohort_firmware_target` row;
  non-default cohorts are currently constrained to a single miner manufacturer/model. The reconciler
  must choose the target by device type, not by assuming the cohort row has exactly one desired file.
- **Curtailment wins:** a device under active curtailment will have cohort dispatches filter-skipped;
  treat that skip as "hold, don't burn retry budget" (special-case the `curtailment_active` skip reason).
- **Reset = convergence:** no reset-specific code. A released device is in the default cohort; if the
  default has desired firmware/config, the same loop drives it there; if not, it's left as-is.
- **Multi-instance caution:** the reconciler is a singleton with a heartbeat but no leader election.
  Because firmware isn't idempotent, guard the firmware dispatch path with a `pg_advisory_xact_lock`
  (cf. authz reconcile) or `SELECT … FOR UPDATE`, beyond the optimistic-concurrency state writes.

## Acceptance criteria

- A cohort with a matching desired firmware target drives its members to that version, and
  **re-applies** if a device drifts off it; a cohort with desired pools/cooling corrects config drift.
- Releasing/expiring a cohort converges its devices to the default cohort's desired state (or leaves
  them if the default has none) — with no reset-specific code path.
- Stale observations do not trigger re-dispatch; no reflash storms (cooldown + open-batch guard hold).
- A device whose effective cohort has no desired firmware target for its manufacturer/model is left unmanaged for firmware
  (surfaced clearly, not silently no-op'd).

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
