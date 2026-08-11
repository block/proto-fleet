---
title: "Curtailment building, rack, and group targets"
date: 2026-08-11
status: draft
type: plan
tracker: https://github.com/block/proto-fleet/issues/909
---

# Curtailment building, rack, and group targets

## Context

Both operator flows use the same form component:

- Energy's **New curtailment** flow renders
  `client/src/protoFleet/features/energy/CurtailmentStartModal.tsx` through
  `CurtailmentManagementPanel.tsx`.
- Settings' **Create/Edit response profile** flow renders the same component
  with `variant="responseProfile"` through
  `CurtailmentSettingsPage.tsx`.

The shared **Apply to** section currently exposes Sites, Miners, and
Infrastructure. Sites and miners are composable miner selectors; Infrastructure
is an independent list of facility fans plus sequencing delays. Start and
Preview already share `buildCurtailmentScopes`, but the surrounding scope
semantics are still distributed across modal normalization, preview labels,
response-profile proto hydration/construction, settings and energy-page form
adapters, and event mappers. Those paths independently reconstruct parts of the
same rules (whole-org dominance, site/miner unions, legacy scalar fallbacks,
deduplication, and display labels), so adding another target category in only
one path can silently lose it in another.

The API already supports repeated `CurtailmentScope` entries and unions them on
the server. It can persist composite scopes in `curtailment_event.scope_jsonb`
and `curtailment_response_profile.scope_json`, so adding topology target fields
does not require a new database column. There is a published generic
`ScopeDeviceSets` selector, but the curtailment domain has deliberately
returned Unimplemented for it since the initial backend implementation, and
response-profile creation rejects it too. There are no old clients or manually
inserted records that require its runtime or storage representation to remain
readable. Treat it as dead contract surface, not as a compatibility path or an
implementation shortcut for racks and groups.

The closest implemented precedent is Settings > Schedules:

- `ScheduleModal.tsx` presents Sites → Buildings → Racks → Groups → Miners.
- `BuildingSelectionModal.tsx`, `RackSelectionModal.tsx`, and
  `GroupSelectionModal.tsx` already provide multi-select, loading, empty, and
  error states.
- `server/internal/domain/schedule/targets/expand.go` resolves logical targets
  at execution time and deduplicates their miner union.

Curtailment needs the same logical targeting vocabulary, but it must retain its
own eligibility, cooldown, active-event exclusion, preview, authorization, and
event-snapshot semantics.

## Approach

Add Buildings, Racks, and Groups as first-class, persisted curtailment scope
selectors. Keep target selections as a union: a miner is in scope when it is in
any selected site, building, rack, group, or explicit-miner list; deduplicate
before selection and dispatch. Infrastructure remains independent and does not
contribute miners to the union.

Resolve topology membership on the backend, not in the browser:

- Preview resolves current membership and reports the current eligible target
  set.
- A non-FULL_FLEET Start resolves current membership once, then freezes
  concrete miner targets in `curtailment_target`, matching existing open-loop
  behavior.
- FULL_FLEET without `Target all paired miners` preserves existing scope
  behavior: whole-org/site selectors remain closed-loop and buildings/racks/
  groups join that topology-following watcher, while explicit-miner selectors
  remain snapshot-only. A mixed union watches its topology portion and includes
  explicit miners only when they are eligible in the Start snapshot.
- A response profile stores logical IDs, so each later manual or automation
  execution resolves the then-current members.
- For FULL_FLEET with `Target all paired miners`, extend the durable closed-loop
  policy to every composable miner selector: sites, buildings, racks, groups,
  and explicit miners. The reconciler re-resolves the union while the event is
  active and admits newly paired/in-scope miners. A newly considered UNPAIRED
  miner cannot receive commands and remains outside ownership until it becomes
  paired; paired-like but temporarily unavailable miners retain the existing
  `UNAVAILABLE` behavior. When an owned target becomes unpaired or leaves the
  selected topology, release it immediately only if no Curtail command could
  have been dispatched. A dispatched or confirmed target becomes a durable
  restore obligation: retain ownership and use the normal safe restore
  lifecycle before release. If it is unreachable or unpaired, retry after it
  becomes commandable; if facility fans are off, defer miner restoration until
  the existing fan-before-miner restore sequence makes restoration safe.

Use explicit proto cases for buildings, racks, and groups rather than encoding
racks and groups into the legacy `device_set_ids` case. This preserves target
type fidelity after reload, makes stale-target errors intelligible, and avoids
requiring the frontend to refetch and infer whether each generic set ID was a
rack or a group.

The Apply-to order in both flows should be:

1. Sites
2. Buildings
3. Racks
4. Groups
5. Miners
6. Infrastructure

Reuse the Schedule selection modals by moving them to a neutral shared
ProtoFleet target-picker location, or make their current module explicitly
shared without importing curtailment into Schedules. Preserve the current soft
site default: a selected topbar site filters building/rack/miner inventory;
all-sites shows org-wide inventory; groups stay org-wide/cross-site. Existing
off-site selections must remain visible/preserved when editing a profile under
a narrower topbar filter.

## Scope model and compatibility

Extend the client form models with separate arrays:

