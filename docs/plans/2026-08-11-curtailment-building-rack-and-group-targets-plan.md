---
title: "Curtailment building, rack, and group targets"
date: 2026-08-11
status: draft
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
Sites and miners compose as a miner-target union. Infrastructure is a separate
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
- Its building/rack/group modals already provide multi-select, loading, empty,
  error, and site-filter behavior.
- `server/internal/domain/schedule/targets/expand.go` expands logical targets
  and deduplicates their miner union.

Curtailment needs that targeting vocabulary while preserving its own
eligibility, cooldown, event exclusivity, facility-fan sequencing,
authorization, and closed-loop lifecycle.

The published generic `ScopeDeviceSets` case is not a compatibility route:
curtailment has always returned Unimplemented for it, response-profile creation
rejects it, and there are no old clients or manually inserted records requiring
runtime/storage support. Keep its protobuf numbers reserved and deprecated for
Buf compatibility, but remove generic device-set handling from this feature.

## Approach

Add Buildings, Racks, and Groups as explicit, persisted
`CurtailmentScope` cases. A miner is in scope when it matches any selected
site, building, rack, group, or explicit identifier; the backend validates,
unions, and deduplicates that selection. Whole organization dominates and
clears narrower miner selectors. Infrastructure remains independent, and a
request without a recognized miner selector is invalid even if fans are
selected.

Both modal variants use this order:

1. Sites
2. Buildings
3. Racks
4. Groups
5. Miners
6. Infrastructure

Resolve topology membership on the backend. Persist logical selectors for
response profiles and closed-loop events; persist concrete targets for frozen
events and for every dispatched miner that may require restoration.

### Execution semantics

| Start mode | Membership behavior | Zero current miners |
| --- | --- | --- |
| Non-FULL_FLEET | Resolve once and freeze `curtailment_target` rows. | Fail as insufficient load. |
| FULL_FLEET without `force_include_all_paired_miners` | Whole-org/site/building/rack/group selectors remain topology-following; explicit identifiers are Start-time snapshots. A mixed union watches only its topology portion. | A topology selector may remain as a targetless watcher; explicit-only completes as the existing no-op. |
| FULL_FLEET with `force_include_all_paired_miners` | Admin-only durable policy over the complete union, including explicit identifiers. Newly assigned or newly paired miners can be admitted later. | Persist the watcher without commanding fans until a miner is admitted and confirmed. |

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
- Create one pure curtailment-scope module for normalization, whole-org
  dominance, deterministic deduplication, proto construction/hydration,
  all-paired eligibility, counts, and summaries.
- Use it from `CurtailmentStartModal.tsx`,
  `curtailmentRequestBuilders.ts`, `useCurtailmentPlanPreview.ts`,
  `useCurtailmentResponseProfiles.ts`, `CurtailmentSettingsPage.tsx`,
  `CurtailmentManagementPanel.tsx`, and `curtailmentMappers.ts`.
- Treat scalar `scopeType`, `scopeId`, `siteId`, `siteSelection`, and
  `minerSelectionMode` as derived UI/legacy fields, not independent targeting
  inputs.
- Ensure the typed IDs round-trip through Preview, Start, profile create/list/
  reload/edit, profile selection, Test curtailment, automation, event history,
  and session-cache normalization.
- Selecting a profile rehydrates its logical union; subsequent edits switch to
  Custom plan. Settings Test saves and starts that same union without a
  whole-org fallback. `startRequestFromAutomationProfile` uses the same path.
- Render summaries by real type, such as `2 buildings + 1 group`.

### 2. Add shared Apply-to controls

- Reuse/extract the Schedule building/rack/group pickers into a neutral
  ProtoFleet location without creating imports between feature areas.
- Preserve the topbar site as a soft inventory filter: it filters buildings,
  racks, and miners; groups remain org-wide/cross-site.
- Add a curtailment/profile picker mode that retains missing stored IDs,
  labels them stale/unavailable from hydrated metadata or their ID, and removes
  them only through explicit operator action. Schedule callers retain their
  existing prune-missing behavior.
- Preserve existing off-site selections when editing under a narrower topbar
  filter or when the operator cannot open a picker.
- Add resource-aware picker permissions instead of plain `useHasPermission`:
  evaluate the selected site grants and use site-filtered building/rack/group
  catalog handlers that authenticate/derive the org before scoped authorization.
  Hide a control only when no authorized resource remains; hide rack/group miner
  facets when the scoped role lacks `rack:read`, matching Schedules.
- Allow **Target all paired miners** for FULL_FLEET with any site/building/rack/
  group/miner union, and clear it when switching away from FULL_FLEET.

### 3. Extend contracts and persistence

