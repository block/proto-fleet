---
title: "Curtailment building, rack, and group targets"
date: 2026-08-11
status: implementing
type: plan
tracker: https://github.com/block/proto-fleet/issues/909
---

# Curtailment building, rack, and group targets

## Context

Both affected flows render the shared
`client/src/protoFleet/features/energy/CurtailmentStartModal.tsx`:

- Energy > **New curtailment**, through `CurtailmentManagementPanel.tsx`.
- Settings > **Create/Edit response profile**, through
  `CurtailmentSettingsPage.tsx` with `variant="responseProfile"`.

Their **Apply to** section currently exposes Sites, Miners, and Infrastructure.
Sites and miners currently compose as a miner-target union. This work replaces
that behavior with one terminal miner scope. Infrastructure remains a separate
facility-fan selection with sequencing delays and never contributes miners.

Start and Preview already share `buildCurtailmentScopes`, but form
normalization, request construction, proto hydration, profile adapters, event
mappers, and display labels each reconstruct parts of the site/miner rules.
Adding a category to only one copy can therefore lose it on preview, save,
reload, test, automation, or live Start. This work should establish one client
scope representation and one server resolver rather than extending those
copies independently.

Settings > Schedules is the closest UI/domain precedent:

- `ScheduleModal.tsx` orders Sites → Buildings → Racks → Groups → Miners.
- Its target pickers provide reusable loading, empty, error, multi-select, and
  filtering behavior. Curtailment reuses those Apply-to controls and adds
  parent-constrained drill-down within the selected scope.
- `server/internal/domain/schedule/targets/expand.go` expands logical targets
  and deduplicates their miner union. That union remains schedule-specific.

Curtailment needs that targeting vocabulary while preserving its own
eligibility, cooldown, event exclusivity, facility-fan sequencing,
authorization, and closed-loop lifecycle.

The published generic `ScopeDeviceSets` case is not a compatibility route:
curtailment has always returned Unimplemented for it, response-profile creation
rejects it, and there are no old clients or manually inserted records requiring
support. Remove the message and its oneof fields rather than deprecating them.
Reserve each removed field's name and tag in its enclosing message: tag 2 for
`CurtailmentScope`, Preview, and Start; tag 8 for events; and tag 11 for the
signal-ingestion override.

## Approach

Add Buildings, Racks, and Groups as explicit, persisted
`CurtailmentScope` cases. A curtailment or response profile has exactly one
terminal miner-scope type: Whole organization, Sites, Buildings, Racks, Groups,
or Miners. Multiple IDs may be selected within that type, but different types
never compose. Whole organization cannot coexist with a narrower selector.
Infrastructure remains independent, and a request without a recognized miner
selector is invalid even if fans are selected.

Both modal variants reuse the Schedules Apply-to behavior with hierarchical
drill-down. Ancestor selection constrains child catalogs (for example, Site A
exposes only its buildings and their racks), but navigation ancestors are not
submitted as selectors. Changing the terminal type replaces the previous
miner scope, and duplicate labels include ancestor context.

The builder reuses the Schedules Apply-to presentation: ordered Sites,
Buildings, Racks, Groups, and Miners rows open the corresponding multi-select
picker, and each saved parent selection constrains the next child picker.
Infrastructure remains an explicit independent selection. Schedules keeps its
existing multi-type target semantics; only reusable picker and filtering
behavior is shared.

Resolve topology membership on the backend. Persist logical selectors for
response profiles and closed-loop events; persist concrete targets for frozen
events and for every dispatched miner that may require restoration.

### Execution semantics

| Start mode | Membership behavior | Zero current miners |
| --- | --- | --- |
| Non-FULL_FLEET | Resolve once and freeze `curtailment_target` rows. | Fail as insufficient load. |
| FULL_FLEET without `force_include_all_paired_miners` | Whole-org/site/building/rack/group selectors remain topology-following; explicit identifiers are Start-time snapshots. | A topology selector may remain as a targetless watcher; explicit-only completes as the existing no-op. |
| FULL_FLEET with `force_include_all_paired_miners` | Admin-only durable policy over the selected terminal scope, including explicit identifiers. Newly assigned or newly paired miners can be admitted later. | Persist the watcher without commanding fans until a miner is admitted and confirmed. |

An unpaired miner can belong to a site, building, rack, or group. It remains
logically covered by an all-paired policy but cannot receive a command or own a
target until paired. Paired-like unavailable miners retain existing ownership
semantics.

When an owned miner leaves scope or becomes unpaired:

- Release it immediately only if no Curtail command could have been dispatched.
- Otherwise retain ownership as a durable restore obligation until restoration
  is confirmed.