- `buildingTargetIds: string[]`
- `rackTargetIds: string[]`
- `groupTargetIds: string[]`

Retain `siteIds`, `deviceIdentifiers`, and facility-fan fields. Treat the
existing scalar `scopeType` as a derived compatibility/display field rather
than the source of truth for composite selections. Selecting Whole fleet / All
miners clears every narrower miner selector. Clearing one selector category
does not clear the others.

Add three selectors to `CurtailmentScope` in
`proto/curtailment/v1/curtailment.proto`, each with validated, unique positive
IDs and new protobuf field numbers. Mark the unused generic `device_set_ids`
fields deprecated and leave their field numbers allocated so this additive
feature satisfies the repository's Buf `FILE` compatibility policy; do not add
new runtime/storage support or reinterpret those numbers as buildings, racks,
or groups. Remove the deprecated declarations in a separate intentional API
cleanup if the repository's breaking-change policy is relaxed for them.

Bound selector cardinality using named shared constants: at most 256 IDs for
each topology type (sites, buildings, racks, and groups), at most 1,024 topology
IDs across the normalized union, at most 10,000 explicit device identifiers,
and at most 1,024 repeated `CurtailmentScope` entries. Apply protobuf
`max_items` validation where the wire shape permits. Before deduplication,
enforce the same 256-per-type, 1,024-total-topology, and 10,000-total-explicit
limits across all repeated scope entries, then enforce them again after
normalization in the domain for direct requests, response profiles, automation
execution, and persisted closed-loop reconciliation. Configure a 2 MiB request
body limit for curtailment RPCs so oversized protobuf payloads are rejected
before decoding/normalization. The frontend mirrors these limits for usability;
the server remains authoritative.

Before enabling limit enforcement, inventory every persisted profile and
active event using the normalized counters. Gate rollout on remediating all
oversized profiles or marking them `scope_limit_remediation_required`; keep
Get/List/Delete available and allow only shrink-to-valid updates for marked
profiles. Do not bind or execute their automation after the announced
remediation cutoff. Grandfather already-active oversized events through safe
completion so an upgrade cannot strand curtailed miners, but reject new events
at the limits. Since records are API-created rather than manually inserted,
this audit/backfill is the authoritative migration path.

Extend the Go domain `Scope` and JSON codec with `building_ids`, `rack_ids`, and
`group_ids`, and remove generic `DeviceSetIDs`/`device_set_ids` domain and JSON
handling. Preserve existing decoding and proto rendering for whole-org, site,
device-list, and mixed records. New topology-only or composite records can
continue to persist with the existing `mixed` scope type plus the richer JSON
object, avoiding a response-profile schema migration and new database scope-
type values. For response profiles, the same JSON object should also carry an
authorization envelope
with separate miner-scope and facility-fan site coverage captured when the
profile is created or updated. The fan dimension must preserve the existing
requirement for both `site:read` and `curtailment:manage`; miner coverage
continues to require `curtailment:manage`.
Persist the same envelope and authorizing-principal reference on every
closed-loop FULL_FLEET event, with or without the all-paired flag; frozen
non-FULL_FLEET and unflagged explicit-miner events continue to rely on
authorization at Preview/Start plus their concrete target rows.
Add an automation-rule migration for a bound profile scope revision, bound
miner/fan authorization envelope, and required-admin-controls marker. These
fields preserve the exact profile scope and privilege class an operator
authorized when the rule was created, updated, or enabled; they are also the
stable authorization source for managing a rule after its profile topology
becomes stale.

Generated protobuf output must be regenerated with `just gen` and committed
with the proto source.

## Steps

### 1. Centralize client scope conversion

- Define one canonical miner-target selection independent of presentation
  metadata:
  - either whole organization, or
  - a union of `siteIds`, `buildingIds`, `rackIds`, `groupIds`, and explicit
    `deviceIdentifiers`.
  Keep target names/labels and Infrastructure fields outside this semantic
  scope object.
- Require every new Preview, Start, and response-profile Create/Update request
  to contain a recognized, explicit miner selector: Whole organization or at
  least one site, building, rack, group, or miner. Infrastructure does not make
  an empty or unrecognized miner scope valid, and those requests must fail
  before an event, profile, or facility-fan action is created.
- Create a pure curtailment-scope module used by:
  - `CurtailmentStartModal.tsx`
  - `curtailmentRequestBuilders.ts`
  - `useCurtailmentPlanPreview.ts`
  - `useCurtailmentResponseProfiles.ts`
  - `CurtailmentSettingsPage.tsx`
  - `CurtailmentManagementPanel.tsx`
  - `curtailmentMappers.ts`
- Put normalization, whole-org dominance/clearing, deterministic deduplication,
  proto scope construction, proto scope hydration, target counts,
  all-paired eligibility, and human-readable summaries in that module. Start,
  Preview, and response-profile create/update must call the same proto scope
  constructor; event and response-profile hydration must call the same proto
  scope decoder.
- Keep boundary-specific adapters only for genuinely different concerns such
  as string-to-`bigint` validation, preview debounce/request keys, site-name
  lookup, and non-scope response-profile fields. Treat `scopeType`, `scopeId`,
  scalar `siteId`, `siteSelection`, and `minerSelectionMode` as derived legacy/UI
  compatibility fields rather than independent sources of targeting truth.
