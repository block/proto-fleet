---
title: Curtailment hidden events and bulk action blocking report
date: 2026-06-24
status: draft
type: plan
---

# Curtailment hidden events and bulk action blocking report

## Summary

This document records the June 24 curtailment incident, the verified code paths
that allowed it, and the follow-up work needed to make curtailment operationally
safe. The core operational requirement is:

> Curtailment must never leave operators blocked behind command preflight with
> no API/UI recovery path. An org-level admin must always be able to release
> curtailment ownership immediately without a manual database override.

The curtailment issues verified in code are:

1. A `device_list` / explicit-miner curtailment event can remain active and
   continue blocking command preflight, while not appearing in the Energy page
   active-curtailments UI.
2. `StopCurtailment` is graceful restore, not immediate release. A whole-org
   closed-loop `FULL_FLEET` event in `restoring` can still block the whole org
   until it reaches a terminal state.
3. Bulk command preflight reports only counts, not the blocking curtailment
   event/reason/state or the recovery action.
4. Active curtailment status on the Energy page can show event-start snapshot
   counts instead of live target/curtailment state.
5. Current selection targets only currently eligible miners; offline,
   unauthenticated, stale, or otherwise non-actionable miners can be excluded
   instead of being persistently owned by the curtailment policy.
6. Restore controls cannot reliably express "bring everything back now"; server
   defaults and effective-batch clamps can impose a batch size or interval.

The firmware issue observed later is separate from curtailment recovery:

- Proto Rig firmware update accepts a `.zip` upload at the fleet layer, then
  fails at the rig layer because Proto Rig expects an `.swu` firmware package.

The immediate production state was cleared by an operator DB override:

- Event `6` / `a7243b6d-d00b-47ae-a188-4465fd1c523f` / reason `Test Party`
  was changed from `active` to `cancelled`; its 52 non-terminal targets were
  swept to `restore_failed`.
- Event `9` / `b6b9d9eb-b60b-41ca-b697-6a4bdb1d4b52` / reason `Test`
  had already completed naturally by the time the override transaction ran.
- After the override, the active curtailment-blocked miner count was `0`.

Terminology note: the hidden event investigated here was `device_list`
(explicit miners), not `device_sets`. Device-set curtailment appears in the
client/server type vocabulary but is intentionally unimplemented for
preview/start today.

## Execution Scope

Use this section to keep implementation scoped. The report contains incident
facts, immediate recovery fixes, and larger product/design proposals; they should
not all ship as one broad change.

### Verified Incident Facts

- Event `6` (`device_list`, reason `Test Party`) was active, hidden from the
  active-curtailments UI, and blocking command preflight.
- Event `9` (`whole_org`, closed-loop `FULL_FLEET`, reason `Test`) was
  `restoring` and continued to block the whole org until terminal completion.
- The only successful recovery for event `6` was a manual DB override.
- Firmware batch `4142` failed because a `.zip` package was sent to Proto Rig
  firmware update; this is a separate validation issue.

### Immediate Accepted Fixes

These are the right-sized operational recovery fixes for the curtailment
incident:

1. Add a distinct `ForceReleaseCurtailmentOwnership` recovery operation for
   org-level admins.
2. Make hidden `device_list` events visible/manageable to org-level curtailment
   admins even when target site coverage is incomplete.
3. Add structured preflight diagnostics that identify the blocking curtailment
   event(s) and recovery action.
4. Show live active-curtailment target/phase counts in the Energy page.

### Design Proposals Requiring Follow-Up Decisions

These are important, but should be designed separately from the immediate
recovery fix:

- All-paired curtailment targeting (`force_include_all_paired_miners`): changes
  the model from "currently eligible selection" to durable policy ownership.
- Immediate restore semantics: the desired operator outcome is accepted, but the
  API contract must decide how to distinguish omitted/default from no-delay and
  no-cap restore.
- Whole-scope locking during restore: current behavior should be explained now;
  changing lock semantics is a separate product/safety decision.

### Separate Follow-Up

- Firmware package validation should become its own implementation task. Keep it
  linked from this incident because it was discovered during the same session,
  but do not block the curtailment recovery work on it.

## What Happened

The initial operator symptom was a bulk command failure:

```text
command blocked: 17 of 17 device(s) excluded by preflight filters
```

Database activity showed the latest matching failure was:

- `activity_log.id = 703`
- `command_type = blink_led`
- `filters = ["curtailment_active"]`
- `skipped_count = 17`
- `requested_count = 17`

Those 17 devices were blocked by curtailment event `6`, reason `Test Party`.
That event was still `active`, had `device_list` scope, and was not visible on
the Energy page.

Later, after a whole-org curtailment was stopped, the block set grew to the full
org:

- Event `9`, reason `Test`, state `restoring`, scope `whole_org`,
  mode `FULL_FLEET`, loop type `closed`, blocked `5020` devices.
- Event `6`, reason `Test Party`, state `active`, scope `device_list`,
  mode `FULL_FLEET`, loop type `open`, blocked 52 devices.

Event `9` was progressing. Its `Uncurtail` batches were finishing successfully,
but while the event remained `restoring`, the command preflight query still
treated the entire whole-org closed-loop scope as locked.

After curtailment was cleared, a firmware update batch was attempted against 14
Proto Rig miners using a `.zip` file instead of the expected `.swu` package. The
batch finished with 14 device-level failures. One miner rejected the upload with
HTTP 413; the remaining miners accepted the upload request but failed during the
rig install phase with an empty version in the rig-reported install error.