- Retry unreachable/unpaired obligations when commandable and preserve the
  existing fan-before-miner restore sequence.

## Steps

### 1. Centralize the client scope model

- Add `buildingTargetIds`, `rackTargetIds`, and `groupTargetIds` beside
  `siteIds` and `deviceIdentifiers`. Keep fan fields outside the miner scope.
- Treat drill-down state as a view model and normalize only its active terminal
  selector into the canonical scope before validation, persistence, Preview, or
  Start. Parent IDs are filters, not additional target collections.
- Create one pure curtailment-scope module for single-type validation,
  deterministic deduplication, proto construction/hydration, all-paired
  eligibility, counts, and summaries.
- Use it from `CurtailmentStartModal.tsx`,
  `curtailmentRequestBuilders.ts`, `useCurtailmentPlanPreview.ts`,
  `useCurtailmentResponseProfiles.ts`, `CurtailmentSettingsPage.tsx`,
  `CurtailmentManagementPanel.tsx`, and `curtailmentMappers.ts`.
- Treat `scopeType` as the terminal selector discriminant. Keep `scopeId`,
  `siteId`, `siteSelection`, and `minerSelectionMode` as derived navigation/UI
  fields rather than independent targeting inputs.
- Ensure the typed IDs round-trip through Preview, Start, profile create/list/
  reload/edit, profile selection, Test curtailment, automation, event history,
  and session-cache normalization.
- Selecting a profile rehydrates its terminal selector; subsequent edits switch
  to Custom plan. Settings Test saves and starts that same selector without a
  whole-org fallback. `startRequestFromAutomationProfile` uses the same path.
- Render summaries by the terminal type, such as `2 buildings`.
- This UI-neutral model plus contracts, persistence, and backend resolution can
  land before the final target-builder visuals are ready.
- During that staged rollout, topology-scoped profiles remain visible but
  read-only in Settings and are excluded from the New curtailment profile
  selector until the UI can rehydrate their terminal selector. Once the UI
  lands, operators may preview and save topology scopes. Fixed-kW Run and Test
  become available with frozen topology execution; FULL_FLEET Run/Test and
  automation remain disabled until topology-following lifecycle support lands.
  No intermediate adapter may synthesize Whole organization for an unsupported
  typed scope.

### 2. Add the shared drill-down target builder

- Reuse or extract Schedules picker behavior in a neutral ProtoFleet location
  without cross-feature imports. In curtailment, choosing a scope opens its
  parent-to-child path and filters every child catalog by the selected ancestor;
  disambiguate duplicate labels. The topbar site remains a soft initial filter
  rather than a hidden selector, and groups retain cross-site membership
  semantics after selection.
- Add a curtailment/profile picker mode that retains missing stored IDs,
  labels them stale/unavailable from hydrated metadata or their ID, and removes
  them only through explicit operator action. Schedule callers retain their
  existing prune-missing behavior.
- Preserve the active terminal selection when editing under a narrower topbar
  filter or when the operator cannot open its picker.
- Add resource-aware picker permissions instead of plain `useHasPermission`:
  authenticate/derive the org, evaluate selected-site grants, and filter each
  catalog by full authorization coverage. For groups, evaluate every current
  member site—not an any-member site match—and hide or disable groups with
  unauthorized or unbounded coverage.
  Hide a control only when no authorized resource remains; hide rack/group miner
  facets when the scoped role lacks `rack:read`, matching Schedules.
- Allow **Target all paired miners** for FULL_FLEET with any terminal scope
  type, and clear it when switching away from FULL_FLEET.

### 3. Extend contracts and persistence

- Add validated building/rack/group messages and oneof cases to
  `proto/curtailment/v1/curtailment.proto`; use new field numbers.
- Remove the unused generic `device_set_ids` and `device_set_ids_override`
  fields and `ScopeDeviceSets`; reserve the field names/tags listed above and
  remove frontend/domain/JSON runtime paths. Do not retain deprecated typed
  accessors: after decode, a payload containing only removed tags fails the
  required recognized-scope validation.
- Extend the Go `Scope`, proto translators, `MarshalScopeJSON`, and
  `ScopeFromJSON` with typed ID arrays. Continue using the existing `mixed`
  storage type only for multiple IDs of one terminal type; it does not represent
  a multi-type union. Keep the existing scope JSON columns.
- Require explicit Whole organization or at least one typed miner selector for
  every new Preview, Start, profile Create/Update, test, and automation path.
  Missing, unknown, unsupported, or infrastructure-only scope never widens to
  whole organization or reaches fan dispatch.