- Update `ResponseProfileFormValues`, `CurtailmentFormValues`, session-cache
  normalization, and settings-page form adapters so building/rack/group IDs
  round-trip through API-backed create, list/reload, edit, profile selection,
  test-curtailment, and live Start.
- Update event/profile display helpers to render counts by their real type
  (for example, `2 buildings + 1 group`) instead of `device sets` or `Unknown
  scope`.

### 2. Add the shared Apply-to controls

- Add Buildings, Racks, and Groups `TargetSelectButton`s to the shared modal in
  broad-to-narrow order.
- Reuse/extract the Schedule building, rack, and group selection modals,
  including select-all, empty/error states, stale selection preservation, and
  site filters.
- Do not reuse the Schedule pickers' current behavior of pruning IDs absent
  from the live inventory. Add an explicit curtailment/profile mode that keeps
  stored missing IDs selected, renders them from hydrated profile metadata (or
  their ID) as unavailable/stale, and removes them only through an operator's
  explicit action. Preserve the existing pruning behavior for Schedule callers.
- Pass the topbar-derived `SiteFilterFields` from both parent flows. Pass the
  same scope to the miner selector and hide rack/group miner facets when the
  current role lacks `rack:read`, following `ScheduleModal`.
- Gate target buttons on the list permissions their picker needs:
  Buildings require `site:read`; Racks/Groups require `rack:read`; Miners
  require `miner:read`. Keep submitted existing selections intact when a
  picker is unavailable so editing does not erase data the user cannot list.
- Update Apply-to helper text, confirmation copy, profile-card summaries, and
  preview scope labels for the additional categories.
- Allow `Target all paired miners` for FULL_FLEET across any union of site,
  building, rack, group, and explicit-miner selectors. Continue clearing it
  when switching away from FULL_FLEET. Confirmation copy must describe the
  selected union rather than implying the entire organization.

### 3. Extend proto translation and JSON round trips

- Add explicit building/rack/group scope messages and cases to
  `CurtailmentScope`.
- Extend `toCompositeScope`, legacy/request translators,
  `protoScopesFromDomainScope`, event rendering, response-profile rendering,
  `MarshalScopeJSON`, and `ScopeFromJSON`.
- Ensure a whole-org entry still dominates narrower selectors and mixed
  entries are normalized/deduplicated deterministically.
- Reject an empty or unrecognized miner scope at every new submission boundary,
  including when facility fans are present. Never synthesize whole-org scope
  from missing, unknown, or unsupported cases, and never dispatch a fan unless
  the execution has at least one admitted miner target and the existing
  confirmation/sequencing preconditions are satisfied.
- Preserve existing persisted response profiles whose empty legacy
  `scope_json` plus absent `site_id` means whole organization, but backfill them
  to explicit `{"whole_org":true}` before enabling the new submission rules.
  There are no supported old clients, but a browser tab opened before the
  frontend deployment can still submit the old shape. Add a required scope-
  schema version to every Create/Update/Preview/Start request, including test
  curtailments and profile-derived starts. Deploy server support and the
  frontend that always emits the new version and explicit Whole organization,
  complete the data backfill, then raise the server's minimum accepted version
  before any building/rack/group scope can be created or returned. Reject a
  missing, old, or unknown version rather than interpreting its recognized
  subset. This is a bounded cutover guard, not a retained old-client runtime
  compatibility path.
- Give each response profile an opaque scope revision derived from its canonical
  persisted miner/fan scope. Return it from Get/List and require an exact match
  on Update, every profile-derived execution, and automation-rule Create,
  Update, or enable. The server must load and use the matching stored logical
  scope instead of trusting a client-expanded copy; stale or missing revisions
  fail with a refresh-required error. Disabling or deleting an automation rule
  does not require a current revision. Together with the minimum schema version,
  this prevents a stale tab that ignores unknown topology cases from
  overwriting, running, or binding a narrow profile as explicit Whole
  organization.
- Add `max_items` validation to every repeated scope/identifier field and
  domain validation for raw pre-deduplication totals and normalized per-type/
  aggregate selector limits. Enforce the transport byte cap before decoding and
  reject oversized persisted profiles before execution. For an already-active
  oversized legacy event, do not resolve its logical scope for new admissions;
  mark it degraded and continue confirmation, facility-fan sequencing,
  restoration, ownership release, and terminalization from its durable target
  rows. This active-event drain exception must run before general persisted-
  scope rejection so enforcement cannot strand curtailed miners.
- Remove generic device-set translation, domain/JSON decode/render paths,
  frontend `deviceSetIds` scope state, and the special unsupported-device-set
  preview branch. Keep only deprecated protobuf declarations/field numbers as
  inert schema surface; reject them at the transport boundary if they are ever
  submitted. New explicit topology cases must preview normally.
- Apply the same deprecation to the unused
  `IngestCurtailmentSignalRequest.device_set_ids_override` field; the ingest RPC
  itself is currently Unimplemented, so it must not remain as a hidden generic
  device-set route when that RPC is implemented later.

### 4. Resolve and validate topology scopes in the curtailment backend