## Production Data Observed

Active events before the override:

| Event | UUID | State | Scope | Mode | Loop | Reason | Blocking effect |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `6` | `a7243b6d-d00b-47ae-a188-4465fd1c523f` | `active` | `device_list` | `FULL_FLEET` | `open` | `Test Party` | 52 explicit targets |
| `9` | `b6b9d9eb-b60b-41ca-b697-6a4bdb1d4b52` | `restoring` | `whole_org` | `FULL_FLEET` | `closed` | `Test` | all 5020 live org devices |

Event `6` site coverage:

- 52 total targets.
- 50 targets with a site.
- 2 targets without a site:
  - `172.16.21.119`, worker `d202`
  - `172.16.21.250`, worker `C8:98:DB:10:D4:09`

Event `9` restore progress shortly before completion:

| Target state | Restore state | Count |
| --- | --- | --- |
| `resolved` | `resolved` | 1099 |
| `dispatched` | `dispatched` | 100 |
| `pending` | `pending` | 101 |

Restore command batches linked to event `9` looked healthy:

- 11 `Uncurtail` command batches observed.
- Each batch was `FINISHED`.
- Each batch had 100 device-level `SUCCESS` rows.
- One target had `restore telemetry timeout`, but it was already `resolved`.

Firmware update batch observed after curtailment cleanup:

- `command_batch_log.id = 4142`
- `uuid = 019efb4c-ba91-75ec-a2e1-0ff5ab3e508d`
- `type = FirmwareUpdate`
- `devices_count = 14`
- `created_at = 2026-06-24 20:22:50.257378+00`
- `started_at = 2026-06-24 20:23:06.5454+00`
- `finished_at = 2026-06-24 20:33:10.57168+00`
- Device-level result: 0 `SUCCESS`, 14 `FAILED`

The device-level firmware failures split into two categories:

| Category | Count | Evidence |
| --- | --- | --- |
| Upload rejected by rig reverse proxy | 1 | HTTP `413 Request Entity Too Large`, `nginx/1.24.0`, file size `224514990` bytes |
| Upload accepted, rig install failed | 13 | `[INSTALL] Installation failed for version . Exit code: Some(1)` |

The HTTP 413 row was recorded for miner `172.16.21.143` at:

- `queue_message.updated_at = 2026-06-24 20:23:10.310865+00`
- `command_on_device_log.updated_at = 2026-06-24 20:23:10.314358+00`

The error body came from the miner-side HTTP endpoint/reverse proxy and was then
wrapped by Proto Fleet:

```html
<head><title>413 Request Entity Too Large</title></head>
<center><h1>413 Request Entity Too Large</h1></center>
<hr><center>nginx/1.24.0</center>
```

The same 14 firmware-targeted miners had zero-hash samples between roughly
`20:09` and `20:16 UTC`, but the firmware batch did not start until
`20:23 UTC`. After firmware start, those 14 devices had non-zero hash-rate
samples only. That ties the zero-hash/hashing transition for those miners to the
earlier curtailment/restore window, not to the firmware update attempt.

## Codebase Cross-Check

### Bulk commands fail closed on any preflight skip

`server/internal/domain/command/service.go` applies registered command filters
before enqueueing. For external traffic, any skipped device turns the whole
command into a failed precondition:

```go
if isExternalCommand(info) && len(skipped) > 0 {
    // ...
    return nil, fleeterror.NewFailedPreconditionErrorf(
        "command blocked: %d of %d device(s) excluded by preflight filters",
        len(skipped), len(identifiers))
}
```

That explains why a partial curtailment skip can block an otherwise valid bulk
action rather than dispatching to the remaining devices.

### Curtailment owns miners through `curtailment_active`

`server/internal/domain/command/curtailment_active_filter.go` blocks selected
devices if they are returned by `ListActiveCurtailedDevices`. The only bypass is
for curtailment reconciler self-traffic issuing `Curtail` or `Uncurtail`.

The backing SQL in `server/sqlc/queries/curtailment.sql` has two important arms:

- Explicit target rows for non-terminal events:
  - event state in `pending`, `active`, `restoring`
  - target state not in `resolved`, `restore_failed`, `released`
- Whole-org or site-scoped closed-loop `FULL_FLEET` events:
  - every live device in the whole-org/site scope is returned while the event is
    non-terminal

That second arm explains why event `9` kept blocking all 5020 devices while
`restoring`. The block set was tied to the event's scope and lifecycle state,
not to the shrinking count of unresolved target rows.

### `device_list` events can be hidden by incomplete target site context

`ListActiveCurtailments` fetches active events, then runs
`filterEventsByPermission`. For non-whole-org, non-site-scope events such as
`device_list`, the handler calls `ListTargetSiteIDsByEvent`. The same helper
would also apply to `device_sets`, but `device_sets` start/preview is currently
unimplemented and was not the incident scope.

`eventSiteResourceContexts` fails closed when target site coverage is incomplete:

```go
siteIDs, complete, err := h.service.ListTargetSiteIDsByEvent(ctx, orgID, event.EventUUID)
if err != nil {
    return nil, err
}
if !complete {
    return nil, fleeterror.NewForbiddenError("curtailment target site context is incomplete")
}
```

`filterEventsByPermission` silently drops events that produce a forbidden error.
The behavior is pinned by
`TestHandler_ListActiveCurtailments_FiltersDeviceListEventsWithIncompleteTargetSites`.

