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
- A normal Start resolves current membership once, then freezes concrete miner
  targets in `curtailment_target`, matching existing open-loop explicit-miner
  behavior.
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
`max_items` validation where the wire shape permits and enforce the normalized
per-type/aggregate limits again in the domain for direct requests, response
profiles, automation execution, and persisted all-paired reconciliation. The
frontend mirrors these limits for usability; the server remains authoritative.

Extend the Go domain `Scope` and JSON codec with `building_ids`, `rack_ids`, and
`group_ids`, and remove generic `DeviceSetIDs`/`device_set_ids` domain and JSON
handling. Preserve existing decoding and proto rendering for whole-org, site,
device-list, and mixed records. New topology-only or composite records can
continue to persist with the existing `mixed` scope type plus the richer JSON
object, avoiding a migration and new database scope-type values. For response
profiles, the same JSON object should also carry an authorization envelope
(`authorized_site_ids` plus an `org_wide_authorized` flag) captured when the
profile is created or updated.
Persist the same envelope on an event when all-paired topology reconciliation
is enabled; normal frozen-target events continue to rely on authorization at
Preview/Start plus their concrete target rows.

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
- Add `max_items` validation to every repeated scope/identifier field and
  domain validation for the normalized per-type and aggregate selector limits.
  Reject oversized persisted profile/event scopes before resolving them.
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
- Deduplicate candidates across overlapping selectors before classification.
- For cooldown lookup, use the already-resolved candidate identifiers (or add
  equivalent topology predicates) so a building/rack/group request cannot
  accidentally apply org-wide cooldown exclusions.
- Keep normal event Start behavior frozen. When
  `force_include_all_paired_miners=true`, persist the logical selector union on
  the event and extend the reconciler's candidate-parameter translation beyond
  whole-org/sites to buildings, racks, groups, and explicit identifiers.
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

### 5. Make authorization fail closed

- Add a single server-side scope-resolution result that reports:
  - validated selector IDs and types,
  - the covered site IDs,
  - whether any selected resource/member is unassigned,
  - the resolved current device identifiers.
- Reuse strict current-topology resolution for Preview, Start,
  response-profile Create/Update, profile execution, and automation-rule
  profile checks. Require `curtailment:manage` at every covered site; require
  org-wide permission when scope coverage is incomplete or includes unassigned
  resources. Picker filtering is usability only, never authorization.
- Treat Preview as a point-in-time estimate, but make Start and every all-paired
  reconciliation admission atomic with authorization-envelope enforcement.
  Selector validation, membership expansion, current site coverage, and target
  claim/insertion must use one transaction/locked snapshot, or every
  materialized device and current site must be revalidated inside the target
  claim transaction. Dispatch starts only after that transaction commits.
  A concurrent topology move must make the claim fail/retry or exclude the
  moved device; it must never admit a device outside the caller's or event's
  authorization envelope.
- On response-profile Create/Update, persist the resolved authorization
  envelope in the existing `scope_json`: exact covered site IDs for narrowed
  authorization, or `org_wide_authorized=true` when org-wide permission was
  required and granted. Legacy profiles without topology selectors do not need
  a backfill.
- Before automation execution, resolve current topology coverage again and
  require it to remain inside the stored authorization envelope. A rack or
  building moved to another site, a group gaining an out-of-envelope member,
  or newly unassigned membership must fail with a clear FailedPrecondition
  instead of silently broadening the operation. Org-wide-authorized profiles
  may follow current membership across sites.
- When Start enables all-paired topology reconciliation, stamp the applicable
  authorization envelope onto the event. Each reconciliation pass must compare
  current topology coverage with that envelope before admitting targets.
  Out-of-envelope miners are not admitted, already-owned miners that move
  outside the envelope follow the same dispatch-aware restore-before-release
  rule, and the event surfaces an actionable authorization-change reason. An
  org-wide-authorized event may continue following the selected topology across
  sites.
- Do not require current topology resolution for response-profile Get/List or
  Delete. Authorize those operations against the profile's persisted
  authorization envelope and hydrate its stored typed IDs even when a target
  was deleted or moved. Surface unresolved targets as unavailable/stale so an
  authorized operator can inspect, edit, or delete the profile; Create/Update
  and every execution path still perform strict current-topology validation,
  so a stale ID must be removed or replaced before resave and can never execute.

### 6. Cover manual and automated execution

- Ensure `startRequestFromAutomationProfile` carries the new scope arrays and
  exercises the same selector pipeline as direct Start.
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
- Response-profile API tests proving create/update payloads and list/reload
  hydration retain each target type without relying on the in-memory session
  cache.
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
- Response-profile CRUD/list filtering and automation execution with each new
  scope, including a deleted or moved target that remains visible and
  deletable under its persisted authorization envelope but fails execution and
  resave until corrected.
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
- Normal started events freeze concrete miner targets; later topology changes
  affect future profile executions, not the in-flight event.
- FULL_FLEET events with `Target all paired miners` continuously re-resolve the
  selected site/building/rack/group/miner union: newly paired or newly assigned
  miners are admitted. Unpaired or no-longer-in-scope miners are released
  immediately only when undispatched; any miner that may already be curtailed
  remains owned until it completes the safe restore lifecycle.
- Start and each reconciliation pass atomically bind topology membership,
  authorization-envelope validation, and target claim before dispatch, so a
  concurrent cross-site move cannot admit an unauthorized miner.
- Per-type and aggregate selector limits bound direct and persisted scope
  resolution and are enforced server-side after normalization.
- Deleted, wrong-type, cross-org, or unauthorized topology targets fail closed
  with actionable errors and never broaden to whole-org scope. Unassigned
  in-org targets are admitted only when the caller has org-wide
  `curtailment:manage`; otherwise they are treated as uncovered and rejected.
- A saved profile with a later-deleted or moved target remains visible and
  deletable to operators authorized for its persisted envelope, renders the
  unresolved target as stale, and fails strict resave or execution until fixed.
- Whole-org/site all-paired behavior, explicit-miner targeting, facility-fan
  sequencing, and active/history displays keep working. Deprecated generic
  device-set wire input fails closed and is never persisted, executed as, or
  widened to a new topology scope.
- Proto source and generated output are committed together; no schema migration
  is added unless implementation proves JSON persistence is insufficient.