- Extend `interfaces.ListCandidatesParams`, the SQL store adapter, and
  `ListCurtailmentCandidatesByOrg` with building/rack/group filters whose
  predicates are unioned with sites and explicit miners.
- Match existing fleet semantics:
  - Building: direct `device.building_id` or membership in a rack assigned to
    the building.
  - Rack: live rack `device_set` membership only.
  - Group: live group `device_set` membership only, including cross-site
    members.
  - Always constrain resources, sets, memberships, and devices to the caller's
    organization and exclude deleted rows.
- Validate every submitted topology ID even when it currently contains zero
  miners. Return NotFound for deleted, wrong-type, or cross-org IDs rather than
  converting them into an empty/insufficient-load preview.
- Resolve authorization coverage from both the selected topology resources and
  their current members. A building contributes its owning site even when
  empty. A rack contributes `device_set_rack.site_id` even when it has no
  building or members; if that field is NULL, the rack requires org-wide
  authorization. When a rack also has a building, validate that the building's
  site agrees with the rack's site and reject inconsistent topology rather than
  silently selecting either value. An unassigned building also requires
  org-wide authorization. A group contributes every current member site, and
  an empty group requires org-wide authorization because its future site
  coverage is unbounded.
- Deduplicate candidates across overlapping selectors before classification.
- For cooldown lookup, use the already-resolved candidate identifiers (or add
  equivalent topology predicates) so a building/rack/group request cannot
  accidentally apply org-wide cooldown exclusions.
- Keep non-FULL_FLEET event targets frozen. Persist the logical selector union
  for closed-loop FULL_FLEET events and extend the existing watcher beyond
  whole-org/sites to buildings, racks, and groups. Do not turn an unflagged
  explicit-miner-only FULL_FLEET request into a watcher.
- Preserve this lifecycle matrix:
  - Non-FULL_FLEET: resolve/freeze once; zero resolved miners fails as
    insufficient load.
  - FULL_FLEET without `force_include_all_paired_miners`: maintain the existing
    eligible-miner watcher for whole-org/site/building/rack/group selectors and
    admit newly eligible members of those selectors. Explicit identifiers are
    snapshot-only; an explicit-only empty selection completes as the existing
    no-op rather than leaving a watcher.
  - FULL_FLEET with `force_include_all_paired_miners`: maintain the durable
    paired-miner policy across the full union, deliberately including explicit
    identifiers. This is new admin-only behavior: an explicitly selected
    unpaired miner is logically reserved and admitted after pairing. Preserve
    unavailable ownership and the restore-before-release rules below.
  A topology-following FULL_FLEET scope, or a flagged explicit-miner policy,
  may create a targetless watcher. A targetless watcher sends no facility-fan
  command until at least one miner is admitted and confirmed through the normal
  sequencing gates; this never permits infrastructure-only or missing-scope
  submissions.
- On every all-paired reconciliation pass, resolve the current union and:
  - admit miners that newly enter the selected topology or become paired,
  - retain paired-like unavailable miners under policy ownership,
  - immediately release miners that leave every selector or become UNPAIRED
    only when no Curtail command could have been dispatched,
  - move dispatched or confirmed miners that leave scope into the existing safe
    restore lifecycle and retain ownership until restore confirmation,
  - persist and retry restore obligations for unreachable/unpaired miners, and
    respect facility-fan sequencing before issuing their restore command,
  - deduplicate miners that match multiple selector categories.
- Apply the same topology scope to cooldown/admission queries and preserve the
  existing event/device exclusivity guarantees while targets are added,
  reopened, restored, or released.
- Treat every active closed-loop FULL_FLEET logical scope as a reservation,
  including devices that currently match but are unpaired, unavailable, or not
  yet represented by `curtailment_target`. A flagged explicit-miner policy
  participates in the same reservation path. Extend
  `CountCurtailmentScopeConflicts`, `ListActiveCurtailedDevicesByOrg`,
  `hierarchicalScopeSiteIDs`, and every direct `scope_jsonb` consumer to use one
  canonical typed-scope resolver rather than whole-org/site-only logic.
- Serialize Start, closed-loop admission/release, and scope-conflict checks with
  an organization-scoped transaction/advisory lock. Before any event claims a
  device, resolve it against older active logical reservations inside that
  critical section; the older reservation wins and the competing Start fails
  or excludes the conflict explicitly. Membership/assignment and pairing
  transitions must trigger immediate reservation reconciliation under the same
  ordering. If a device was already owned before moving into a reserved scope,
  retain its current owner but mark the watcher degraded with an actionable
  conflict and retry after release—never silently treat the policy as fulfilled.

### 5. Make authorization fail closed

- Add a single server-side scope-resolution result that reports:
  - validated selector IDs and types,
  - site IDs contributed by selected building/rack resources,
  - site IDs contributed by current miner members,
  - whether any selected resource/member is unassigned or has unbounded future
    coverage,
  - the resolved current device identifiers,
  - separately resolved facility-fan site IDs and unassigned status.