- Reject requests and stored JSON that contain more than one selector type,
  including Whole organization plus a narrower selector. Repeated proto entries
  are valid only when every entry has the same terminal type.
- Add a required scope-schema version to new submissions. Deploy server support
  before the updated frontend, then raise the minimum accepted version before
  topology scopes are emitted. This rejects stale browser tabs rather than
  supporting two request shapes.
- Maintain an opaque profile revision that changes on every execution-affecting
  update: miner/fan scope, mode, power target, maintenance inclusion, all-paired
  or other admin controls, batching, and sequencing. Require it on profile
  Update, profile-derived execution, and automation Create/Update/enable; reload
  the matching stored profile and recompute its required-admin marker.

Bound inputs before expensive resolution:

| Limit | Bound |
| --- | ---: |
| IDs per topology type | 256 |
| Explicit device identifiers | 10,000 |
| Deduplicated resolved miners per execution/event | 10,000 |
| Repeated `CurtailmentScope` entries | 1,024 |
| Curtailment RPC body | 2 MiB |
| Active closed-loop watchers per org | 64 |
| Active-watcher topology IDs per org | 1,024 |
| Active-watcher explicit IDs per org | 10,000 |

Enforce cardinality on raw repeated input before deduplication and after domain
normalization. Page/stream topology expansion, track the deduplicated count, and
return ResourceExhausted before fully materializing, persisting, or dispatching
when it exceeds 10,000. Apply the limits to Preview, Start, profiles, automation,
and reconciliation; closed-loop admission is paged and stops at the per-event
cap. Use protobuf `max_items` where possible and enforce active aggregate quotas
transactionally before persisting a watcher/reservation.

### 4. Resolve targets and authorization once

- Extend `ListCandidatesParams`, the SQL adapter, and
  `ListCurtailmentCandidatesByOrg` with building/rack/group predicates selected
  by the active terminal type.
- Match fleet semantics:
  - Building: direct `device.building_id` or rack assigned to the building.
  - Rack: live rack device-set membership.
  - Group: live group membership, including cross-site members.
  - Always constrain resources, memberships, and devices to the organization
    and exclude deleted rows.
- Validate every topology ID even when it has zero members. Deleted,
  wrong-type, and cross-org IDs return NotFound rather than looking like empty
  load.
- Resolve cooldowns from the already-resolved candidate identifiers so a
  topology request cannot accidentally apply org-wide exclusions.
- Return one resolution result containing the terminal type and IDs,
  selected-resource sites,
  current-member sites, uncovered/unbounded state, concrete identifiers, and
  separate facility-fan sites/unassigned state.

Authorization coverage follows these rules:

| Selector/resource | Required coverage |
| --- | --- |
| Site | That site. |
| Building | Its owning site even when empty; org-wide when unassigned. |
| Rack | `device_set_rack.site_id` even without a building or members; org-wide when NULL. If a building exists, reject a rack/building site mismatch. |
| Group | Every current member site; org-wide when empty or when future coverage is otherwise unbounded. |
| Explicit miner | Its current site; org-wide when unassigned. |
| Facility fan | Separate exact-site envelope requiring both `site:read` and `curtailment:manage`; org-wide recovery for unproven coverage. |

Require `curtailment:manage` for every miner site and org-wide permission for
unassigned/unbounded coverage. Bind selector validation, membership expansion,
site coverage, envelope validation, and target claim in one transaction. Within
it, re-resolve and reauthorize every original selector, selected resource,
membership, logical reservation, and facility fan—not only concrete miners.
Lock relevant topology rows or compare topology revisions; dispatch only after
commit. Immediately before each physical Curtail send, the dispatcher must
reload the claimed miner's current topology and the event's persisted envelope,
then reauthorize the principal with no intervening asynchronous work. A failed
check sends no command, marks the watcher degraded, and safely restores any
previously dispatched ownership; the claim alone is never authority to send.

Replace the org-scoped `RequirePermission(..., ResourceContext{})` entry gates
across Preview/Start, response-profile/automation, and event Get/List/Update/Stop
RPCs with authentication/org derivation followed by scoped resolution or
persisted-envelope checks. Event Get/Update/Stop authorize against that event's
envelope; List returns only events whose envelopes the caller can access.

Keep `scope_json` executable-selector-only. On profile Create/Update, persist
selected-resource/current-member site coverage, independent fan-site coverage,
and separate unbounded flags in a dedicated authorization-envelope column that
target resolution never interprets as selectors.

### 5. Preserve closed-loop ownership and exclusivity