- Add validated building/rack/group messages and oneof cases to
  `proto/curtailment/v1/curtailment.proto`; use new field numbers.
- Deprecate and reserve the unused generic `device_set_ids` and
  `device_set_ids_override` fields. Reject them at transport boundaries and
  remove frontend/domain/JSON runtime paths.
- Extend the Go `Scope`, proto translators, `MarshalScopeJSON`, and
  `ScopeFromJSON` with typed ID arrays. Continue using the existing `mixed`
  scope type and scope JSON columns.
- Require explicit Whole organization or at least one typed miner selector for
  every new Preview, Start, profile Create/Update, test, and automation path.
  Missing, unknown, unsupported, or infrastructure-only scope never widens to
  whole organization or reaches fan dispatch.
- Backfill persisted empty-scope/absent-site whole-org profiles to explicit
  `{"whole_org":true}` before omitted scopes are rejected.
- Add a required scope-schema version to new submissions. Deploy server support
  and the updated frontend, complete the backfill, then raise the minimum
  accepted version before topology scopes are emitted. This rejects stale
  browser tabs without retaining an old-client compatibility path.
- Maintain an opaque profile revision that changes on every execution-affecting
  update: miner/fan scope, mode, power target, maintenance inclusion, all-paired
  or other admin controls, batching, and sequencing. Require it on profile
  Update, profile-derived execution, and automation Create/Update/enable; reload
  the matching stored profile and recompute its required-admin marker.

Bound inputs before expensive resolution:

| Limit | Bound |
| --- | ---: |
| IDs per topology type | 256 |
| Total topology IDs | 1,024 |
| Explicit device identifiers | 10,000 |
| Repeated `CurtailmentScope` entries | 1,024 |
| Curtailment RPC body | 2 MiB |
| Active closed-loop watchers per org | 64 |
| Active-watcher topology IDs per org | 1,024 |
| Active-watcher explicit IDs per org | 10,000 |

Enforce cardinality on raw repeated input before deduplication and again after
domain normalization. Apply the same limits to direct requests, persisted
profiles, automation, and reconciliation; use protobuf `max_items` where the
wire shape permits. Enforce the active aggregate quotas transactionally before
persisting a watcher/reservation.

### 4. Resolve targets and authorization once

- Extend `ListCandidatesParams`, the SQL adapter, and
  `ListCurtailmentCandidatesByOrg` with unioned building/rack/group predicates.
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
- Return one resolution result containing typed IDs, selected-resource sites,
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
commit.

Replace the org-scoped `RequirePermission(..., ResourceContext{})` entry gates
across Preview/Start and response-profile/automation RPCs with authentication/
org derivation followed by their scoped resolver or persisted-envelope checks;
otherwise site-scoped grants cannot reach those authorization paths.

On profile Create/Update, persist the union of selected-resource and current-
member site coverage plus the independent fan-site coverage in `scope_json`,
with separate org-wide flags for incomplete or unbounded dimensions.

### 5. Preserve closed-loop ownership and exclusivity

- Persist the logical union for topology-following FULL_FLEET events; keep
  non-FULL_FLEET and unflagged explicit-miner targets frozen.
- On reconciliation, admit newly eligible/paired members, deduplicate overlap,
  retain unavailable ownership, and apply the restore-before-release rules
  above to targets leaving the union.
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
  admin-only controls are required on every closed-loop event. Each admission
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
| Profile with missing/unproven envelope | Org-wide `curtailment:manage`, plus org-wide `site:read` when fans exist; no current-location fallback. |
| Stale profile resave/execution | Reject until stale IDs are explicitly removed/replaced. |
| Automation Get/List/Delete/disable | Authorize against the rule's bound envelope; remain available when profile topology or revision is stale. |
| Automation Create/Update/enable/OFF start | Require strict topology, exact executable-profile revision/envelope, current principal permissions, and current Admin/SuperAdmin role for admin-only controls. |
| Automation ON/stop with an active event | Restore from the event's durable targets and fan settings without requiring the current profile revision/topology/admin grant; never admit or recurtail targets. |

Add immutable up/down migrations for automation-rule binding fields: executable-
profile revision, miner/fan envelope, execution principal, and required-admin
marker. In the same transaction that saves/enables a rule, lock the profile,
compare the handler-authorized revision/envelope, and persist that exact
binding. Extend the existing store-side fan-settings race check; mismatch
returns FailedPrecondition without saving.

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

1. Ship a compatibility-floor server that rejects unknown nonempty scope keys,
   plus proto parsing, schema versions, profile revisions, automation bindings,
   and a durable topology-scope feature gate, without emitting topology scopes.
   Require every replica at this floor before opening the gate; rollback below
   it requires disabling affected automation and draining topology events first.