- Reuse strict current-topology resolution for Preview, Start,
  response-profile Create/Update, profile execution, automation-rule Create/
  Update/enable, and automation execution. Do not use strict live topology for
  response-profile or automation-rule read/delete paths, or for disabling a
  rule. Require `curtailment:manage` at every covered site; require org-wide
  permission when scope coverage is incomplete or includes unassigned
  resources. Picker filtering is usability only, never authorization.
- Treat Preview as a point-in-time estimate, but make Start and every
  closed-loop reconciliation admission atomic with authorization-envelope
  enforcement.
  Selector validation, membership expansion, current site coverage, and target
  claim/insertion must use one transaction/locked snapshot, or every
  materialized device and current site must be revalidated inside the target
  claim transaction. Dispatch starts only after that transaction commits.
  A concurrent topology move must make the claim fail/retry or exclude the
  moved device; it must never admit a device outside the caller's or event's
  authorization envelope.
- On response-profile Create/Update, persist the resolved authorization
  envelope in the existing `scope_json`: the union of selected-resource and
  current-member site coverage for miner selectors, exact facility-fan site
  IDs, and separate org-wide authorization flags for either dimension when
  coverage is incomplete, unassigned, or unbounded. Preserve the current
  `site:read` plus `curtailment:manage` checks for every fan site rather than
  treating miner-scope authorization as permission to control fans.
- Backfill existing API-created profiles only where the original authorization
  envelope is provable from persisted scope: whole-org scope becomes
  org-wide miner authorization, and site-only scope with no facility fans
  retains its exact miner site IDs. Profiles containing explicit miners,
  facility fans without persisted site coverage, or any otherwise ambiguous
  coverage are marked `reauthorization_required` until an operator with current
  authority reviews and resaves them. Automation must reject a profile with a
  missing or unproven envelope; it must never derive authority from a miner's
  or fan's current site at execution time. Surface this state in profile
  Get/List so it can be fixed or deleted.
- Before enforcing profile envelopes or bound-revision checks, inventory every
  enabled automation rule that references a profile marked
  `reauthorization_required` or lacks a proven bound revision, envelope, or
  execution principal/admin-control requirement. Expose the affected rules and
  profiles to operators and reauthorize/rebind or disable them. Gate enforcement on that inventory
  reaching zero, unless owners explicitly approve an announced cutoff for the
  remaining rules. After the gate, keep execution fail closed; do not
  temporarily infer binding state to preserve an unremediated automation.
- Bind every automation rule execution to an explicit user or service-account
  principal; the stored envelope is a maximum boundary, not a durable
  capability. At every trigger, reload that principal's current effective
  permissions, resolve current topology and facility-fan sites, and require
  both the live permissions and stored envelope to authorize the operation. A
  deactivated principal or revoked grant blocks execution. Do not fall back to
  the profile creator or an ambient system principal.
- Record whether an automation binding or event uses admin-only controls,
  including `force_include_all_paired_miners`. Every manual/profile execution,
  automation trigger, and closed-loop reconciliation pass must separately
  revalidate that the current principal is still Admin or SuperAdmin; site/org
  `curtailment:manage` alone is insufficient. Demotion blocks new events and
  admissions. An active event then stops expansion and retains already-owned
  targets through the same fan-aware safe restore-before-release lifecycle.
- Stamp the applicable authorization envelope and authorizing principal onto
  every closed-loop FULL_FLEET event, not only all-paired events, together with
  its required-admin-controls marker. Each reconciliation pass must reload the
  principal's current effective
  permissions and compare current topology/fan coverage with both those
  permissions and the envelope before admitting targets. A rack or building
  moved to another site, a group gaining an out-of-envelope member, a fan
  moving sites, newly unassigned membership, or permission revocation blocks
  new admission with a clear reason.
  Out-of-envelope miners are not admitted, already-owned miners that move
  outside the envelope—or lose current principal authorization—follow the same
  dispatch-aware restore-before-release rule. Restore facility fans first when
  required, surface an actionable authorization-change reason, and terminate or
  remain degraded until safe restoration completes. An org-wide envelope may
  follow topology across sites only while the principal still has the required
  live org-wide permissions.
- Before requiring envelopes on reconciliation, inventory active closed-loop
  events created by the old schema. Backfill an envelope only when the event's
  persisted whole-org/site scope and original authorizing principal prove the
  exact boundary. If either is absent or ambiguous, place the event in explicit
  drain mode: admit no new targets and do not re-resolve its logical scope, but
  continue confirmation, fan sequencing, safe restoration, ownership release,
  and terminalization from durable target rows. Run this migration before the
  reconciler begins rejecting missing envelopes so an upgrade cannot either
  widen authority or strand already-curtailed miners.
- Do not require current topology resolution for response-profile Get/List or
  Delete. Authorize those operations against the profile's persisted
  miner and facility-fan envelope dimensions, including `site:read` on fan
  sites, and hydrate stored typed IDs even when a target was deleted or moved.
  Surface unresolved targets as unavailable/stale so an authorized operator
  can inspect, edit, or delete the profile; Create/Update and every execution
  path still perform strict current-topology and current-fan-site validation,
  so a stale ID must be removed or replaced before resave and can never execute.