- Persist the logical selector for topology-following FULL_FLEET events; keep
  non-FULL_FLEET and unflagged explicit-miner targets frozen.
- On reconciliation, admit newly eligible/paired members,
  retain unavailable ownership, and apply the restore-before-release rules
  above to targets leaving the selector.
- If admission finds the event's facility fans off, persist a fans-on
  transition, wait the airflow delay, then re-resolve/re-authorize before
  claiming and curtailing the miner. Turn fans off again only after every owned
  target is confirmed curtailed; recovery resumes this sequence after crashes.
- Treat every active closed-loop logical scope as a reservation, including
  matching miners not yet represented by target rows. Flagged explicit-miner
  policies use the same reservation path.
- Update `CountCurtailmentScopeConflicts`,
  `ListActiveCurtailedDevicesByOrg`, `hierarchicalScopeSiteIDs`, and direct
  `scope_jsonb` consumers to use the canonical typed resolver.
- Serialize Start, reconciliation, and conflict checks with an org-scoped
  lock. Older reservations win deterministically. A device already owned when
  it moves into a watched scope retains its owner; mark the watcher degraded
  and retry after release.
- Persist the authorization envelope, authorizing principal, and whether
  admin-only controls are required on every event. Each closed-loop admission
  reloads current permissions, current Admin/SuperAdmin role when required, and
  current topology before claiming a target.
- Permission revocation, role demotion, or out-of-envelope topology stops new
  admissions and moves owned targets through safe restoration; it never
  releases a possibly curtailed miner directly.
- Reconcile watchers/targets in pages under a per-tick time budget. Prioritize
  stop/restore work; overload backpressure pauses new admissions before it can
  delay safety-critical restoration.

### 6. Make profiles and automation strict but recoverable

Use strict current topology for profile Create/Update/execution and automation
Create/Update/enable/execution. Use persisted envelopes for recovery operations:

| Record state/operation | Behavior |
| --- | --- |
| Valid profile Get/List/Delete | Authorize against persisted miner/fan envelope; hydrate stale typed IDs without live topology. |
| Stale profile resave/execution | Reject until stale IDs are explicitly removed/replaced. |
| Automation Get/List/Delete/disable | Authorize against the bound envelope; remain available when profile topology or revision is stale. |
| Automation Create/Update/enable/OFF start | Require strict topology, exact executable-profile revision/envelope, current principal permissions, and current Admin/SuperAdmin role for admin-only controls. |
| Automation ON/stop with an active event | Restore from the event's durable targets and fan settings without requiring the current profile revision/topology/admin grant; never admit or recurtail targets. |

Add immutable up/down migrations for separate profile/event authorization-
envelope columns and automation-rule binding fields: executable-profile
revision, miner/fan envelope, execution principal, and required-admin marker.
In the same transaction that saves/enables a rule, lock the profile, compare the
handler-authorized revision/envelope, and persist that exact binding. Extend the
existing store-side fan-settings race check; mismatch returns FailedPrecondition
without saving.

Reload the bound user or service-account permissions at every trigger; never
fall back to the profile creator or an ambient system principal.

Before an OFF/start or recurtail action, compare the current profile revision
with the bound revision; mismatch reports `rebind_required`. An authenticated
ON/stop for an already-owned event must always follow its durable restore path,
even after profile change/deletion, permission loss, or admin demotion.
Disable/Delete likewise do not require a current revision or strict topology.
The event-create/recurtail transaction must lock the rule and profile, then
recheck enabled state, binding, revision, envelope, principal authorization,
admin requirement, topology, and quotas before claiming targets.

### 7. Roll out fail-closed behavior safely

1. Ship server parsing, schema versions, profile revisions, automation bindings,
   rejection of unknown nonempty scope keys, and a durable topology-scope
   feature gate without emitting topology scopes.
2. Require every replica at that minimum version. The migration assigns an
   current scope schema version and an initial revision to existing profiles,
   binds each existing automation rule to its profile's revision, and stamps
   that binding into live automation event snapshots that can be recovered
   through idempotency replay. Other existing events keep their durable recovery
   data.
   After migration, records missing a required scope, envelope, revision, or
   binding fail closed.
3. Deploy the frontend that always sends the new version and explicit whole-org
   scope, raise the minimum accepted schema version, then open the feature gate.
   Rollback below the minimum requires closing the gate, blocking profile Test/
   Start/Create/Update, disabling dependent automation, draining topology events,
   and deleting topology-scoped profiles before an older server starts.

## Verification

Frontend coverage:

- Both modal variants: Schedules-style Apply-to controls, ancestor-filtered
  drill-down, duplicate labels, single terminal type, counts, permissions,
  stale-ID preservation, and all-paired behavior. Schedules retains its own
  union tests.