2. Deploy the frontend that always sends the new version and explicit
   whole-org scope.
3. Inventory and remediate persisted data:
   - Backfill provable whole-org/site profile envelopes.
   - Mark ambiguous profiles `reauthorization_required`.
   - Inventory enabled rules missing a proven revision, envelope, principal, or
     admin marker; rebind, disable, or approve an announced cutoff.
   - Mark oversized profiles `scope_limit_remediation_required`; keep Get/List/
     Delete and shrink-to-valid Update available.
   - Backfill active event envelopes only when scope and principal prove the
     boundary.
4. Put ambiguous or oversized active events into no-admission drain mode:
   continue confirmation, fan sequencing, restoration, release, and
   terminalization from durable target rows without re-resolving logical scope.
5. Gate enforcement on remediation/cutoff completion, raise the minimum schema
   version, then enable topology-scope creation and emission.

## Verification

Frontend coverage:

- Both modal variants: ordering, counts, select-all/clear, site filters,
  permissions, stale-ID preservation, and all-paired behavior.
- Canonical request/hydration tests for each target type and mixed unions across
  Preview, Start, profile create/reload/edit/test, automation, and display.
- Stale-browser schema rejection and profile/automation revision handling;
  disable/delete remain available without a current revision.

Backend coverage:

- Proto/domain/JSON round trips and rejection of generic device-set input.
- Candidate membership, deduplication, cooldowns, zero-member resources,
  wrong-type/deleted/cross-org IDs, direct-site racks, rack/building mismatch,
  empty/cross-site groups, and unassigned resources.
- Frozen versus topology-following FULL_FLEET behavior, empty watchers,
  explicit-miner snapshot compatibility, flagged explicit-miner policy,
  reservation conflicts, pairing transitions, fan-on admission (including
  crash/concurrency recovery), and restore obligations.
- Authorization races for empty watchers and moved fans, separate fan coverage,
  stale CRUD recovery, missing envelopes, current permission revocation,
  Admin/SuperAdmin demotion, and current topology exceeding an envelope.
- Full executable-profile revision coverage, automation binding races,
  `rebind_required`, stale rule management, ON/stop recovery after profile or
  permission changes, and rollout remediation.
- Raw/normalized and active-watcher quotas, paginated/time-budgeted overload
  behavior with restore priority, oversized remediation, and safe event draining.

Run:

```sh
bin/just gen
(cd server && ../bin/go test ./internal/domain/curtailment/... ./internal/handlers/curtailment ./internal/domain/stores/sqlstores)
(cd client && ../bin/npx vitest run src/protoFleet/features/energy/CurtailmentStartModal.test.tsx src/protoFleet/features/energy/curtailmentRequestBuilders.test.ts src/protoFleet/features/energy/useCurtailmentPlanPreview.test.ts src/protoFleet/api/useCurtailmentResponseProfiles.test.tsx src/protoFleet/features/settings/components/Curtailment/CurtailmentSettingsPage.test.tsx)
bin/just lint
```

Add ProtoFleet Playwright coverage for creating, reloading/editing, testing, and
running topology-scoped profiles from both entry points. Do not refresh visual
snapshots without explicit approval.

## Acceptance

- Both Apply-to flows expose Sites, Buildings, Racks, Groups, Miners, and
  Infrastructure with consistent union semantics and typed round trips.
- Preview, Start, profiles, automation, active events, and history use the same
  canonical scope conversion/resolution.
- Normal and unflagged explicit-miner events remain snapshots; closed-loop
  topology and flagged all-paired policies follow their documented unions.
- Empty topology watchers never command fans before a miner is confirmed;
  empty, unknown, and infrastructure-only scope never widens to whole org.
- Authorization includes selected resources, members, and independent fan
  coverage; unassigned/unbounded targets require org-wide permission.
- Target claims are atomic with topology and authorization checks, and active
  logical reservations prevent competing events from stealing future members.
- Dispatched targets remain owned through fan-aware safe restoration when they
  leave scope, lose authorization, become unpaired, or their principal is
  demoted.
- Stale profiles/rules remain visible and removable under persisted envelopes,
  but resave, enable, and execution fail closed until corrected or rebound.
- Schema versions and opaque revisions prevent stale clients or profile races
  from broadening or silently retargeting a scope.
- Rollout remediates ambiguous/oversized records and drains unsafe active events
  without new admissions or stranded miners; the durable compatibility floor
  prevents mixed-version execution or unsafe rollback of topology scopes.
- Response profiles reuse existing scope JSON; automation bindings use a new
  immutable up/down migration. Proto and generated output are committed
  together.