- When a profile has a missing or unproven authorization envelope, require
  org-wide `curtailment:manage` for Get/List/Delete and reauthorization, plus
  org-wide `site:read` when it contains facility fans. Do not fall back to
  current topology, miner locations, or fan locations to grant access.
  Site-scoped operators do not see the profile until an org-wide operator with
  the required permissions establishes a proven envelope or deletes it.
- Apply the same recovery boundary to automation-rule management. Get/List,
  Delete, and disable must authorize against the rule's persisted bound miner/
  fan envelope and hydrate the stored profile reference without resolving its
  current topology. A deleted or moved topology target therefore marks the rule
  stale or `rebind_required` but cannot make it unlistable or prevent an
  authorized operator from disabling or deleting it. A legacy rule with a
  missing or unproven bound envelope uses the same org-wide recovery rule as an
  unproven profile. Create, Update, enable, and execution remain strict and
  cannot use this recovery path.

### 6. Cover manual and automated execution

- Ensure `startRequestFromAutomationProfile` carries the new scope arrays and
  exercises the same selector pipeline as direct Start.
- Add the profile's expected scope revision to automation-rule Create, Update,
  and enable requests. In the same database transaction that saves or enables
  the rule, lock and reload the profile, compare its canonical scope revision
  and authorization envelope with the values authorized by the handler, and
  persist that revision, envelope, execution principal, and whether the profile
  requires admin-only controls as the rule's bound profile state. Extend the
  existing store-side facility-fan race check rather than leaving scope or
  privilege validation only in the handler. Any mismatch returns
  FailedPrecondition and saves nothing.
- At every trigger, require the current profile revision to equal the rule's
  bound revision before resolving or starting an event, then enforce the bound
  envelope, the execution principal's current permissions, and any bound Admin/
  SuperAdmin requirement. A profile scope change never silently retargets an
  enabled rule: the revision mismatch makes
  linked bindings report `rebind_required`, blocks event and fan creation, and
  requires an authorized operator to review the current profile and re-enable/
  rebind the rule.
- Keep recovery operations independent of live topology and revision matching.
  Get/List render the stale/rebind state, while disable and Delete authorize
  from the bound envelope and remain available even when the profile target is
  stale or its revision changed. Disabling must not call the strict enable path
  or require a client-supplied profile revision.
- Ensure selecting a saved response profile in New curtailment restores its
  building/rack/group selections and that subsequent manual edits switch the
  dropdown to Custom plan, matching current site/miner behavior.
- Ensure Settings' Test curtailment path saves the logical profile scope first,
  then starts with the same logical scope; it must not fall back to whole fleet
  if a topology mapper omits fields.

### 7. Verification

Frontend unit/component coverage:

- Request-building tests for building-only, rack-only, group-only, and mixed
  site/building/rack/group/miner unions.
- Shared modal tests in both variants for buttons, selection counts, clear/
  select-all, active-site filtering, stale selection preservation, and
  permission-gated pickers.
- Picker tests proving the curtailment/profile mode retains and labels missing
  rack/group IDs until explicit removal while Schedule mode keeps its existing
  pruning behavior.
- Response-profile API tests proving create/update payloads and list/reload
  hydration retain each target type without relying on the in-memory session
  cache.
- Automation API tests proving Create/Update/enable carry the selected profile's
  scope revision, stale revisions surface a refresh-required state, and disable/
  Delete remain available without a current revision.
- Cross-boundary contract tests proving the same canonical selection emits the
  same normalized scopes for Preview, Start, and response-profile
  create/update, and hydrates back without losing a category.
- Display tests for preview, confirmation, response-profile cards, active
  events, and history.

Backend unit/store/integration coverage:

- Proto ↔ domain ↔ JSON round trips for the supported scope cases, plus a
  transport test that deprecated generic device-set input fails closed.
- Candidate resolution for direct-building and rack-in-building devices, rack
  membership, cross-site group membership, mixed overlap deduplication, empty
  valid resources, wrong-type IDs, deleted IDs, and cross-org IDs.
- Cooldown, active-event exclusion, fixed-kW, and full-fleet selection over
  each topology scope.
- All-paired reconciliation for building/rack/group membership additions,
  removals before dispatch, removals after confirmed curtailment, cross-selector
  overlap, unpair/re-pair restore retries, facility-fan sequencing, and
  paired-like unavailable miners.
- Authorization for one-site, multi-site, cross-site group, unassigned,
  narrowed-role, and topology-change-after-profile-save cases, including race
  tests that move topology across sites between resolution and target claim.
- Per-type and aggregate selector-limit tests for direct Preview/Start,
  response-profile create/update, automation execution, and forged oversized
  persisted scopes.
- Raw-limit tests with duplicate-heavy identifiers split across many scope
  entries, per-type and cross-type raw totals, and payloads above the 2 MiB
  transport limit, proving rejection occurs before normalization work.
- Scope-reservation tests for concurrent starts, membership changes between
  reconciliation ticks, explicit-miner reservations, unpair/re-pair gaps,
  deterministic older-event precedence, and a pre-owned miner moving into a
  watched topology with a surfaced/retried conflict.
- FULL_FLEET lifecycle tests proving unflagged explicit-miner-only scope stays
  snapshot-only and completes as a no-op when empty; mixed unflagged scope
  watches only its topology portion; and the flagged admin policy deliberately
  reserves/re-admits explicit identifiers after pairing.