- Canonical request/hydration tests for each target type and mixed-type
  rejection across
  Preview, Start, profile create/reload/edit/test, automation, and display.
- Staged-rollout tests prove unsupported topology profiles remain read-only and
  cannot enter an execution path through a Whole organization fallback.
- Stale-browser schema rejection and profile/automation revision handling;
  disable/delete remain available without a current revision.

Backend coverage:

- Proto/domain/JSON round trips and required-scope rejection for payloads that
  contain only removed generic device-set wire tags.
- Candidate membership, deduplication, cooldowns, zero-member resources,
  wrong-type/deleted/cross-org IDs, direct-site racks, rack/building mismatch,
  empty/cross-site groups, unassigned resources, oversized selectors, mixed-type
  rejection, and selectors whose deduplicated expansion exceeds 10,000.
- Frozen versus topology-following FULL_FLEET behavior, empty watchers,
  explicit-miner snapshot behavior, flagged explicit-miner policy,
  reservation conflicts, pairing transitions, fan-on admission (including
  crash/concurrency recovery), and restore obligations.
- Authorization races for empty watchers and moved fans, separate fan coverage,
  stale CRUD recovery, current permission revocation,
  Admin/SuperAdmin demotion, and current topology exceeding an envelope.
- A miner moving outside the envelope after claim/commit but before dispatch is
  rejected by the final send-time check and receives no Curtail command.
- Site-scoped Start followed by event List/Get/Update/Stop, denial outside the
  persisted envelope, and proof that selector resolution ignores envelope data.
- Full executable-profile revision coverage, automation binding races,
  `rebind_required`, stale rule management, ON/stop recovery after profile or
  permission changes, and fail-closed rollout sequencing.
- Raw/normalized and active-watcher quotas, paginated/time-budgeted overload
  behavior with restore priority and admission backpressure.

Run:

```sh
bin/just gen
(cd server && ../bin/go test ./internal/domain/curtailment/... ./internal/handlers/curtailment ./internal/domain/stores/sqlstores)
(cd client && ../bin/npx vitest run src/protoFleet/features/energy/CurtailmentStartModal.test.tsx src/protoFleet/features/energy/CurtailmentManagementPanel.test.tsx src/protoFleet/features/energy/curtailmentRequestBuilders.test.ts src/protoFleet/features/energy/useCurtailmentPlanPreview.test.ts src/protoFleet/api/curtailmentScopes.test.ts src/protoFleet/api/useCurtailmentResponseProfiles.test.tsx src/protoFleet/features/settings/components/Curtailment/CurtailmentSettingsPage.test.tsx)
bin/just lint
```

Add ProtoFleet Playwright coverage for creating, reloading/editing, testing, and
running topology-scoped profiles from both entry points. Do not refresh visual
snapshots without explicit approval.

## Acceptance

- Both Apply-to flows reuse Schedules picker behavior: child options follow
  selected ancestors and the result contains exactly one typed terminal scope.
  Multiple IDs within that scope are deduplicated; parent selections are not
  submitted as targets. Schedules retains its existing union semantics.
- Infrastructure remains an explicit independent selection.
- Preview, Start, profiles, automation, active events, and history use the same
  canonical scope conversion/resolution.
- Normal and unflagged explicit-miner events remain snapshots; closed-loop
  topology and flagged all-paired policies follow their persisted terminal
  selector.
- Empty topology watchers never command fans before a miner is confirmed;
  empty, unknown, and infrastructure-only scope never widens to whole org.
- Authorization includes selected resources, members, and independent fan
  coverage; unassigned/unbounded targets require org-wide permission.
- Selector JSON and authorization envelopes are stored separately; envelope
  coverage can never become an executable site selector.
- Target claims are atomic with topology and authorization checks; a final
  current-envelope check gates each physical send, and active logical
  reservations prevent competing events from stealing future members.
- Dispatched targets remain owned through fan-aware safe restoration when they
  leave scope, lose authorization, become unpaired, or their principal is
  demoted.
- Stale profiles/rules remain visible and removable under persisted envelopes,
  but resave, enable, and execution fail closed until corrected or rebound.
- Schema versions and opaque revisions prevent stale clients or profile races
  from broadening or silently retargeting a scope.
- The deployment gate prevents mixed-version execution; missing versioned scope,
  envelope, revision, or binding data is rejected rather than preserved.
- Response profiles keep executable selectors in existing scope JSON; immutable
  up/down migrations add separate profile/event envelopes and automation
  bindings. Proto and generated output are committed together.
