---
title: "Curtailment PR sequence plan"
date: 2026-06-06
status: draft
type: plan
---

# Curtailment PR sequence plan

## Summary

This plan tracks the focused curtailment follow-up PRs after MQTT ingest landed
in [#336](https://github.com/block/proto-fleet/pull/336).

Last checked against GitHub issues, PRs, recent merges, and local `main`
(`debca723`) on 2026-06-10.

Current state:

- [#336](https://github.com/block/proto-fleet/pull/336) merged on
  2026-06-06: MQTT continuous-state ingest is on `main`, with one worker per
  enabled source, durable source state, fail-safe watchdog behavior, in-process
  curtailment dispatch, and runtime rejection of `site` scope until first-class
  site support lands. The in-process MQTT dispatch path is now legacy
  transitional behavior. The final product should remove source-owned dispatch
  entirely and make automated curtailment flow through source trigger +
  response profile + automation.
- [#408](https://github.com/block/proto-fleet/pull/408) merged on
  2026-06-08 and closed
  [#392](https://github.com/block/proto-fleet/issues/392): public
  `PreviewCurtailmentPlan`, `StartCurtailment`, and `StopCurtailment` now
  require org-scoped `curtailment:manage`.
- [#407](https://github.com/block/proto-fleet/pull/407) merged on
  2026-06-08 and closed
  [#394](https://github.com/block/proto-fleet/issues/394): `FULL_FLEET`
  now distinguishes genuinely empty scopes from non-empty all-skipped scopes,
  and MQTT all-skipped retries are throttled with durable `pending_retry_at`.
- [#406](https://github.com/block/proto-fleet/pull/406) merged on
  2026-06-09 and closed
  [#403](https://github.com/block/proto-fleet/issues/403): batched curtail
  dispatch, grouped activity, durable curtail/restore phase summaries,
  event-wide rollups, bounded detail snapshots, and paginated
  `GetCurtailmentEvent` are now on `main`.
- [#411](https://github.com/block/proto-fleet/pull/411) merged on
  2026-06-09 and closed
  [#380](https://github.com/block/proto-fleet/issues/380): manual
  `FULL_FLEET` UI support is now on `main` for the currently supported
  whole-org and explicit-device scopes.
- [#415](https://github.com/block/proto-fleet/pull/415) merged on
  2026-06-10: the Docker Compose-backed MQTT curtailment simulator, root/server
  `just mqtt-sim-*` recipes, and simulator README are now on `main`. This has
  no migration or product-issue closure, but it is useful validation tooling for
  the MQTT source CRUD and connection-test work.
- [#409](https://github.com/block/proto-fleet/pull/409) is open draft and
  currently does too much for the desired product model. It should be split so
  #409 itself is only MQTT source CRUD/runtime status/reload. Response behavior
  must move to response profiles and automation rules instead of being stored on
  the source API contract.
- [#413](https://github.com/block/proto-fleet/pull/413) is open draft and
  stacked on #409: frontend Settings > Curtailment source listing, create
  dialog, and enablement toggles wired to the MQTT source settings API. Treat it
  as the alpha preview of the desired end-to-end operator flow, but split its
  current behavior into MQTT source creation, site-targeted response profile
  creation, and automation creation. It must rebase onto the source-only API and
  stop sending hardcoded response behavior such as whole-org `FULL_FLEET`.
- [#414](https://github.com/block/proto-fleet/pull/414) is open draft and
  implements [#404](https://github.com/block/proto-fleet/issues/404): first
  class `site` scope in the curtailment API/domain/store paths and minimal
  client display handling. Site scope remains useful for response profiles and
  manual curtailment; it should not make a source row itself an automatic site
  dispatch target.
- [#387](https://github.com/block/proto-fleet/issues/387),
  [#401](https://github.com/block/proto-fleet/issues/401), and
  [#384](https://github.com/block/proto-fleet/issues/384) remain open follow-up
  issues with no active implementation PR found in this check.

Near-term product outcomes:

1. `main` now has MQTT ingest, `FULL_FLEET`, all-skipped safety, public
   `curtailment:manage` gates, grouped event detail, manual `FULL_FLEET` UI,
   and a local MQTT simulator.
2. Operator-configurable MQTT sources should be enabled by default, but an
   enabled source should only connect, subscribe, decode signals, and expose
   source health/state. It must not imply a whole-fleet or any other curtailment
   response.
3. The user-visible outcome of the old #409 behavior should be rebuilt through
   explicit automation: an operator creates an MQTT source, creates a response
   profile, then creates an automation rule that binds that source trigger to
   that profile.
4. The initial automated integration is MaestroOS over MQTT only. Energy price,
   ERCOT ERS, API webhooks, and time-of-use triggers are product/design
   examples, not near-term backend scope for this sequence.
5. The #413 alpha end effect should remain achievable after the split, but via a
   source + site-targeted response profile + automation rule. Users should
   target a site in the response profile and should not have to select
   individual miners for the MaestroOS integration.
6. Site-scoped curtailment is now active work in #414. Site scope belongs in
   manual start requests and future response profiles/rules; source CRUD should
   not use site scope as direct response behavior.
7. The active-event frontend still needs #387: local client code continues to
   poll `GetActiveCurtailment`, while the backend already exposes
   `ListActiveCurtailments` and paginated `GetCurtailmentEvent`.

Codebase cross-check on `main`:

- `server/migrations/` currently reaches `000079`; #409's `000080` and
  `000081` migrations are not merged.
- `proto/curtailment/v1/curtailment.proto` on `main` has
  `ListActiveCurtailments` and `GetCurtailmentEvent`, but does not yet have the
  MQTT source settings RPCs from #409 or the `ScopeSite` API from #414.
- `server/internal/domain/curtailment/mqttingest/scopeForSource` still rejects
  `site` scope on `main`.
- `client/src/protoFleet/features/settings/components/` has no Curtailment
  settings page on `main`; that is still #413.
- `client/src/protoFleet/api/activeCurtailmentData.ts` still calls
  `getActiveCurtailment`, so #387 remains real frontend migration work.
- `server/devtools/mqttsim/` and root/server `just mqtt-sim-*` recipes are now
  present from #415.

## Guiding principles

- Separate source state from response behavior. Source enablement means
  connection/subscription and signal observation; response behavior lives in
  response profiles and automation rules.
- Do not hardcode an enabled MQTT source to whole-org `FULL_FLEET` or any other
  default response.
- In the final product, source ingestion must not call `StartCurtailment`,
  `StopCurtailment`, or `Recurtail` directly. Source ingestion should publish
  durable signal state/edges; automation execution should own curtailment
  service calls.
- Preserve best-effort curtailment only when an enabled automation binds an
  MQTT source trigger to a response profile.
- Do not report an automation-triggered OFF as satisfied when no actionable
  miners were curtailed.
- Keep high-blast-radius manual/API starts behind explicit authorization.
- Keep MQTT source settings simple: source CRUD, redacted status, enabled
  default, runtime reload, and connection testing. Do not expose response scope,
  curtailment mode, target kW, priority, or duration fields on the source API.
- Treat MQTT source deletion as permission-gated, not Admin-only. A user with
  the required org-scoped `curtailment:manage` permission should be able to
  delete a disabled MQTT source; deleting an enabled source should remain
  blocked so the runtime is disabled intentionally first.
- Keep each PR focused; avoid combining safety semantics, UI work, site scope,
  and batching unless there is a direct code dependency.
- Coordinate migration numbering explicitly across parallel PRs.

## Issue map

| Issue | State | Current PR | Role in sequence |
| --- | --- | --- | --- |
| [#392](https://github.com/block/proto-fleet/issues/392) | Closed | [#408](https://github.com/block/proto-fleet/pull/408), merged | Public preview/start/stop authorization boundary. |
| [#394](https://github.com/block/proto-fleet/issues/394) | Closed | [#407](https://github.com/block/proto-fleet/pull/407), merged | FULL_FLEET safety fix and MQTT insufficient-load retry throttling. |
| [#403](https://github.com/block/proto-fleet/issues/403) | Closed | [#406](https://github.com/block/proto-fleet/pull/406), merged | Large-fleet dispatch, grouped activity, full-cycle event detail backend, and paginated target detail. |
| [#405](https://github.com/block/proto-fleet/issues/405) | Open | [#409](https://github.com/block/proto-fleet/pull/409), draft to be narrowed | MQTT source CRUD/runtime status only; response behavior moves to response profile and automation follow-ups. |
| [#380](https://github.com/block/proto-fleet/issues/380) | Closed | [#411](https://github.com/block/proto-fleet/pull/411), merged | Manual FULL_FLEET UI exposure for whole-org and explicit-device scopes. |
| [#384](https://github.com/block/proto-fleet/issues/384) | Open | [#413](https://github.com/block/proto-fleet/pull/413), draft alpha UI reference | Response profile CRUD, automation CRUD, and automation executor remain missing; #413 should be split across source/profile/automation work and should not close #384 by itself. |
| [#404](https://github.com/block/proto-fleet/issues/404) | Open | [#414](https://github.com/block/proto-fleet/pull/414), draft | First-class site scope for curtailment and MaestroOS-style site integrations. |
| [#387](https://github.com/block/proto-fleet/issues/387) | Open | Not started | Frontend migration to keyed event detail after #406's backend detail path. |
| [#401](https://github.com/block/proto-fleet/issues/401) | Open | Not started | Frontend permission-scope distinction before UI gates rely on org-scoped manage/read checks. |

## Current feature gaps

The desired product model is not fully present yet.

- There is no persisted response-profile API or table on `main`. The only
  current frontend "profile" is the manual curtailment modal's local
  `responseProfileId: "customPlan"` form state.
- There is no MQTT-triggered automation API or rule table on `main`.
- The existing `ScheduleService` is not the needed automation system. It
  manages scheduled miner operations (`SET_POWER_TARGET`, `REBOOT`, and
  `SLEEP`), not event-triggered curtailment responses.
- There is no automation executor that observes MQTT source signal transitions,
  resolves an enabled rule, loads a response profile, and calls
  `StartCurtailment` / `StopCurtailment`.
- MQTT ingest still contains a legacy direct-dispatch path that can call the
  curtailment service from source config alone. That path should be removed
  once source state and automation execution are in place.
- There is no persisted source-origin site field separate from response scope.
  The existing `scope_site_id` on the MQTT source table is response targeting
  state and should not be reused for the Sources table's site column.
- The MQTT source table created by #336 already contains response-behavior
  columns (`curtail_mode`, `contracted_curtailment_kw`, `scope_*`,
  `min_curtailed_duration_sec`) and defaults `enabled = TRUE` plus
  `curtail_mode = 'FULL_FLEET'`. Source CRUD must not expose or rely on those
  fields as the product contract.

## #413 frontend cross-check

[#413](https://github.com/block/proto-fleet/pull/413) is the working alpha
preview of the desired Settings > Curtailment operator outcome. Keep that end
effect, but do not keep the current source-row-as-response implementation. The
final flow should be:

1. Create and enable an MQTT source for a MaestroOS site.
2. Create a response profile that targets that site.
3. Create and enable an automation rule that binds the MQTT source trigger to
   the site-targeted response profile.

#413 is also the tactical frontend consumer of the #409 source API. Its
implementation reinforces the split but exposes backend contract details that
should be cleaned up before the backend PRs merge.

Frontend assumptions to account for:

- The add/edit source modal only collects source fields: name, two broker hosts,
  port, topic, username, and password. It does not collect response profile or
  automation behavior.
- The hook currently fills missing backend behavior fields with hardcoded
  defaults: `curtailMode = "FULL_FLEET"`, whole-org scope,
  `payloadFormat = "target_timestamp"`, `stalenessThresholdSec = 240`,
  `minCurtailedDurationSec = 600`, and `enabled = true`.
- Those hardcoded response defaults are an alpha shortcut only. After the split,
  the same user-visible outcome should come from the automation loading the
  selected site-targeted response profile.
- The Sources table polls list status every 10 seconds and expects redacted
  source status: enabled, runtime state, stale/no-signal status, last signal
  value, last signal timestamp, and `has_password`.
- The edit flow preserves the saved password when the password field is left
  blank. If broker host, port, transport, or username changes, the backend
  should continue requiring a replacement password unless it intentionally
  supports reusing credentials across changed broker bindings.
- The UI already has a disabled "Test connection" button, so the connection-test
  RPC should support both unsaved create-form values and saved-source edit
  values.
- The UI offers Delete from the edit modal. Source CRUD should allow users with
  org-scoped `curtailment:manage` to delete disabled MQTT sources without an
  Admin-role requirement. Deleting enabled sources remains blocked; #413 must
  handle the `FailedPrecondition` path by asking the user to disable the source
  first. Later automation work should also block deletion when automations
  reference the source.
- #413 defines placeholder `ResponseProfile` and `AutomationRule` TypeScript
  types only. There is no frontend API integration for profile/rule CRUD or an
  executor yet.

Backend API updates implied by #413:

- Remove response-behavior fields from source create/update requests and source
  list/get responses. #413 should stop sending `curtailMode`, response scope,
  and `minCurtailedDurationSec`; those belong to response profile CRUD.
- Default source creation to enabled on the backend so the frontend does not
  need to send `enabled: true` through a proto3 bool that cannot distinguish
  omitted from false.
- Use dedicated enable/disable RPCs, `optional bool`, or field-mask-style
  updates for mutable booleans. Do not let omitted proto3 booleans accidentally
  disable sources or automation rules.
- Keep `payload_format` source-owned, but default it server-side while the UI
  has no payload-format selector.
- Add a source-owned site/origin field for display and validation. Do not reuse
  `scope_site_id` for this; that existing column means response targeting and
  is intentionally moving out of the source API contract.
- Define status fields explicitly enough for the UI. If the UI should display
  raw MQTT payload values such as `0`/`100`, add a separate raw signal field;
  keep canonical target/state (`ON`/`OFF`) separate for automation decisions.
- Shape the connection-test response as per-broker results with success/error,
  and avoid persisting state or dispatching automation from that RPC.

## MaestroOS MQTT contract

The first and only integration in this sequence is MaestroOS over MQTT. Broader
trigger types can be designed later, but they should not expand the current PR
set.

Contract captured from `Proto Maestro API.docx.md`:

- Each site publishes to `maestro/target` at approximately 30-second intervals.
- MQTT protocol version is 3.1.1. The site power target topic uses QoS 1.
- Payload format is JSON with `target` and `timestamp`, where `target` is always
  `100` or `0`, and `timestamp` is Unix epoch seconds.
- `target = 100` means the site can operate at full power (`ON`).
- `target = 0` means the site should curtail (`OFF`).
- If the last received site target is older than 4 minutes, the source should
  enter fail-safe curtailment behavior by producing the same automation-facing
  state as an `OFF` signal.
- The site is expected to curtail below its contracted curtailment power level
  within 3 minutes of the first `OFF` signal. The contracted value is supplied
  separately and belongs in the response profile, not the source.
- Normal `OFF` periods are expected to last at least 10 minutes, but `ON` can
  move to `OFF` immediately when grid conditions require it.
- Each site has two brokers for redundancy. If broker signals differ, the lower
  IP address takes precedence. Model this as primary/secondary broker ordering
  rather than "latest message wins."

Implications:

- MQTT source CRUD should default topic to `maestro/target`, port to `1883`,
  payload format to the Maestro target/timestamp decoder, and stale threshold to
  240 seconds when the UI does not expose those fields.
- The source row should be associated with exactly one site for the MaestroOS
  integration. That site association is source metadata and helps automation
  validation; it is not the curtailment response scope.
- The automation rule should reject or require explicit override when the source
  site and response profile site do not match. Prefer rejecting mismatches for
  the first MaestroOS implementation.
- Open product/implementation decision: confirm whether "contracted
  curtailment power level" means the amount of load to shed or a maximum
  residual site power cap. The current `FIXED_KW` mode selects miners until a
  kW shed target is reached; if the contract is a site power cap, the response
  profile needs a site-cap mode or a safe translation from current site load.

## #414 site-scope fit

[#414](https://github.com/block/proto-fleet/pull/414) is best treated as an
independent foundation PR for first-class site-scoped curtailment. It can merge
before #409, response profiles, and automation if it stays focused on manual/API
site scope.

What fits independently:

- Add `ScopeSite` to `PreviewCurtailmentPlan`, `StartCurtailment`, and
  `CurtailmentEvent`.
- Validate that the site belongs to the caller's organization before candidate
  selection.
- Filter selector candidates through `device.site_id`.
- Persist/audit site-scoped events as `scope_type = site` with `site_id`
  metadata.
- Add minimal frontend mapping so site-scoped events display as `Site <id>` and
  do not silently map back to whole-fleet edit state.

Required changes before merging independently:

- Rebase #414 onto current `main`; its current base predates #415.
- Remove or defer the MQTT runtime change that makes
  `server/internal/domain/curtailment/mqttingest/scopeForSource` accept
  `scope_type = site`.
- Revert the associated MQTT tests from "site scope allowed at subscriber
  startup" back to rejection, or replace them with explicit documentation that
  MQTT source site response behavior is handled by future automation/profile
  work.
- Update the PR body and #404 wording so it no longer claims that MQTT
  `scope_type = site` sources can run directly from source rows.

Reasoning:

- Site scope is response targeting, not source connectivity. It belongs in
  manual start requests and future response profiles.
- Allowing MQTT `scope_type = site` in the current direct-dispatch driver would
  extend the old source-row-as-response-model just as #409 is being split away
  from it.
- Once response profile CRUD exists, site scope should be one profile targeting
  option. Once automation execution exists, an MQTT source can trigger that
  site-scoped profile through an explicit rule.

Non-blocking follow-ups:

- #387 becomes more valuable after #414 because site-scoped events make
  multiple active scoped events more visible.
- #401 remains relevant for UI gating; #414 should keep public site-scoped
  preview/start/stop behind the existing org-scoped `curtailment:manage` gate
  until site-scoped RBAC is designed.

## Design-driven backend contract

The latest settings designs show three first-class resources on the
Curtailment settings page: response profiles, sources, and automations. The
backend should model those as separate APIs instead of making source rows carry
response behavior.

Response profile API needs:

- List cards with display-ready summaries such as `50% reduction`,
  `100% reduction`, `2,000 kW target`, `All sites`, and `Austin, TX`.
- Create/edit fields for name, curtailment mode, target value, curtail batch
  size, curtail batch interval, restore batch size, restore batch interval, and
  apply-to selectors.
- The first MaestroOS-backed profile flow must support a site target so users
  do not have to select individual miners. Store the site target as profile
  response scope and resolve the site's current eligible miners at
  preview/execution time.
- A profile preview/test API for the "Test curtailment" action. This should
  reuse the curtailment preview path where possible and must not persist an
  event or issue miner commands.

Response profile gaps:

- `main` only supports `FIXED_KW` and `FULL_FLEET`. The design's "percentage
  reduction" mode is not currently implemented as a selector mode. `100%`
  reduction can map to `FULL_FLEET`, and fixed kW can map to `FIXED_KW`, but
  partial percentage reduction needs a new backend mode or a product decision
  to postpone that option.
- #414 covers site scope, but buildings/racks/groups still need backend
  resolvers before they can be reliable saved profile scopes. Defer those
  selectors from the first response profile CRUD PR unless the resolver work is
  explicitly added.
- The manual curtailment UI currently has a local `customPlan` form concept,
  not reusable saved profiles.

Source API needs:

- Treat source `trigger_type` as part of the source model, but only accept MQTT
  in this sequence.
- Keep create/edit focused on configuration name, trigger type, broker hosts,
  port, topic, username, password, enabled flag, and source-owned payload
  format/defaults.
- Include source origin site metadata for the Sources table's `Site` column and
  automation validation. This is source metadata, not the curtailment response
  target.
- Return connection/runtime status for the table: enabled, connected/error,
  stale/no-signal, last raw signal value, last signal timestamp, and redacted
  credential state.
- Keep the MQTT connection-test RPC side-effect free. It should not update
  source state, start automation, or start curtailment.

Automation API needs:

- Store rule name, enabled flag, display order/priority, MQTT trigger config,
  and response profile binding.
- Support list summaries for the table: rule name, MQTT source/condition text,
  response profile name, enabled state, and order.
- Only MQTT source triggers are in scope for this sequence. Energy price, ERCOT
  ERS, API webhook, and time-of-use triggers should be rejected or hidden until
  their data sources and executors are intentionally designed.
- Store the MQTT trigger as a source binding: `OFF` or stale starts/maintains
  curtailment through the selected profile; `ON` restores/stops the matching
  automation-owned event.
- Keep response behavior in the bound response profile. An enabled automation
  is what turns an enabled source signal into curtailment.

## #409 refactor plan

Refactor [#409](https://github.com/block/proto-fleet/pull/409) into four
focused PRs. The main rule is that a source can be enabled without selecting any
curtailment response.

### 1. MQTT source CRUD and runtime source state

Scope:

- Provide backend source CRUD/read APIs for broker connection details only:
  source name, trigger type (`MQTT` only for now), required MaestroOS origin
  site, topic, broker hosts, port, transport, username, write-only password,
  payload format, staleness threshold, enabled flag, and redacted
  runtime/source status.
- Add source-owned site metadata with a name that cannot be confused with
  response scope, such as `origin_site_id` or `site_id` on the source config.
  Do not expose the existing response-oriented `scope_site_id` as the source's
  site field.
- Keep source creation enabled by default.
- Allow deleting disabled sources with org-scoped `curtailment:manage`; do not
  require the Admin role for delete. Continue rejecting delete for enabled
  sources so users must explicitly disable the source before removal.
- Use dedicated enable/disable RPCs, `optional bool`, or field-mask-style update
  semantics so partial updates cannot accidentally disable a source.
- Keep runtime reconciliation/hot reload so create/update/enable/disable/delete
  changes affect MQTT workers without restarting `fleetd`.
- Keep password encryption and redaction.
- Keep `service_user_id` internal/server-owned if the existing table still
  requires it, but do not expose it in the API.
- Introduce a source-signal boundary that records decoded MQTT state and can be
  consumed by automation later. With no automation binding, OFF/ON/watchdog
  signals should update source state only.
- Implement the MaestroOS decoder defaults: topic `maestro/target`, target
  values `0`/`100`, Unix-second timestamp, 240-second stale threshold, and
  primary/lower-IP broker precedence when the two brokers disagree.

Out of scope:

- No response scope on source CRUD.
- No `curtail_mode`, `contracted_curtailment_kw`, response priority, restore
  options, or `min_curtailed_duration_sec` on source CRUD.
- No hardcoded whole-org `FULL_FLEET` dispatch.
- No source-owned calls to `StartCurtailment`, `StopCurtailment`, or
  `Recurtail`.
- No admin ingest backfill just to let source CRUD users curtail.
- No API/webhook source type in this sequence.

Implementation notes:

- Remove or replace `000080_mqtt_source_disabled_default`; the new source
  default should stay enabled. If the actor-ID rewrite remains useful, put it in
  a renamed migration without changing the enabled default.
- Revisit `000081_backfill_admin_curtailment_ingest`. It likely belongs to the
  automation executor PR, not source CRUD, because source CRUD should not
  dispatch curtailment.
- Remove or bypass the legacy MQTT direct-dispatch path as part of the
  source-signal boundary. After this PR, MQTT ingestion may connect, decode,
  persist state, and emit edges for automation, but it should not execute a
  response by itself.
- Add tests that creating/enabling a source starts connection/subscription but
  does not call the curtailment service when a decoded OFF arrives and no
  automation rule exists.
- Add tests for broker disagreement precedence, stale-after-240s behavior, and
  raw `0`/`100` status reporting separate from canonical `OFF`/`ON` state.
- Preserve enough durable source state for the future executor to know current
  target, last received timestamp, stale/watchdog state, and pending signal
  transitions if needed.

### 2. MQTT connection-test API

Scope:

- Add a backend RPC such as `TestMqttCurtailmentSourceConnection`.
- Support testing a saved `source_id` and testing an unsaved inline source
  config from the create/edit dialog.
- Validate/decrypt credentials, connect to each configured broker, subscribe to
  the topic with QoS 1, report per-broker result, and disconnect.
- Return structured success/error details suitable for frontend display.

Out of scope:

- Do not persist source state.
- Do not wait for or require an MQTT payload unless the request explicitly asks
  for a short optional sample.
- Do not trigger automation or curtailment.
- No migrations expected.

### 3. Response profile CRUD

Scope:

- Add persistent response profiles for reusable curtailment behavior.
- A profile should hold the fields that source CRUD must not own: scope,
  curtailment mode, target value/unit, curtail batch settings, restore batch
  settings, selection strategy, level, priority, min/max duration, reason
  template, include-maintenance behavior, and a site response target for the
  first MaestroOS-backed flow.
- The first implementation should support site-targeted profiles for MaestroOS.
  It can defer building/rack/group/miner selectors until those resolvers are
  designed.
- Add list/get/create/update/delete APIs and validation that mirrors the
  existing manual preview/start request rules where possible.
- Add a profile preview/test RPC or compose one over the existing plan preview
  API so the frontend can power "Test curtailment" without starting
  curtailment.
- Return summary fields suitable for profile cards.
- Keep response profiles inert: creating or enabling a profile must not start
  curtailment without an automation rule or manual operator action.

Notes:

- Site scope from #414 should be modeled here when profiles need site-level
  targeting.
- MaestroOS requires a site target in practice; users should not need to select
  individual miners to model Dorothy 2 or Kati.
- Confirm the contracted-power semantics before wiring the mode. If the value
  is a shed target, `FIXED_KW` may be sufficient. If the value is a residual
  site power cap, response profile CRUD needs a `SITE_POWER_CAP` path or a
  safe translation layer.
- Percentage reduction is a feature gap. It should either be added as a new
  backend selector mode later or explicitly hidden/deferred in the frontend
  until supported; it is not required for the first MaestroOS integration.
- If product wants manual curtailment to reuse saved profiles later, that should
  be a separate UI wiring task after profile CRUD is stable.

### 4. Automation CRUD plus MQTT executor

Scope:

- Add automation rules that bind MQTT source triggers to response profiles.
- Store rule name, enabled flag, order/priority, response profile ID, trigger
  config, ownership/audit metadata, and any conflict policy needed for multiple
  rules.
- For the first MaestroOS implementation, the trigger config should be source
  ID plus signal mapping: `target=0` or stale starts/maintains curtailment;
  `target=100` restores/stops the matching automation-owned event.
- Validate that the MQTT source and response profile belong to the same
  organization and, for MaestroOS, the same site.
- Add list/get/create/update/delete/enable-disable APIs.
- For MQTT execution, observe source signal transitions, load enabled rules
  bound to that source, load the selected response profile, and call
  `StartCurtailment`, `StopCurtailment`, or `Recurtail` with explicit
  automation attribution.
- Treat this executor as the only automated owner of curtailment service calls.
  Any remaining legacy source-direct dispatch logic should be deleted here if
  it was not already removed in source CRUD.
- Preserve MQTT idempotency and active-event matching with stable source actor
  IDs such as `mqtt:<source_config_id>`.
- Return display summaries such as `MaestroOS target is OFF or stale` and the
  selected response profile name.

Acceptance criteria:

- An enabled MQTT source with no enabled automation rule never starts or stops
  curtailment.
- An enabled MQTT source plus enabled automation rule reproduces the intended
  user-visible outcome of the old direct-dispatch behavior, but all response
  decisions come from the selected response profile and the automation executor.
- Disabling an automation rule stops future automated dispatch without requiring
  the source to be disabled.
- ON handling restores only the curtailment event owned by the matching
  automation/source attribution.
- Insufficient-load and all-skipped behavior remains retry-safe and does not
  report OFF as satisfied when no actionable miners were curtailed.
- The #413 alpha end effect is preserved after the split: an enabled MaestroOS
  MQTT source plus enabled automation and site-targeted profile produces
  automated site curtailment, without requiring the user to select miners.

If this PR becomes too large, split it into automation CRUD first and MQTT
executor second. Do not treat automation CRUD alone as restoring the old #409
behavior.

## Recommended sequence from here

1. Narrow #409 to MQTT source CRUD/runtime state and update #413 to the new
   source-only contract. The source API should create enabled MaestroOS MQTT
   sources, associate them with one origin site, expose status, and never start
   curtailment by itself.
2. Add the MQTT connection-test RPC and frontend action. This can land after or
   alongside the source CRUD PR if it shares the same source validation helpers.
3. Finish or coordinate #414 for first-class site scope. Keep MQTT source rows
   out of response targeting; use site scope in manual starts and response
   profiles.
4. Build response profile CRUD with site-targeted profiles for MaestroOS.
   Resolve the contracted-power semantic question before deciding whether the
   first profile mode is `FIXED_KW`, `FULL_FLEET`, or `SITE_POWER_CAP`.
5. Build automation CRUD plus the MQTT executor that replaces today's direct
   source-driver dispatch. Only MQTT source triggers are in scope.
6. Start #387 now that #406 is merged: migrate the frontend from
   `GetActiveCurtailment` to `ListActiveCurtailments` plus keyed
   `GetCurtailmentEvent`.
7. Address #401 before relying on fine-grained frontend gates: curtailment
   source/profile/automation UIs should eventually check org-scoped manage/read
   rather than a flat permission union.
8. Use #415 MQTT simulator tooling for source CRUD, connection-test, and
   automation-executor validation.

## Parallelization guidance

- #409 source CRUD and the MQTT connection-test RPC can proceed in parallel if
  they share a small validation/connection helper boundary.
- #413 should stay the alpha UI reference, but it needs to rebase in stages:
  first onto the narrowed source-only #409 contract, then onto response profile
  CRUD, then onto automation CRUD/executor.
- Response profile CRUD can start once the profile schema is agreed, but it
  depends on #414 or equivalent site-scope support for the MaestroOS path.
- Automation CRUD/executor should wait for the source-signal boundary and
  response profile CRUD; it is the PR that restores automated curtailment.
  Do not add non-MQTT trigger creation in this sequence.
- #387 can start now because #406 is merged.
- #414 changes proto/generated files for site scope. Coordinate it with response
  profile CRUD if profiles include site-scoped targeting; do not add site scope
  as direct MQTT source response behavior.
- #401 can start in parallel with any curtailment UI work; it is cross-cutting
  authz/client infrastructure.
- #384 should be decomposed into the response profile and automation PRs rather
  than closed by the tactical MQTT source UI.

## Migration coordination

Current merged migration ownership:

- `000076_create_curtailment_mqtt_source`: merged from MQTT subscriber #336.
- `000077_add_mqtt_pending_retry_at`: merged from #407.
- `000078_add_curtailment_target_phase_summary`: merged from #406.
- `000079_validate_curtailment_target_phase_summary`: merged from #406.
- #411 and #415 have no migrations.

Current #409 branch migration state to revise:

- #409 currently has `000080_mqtt_source_disabled_default`; the refactor should
  remove the disabled-default behavior. If the source actor-ID rewrite is still
  needed, keep it in a renamed migration that does not alter source enablement.
- #409 currently has `000081_backfill_admin_curtailment_ingest`; the refactor
  should drop or move it to the automation executor PR unless source CRUD still
  needs a separate server-owned runtime actor permission.

Expected migration ownership after the split:

- MQTT source CRUD: migration likely needed for source-owned origin site
  metadata and possibly actor-ID rewrite; no disabled-default migration. Do not
  reuse `scope_site_id` for the source site column.
- MQTT connection-test RPC: no migration expected.
- Response profile CRUD: migration-bearing.
- Automation CRUD/executor: migration-bearing.

Current no-migration PRs:

- #413: frontend alpha reference for the MQTT source/profile/automation flow.
- #414: first-class site scope.

Before merging #409 or any other migration-bearing branch:

- Re-check fresh `main`.
- Ensure there is exactly one up/down pair per migration version.
- If another migration lands first, renumber the active branch's migration pairs
  together and update this plan plus the PR body.