- Response-profile CRUD/list filtering and automation execution with each new
  scope, including a deleted or moved target that remains visible and
  deletable under its persisted authorization envelope but fails execution and
  resave until corrected.
- Automation-rule authorization tests proving Get/List/Delete/disable use the
  persisted bound miner/fan envelope and remain available when a referenced
  topology target is stale, while Create/Update/enable/execution require strict
  current topology. Cover the org-wide recovery rule for legacy bindings with
  missing or unproven envelopes.
- Automation binding race tests that change a profile's scope or envelope
  between handler authorization and the store transaction. Create, Update, and
  enable must fail atomically on an expected-revision/envelope mismatch; a
  successful bind persists the exact revision, envelope, principal, and admin-
  control requirement.
- Automation trigger tests proving a later profile-scope revision puts the rule
  in `rebind_required`, creates no event or fan command, and resumes only after
  an authorized rebind. Disabling and deleting the mismatched rule must not
  require live topology or revision equality.
- Authorization-envelope rollout tests for provable whole-org/site backfills,
  explicit-miner and mixed profiles requiring reauthorization, a miner moving
  sites before reauthorization, org-wide-only Get/List/Delete access while the
  envelope is missing or unproven, site-scoped filtering, and automation
  failing closed before reauthorization.
- Current-permission tests for automation owner deactivation, site-grant and
  org-wide-grant revocation, facility-fan `site:read` revocation, service-account
  principals, Admin/SuperAdmin demotion for admin-only profiles/events, and
  mid-event revocation or demotion that blocks admission and safely restores
  existing ownership for both ordinary and all-paired FULL_FLEET.
- Facility-fan authorization tests for a fan outside the miner scope, fan site
  movement, unassigned/stale fans, `site:read` without `curtailment:manage` and
  vice versa, CRUD/list filtering against persisted fan-site coverage, and
  automation failing when current fan coverage exceeds its envelope.
- Empty/unknown-scope tests proving Preview, Start, profile save/execution, and
  automation never infer whole organization and reject infrastructure-only or
  unknown-scope-plus-fan requests before any event, profile, target ownership,
  or fan command is created. Prove a normal non-policy execution with a valid
  zero-member selector fails without commanding fans, while an all-paired
  FULL_FLEET execution persists a targetless watcher, sends no fan command,
  later admits an assigned/paired miner, and only then follows normal fan
  sequencing.
- Authorization tests for empty buildings using the resource's owning site,
  building-less racks using `device_set_rack.site_id`, rack/building site
  mismatches failing closed, NULL-site racks and unassigned buildings requiring
  org-wide access, empty groups requiring org-wide access, and cross-site groups
  requiring every current member site while remaining bounded by their
  persisted envelope.
- Rollout tests proving legacy persisted whole-org profiles backfill to the
  explicit representation and the updated frontend emits explicit whole-org
  scope before the server begins rejecting omitted submissions. Cover a stale
  browser missing the minimum scope-schema version, an unknown topology case,
  profile update/execution with a missing or stale scope revision, and a valid
  refreshed profile-derived execution that uses the server-stored scope.
- Authorization-envelope rollout tests for active closed-loop events: provable
  whole-org/site boundaries retain reconciliation, while an event with an
  ambiguous scope or principal enters no-admission drain mode and completes
  safe restoration from durable targets.
- Automation rollout tests inventory enabled rules bound to profiles requiring
  reauthorization or missing a proven bound revision, envelope, principal, or
  admin-control requirement;
  prevent enforcement before rebind/disable remediation or an explicit cutoff;
  preserve authorized read/disable/delete recovery; and fail closed after
  cutover without silently widening scope.
- Selector-limit rollout tests covering inventory of legacy API-created
  oversized profiles/events, readable and deletable remediation state,
  shrink-only updates, automation cutoff, and an active oversized event that
  blocks new admissions but completes confirmation, fan sequencing, safe
  restoration, ownership release, and terminalization after upgrade.
- Regression coverage for whole-org, multi-site, explicit-miner, and
  facility-fan behavior; assert no generic device-set scope state remains in
  frontend/domain/storage models.

Run the smallest relevant suites first, then the canonical checks:

```sh
bin/just gen
(cd server && ../bin/go test ./internal/domain/curtailment/... ./internal/handlers/curtailment ./internal/domain/stores/sqlstores)
(cd client && ../bin/npx vitest run src/protoFleet/features/energy/CurtailmentStartModal.test.tsx src/protoFleet/features/energy/curtailmentRequestBuilders.test.ts src/protoFleet/features/energy/useCurtailmentPlanPreview.test.ts src/protoFleet/api/useCurtailmentResponseProfiles.test.tsx src/protoFleet/features/settings/components/Curtailment/CurtailmentSettingsPage.test.tsx)
bin/just lint
```

Add ProtoFleet Playwright coverage for both entry points: create a topology-
scoped response profile, reload/edit it, and run a new curtailment scoped to a
building/rack/group. Do not refresh visual snapshot baselines without explicit
developer approval.

## Acceptance