The SQL marks coverage incomplete when not every target resolves to a live device
with a non-null `site_id`. Event `6` had two unassigned targets, so it matched
this hidden-event path. Existing handler tests confirm this filtering applies
even to org-level readers, not only site-limited users.

### Hidden events are also hard to stop through normal APIs

The same event permission machinery is used by `GetCurtailmentEvent`,
`StopCurtailment`, and `AdminTerminateEvent`. Therefore, a device-list event
with incomplete target site context can be invisible in list views and also
unreachable via normal event-scoped operations.

There is a second normal-path limitation: admin termination is intentionally
restricted to `pending` and `restoring` events, and refuses events with in-flight
curtail targets. In SQL, `AdminTerminateCurtailmentEvent` only updates events
whose state is in `('pending', 'restoring')`, while
`CurtailmentEventHasInFlightTargets` treats `desired_state = 'curtailed'` and
target states `dispatching`, `dispatched`, `confirmed`, or `drifted` as in
flight.

That means an `active` hidden `device_list` event can be especially difficult to
clear without either:

- making it visible/manageable again by repairing site coverage, or
- using an operator-only override path.

There is also an error-message nuance: the admin-terminate store path reports an
`active` lifecycle state through the same "in-flight curtail commands" recovery
surface as true in-flight target commands. That is directionally useful ("stop
first") but can be misleading when the event is simply `active`.

### Device-set curtailment is a separate unsupported scope

`device_sets` should be tracked separately from the hidden `device_list` bug.
The client can render existing `deviceSetIds` event scopes as "N device sets",
but preview/start intentionally reject device-set curtailment today. The domain
service returns an unimplemented error for `ScopeTypeDeviceSets` with the message
"device-set scope is not implemented; use whole_org, site, or device_list".

### Active curtailment status can prefer event-start snapshot over live target state

The Energy page active event card should describe current operational state:
current scope size, currently targeted miners, actually curtailed miners, pending
targets, restore failures, and restoration progress. Today, part of the UI can
prefer the event-start decision snapshot instead.

On the backend, `ListActiveCurtailments` returns active event metadata and scope,
but intentionally does not populate per-target rows, decision snapshot, or target
rollup. The selected active event is later hydrated through `GetCurtailmentEvent`,
and that path does fetch a live target rollup through `GetTargetRollupByEvent`.
However, non-selected active list items do not receive live rollups, and the
client-side selected-miner helper still prefers the decision snapshot count when
one is present:

```ts
const snapshotSelectedCount = getSnapshotNumber(event, selectedCountSnapshotKeys);
return snapshotSelectedCount ?? event.targetRollup?.total ?? event.targets.length;
```

That priority is wrong for live status. For closed-loop `FULL_FLEET`, the
event-start snapshot is audit context only. The live status should prefer
`target_rollup.total` and phase counts (`pending`, `dispatched`, `confirmed`,
`drifted`, `resolved`, `restore_failed`) when available. Otherwise an event that
started with 10 miners and later owns 5,000 can still appear to be a 10-miner
curtailment.

### Firmware update accepts file types that are invalid for the target driver

Fleet firmware upload validation currently accepts `.swu`, `.tar.gz`, and
`.zip` files in the shared firmware file service. That is intentionally broad
for all miner families, but Proto Rig's plugin path simply streams the selected
file to the rig's MDK REST endpoint:

```go
if err := d.client.UploadFirmware(ctx, firmware); err != nil {
    return fmt.Errorf("device firmware upload: %w", err)
}
```

The upload client sends the file to `PUT /api/v1/system/update`. It does not
derive or pass a version string, and it does not validate that a Proto Rig
target is receiving an `.swu` package before dispatch. The `version .` text in
the observed error is from the rig install status/error, not a Proto Fleet
version parsing path.

HTTP 413 is handled in `plugin/proto/pkg/proto/client.go` by mapping the rig's
`StatusRequestEntityTooLarge` response into a `FailedPrecondition`:

```go
case http.StatusRequestEntityTooLarge:
    return grpcstatus.Errorf(codes.FailedPrecondition,
        "firmware upload rejected: payload too large (%d bytes, HTTP 413). %s",
        firmware.Size, withDetail("rig reverse-proxy body limit is smaller than this firmware file", detail))
```

That code path explains why the 413 message included the rig's `nginx` HTML
body while still being stored as a Proto Fleet command failure.

## Issues Identified

### Issue 1: Hidden active `device_list` curtailments

**Impact:** High. An active curtailment can own miners and block bulk actions
without appearing in the active-curtailments UI.

**Observed trigger:** A `device_list` event with at least one target whose device
does not resolve to a live device with a site. In the incident, two targets were
unassigned.

**Root cause:** Permission filtering treats incomplete target-site coverage as a
forbidden event. The list endpoint suppresses forbidden events rather than
returning them with degraded site context. This is defensible for site-scoped
operators, but too strict for org-level visibility and recovery.

**Recommended fix:** Make org-level curtailment readers/managers able to see and
act on device-list events even when target-site coverage is incomplete. Keep
site-limited behavior conservative, but return an explicit incomplete-site
indicator for org-level users so the UI can render a warning.

**Tests to add/update:**

- Org-level `ListActiveCurtailments` includes a device-list event with
  incomplete target site context.
- Org-level `GetCurtailmentEvent` and `StopCurtailment` can operate on that
  event.
- Site-limited users still cannot access events whose target sites cannot be
  proven permitted.
- UI shows an active card/detail warning for incomplete target site coverage.

### Issue 2: Whole-org restoring event blocks all bulk actions until terminal

**Impact:** Medium to high. This may be intentional from a single-writer policy
perspective, but the operator experience is confusing because the block count
does not gradually reduce as restore succeeds.

**Observed trigger:** Whole-org, closed-loop, `FULL_FLEET` event in `restoring`.

**Root cause:** `ListActiveCurtailedDevicesByOrg` returns all live devices for
non-terminal whole-org/site closed-loop `FULL_FLEET` events. This bypasses
per-target state and keeps the entire scope locked until the event reaches a
terminal state.

**Recommended fix options:**

1. Keep the current lock semantics, but make the UI and preflight error explain
   that restore is in progress and bulk actions are blocked until the event is
   terminal.
2. Revisit whether closed-loop `FULL_FLEET` should continue whole-scope locking
   during restore, or only lock unresolved targets. This is a product/safety
   decision because it changes the single-writer guarantee.
3. Add an explicit admin override action that terminates ownership and surfaces
   the consequence: it does not wake miners; it only releases the curtailment
   lock so manual commands can be sent.

**Tests to add/update:**

- A restoring whole-org closed-loop `FULL_FLEET` event blocks all live org
  devices.
- UI copy for the blocked state distinguishes "restore in progress" from
  "miner command failed".
- If semantics change, preflight tests should assert partial release behavior.

### Issue 3: Preflight error is not actionable enough

**Impact:** Medium. The operator sees only the count:

```text
command blocked: N of N device(s) excluded by preflight filters
```

The detailed filter names and skipped IDs exist in `activity_log.metadata`, but
the UI does not surface the blocking event, reason, event state, or what action
can clear it.

**Recommended fix:**

- Return or fetch a structured preflight explanation for command failures:
  - filter name (`curtailment_active`, `schedule_conflict`)
  - blocking event UUID/reason/state when available
  - sample devices and count
  - suggested next action: stop/finish curtailment, wait for restore completion,
    or request admin override
- Link from the error toast/modal to the relevant Energy curtailment event if it
  is visible.
- If the event is hidden due to incomplete site coverage, show a specific
  recovery message to org admins.

### Issue 4: Operational override exists only as manual DB work

**Impact:** Critical. We were able to clear the incident safely after verifying
the command batches and lifecycle state, but the operation required direct DB
updates. That is not acceptable for curtailment operations: miner operators must
never be stuck behind command preflight filters with no API/UI recovery path.

**Operational safety requirement:** There must always be an operator-accessible
way to immediately release curtailment ownership for an org or event so manual
bulk commands can proceed. This recovery action is separate from graceful
restore. It may not wake miners itself, but it must remove the
`curtailment_active` preflight lock immediately and leave an audit trail.

**Accepted requirement:** Add a distinct admin recovery operation. Do not
quietly loosen `AdminTerminateEvent` without explicitly revising its contract;
today it is documented and implemented as a Stop-first recovery path for active
events.

Canonical API name for planning:

```text
ForceReleaseCurtailmentOwnership
```

Suggested UI copy:

```text
Force release curtailment ownership
```

**Required contract:**

- The operation:
  - works for `pending`, `active`, and `restoring` events,
  - checks org-level `curtailment:manage` plus admin role before event
    site-context checks,
  - bypasses incomplete target-site context for org-level admins,
  - locks the event row and transitions the event to terminal state `cancelled`,
  - sweeps every non-terminal target to a terminal state in the same
    transaction,
  - writes an activity audit row with actor, reason, event UUID, and swept target
    count,
  - returns enough detail for the UI to say "curtailment ownership released;
    miners may still need manual wake/start",
  - guarantees the event no longer appears in `ListActiveCurtailedDevicesByOrg`
    after the transaction commits.

**State decision required before implementation:** choose the target terminal
state for force-released targets. Existing choices are imperfect:

- `restore_failed` releases command preflight but implies restore was attempted
  and failed.
- `released` releases ownership but currently means closed-loop removed the
  target mid-event.
- A new explicit target state such as `force_released` would be clearest but
  requires proto, SQL, generated code, rollups, and UI updates.

**Acceptance criteria:**

- An org-level admin can force-release an event even when target site coverage is
  incomplete.
- An org-level admin can force-release an `active` event without first running
  `StopCurtailment`.
- An org-level admin can force-release a `restoring` whole-org closed-loop
  `FULL_FLEET` event and immediately unblock manual bulk commands.
- The action is audited and visible in activity/history.
- The API response explicitly distinguishes "ownership released" from "miners
  restored" so operators know a follow-up wake/start may still be required.

### Issue 5: Curtailment only targets currently eligible miners

**Impact:** High for operational predictability. Today, curtailment behaves as
"select currently curtailable miners" rather than "declare curtailment ownership
over the selected scope." Offline, unauthenticated, stale, updating, and other
non-actionable miners can be excluded before they become durable targets. That
means a miner may escape an active curtailment simply because it was offline or
not authenticated when the event started.

**Observed/current gates:** The selector excludes candidates before target
creation for many reasons:

- already owned by another active event,
- non-`PAIRED` pairing status,
- missing driver metadata,
- `UPDATING`,
- `REBOOT_REQUIRED`,
- `OFFLINE`,
- `INACTIVE`,
- `NEEDS_MINING_POOL`,
- `MAINTENANCE` unless `force_include_maintenance`,
- stale/missing/non-finite telemetry,
- cooldown protection,
- mode-specific power/hash telemetry checks.

`FULL_FLEET` skips the mode-specific dual-signal power/hash filter, but it still
runs the pre-filter gates above. The closed-loop `FULL_FLEET` reconciler also
uses the same candidate/eligibility machinery for dynamic admission, so offline
or unauthenticated miners are not persisted as pending targets that will later be
curtailed when they recover.

**Design proposal requiring separate implementation:** Add an admin-only policy
override, conceptually similar to `force_include_maintenance`, but broader:

```text
force_include_all_paired_miners
```

The semantics should be:

- Include every in-scope miner with a non-deleted device record and a fleet
  pairing row as a curtailment target, regardless of current status or telemetry.
  This likely includes `PAIRED`, `DEFAULT_PASSWORD`, and
  `AUTHENTICATION_NEEDED`; it should not include bare discovered devices with no
  pairing row.
- Persist targets even when they cannot be dispatched immediately.
- Dispatch curtail commands only when a target becomes actionable.
- Keep non-actionable targets in a pending/unavailable state with a clear reason
  such as offline, auth-needed, no driver, or reboot-required.
- Reconciler periodically retries pending unavailable targets while the event is
  non-terminal.
- Force-release/abort can still clear every target immediately.

This is different from simply bypassing every gate and trying to dispatch
unreachable miners. The intent is durable policy ownership, not immediate command
success for devices that cannot currently accept commands.

**Hard boundaries to keep:**

- Org boundary: never target devices outside the caller's org.
- Deletion boundary: do not target soft-deleted devices.
- Ownership boundary: do not target devices already owned by another
  non-terminal curtailment event.
- Dispatch boundary: do not attempt a curtail command until the target has a
  driver/capability and usable credentials.

**New lifecycle required:** Do not insert offline/auth-needed/no-driver targets
as ordinary dispatch-ready `pending` rows unless the reconciler learns to treat
them differently. Current pending targets are eligible for dispatch. This design
needs either:

- a new target state such as `unavailable`, or
- a separate availability/status field on target rows with reason values such as
  `offline`, `auth_needed`, `no_driver`, and `reboot_required`.

The reconciler should transition unavailable/policy-owned targets into
dispatchable `pending` only when they become actionable.

**Recommended scope:** Start with `FULL_FLEET`. For `FIXED_KW`, including
offline or unknown-power miners can make estimated reduction misleading. If
supported for `FIXED_KW`, the UI and API should separate "targeted by policy"
from "counted toward estimated kW reduction."

**Required dependency:** This should not ship without the force-release path in
Issue 4. Creating durable ownership over offline or unauthenticated miners is
operationally safer only if operators can immediately release all ownership when
needed.

**Tests to add/update:**

- With the override enabled, offline paired miners become durable pending targets
  rather than skipped candidates.
- With the override enabled, `DEFAULT_PASSWORD` and `AUTHENTICATION_NEEDED`
  paired miners become durable policy targets rather than skipped candidates.
- When a pending unavailable target later becomes `PAIRED` and actionable, the
  reconciler dispatches curtail.
- Force-release clears all pending/dispatching/confirmed targets created by this
  policy.
- Without the override, existing eligibility behavior remains unchanged.
- `FIXED_KW` either rejects the override or reports unavailable targets
  separately from estimated reduction.

### Issue 6: Active status uses snapshot counts instead of live target state

**Impact:** High for operator trust. Operators need to know how many miners are
currently under curtailment policy and how many are actually curtailed. Showing
the event-start count can make a growing closed-loop whole-fleet curtailment look
like it still applies only to the original target set.

**Observed trigger:** A response profile or event starts when the fleet has a
small number of paired miners. Later, more miners enter scope. The live event may
claim additional targets, but the UI can still headline the snapshot count.

**Root cause:** The active list response does not include live target rollups,
and the client helper `getCurtailmentEventSelectedMinerCount` prioritizes
`decision_snapshot.selected_count` over `target_rollup.total`. The selected
active event may be hydrated with a live rollup via `GetCurtailmentEvent`, but
the count helper can still choose the stale snapshot first.

**Recommended fix:** Treat active-event status as live operational data, not
audit snapshot data.

- Backend: include live `target_rollup` in `ListActiveCurtailments` for every
  active event, or add a lightweight active-status endpoint that returns live
  rollups for all active events.
- Frontend: for active events, prefer `target_rollup.total` over
  `decision_snapshot.selected_count`.
- UI: label event-start counts separately, for example "Started with 10 miners",
  while headline status uses "Targeted now: 5,000".
- Product model: distinguish profile scope, event-start snapshot, live scope,
  live targets, and confirmed curtailed targets.

**Tests to add/update:**

- Active event with `decision_snapshot.selected_count = 10` and
  `target_rollup.total = 5000` displays 5,000 as the live targeted count.
- Completed/history rows can still use snapshot fields where appropriate.
- `ListActiveCurtailments` returns live target rollup for active events.
- Closed-loop `FULL_FLEET` event that claims new targets updates Energy page
  counts on poll.

### Issue 7: Restore batching defaults/caps prevent immediate full restore

**Impact:** High during recovery. Operators may want to bring every curtailed
miner back as quickly as possible. Today, zero/blank restore controls do not mean
"no batching and no interval" consistently. The backend and response-profile
defaults can impose batching and delay even when the operator intent is
immediate restore.

**Current behavior in code:**

- `Start` treats `restore_batch_interval_sec = 0` as "use server default" and
  rewrites it to `defaultRestoreBatchIntervalSec = 30`.
- `ComputeEffectiveBatchSize` stamps `effective_batch_size` as
  `max(restore_batch_size, ceil(1% × selected_count))`, then clamps it to
  `[10, 100]`. A caller cannot express "restore all targets in one batch" through
  `restore_batch_size = 0`.
- Response profile defaults set `RestoreBatchSize = 50` and
  `RestoreBatchIntervalSec = 5` when omitted.
- The client has an "immediate" profile affordance that uses a large
  `restoreBatchSize` value (`10000`), but the backend effective-batch clamp still
  caps restore batches at 100.

This makes restore behavior hard to reason about: user-visible "immediate" does
not necessarily mean one restore wave, and zero values can mean "server default"
rather than "no delay / no cap."

**Accepted outcome:** Operators must be able to choose "restore all miners as
fast as possible" without hidden server-imposed batching or delay.

**API contract decision required:** Do not overload current proto3 zero values
without deciding how to distinguish omitted/default from explicit no-delay or
no-cap. Prefer an explicit contract such as:

- `restore_all_immediately = true`, or
- optional/nullable restore controls where omission means "server default" and
  explicit zero means "no interval / no cap."

Once the contract is chosen:

- Preserve explicit no-delay as "dispatch next restore wave immediately."
- Preserve explicit no-cap restore as "restore all pending targets now."
- Do not apply the adaptive `[10,100]` effective-batch clamp when the user or
  response profile explicitly requests immediate restore.
- Response profile defaults for immediate restore should be zero/no-cap
  semantics, not hidden fallback values.
- If safety limits are still needed, make them explicit admin controls with clear
  UI copy, not silent server defaults.

**Tests to add/update:**

- The chosen immediate-restore API contract persists intent distinctly from
  omitted/default controls.
- Start with "restore all immediately" results in one restore claim covering all
  pending targets, bounded only by explicit safety limits.
- Response profile "automatic immediate restore" does not get capped to 100 by
  `effective_batch_size`.
- Existing batched-restore behavior still works when a positive batch size and
  positive interval are configured.

### Issue 8: Firmware update lacks target-aware file validation

**Impact:** Medium. Operators can upload a file extension that is generally
accepted by the fleet service but invalid for the selected miner family. The
failure happens late, after command dispatch, and produces per-device install
failures instead of a pre-dispatch validation error.

**Observed trigger:** A `.zip` firmware file was selected for Proto Rig targets.
Proto Rig expects an `.swu` firmware package.

**Root cause:** Firmware file validation is global and extension-based. It does
not account for the target devices' driver/model requirements before dispatch.
The Proto Rig plugin streams whatever file was selected to the rig endpoint.

**Recommended fix:** Add target-aware firmware compatibility validation before
creating the `FirmwareUpdate` command batch. For Proto Rig targets, reject files
that do not end in `.swu` with an actionable error. Do not rely on extension
alone: perform a lightweight file-header/package sanity check during upload or
before dispatch so corrupted files or wrong package formats are rejected before
they reach miners. If a selector spans multiple driver families, either reject
mixed incompatible targets up front or require the UI to split the operation by
compatible firmware package type.

**Tests to add/update:**

- Proto Rig `FirmwareUpdate` with `.zip` is rejected before enqueue.
- Proto Rig `FirmwareUpdate` with `.swu` is allowed.
- Corrupt or wrong-format firmware files with a misleading extension are rejected
  by header/package validation before enqueue.
- Antminer/other supported drivers retain their expected accepted formats.
- Mixed-driver firmware update selectors either fail with a clear compatibility
  error or are not offered by the UI.

## Prioritized GitHub Issue Backlog

Open these as separate GitHub issues in the order below. The order is chosen to
remove the "manual DB override required" failure mode first, then improve
operator visibility, then tackle broader product/design changes. Do not bundle
the firmware work into the curtailment recovery PRs.

### 1. P0: Add audited force-release recovery for curtailment ownership

**Impact radius:** backend API, curtailment domain/store, activity audit, minimal
Energy UI action.

**Why first:** This is the operational safety backstop. Until this exists,
operators can still get trapped behind `curtailment_active` with no UI/API way
out.

**Depends on:** none.

**Scope:**

- Add a distinct `ForceReleaseCurtailmentOwnership` operation, not a silent
  loosening of `AdminTerminateEvent`.
- Require org-level `curtailment:manage` and admin role before target-site
  coverage checks.
- Support `pending`, `active`, and `restoring` events.
- Transition the event to terminal `cancelled`.
- Sweep every non-terminal target to the chosen terminal target state.
- Write a durable activity row with actor, reason, event UUID, and swept target
  count.
- Return response data that lets the UI say: "Curtailment ownership released;
  miners may still need manual wake/start."

**Decision needed before implementation:** choose the target terminal state for
force-released targets:

- `restore_failed` is available today but implies restore was attempted and
  failed.
- `released` is available today but currently means closed-loop removed a target
  mid-event.
- A new `force_released` state is clearest but requires proto, SQL, generated
  code, rollups, and UI updates.

**Out of scope:**

- Changing graceful restore semantics.
- All-paired targeting.
- Firmware validation.

**Acceptance criteria:**

- Org admin can force-release a `pending`, `active`, or `restoring` event.
- Force-release bypasses incomplete target-site context for org-level admins.
- Force-release immediately removes the event from
  `ListActiveCurtailedDevicesByOrg`.
- Force-release is audited and visible in activity/history.
- Site-limited users cannot use force-release to access events whose target sites
  are not proven authorized.

### 2. P0: Make incomplete-site `device_list` curtailments visible and manageable to org admins

**Impact radius:** curtailment handlers, permission filtering, Energy active
curtailment UI.

**Why second:** Once force-release exists, operators still need to see hidden
events and reach the recovery action without asking support for an event UUID.

**Depends on:** issue 1 for the final recovery action; backend visibility can
start in parallel if needed.

**Scope:**

- Change org-level curtailment read/manage behavior so `device_list` events with
  incomplete target-site coverage are not silently dropped.
- Preserve conservative behavior for site-limited users unless every target site
  is known and authorized.
- Return an incomplete-site/target-coverage indicator to the client.
- Energy UI should show the active event with a warning and recovery controls.

**Out of scope:**

- Implementing `device_sets` curtailment.
- Changing how target sites are assigned.

**Acceptance criteria:**

- Org-level `ListActiveCurtailments` includes incomplete-site `device_list`
  events.
- Org-level `GetCurtailmentEvent`, `StopCurtailment`, and
  `ForceReleaseCurtailmentOwnership` can operate on those events.
- Energy UI renders the event with an incomplete-site warning and unassigned
  target count.
- Site-limited users remain denied when target site coverage is incomplete.

### 3. P1: Add structured curtailment blocker diagnostics to command preflight

**Impact radius:** command preflight filter contract, API errors or diagnostic
endpoint, fleet-management bulk-action UI.

**Why third:** Operators need to understand why bulk actions are blocked and how
to recover. This should link to the force-release path from issue 1 and the
visible event from issue 2.

**Depends on:** issue 1 for the recovery action; issue 2 for best UI linking.

**Scope:**

- Extend curtailment preflight diagnostics beyond skipped device IDs/filter
  names.
- Return blocking event UUID, reason, state, scope, and incomplete-site coverage
  status when available.
- Support multiple blockers.
- Surface actionable copy in bulk action errors.
- Link to the relevant curtailment event or force-release action when the caller
  can manage it.

**Out of scope:**

- Partial bulk dispatch policy changes.
- Schedule-conflict redesign, unless using the same diagnostic envelope.

**Acceptance criteria:**

- A curtailment-blocked bulk command reports the blocking event(s), not just
  "N of N excluded."
- Hidden/incomplete-site blockers produce a specific recovery message for org
  admins.
- Non-managers see safe explanatory copy without unauthorized event details.

### 4. P1: Show live curtailment status on the Energy page

**Impact radius:** active curtailment list/detail API shape, Energy status card,
header pill if it uses the same active store.

**Why fourth:** This improves operator trust and reduces confusion, but it does
not itself unblock stuck miners.

**Depends on:** can ship independently after issue 2; pairs well with issue 3.

**Scope:**

- Return live `target_rollup` for every active event in
  `ListActiveCurtailments`, or add a lightweight active-status endpoint.
- Active UI should prefer `target_rollup.total` and phase counts over
  `decision_snapshot.selected_count`.
- Keep decision snapshot fields as event-start/audit context.
- For closed-loop `FULL_FLEET`, show current scope size, current targets,
  confirmed curtailed, pending unavailable, restore failed, and restore progress.

**Out of scope:**

- All-paired targeting semantics.
- Changing preflight ownership semantics.

**Acceptance criteria:**

- Active event with `decision_snapshot.selected_count = 10` and
  `target_rollup.total = 5000` displays 5,000 as live targeted count.
- Polling updates active event counts after closed-loop target claims.
- Completed/history views can still show initial snapshot context.

### 5. P1: Make immediate restore an explicit API/UI contract

**Impact radius:** curtailment proto/API, response profiles, restore reconciler,
Energy start/profile UI.

**Why fifth:** Recovery speed matters, but the API contract needs a small design
decision before implementation because current zero values mean "server default."

**Depends on:** issue 1 for emergency release; does not need to block issues 2-4.

**Scope:**

- Add an explicit way to request immediate restore, for example
  `restore_all_immediately`, or migrate restore controls to optional/nullable
  fields where omitted means default and explicit zero means no-delay/no-cap.
- Immediate restore must bypass the adaptive `[10,100]` effective-batch clamp.
- Keep positive batch size + positive interval behavior for batched restore.
- Align response profile "immediate restore" UI copy with actual backend
  behavior.

**Out of scope:**

- Force-release ownership; that is issue 1.
- All-paired targeting.

**Acceptance criteria:**

- Immediate-restore intent is persisted distinctly from omitted/default controls.
- Immediate restore claims all pending restore targets in one restore wave,
  bounded only by explicit safety limits.
- Response profile "automatic immediate restore" is not capped to 100 by
  `effective_batch_size`.
- Existing batched restore behavior remains available and tested.

### 6. P2: Design all-paired curtailment targeting policy

**Impact radius:** curtailment target lifecycle, selector, reconciler, SQL/proto
models, UI accounting.

**Why sixth:** This is a larger product/domain shift. It simplifies operator
mental model but should not be mixed with incident recovery.

**Depends on:** issue 1. Do not create durable ownership over unavailable miners
without force-release.

**Scope:**

- Define `force_include_all_paired_miners` or equivalent policy mode.
- Define which pairing states count as policy-targetable. Current working
  assumption: non-deleted devices with a fleet pairing row, likely including
  `PAIRED`, `DEFAULT_PASSWORD`, and `AUTHENTICATION_NEEDED`.
- Add a durable unavailable/policy-owned target state or availability field.
- Persist paired but non-actionable devices as policy targets.
- Reconcile unavailable targets into dispatchable targets once they become
  actionable.
- Preserve org, deletion, active ownership, and dispatch capability boundaries.

**Out of scope:**

- Immediate incident recovery.
- `FIXED_KW` support unless explicitly decided. Start with `FULL_FLEET`.

**Acceptance criteria:**

- Offline paired devices become durable policy targets.
- `DEFAULT_PASSWORD` and `AUTHENTICATION_NEEDED` devices become durable policy
  targets but are not dispatched until actionable.
- Recovered targets dispatch later.
- Force-release clears these targets immediately.
- Existing normal selector behavior remains unchanged when the override is off.

### 7. P2: Add target-aware firmware package validation

**Impact radius:** firmware upload metadata, firmware command preflight,
driver/model capabilities, Firmware Update modal.

**Why seventh:** This is a separate incident finding. It should not delay
curtailment recovery.

**Depends on:** none of the curtailment issues.

**Scope:**

- Define driver/model firmware package compatibility metadata.
- Persist lightweight firmware file metadata/header validation at upload.
- Validate expanded target drivers before `FirmwareUpdate` enqueue.
- Surface expected package type in the Firmware Update modal.
- Preserve rig-origin HTTP 413 detail for genuine rig-side size failures.

**Out of scope:**

- Curtailment recovery.
- Changing rig nginx limits.

**Acceptance criteria:**

- Proto Rig `.zip` is rejected before enqueue.
- Proto Rig `.swu` is allowed.
- Corrupt or wrong-format files with misleading extensions are rejected before
  enqueue.
- Mixed-driver selections fail with a clear compatibility error or are blocked
  in the UI.

## Operational Queries Used

Current active curtailment blockers:

```sql
SELECT DISTINCT ct.device_identifier
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
WHERE ce.org_id = $1
  AND ce.state IN ('pending', 'active', 'restoring')
  AND ct.state NOT IN ('resolved', 'restore_failed', 'released')
UNION
SELECT d.device_identifier
FROM curtailment_event ce
JOIN device d ON d.org_id = ce.org_id
  AND d.deleted_at IS NULL
  AND (
    ce.scope_type = 'whole_org'
    OR (ce.scope_type = 'site' AND d.site_id = (ce.scope_jsonb->>'site_id')::BIGINT)
  )
WHERE ce.org_id = $1
  AND ce.state IN ('pending', 'active', 'restoring')
  AND ce.mode = 'FULL_FLEET'
  AND ce.loop_type = 'closed';
```

Device-list target site coverage:

```sql
SELECT
  COUNT(*) AS total_targets,
  COUNT(d.site_id) AS targets_with_site,
  COUNT(*) - COUNT(d.site_id) AS targets_without_site
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
LEFT JOIN device d ON d.device_identifier = ct.device_identifier
  AND d.deleted_at IS NULL
WHERE ce.id = $1;
```

Firmware batch failure summary:

```sql
SELECT
  cbl.id AS batch_id,
  cbl.uuid AS batch_uuid,
  cbl.created_at AS batch_created_at,
  cbl.started_at AS batch_started_at,
  cbl.finished_at AS batch_finished_at,
  codl.id AS command_on_device_log_id,
  codl.device_id,
  d.device_identifier,
  dd.ip_address,
  codl.status,
  codl.updated_at AS error_recorded_at,
  codl.error_info
FROM command_batch_log cbl
JOIN command_on_device_log codl ON codl.command_batch_log_id = cbl.id
JOIN device d ON d.id = codl.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE cbl.id = $1
  AND codl.error_info ILIKE '%payload too large%';
```

## Open Questions

- Should site-limited users see a redacted active curtailment card when target
  site coverage is incomplete, or should only org-level users see it?
- During restore, should whole-org closed-loop ownership remain all-or-nothing,
  or should resolved targets be released from preflight blocking?
- Should the Energy page show "restoring but still blocking manual commands" as
  a distinct state?
- Should manual bulk actions offer an admin override prompt when the only blocker
  is a curtailment event the caller can manage?
- Should `force_include_all_paired_miners` be limited to `FULL_FLEET`, or should
  `FIXED_KW` support it with separate "targeted but not counted toward kW"
  accounting?
- For active events, which count should be the primary headline: current scope,
  currently targeted miners, or confirmed curtailed miners?
- What exact API shape should represent immediate restore:
  `restore_all_immediately`, optional restore controls, or another explicit
  contract that distinguishes omitted/default from no-delay/no-cap?
- What target state or phase should represent "owned by curtailment policy but
  not currently dispatchable" so it does not get confused with command failure?
- Should firmware compatibility be modeled as driver capability metadata so the
  UI can filter allowed firmware file types before upload/dispatch?
- Should firmware updates be blocked for mixed-driver selections unless a single
  firmware package is valid for every selected device?
- What is the minimal reliable validation for each firmware package type
  (`.swu`, `.tar.gz`, `.zip`) — magic bytes, archive structure, manifest file,
  signature, or device/vendor metadata?