- New curtailment and Create/Edit response profile show Sites, Buildings,
  Racks, Groups, Miners, and Infrastructure in the same order and with
  consistent selection behavior.
- Operators can combine any narrower miner target categories; the backend
  unions and deduplicates current membership for Preview and Start.
- Building/rack/group IDs survive profile save, page reload, edit, manual run,
  and automation execution without becoming explicit-miner snapshots or whole
  fleet.
- Preview and Start resolve the same miners for the same scope, subject only to
  normal time-varying telemetry/eligibility.
- Non-FULL_FLEET and unflagged explicit-miner events freeze concrete miner
  targets; closed-loop FULL_FLEET topology watchers follow their logical scope
  within their persisted authorization boundary.
- FULL_FLEET events with `Target all paired miners` continuously re-resolve the
  selected site/building/rack/group/miner union: newly paired or newly assigned
  miners are admitted. Unpaired or no-longer-in-scope miners are released
  immediately only when undispatched; any miner that may already be curtailed
  remains owned until it completes the safe restore lifecycle.
- Start and each reconciliation pass atomically bind topology membership,
  authorization-envelope validation, and target claim before dispatch, so a
  concurrent cross-site move cannot admit an unauthorized miner.
- Per-type and aggregate selector limits bound direct and persisted scope
  resolution and are enforced on raw input before deduplication and server-side
  after normalization, with a transport byte limit. Existing oversized records
  are inventoried and remediated before enforcement; profiles remain readable/
  deletable, while active events stop new admissions and drain safely from
  durable targets.
- Deleted, wrong-type, cross-org, or unauthorized topology targets fail closed
  with actionable errors and never broaden to whole-org scope. Unassigned
  in-org targets are admitted only when the caller has org-wide
  `curtailment:manage`; otherwise they are treated as uncovered and rejected.
- A saved profile with a later-deleted or moved target remains visible and
  deletable to operators authorized for its persisted envelope, renders the
  unresolved target as stale, and fails strict resave or execution until fixed.
- Existing profiles receive a provable whole-org/site authorization envelope
  or are marked as requiring operator reauthorization. Automation cannot run a
  profile with a missing or unproven envelope, and only an operator with the
  required org-wide permissions can view, delete, or reauthorize it.
- Scope-schema version enforcement rejects stale browser submissions before
  topology scopes are exposed, and profile updates/executions require a matching
  opaque scope revision and use the server-stored canonical scope. An unknown
  topology case can never round-trip as intentional Whole organization.
- Automation rules bind atomically to an exact profile scope revision, miner/
  fan envelope, execution principal, and required admin-control state. A
  profile-scope change blocks triggers as `rebind_required` until an authorized
  rebind; it never silently retargets an enabled rule.
- Stale or revision-mismatched automation rules remain visible, disableable, and
  deletable under their bound envelope without current-topology resolution.
  Create, Update, enable, and execution still fail closed on stale topology,
  revision, envelope, or current permissions.
- Response-profile envelopes independently cover miner and facility-fan sites;
  profile CRUD and execution preserve both `site:read` and
  `curtailment:manage` requirements for every selected fan site.
- Every closed-loop FULL_FLEET event persists its envelope and authorizing
  principal. Automation triggers and reconciliation require that principal's
  current effective permissions; revocation blocks admission and safely
  restores owned targets instead of leaving the envelope as a perpetual grant.
- Admin-only profiles, rules, and events persist that privilege requirement and
  recheck the principal's current Admin/SuperAdmin role at execution and every
  reconciliation pass. Demotion blocks new work and safely restores active
  ownership even when `curtailment:manage` remains granted.
- Legacy active closed-loop events receive a provable envelope and principal or
  enter no-admission drain mode before envelope enforcement begins. Enabled
  automations referencing ambiguous profiles or lacking proven binding state
  are inventoried and rebound, disabled, or explicitly cut off before
  enforcement, then remain fail closed while recovery operations stay
  available.
- Whole organization is explicit for every new submission. Empty, unknown,
  unsupported, and infrastructure-only miner scope never widens to whole-org
  targeting or reaches facility-fan dispatch; legacy persisted whole-org
  profiles are backfilled to the explicit representation during rollout.
- Valid zero-member building/rack/group selectors fail non-FULL_FLEET Start,
  but FULL_FLEET topology watchers may persist targetless and admit future
  eligible members without commanding fans before a miner is confirmed.
- Unflagged explicit-miner FULL_FLEET remains snapshot-only. The admin-only
  all-paired flag deliberately makes explicit identifiers durable members of
  the union, including admission after an explicitly selected miner pairs.
- Active FULL_FLEET logical scopes reserve matching devices between
  reconciliation ticks; competing events cannot silently take newly eligible
  or newly assigned members.
- Whole-org/site all-paired behavior, explicit-miner targeting, facility-fan
  sequencing, and active/history displays keep working. Deprecated generic
  device-set wire input fails closed and is never persisted, executed as, or
  widened to a new topology scope.
- Proto source and generated output are committed together. Response-profile
  topology and envelope persistence reuse the existing scope JSON unless
  implementation proves that insufficient; the durable automation binding
  fields explicitly require a new immutable up/down schema migration committed
  with their sqlc source and generated output.
