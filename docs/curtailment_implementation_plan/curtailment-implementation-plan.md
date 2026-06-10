# Proto Fleet Curtailment

_Last updated: 2026-06-06._

## Overview

### Definition and terminology

Curtailment means intentionally reducing normal electrical load or generation
for a period of time. The term comes from electric-grid operations, where a
utility, grid operator, aggregator, or site operator may ask a participant to
reduce consumption or output during congestion, scarcity, equipment
constraints, price events, or demand-response dispatches.

For Bitcoin mining, curtailment applies naturally because miners are large,
controllable electrical loads. A mining site can reduce demand by stopping
mining, lowering power targets, pausing selected machines, or eventually
turning off supporting loads and power-control equipment.

In the current Proto Fleet implementation, curtailment means commanding
selected miners to stop mining and verifying through telemetry that their draw
dropped. It does not mean cutting power at a breaker, PDU, rack, or outlet.
Curtailed miners may still draw idle power, fans may keep running, and
site-level consumption may not fall by the miner nameplate amount. Fleet treats
curtailment as an operational control loop around miners and telemetry, not as
a settlement-grade grid-compliance product yet.

### Proto Fleet's role

- **Role:** Proto Fleet is the execution and verification layer for
  curtailment.
- **Not Fleet's role:** Fleet is not the grid operator and does not decide
  market participation, program bids, or settlement rules.
- **Trigger sources:** an operator action in ProtoFleet, a continuous state
  feed such as MQTT, a webhook or API dispatch from an external provider, or
  another site-control integration.
- **Integration principle:** MQTT is the first implemented external
  continuous-state trigger path, but it is only one trigger path. The feature
  remains centered on fleet operations, not on a single transport.
- **Fleet responsibilities:**
  - Decide which miners are eligible and useful to curtail.
  - Dispatch the right plugin command for each selected miner.
  - Block conflicting schedules or manual commands while an event is active.
  - Verify behavior with telemetry instead of trusting command acknowledgement.
  - Retry or surface drift when a selected miner stops satisfying the desired
    curtailed state.
  - Preserve an audit trail of who or what triggered the event.
  - Restore mining in controlled batches when the event ends.

### Event stages

- A curtailment event starts when Fleet receives a requirement to reduce power
  draw.
- The high-level lifecycle is: choose what to curtail, hold the reduction, then
  restore safely.

#### Pre-curtailment: choose what to curtail

- Resolve the requested scope. The implemented scopes today are the whole
  fleet and an explicit miner list. A device-set scope exists in the proto but
  is not yet implemented by the service; site scope is a future direction.
- Filter out miners that are unsafe, unavailable, stale, unpaired, already in
  blocked device states, recently restored inside cooldown, or unsupported by
  their plugin.
- Estimate reducible load from current telemetry.
- Rank eligible miners and select enough miners to satisfy the requested kW
  reduction when possible.
- Reject the request when there is insufficient curtailable load.
- Capture the chosen target set, baselines, and skipped reasons so operators
  can understand the plan.

#### Curtailment in progress: hold the reduction

- Dispatch curtail commands to the selected miners.
- Verify through telemetry; command acknowledgement alone is not enough.
- Keep checking selected miners while the event is active.
- Re-dispatch within retry limits when a selected miner drifts.
- Suppress conflicting non-curtailment commands so schedules or manual actions
  do not undo the curtailment.
- Future closed-loop control should pick replacement miners or otherwise
  maintain an exact kW band when selected miners disappear or site draw changes.

#### Post-curtailment: restore safely

- Transition the event into restore when the curtailment requirement ends.
- Restore miners in batches rather than all at once to avoid inrush, thermal
  shock, and a large command burst.
- Use persisted restore state so a restart can resume the restore process.
- Verify restoration target by target.
- Allow completion with failures when some targets do not restore cleanly.
- Coordinate non-miner facility devices that are part of the curtailment
  contract, for example fans that switch off during curtailment and back on
  around miner restore (see Facility device control below).

### Facility device control

- Curtailment is not limited to paired miners. Proto Fleet also needs a way to
  control other facility devices, such as fans, lights, and other non-miner
  equipment, as part of a curtailment event.
- The anticipated mechanism is a webhook that these smart devices expose, which
  Proto Fleet calls with a POST request to switch them off during curtailment
  and back on during restore.
- These actions should be sequenced with the miner lifecycle and audited like
  other curtailment actions.
- The exact contract, configuration, and design are not yet defined. This is a
  known requirement to account for, not a finalized mechanism.

### Roadmap at a glance

- **Current baseline:** operator-triggered fixed-kW curtailment for miners and
  configured MQTT continuous-state ingest. Fleet previews eligible miners,
  starts events, verifies selected miners through telemetry, suppresses
  conflicting commands, restores in batches, and can now drive
  Start/Stop/Recurtail from an in-process MQTT subscriber in `fleetd`.
- **Open MQTT follow-ups:** operator CRUD or seed/runbook flow for source
  config, production heartbeat/metrics, grouped command activity and batched
  curtail dispatch ([#403](https://github.com/block/proto-fleet/issues/403)),
  and first-class site scope ([#404](https://github.com/block/proto-fleet/issues/404)).
- **Future capabilities:** closed-loop site-level kW maintenance, source or
  site-scoped events, non-miner facility-device control (fans, lights, and
  similar, likely via device webhooks), PDU/outlet/rack controls,
  provider-specific dispatch adapters, and evidence export for grid-program
  reporting.

## Architecture

Curtailment is one domain service wrapped in a telemetry verification loop,
with an interchangeable trigger layer in front of it. Every trigger converges
on the same curtailment service lifecycle, so selection, dispatch,
verification, and restore behave identically no matter who started the event.
MQTT also uses `Service.Recurtail` to reassert a source-owned OFF signal when
that event is already in restore.

### Control plane

```text
Operator UI / API key caller
        |
        v
CurtailmentService (Connect RPC handlers)
  Preview / Start / Update / Stop / GetActive / List / AdminTerminate
        |
        v
curtailment domain service
  selector -> persistence -> reconciler (verify + restore)
        |
        +--> command service + CurtailmentActiveFilter
        +--> device telemetry / metric aggregates
        +--> activity log (audit)
        +--> plugin Curtail / Uncurtail (via SDK)
```

### Components

- **API / handlers** (`server/internal/handlers/curtailment/`): map the Connect
  RPCs from `proto/curtailment/v1/curtailment.proto` to domain calls and
  enforce the auth gates.
- **Domain service** (`server/internal/domain/curtailment/`): owns the event
  lifecycle. The selector chooses targets, the service persists the event and
  its targets, and it exposes the start/stop/update/read operations.
- **Reconciler** (`server/internal/domain/curtailment/reconciler/`): a single
  background loop launched from `server/cmd/fleetd/main.go`. Each tick reads
  telemetry, confirms or re-dispatches drift, and advances restore batches.
- **MQTT ingest** (`server/internal/domain/curtailment/mqttingest/` plus
  `server/internal/infrastructure/mqttclient/`): one worker per enabled
  source, launched from `fleetd`, consumes dual-broker ON/OFF state, persists
  durable edge state, runs the stale-signal watchdog, and calls the same
  curtailment service entry points as the operator API.
- **Persistence** (Postgres via sqlc): the event, per-target, reconciler
  heartbeat, and org-config tables are the source of truth, so a `fleetd`
  restart resumes work in place rather than from memory.
- **Command path** (`server/internal/domain/command/`): curtail and uncurtail
  go out as device commands. `CurtailmentActiveFilter` blocks conflicting
  schedule or manual commands for locked miners, while curtailment's own
  traffic bypasses the filter.
- **Plugins / SDK**: each plugin implements `Curtail` / `Uncurtail` and
  advertises capability flags, keeping model-specific behavior in the plugin.
- **Telemetry**: device metrics and aggregates feed both selection (ranking)
  and verification (did draw actually drop).
- **Audit and metrics**: lifecycle actions write activity rows; a metrics
  interface is defined with a no-op default until platform wiring lands.
- **Frontend** (`client/src/protoFleet/features/energy/`): the operator surface
  for preview, start, active management, stop/restore, and history.

### Verification model

Verification is telemetry-based, not acknowledgement-based. A target counts as
curtailed or restored by comparing observed power against a baseline-relative
threshold captured at selection. This is the load-bearing design decision: it
keeps curtailment trustworthy when a command is accepted but the device does
not actually change behavior.

### Trigger and ingestion layer

Triggers sit in front of the domain service and are intentionally
interchangeable:

- **Operator (live):** ProtoFleet calls the RPCs directly.
- **MQTT subscriber (live for configured sources):**
  `server/internal/domain/curtailment/mqttingest/` turns continuous ON/OFF
  state into `Service.Start` / `Service.Stop` / `Service.Recurtail` calls.
  Source config is operator-managed in SQL for now; there is no CRUD API or
  hot reload yet.
- **HTTP provider dispatch (future):** `IngestCurtailmentSignal` is defined and
  permission-gated but returns `Unimplemented`; provider adapters are a later
  concern.

```text
MQTT publisher(s)
  target=0/100 every ~30s
        |
        v
mqttingest subscriber core
  edge detect + watchdog
        |
        v
Service.Start / Service.Stop
```

Facility-device control (fans, lights, and other non-miner equipment, likely
via device webhooks) is a planned extension of this same flow; its contract is
not yet defined.

## Goals and Non-goals

### Goals

- Let an operator curtail a supported scope by target kilowatt reduction,
  preview the selected miners before committing, and restore on demand.
- Verify curtailment through telemetry rather than command acknowledgement.
- Re-issue commands when the selected target set drifts.
- Restore in bounded batches to avoid sudden inrush and thermal shock.
- Emit an audit trail for trigger, start, update, admin-recovery, and override
  actions, while event/target state remains the source of truth for restore
  progress.
- Suppress schedules and manual commands that target miners locked by an
  active curtailment event.
- Expose `Curtail` and `Uncurtail` through the SDK/plugin contract so each
  plugin can use the safest model-specific behavior.
- Run v2 MQTT automation through continuous-state signal ingest.
- Preserve a v3 path for HTTP discrete-dispatch providers and closed-loop
  curtailment without reshaping the v1 execution path.

### Non-goals

- No first-class site-scoped curtailment in the current service path. The
  implemented service path uses whole-org or explicit miner lists; device-set
  scope is present in the proto but not implemented by the service. The MQTT
  schema reserves `scope_type='site'`, but the subscriber rejects it at runtime
  until the curtailment core supports site scope.
- No closed-loop kW-total maintenance in v1. `FIXED_KW` freezes the target
  set at event creation.
- No Fleet-level efficiency curtailment event before v3.
- No policy evaluator, price-feed poller, demand-response platform, or
  OpenADR-conformant VEN before v3.
- No hardware power cut in v1/v2.
- No facility-device (fan/light) webhook control in v1/v2. The webhook contract
  for non-miner equipment is deferred to v3 and not yet defined.
- No reconciler or MQTT subscriber leader election until the deployment model
  requires multiple `fleetd` instances.
- No public compliance/settlement claim until v3 program-specific
  integrations, revenue-meter primitives, and evidence export exist.

## Version Boundaries and Work Plan

The plan uses three version boundaries: v1 is the manual-curtailment baseline,
v2 is the merged MQTT continuous-state ingest implementation, and v3 is
everything after MQTT.

### v1: mainline manual curtailment (complete on main)

Already represented in the codebase:

- `CurtailmentService` RPCs: `PreviewCurtailmentPlan`, `StartCurtailment`,
  `UpdateCurtailmentEvent`, `StopCurtailment`, `GetActiveCurtailment`,
  `ListCurtailmentEvents`, and `AdminTerminateEvent`.
- Domain logic under `server/internal/domain/curtailment/`: selector, start,
  stop, update, list, active-event read, admin terminate, audit, the metrics
  interface, and enum stability tests.
- The reconciler wired from `server/cmd/fleetd/main.go`, covering telemetry
  verification, drift retry, staggered restore, max-duration handling, and
  race hardening.
- Command-service preflight filtering: `CurtailmentActiveFilter` plus
  schedule-skip handling.
- Persistence through migrations `000042`, `000050`, `000051`, `000055`,
  `000056`, `000057`, `000058`, and `000059` (event, target, reconciler
  heartbeat, and org-config tables with idempotency indexes).
- ProtoFleet frontend for start, preview, active-event management, update,
  stop/restore, event history, and the page-header pill.

The implemented service path supports whole-org and explicit-miner curtailment;
device-set scope is present in the proto but rejected as unimplemented.

### v2: MQTT continuous-state ingest (merged)

Already in the repo:

- `server/internal/domain/curtailment/mqttingest/`: subscriber lifecycle,
  source worker, payload decoder, broker precedence, edge detection, watchdog,
  curtailment driver, SQL-backed store, and tests.
- `server/internal/infrastructure/mqttclient/`: production Eclipse Paho
  adapter with MQTT 3.1.1, QoS 1 subscriptions, ordered callbacks, deterministic
  client IDs, `CleanSession=false`, pre-connect route registration, TCP/TLS
  support, and reconnect behavior.
- `server/cmd/fleetd/main.go`: constructs and starts the subscriber with the
  sqlc-backed store, curtailment service driver, encrypt decryptor, and Paho
  MQTT client factory.
- Migration `000076`, creating `curtailment_mqtt_source_config` and
  `curtailment_mqtt_source_state`.
- `server/sqlc/queries/curtailment_mqtt.sql` and generated sqlc support.

Remaining follow-up after the v2 merge:

- Provision source rows through an accepted operator workflow, including
  service-user setup and encrypted password handling. CRUD and hot reload are
  not implemented.
- Add MQTT metrics, a monitorable heartbeat/liveness signal, and an operator
  runbook covering broker failure, watchdog fire, repeated precedence flips,
  and source disablement.
- Decide whether to emit a dedicated `curtailment_signal_ingested` activity
  row. Current code does not define that type.
- Add broker-backed integration tests.
- Complete [#403](https://github.com/block/proto-fleet/issues/403) and
  [#404](https://github.com/block/proto-fleet/issues/404) before claiming
  large-site MaestroOS readiness.

### v3: future expansion

Everything not required for the current MQTT continuous-state ingest:

- HTTP provider dispatch through `IngestCurtailmentSignal`, with a provider
  adapter registry and concrete adapters (QSE bridge, Voltus, OpenADR, etc.).
- Source config CRUD and optional frontend management, including history or
  source badges for external-origin events.
- Per-source or site-scoped lifecycle.
- Closed-loop kW-total maintenance and dynamic target-set mutation.
- Fleet-level efficiency curtailment.
- Non-miner facility-device control (fans, lights) and smart PDU/outlet/rack
  and other hardware power-control targets.
- Grid-program evidence export, retention, and compliance packaging.
- Multi-instance leader election or another HA strategy.
- Structured error-detail cleanup and other API ergonomics.

## Implementation Detail and Reference

_Deep reference for implementers. Readers who only need the overview,
architecture, scope, and roadmap can safely skip this tab._

### v1: Manual Curtailment on Main

#### API and auth

`CurtailmentService` exposes the full v1 lifecycle plus the dormant v3 HTTP
ingest placeholder RPC. Important auth boundaries:

- `AdminTerminateEvent` is session-only and role-gated. Its request reserves
  field 4 / `idempotency_key`; idempotency is state-based.
- `UpdateCurtailmentEvent`, `AdminTerminateEvent`, and
  `IngestCurtailmentSignal` are also in the central procedure-permission map.
- Start/Preview/Stop are absent from the central procedure-permission map;
  only their admin override fields gate inline (`allow_unbounded` and
  `force_include_maintenance` on Start, `force` on Stop, and the
  candidate-min-power override on Start/Preview). The base operation requires
  only authentication — unlike reads, Update, and AdminTerminate, which require
  `curtailment:read` / `curtailment:manage` — so any authenticated caller can
  start or stop a whole-org event. This gap is pending the broader curtailment
  authz redesign.
- Admin-gated controls include `allow_unbounded`,
  `force_include_maintenance`, stop `force`, and update values above normal
  operator limits.
- `priority=EMERGENCY` bypasses post-event cooldown. The v1 plan originally
  expected an admin-only gate; current code should be treated as needing an
  explicit operate-or-ratify decision.

#### Data model

`curtailment_event` stores the event lifecycle, operator controls, scope and
mode JSON, the decision snapshot, external idempotency fields, actor
attribution, and timestamps.

Key event-table behavior:

- One non-terminal curtailment event per org is enforced by a partial unique
  index on `(org_id)` for `pending`, `active`, and `restoring`.
- `idempotency_key` is unique only for non-terminal events, so a key can be
  reused after the original event ends.
- `(org_id, external_source, external_reference)` is also unique only for
  non-terminal events, matching the v2 replay contract.
- `effective_batch_size` is stamped at Start and is the value restore uses;
  update does not recompute it mid-event.

`curtailment_target` stores each selected miner target:

- Composite identity is `(curtailment_event_id, device_identifier)`.
- `baseline_power_w` is captured once during selection and used as the restore
  reference.
- `observed_power_w`, `retry_count`, `last_error`, phase-local dispatch
  cursors, and target state drive reconciliation.
- `CONFIRMED` is not terminal. A target can drift and cycle through
  `DRIFTED -> DISPATCHING -> DISPATCHED -> CONFIRMED`.
- Terminal target states are `RESOLVED`, `RESTORE_FAILED`, and reserved
  `RELEASED`.

`curtailment_reconciler_heartbeat` is a singleton row updated by the
reconciler. External monitoring is expected to query this out-of-process.

`curtailment_org_config` stores per-org defaults:

- `max_duration_default_sec` defaults to 4 hours.
- `candidate_min_power_w` defaults to 1500 W.
- `post_event_cooldown_sec` defaults to 600 seconds.

#### Selection

v1 supports `FIXED_KW` with `LEAST_EFFICIENT_FIRST` ranking and `FULL` level.
The selector:

1. Resolves scope: whole org or explicit device list. Device-set scope exists
   in the proto but currently returns unimplemented from the service.
2. Normalizes defaults: unspecified mode/strategy/level/priority become
   `FIXED_KW`, `LEAST_EFFICIENT_FIRST`, `FULL`, and `NORMAL`.
3. Filters candidates using the dual-signal rule:
   `power_w >= candidate_min_power_w` and `hash_rate_hs > 0`.
4. Excludes stale telemetry, non-paired devices, incompatible plugins, devices
   in blocked states, recently resolved/restored miners inside cooldown, and
   maintenance miners unless the admin-only maintenance override pair is set.
5. Ranks by hourly average efficiency, least efficient first.
6. Accumulates miner power until target kW is met, or accepts a tolerated
   undershoot if `target_kw - tolerance_kw <= selected_kw < target_kw`.
7. Rejects insufficient load with structured detail for UI recovery.
8. Captures the full ranking, skipped reasons, baseline power, and selection
   rationale in `decision_snapshot_jsonb`.

The selected target set is frozen for v1. New miners added to the scope during
the event are not pulled in, and removed miners remain part of the event until
resolved or failed.

#### Reconciliation

The reconciler runs from `fleetd` alongside the schedule processor.

Per tick, for each non-terminal event:

- Curtail-side targets (`desired_state=curtailed`) read latest telemetry,
  update `observed_power_w`, and compare against a level-specific predicate.
- Mismatches after dispatch are redispatched while retry budget remains.
- Retry exhaustion marks the target failed or drift-exhausted but does not
  end the event by itself.
- Restore-side targets (`desired_state=active`) are checked against
  baseline-relative restore criteria.
- Event state transitions are lifecycle-driven:
  `pending -> active`, `active -> restoring`, and
  `restoring -> completed | completed_with_failures`.

Race hardening in current code includes:

- A `DISPATCHING` pre-write before `cmd.Curtail` / `cmd.Uncurtail`, so
  concurrent admin termination can observe in-flight commands.
- Per-target event liveness checks before curtail dispatch.
- `ErrCurtailmentEventStateRaceLoss` for zero-row state writes caused by a
  concurrent event-state advance.
- Orphan handling for targets left in `DISPATCHING` across interrupted ticks.

#### Restore

Stop transitions an event into `restoring` and flips non-terminal targets to
`desired_state=active`. Restore is batch-claimed by the reconciler, not by
in-memory workers, so `fleetd` restarts resume from persisted state.

Defaults and sizing:

- `restore_batch_interval_sec` defaults to 30 when unset.
- `restore_batch_size` has no server-side default; it is passed through from the
  request (validated `>= 0`).
- `effective_batch_size = max(restore_batch_size, ceil(0.01 * selected_count))`
  clamped to `[10, 100]` and stamped at Start, so the effective batch floors at
  10 regardless of the requested value.

At the default interval and adaptive batch size, large-fleet restore stays
bounded near 100 batches, roughly 50 minutes at 30 seconds per batch.

#### Schedule and command interaction

`CurtailmentActiveFilter` blocks commands for miners in a non-terminal
curtailment event unless the command actor is curtailment itself. This covers
all command types, including firmware updates.

Operator/API commands fail closed with `FailedPrecondition`. Schedule skips
emit `schedule_skipped_due_to_curtailment`.

#### SDK and plugins

The miner SDK exposes `Curtail` and `Uncurtail`, with level-aware request
types and capability flags:

- `curtail_full_supported`
- `curtail_efficiency_supported`

The long-term contract is capability-gated selection. Current service code
still has a transitional shortcut that treats an empty driver name as not
curtailable rather than relying entirely on the advertised capability flags.

Known plugin intent:

- Virtual: full curtailment through stop/start mining.
- Antminer: full curtailment through stop/start mining.
- Proto: full curtailment and efficiency curtailment through power-target
  snapshot/restore behavior.
- ASIC-RS: full curtailment through pause/resume behavior.

Fleet should not infer curtailment capabilities from unrelated capabilities.

#### Audit and metrics

Current curtailment activity types in code:

- `curtailment_started`
- `curtailment_unbounded_start`
- `curtailment_force_include_maintenance`
- `curtailment_admin_terminated`
- `curtailment_admin_terminated_replay`
- `curtailment_updated`

The schedule processor also emits:

- `schedule_skipped_due_to_curtailment`

Stop/restore changes are visible through event state and target state, but the
current service does not emit a dedicated Stop activity type.

The current `Metrics` interface includes:

- `ObserveTickDuration`
- `IncTickFailure`
- `IncCandidateExcluded`
- `IncMaintenanceOverride`
- `IncEventStateRaceLoss`
- `IncTargetWriteFailure`
- `IncAuditWriteFailure`

`NoOpMetrics` is wired in `fleetd` until the platform observability path lands.

#### Frontend

Current ProtoFleet frontend code includes:

- API hooks and mappers for active event, history, start, update, and stop.
- Start modal and preview request builders.
- Active curtailment status panel.
- Stop/restore confirmation dialog.
- Event history table with pagination and state filters.
- Page-header `CurtailmentPill` plus polling hook.
- Tests and stories for the major curtailment UI pieces.

The active management panel refreshes non-terminal active events every 3
seconds while visible. That is more aggressive than the old plan's concern
about long restore windows, so production load should be revisited if multiple
operators/tabs are common during extended restores.

### v2: MQTT Signal Ingest

#### MaestroOS MQTT requirement

The MaestroOS API requirement is a continuous site-target stream for Soluna
sites, currently Kati and Dorothy 2:

- Protocol: MQTT 3.1.1.
- Topic: `maestro/target`.
- Subscription QoS: `1`.
- Cadence: one message roughly every 30 seconds.
- Payload format: JSON with `target` and `timestamp`.
- Payload target: `100` means full power / ON; `0` means curtail / OFF.
- Payload timestamp: Unix epoch seconds.
- Redundancy: two brokers per site/source publish the same signal.
- Precedence: when broker signals disagree, the lower-IP broker takes
  precedence. For the documented hosts that means `10.xxx.0.3` wins over
  `10.xxx.0.4`.
- Staleness fail-safe: if the last received site-target message is older than
  4 minutes, the site should curtail automatically.
- Response target: on the first OFF signal, the site must reduce below the
  contracted curtailment power level within 3 minutes.
- Hold behavior: normal OFF periods are expected to last at least 10 minutes,
  but ON-to-OFF transitions may happen immediately when grid conditions require
  them.

Documented broker endpoints:

| Site | Brokers | Port | Topic |
| --- | --- | --- | --- |
| Kati | `10.155.0.3`, `10.155.0.4` | `1883` | `maestro/target` |
| Dorothy 2 | `10.144.0.3`, `10.144.0.4` | `1883` | `maestro/target` |

Credentials are username/password values supplied out of band and stored in
Fleet as encrypted source config.

Example payloads:

```json
{"target": 100, "timestamp": 1778538975}
```

```json
{"target": 0, "timestamp": 1778539005}
```

#### Current Fleet behavior

The merged implementation consumes compatible MaestroOS-style streams through
`server/internal/domain/curtailment/mqttingest/`, the production Paho adapter
in `server/internal/infrastructure/mqttclient/`, and `fleetd` startup wiring.

Source configuration is operator-managed in
`curtailment_mqtt_source_config` from migration `000076`. A source row stores
the organization, service user, source name, topic, primary/secondary broker
hosts, port, transport, MQTT credentials, payload format, curtailment scope,
curtailment mode, contracted kW, stale threshold, minimum hold duration, and
enabled flag. The subscriber lists enabled sources once at startup; adding,
disabling, or changing a source currently requires a `fleetd` restart.

The #409 source CRUD refactor narrows the future source API to MQTT connection
and runtime state only: no response scope, curtailment mode, contracted kW,
minimum hold duration, or direct source-owned dispatch. New MQTT sources should
be enabled by default so they connect and record source signals immediately,
but response behavior must come from an automation binding the source trigger
to a response profile. Deleting a disabled MQTT source should require only the
org-scoped `curtailment:manage` permission, not the Admin role; deleting an
enabled source should remain blocked so the source is disabled intentionally
before removal.

Connection and stream handling:

- One worker starts per enabled source.
- Each worker connects to both brokers concurrently.
- Broker roles are resolved by lower IP, matching the MaestroOS precedence
  rule; if the primary is stale relative to the secondary by the broker
  freshness window, the secondary becomes the canonical observation.
- The production MQTT client uses MQTT 3.1.1, QoS 1 subscription, ordered Paho
  callbacks, deterministic per-source/per-broker/topic client IDs,
  `CleanSession=false`, `ResumeSubs=true`, pre-connect route registration, and
  TCP/TLS support.
- Plain TCP is accepted only for private, loopback, or link-local broker
  addresses. The documented MaestroOS broker IPs satisfy this startup guard.
- Initial broker-connect failures retry in place with capped exponential
  backoff and jitter, so a broker can come up after `fleetd` starts.

Edge and watchdog behavior:

- The `target_timestamp` decoder accepts only `target=0` or `target=100` with
  Unix-second timestamps.
- Duplicate, stale, retained, and same-second ambiguous QoS 1 messages are
  suppressed using persisted publisher time, receive time, processed-target
  state, and durable pending-edge state.
- OFF (`ON -> OFF`) triggers `Service.Start`.
- ON (`OFF -> ON`) triggers `Service.Stop` only for the non-terminal event
  owned by the same MQTT source, identified by `source_actor_id =
  "mqtt:<source_name>"`.
- If OFF arrives while the source-owned event is restoring, Fleet calls
  `Service.Recurtail` and flips the restoring event back toward curtailment
  instead of starting a competing event.
- If no fresh message arrives within `staleness_threshold_sec` (default
  240 seconds), the watchdog synthesizes a fail-safe OFF. Existing OFF state
  and durable pending OFF work dispatch immediately on startup; stale non-OFF
  cold-start state gets bounded startup grace so retained/live broker state can
  arrive before fail-safe curtailment.
- Source workers persist pending edges before side effects and clear them only
  after dispatch settlement, so retryable OFF work remains durable across
  restarts.

Curtailment dispatch behavior:

- MQTT starts use `Priority=EMERGENCY`, `Strategy=LEAST_EFFICIENT_FIRST`,
  `Level=FULL`, `AllowUnbounded=true`, and `CanUseAdminControls=true`.
- The source service user is stamped as `CreatedByUserID`, must have
  `curtailment:ingest`, and is rechecked before side-effecting dispatch.
- `external_source` is the configured source name.
- `external_reference` is stable and synthetic:
  `<source>:<edge_unix_ts>` for message OFF edges and
  `<source>:watchdog:<window_start>` for watchdog OFF edges.
- `FULL_FLEET` is the default source mode and curtails every eligible miner in
  the configured scope.
- `FIXED_KW` uses `contracted_curtailment_kw` as the requested kW reduction
  target and applies a 5% tolerance.
- Runtime scope support is `whole_org` and `device_list`. The MQTT schema
  reserves `site` scope with a site FK, but the worker rejects `scope_type =
  'site'` until the curtailment core implements first-class site scope.
- MQTT ON uses forced Stop because the current code treats ON as authoritative;
  `min_curtailed_duration_sec` is stamped on MQTT-created events but does not
  block restore when the publisher sends ON.

#### MaestroOS alignment and gaps

The current implementation lays the right foundation for MaestroOS MQTT:
protocol version, QoS, dual-broker precedence, payload decoding, stale-signal
fail-safe, durable pending OFF handling, and source-owned Start/Stop/Recurtail
are implemented.

It is not yet a full production-compliance implementation for the documented
site contracts:

- Kati and Dorothy 2 source rows still need an accepted provisioning path:
  broker hosts, topic, credentials, service user, mode, scope, threshold, and
  hold settings are not managed through an API or UI.
- MaestroOS is site-oriented. Current runtime execution cannot use first-class
  site scope; use `whole_org` or explicit `device_list` until
  [#404](https://github.com/block/proto-fleet/issues/404) lands.
- The MaestroOS requirement says the site must reduce below the contracted
  curtailment power level within 3 minutes. Current `FIXED_KW` treats
  `contracted_curtailment_kw` as a requested reduction amount, not as a
  remaining-load cap. If the contract value is a site-load ceiling, Fleet needs
  a mapping or closed-loop site-load implementation before claiming exact
  compliance.
- Curtail dispatch still sends commands one target at a time. For large sites,
  [#403](https://github.com/block/proto-fleet/issues/403) is the follow-up for
  grouped command activity and batched curtail dispatch, and it is relevant to
  the 3-minute response target.
- The current code does not enforce the 10-minute OFF hold if MaestroOS sends
  an early ON; it trusts ON as authoritative. This is acceptable only if the
  publisher contract guarantees normal OFF hold behavior or if operations
  explicitly accept early restore.
- At the time of the v2 merge, MQTT had no source CRUD, source hot reload,
  platform heartbeat/metrics, broker-backed integration test, or
  acknowledgement publisher path. #409 covers source CRUD, runtime reload, and
  connection testing; heartbeat/metrics and acknowledgements remain separate
  follow-ups.

### v3 HTTP Provider Shape Sketches

The HTTP-oriented surface already exists as a dormant RPC:

```protobuf
rpc IngestCurtailmentSignal(IngestCurtailmentSignalRequest)
    returns (IngestCurtailmentSignalResponse);
```

Request shape:

- `external_source` identifies the adapter/provider.
- `external_reference` identifies the provider dispatch.
- `signal_payload` carries provider-opaque bytes, up to 64 KiB.
- Optional scope overrides mirror whole-org, device sets, or device list.
- `reason` is operator-facing text.

Response shape:

- `event`
- `created`
- `adapter_warnings`

Current code gates the handler with `curtailment:ingest`, validates the request,
redacts `signal_payload` in request logging, and returns `Unimplemented`.

The examples below are reference shapes only. They do not ship until a concrete
v3 HTTP adapter is built behind `IngestCurtailmentSignal`.

#### OpenADR 3.0 event

```json
{
  "id": "evt_peak_2026_06_12",
  "objectType": "EVENT",
  "programID": "ercot-ers10",
  "eventName": "Peak Load Response",
  "priority": 0,
  "targets": [
    { "type": "GROUP", "values": ["fleet_org_001"] }
  ],
  "payloadDescriptors": [
    {
      "objectType": "EVENT_PAYLOAD_DESCRIPTOR",
      "payloadType": "CONSUMPTION_POWER_LIMIT",
      "units": "KW"
    }
  ],
  "intervalPeriod": {
    "start": "2026-06-12T14:00:00Z",
    "duration": "PT30M"
  },
  "intervals": [
    {
      "id": 1,
      "payloads": [
        { "type": "CONSUMPTION_POWER_LIMIT", "values": [12500] }
      ]
    }
  ]
}
```

Potential mapping:

- `id` to `external_reference`.
- `programID` into payload/audit metadata.
- immediate `intervalPeriod.start` validation.
- `intervalPeriod.duration` to `max_duration_seconds`.
- `CONSUMPTION_POWER_LIMIT` payload to `target_kw`.
- provider priority to NORMAL or EMERGENCY.

#### Voltus thin webhook

```json
{
  "event": { "name": "dispatch.create" },
  "resource": "/2022-04-15/dispatches/dkmq"
}
```

Voltus is deferred because it requires outbound HTTP fetch and separate
provider bearer storage.

#### ERCOT via operator QSE bridge

```json
{
  "dispatch_id": "ERS10-20260612-0915-EVT001",
  "program": "ERS-10",
  "event_type": "DEPLOYMENT",
  "start_time": "2026-06-12T09:15:00Z",
  "end_time": "2026-06-12T09:45:00Z",
  "target_mw": 12.5,
  "priority": "EMERGENCY",
  "qse_id": "QSE001"
}
```

Potential mapping:

- `dispatch_id` to `external_reference`.
- `target_mw * 1000` to `target_kw`.
- `end_time - now` to `max_duration_seconds`.
- `priority` to NORMAL or EMERGENCY after the EMERGENCY-gate decision.
- non-`DEPLOYMENT` event types reject.

## Limitations and Open Questions

### v1 baseline limitations

- Offline miners drawing power are visible but uncontrollable through the
  miner API. v3 hardware power control is required to handle that class.
- Overlapping events are rejected by the one-non-terminal-event-per-org
  constraint.
- v1 maintains a selected miner set, not a fleet-level kW band. If a selected
  miner drops out mid-event, v1 does not pick a replacement.
- Reconciler is single-instance.
- Event and target rows accumulate. Retention remains undefined.
- Full decision snapshots grow linearly with fleet size. List paths trim, but
  full active snapshots can be large.
- Efficiency ranking uses hourly aggregate data, so ranking may lag recent
  miner behavior.
- Restore considers a miner restored once it crosses the current
  baseline-relative predicate, not necessarily full pre-event power draw.
- Verification preserves the curtailed state when a confirmed target's
  telemetry goes missing (no fresh power or hash sample), so a flaky sensor
  does not trigger re-curtailment. A miner that resumed full draw but stopped
  reporting is therefore held as confirmed-curtailed until telemetry returns.
- Per-target activity rows are intentionally omitted in v1.
- Targets that reach `RESTORE_FAILED` (uncurtail never telemetry-confirmed, or
  repeated dispatch failure) drive the event to `completed_with_failures` and
  are not retried, so such a miner may remain curtailed; v1 surfaces no
  dedicated per-target signal enumerating which targets are affected.
- Stop currently has no dedicated activity row; event state changes and target
  state are the source of truth for restore progress.

### v2 limitations

- MQTT publisher signals are trusted. A misconfigured publisher that sends
  OFF can trigger curtailment.
- MQTT runtime scope support is `whole_org` and `device_list`; `site` is
  schema-reserved but rejected at startup.
- Multiple simultaneous MQTT sources still run into v1's
  one-non-terminal-event-per-org constraint. A second source cannot create a
  competing event while another source-owned event is non-terminal; its pending
  OFF remains retryable rather than being considered satisfied.
- Source config is read once at subscriber startup. Adding, disabling, or
  changing a source row requires a `fleetd` restart.
- Contracted curtailment power is static per source row. Mid-event target
  adjustments need v3 closed-loop semantics.
- `FIXED_KW` models a target kW reduction, not a remaining site-load cap.
- MQTT does not publish acknowledgements back to brokers.
- MQTT subscriber is single-instance and has no leader election.
- MQTT ON uses forced restore, so the subscriber does not enforce a hard
  10-minute minimum hold if the publisher sends an early ON.
- MQTT lacks platform metrics, a heartbeat/liveness row, source CRUD, hot
  reload, and broker-backed integration coverage.

### Open questions: v2 MQTT operations

- Should `EMERGENCY` be admin-gated in the backend, explicitly accepted for
  trusted in-process MQTT, or made a source-config policy rather than a
  publisher-controlled behavior?
- Is direct SQL/DML source provisioning acceptable until source CRUD exists?
- What is the production password encryption and rotation procedure for
  `mqtt_password_enc`?
- Which liveness signal should page operators: SQL heartbeat, platform metric,
  or both?
- Should MQTT edge dispatch emit a dedicated `curtailment_signal_ingested`
  activity row, or is the existing `curtailment_started` / `curtailment_updated`
  audit trail enough?
- Does MaestroOS contracted curtailment power mean a reduction target or a
  remaining-load cap? Current Fleet semantics are reduction-target semantics.
- Should Fleet enforce the documented normal 10-minute OFF hold if MaestroOS
  ever sends ON earlier than expected, or should ON remain authoritative?

### Open questions: v3

- HTTP raw payload storage and audit: define a concrete contract when the first
  HTTP adapter ships.
- Source config CRUD: ownership and permission model, and whether it is
  server-only, UI-backed, or both.
- When to relax the one-non-terminal-event-per-org constraint for per-source or
  site-scoped lifecycle.
- Whether to wire the missing `EMERGENCY` admin gate or document the current
  behavior as accepted.
- Structured error details for `AlreadyExists` conflicts and
  `AdminTerminateEvent` reasons, instead of message-text matching.
- Whether non-admin idempotency replays should filter admin-only fields.
- Retention and archival for `curtailment_event` and `curtailment_target`.
- Grid-program evidence: per-target audit rows, stronger telemetry semantics,
  or closed-loop kW-band maintenance.

## Issue and PR Tracking

Curtailment-related issues and PRs. Most PRs have a same-titled tracking issue
that closed on merge; the standalone issues worth watching are the umbrella
issue [#172](https://github.com/block/proto-fleet/issues/172) (frontend),
backend issues [#289](https://github.com/block/proto-fleet/issues/289) and
[#326](https://github.com/block/proto-fleet/issues/326) (both closed),
[#333](https://github.com/block/proto-fleet/issues/333) (still open on GitHub
but largely implemented by merged PR #336 and needing tracking cleanup), and
the active MQTT follow-ups
[#403](https://github.com/block/proto-fleet/issues/403) and
[#404](https://github.com/block/proto-fleet/issues/404).

### Backend PRs

| PR | What it does | Area | Status |
| --- | --- | --- | --- |
| [#118](https://github.com/block/proto-fleet/pull/118) | Add the curtailment proto/SDK contracts, the `Curtail`/`Uncurtail` surface, and per-plugin capability flags (`curtail_full_supported`, `curtail_efficiency_supported`) | Proto/SDK contract, capabilities | Merged |
| [#173](https://github.com/block/proto-fleet/pull/173) | Add the admin RPC surface, operator override fields, and session-only auth | Admin/override, session auth | Merged |
| [#188](https://github.com/block/proto-fleet/pull/188) | Persist the curtailment schema (migration `000042`) and add the preview selector | Persistence, selector | Merged |
| [#192](https://github.com/block/proto-fleet/pull/192) | StartCurtailment: persist targets, dispatch initial Curtail commands, and run the reconciler loop | Start, reconciler | Merged |
| [#232](https://github.com/block/proto-fleet/pull/232) | StopCurtailment, staggered batch restore, and max-duration enforcement | Stop, restore | Merged |
| [#305](https://github.com/block/proto-fleet/pull/305) | Authz redesign sweep (PR 2c): move existing curtailment handler gates onto `RequirePermission` | Authz (cross-cutting) | Merged |
| [#308](https://github.com/block/proto-fleet/pull/308) | Operator read APIs (List / GetActive), UpdateCurtailmentEvent, AdminTerminateEvent, audit trail, and the metrics interface | Read/update/admin, audit, metrics | Merged |
| [#325](https://github.com/block/proto-fleet/pull/325) | Authz redesign sweep (PR 2d.1): gate previously-ungated curtailment handlers with `RequirePermission` | Authz (cross-cutting) | Merged |
| [#327](https://github.com/block/proto-fleet/pull/327) | Define the `IngestCurtailmentSignal` RPC, the `curtailment:ingest` permission gate, and the handler stub | v3 HTTP ingest contract (dormant) | Merged |
| [#336](https://github.com/block/proto-fleet/pull/336) | Add the in-process MQTT signal-source subscriber, durable source state, Paho MQTT client, `fleetd` wiring, source-owned Start/Stop/Recurtail behavior, and fail-safe watchdog | v2 MQTT subscriber | Merged |
| [#355](https://github.com/block/proto-fleet/pull/355) | Add the multi-status `state_filters` field to `ListCurtailmentEvents` (proto, service, list handler); history UI tracked under Frontend PRs | List filters (full-stack) | Merged |

(#299 carried the same scope as #308 and was closed unmerged in favor of it.
#141 and #185 were earlier persistence/selector iterations closed unmerged in
favor of #188.)

### Frontend PRs

| PR | What it does | Area | Status |
| --- | --- | --- | --- |
| [#233](https://github.com/block/proto-fleet/pull/233) | Scaffold the plan-curtailment (start) modal component | Start modal | Merged |
| [#247](https://github.com/block/proto-fleet/pull/247) | Update the plan-curtailment modal form layout | Start modal | Merged |
| [#276](https://github.com/block/proto-fleet/pull/276) | Hook up the curtailment preview API | Preview | Merged |
| [#280](https://github.com/block/proto-fleet/pull/280) | Add the curtailment event-history UI | History | Merged |
| [#284](https://github.com/block/proto-fleet/pull/284) | Extract the active-curtailment status component | Active status | Merged |
| [#295](https://github.com/block/proto-fleet/pull/295) | Add the edit-event modal component | Edit | Merged |
| [#304](https://github.com/block/proto-fleet/pull/304) | Add the page-header curtailment pill component | Page-header pill | Merged |
| [#311](https://github.com/block/proto-fleet/pull/311) | Wire up the start-curtailment API call | Start | Merged |
| [#314](https://github.com/block/proto-fleet/pull/314) | Remove single-option dropdowns from the curtailment modal | Start modal | Merged |
| [#318](https://github.com/block/proto-fleet/pull/318) | Wire the read-status and history UI to live data | Status/history | Merged |
| [#321](https://github.com/block/proto-fleet/pull/321) | Wire the start and restore action buttons | Start/restore actions | Merged |
| [#329](https://github.com/block/proto-fleet/pull/329) | Wire the update-event API call | Update | Merged |
| [#338](https://github.com/block/proto-fleet/pull/338) | Add a shared active-event polling cache and move the page-header pill onto it | Active polling | Merged |
| [#342](https://github.com/block/proto-fleet/pull/342) | Extract the API event mappers | Refactor | Merged |
| [#344](https://github.com/block/proto-fleet/pull/344) | Add control support to the start modal | Start modal | Merged |
| [#350](https://github.com/block/proto-fleet/pull/350) | Refine the page-header status pill | Page-header pill | Merged |
| [#355](https://github.com/block/proto-fleet/pull/355) | Support multi-status event filters in history (the backend `state_filters` change is tracked under Backend PRs) | History filters | Merged |
| [#361](https://github.com/block/proto-fleet/pull/361) | Share active-event API state between the header and the management hook | Active state, refactor | Merged |
| [#364](https://github.com/block/proto-fleet/pull/364) | Add the ProtoFleet curtailment flow and `/energy` route, reconcile restoring/restored states through history, and clean up detail timestamps | Energy page / route | Merged |

## References

- `proto/curtailment/v1/curtailment.proto`
- `server/internal/domain/curtailment/`
- `server/internal/domain/curtailment/reconciler/`
- `server/internal/domain/curtailment/mqttingest/`
- `server/internal/handlers/curtailment/`
- `server/internal/handlers/middleware/rpc_permissions.go`
- `server/internal/handlers/interceptors/config.go`
- `server/migrations/000042_create_curtailment.*`
- `server/migrations/000050_add_curtailment_event_created_by.*`
- `server/migrations/000051_bound_curtailment_operational_controls.*`
- `server/migrations/000055_add_curtailment_event_list_index.*`
- `server/migrations/000056_scope_curtailment_idempotency_to_non_terminal.*`
- `server/migrations/000057_scope_curtailment_idempotency_to_non_terminal.*`
- `server/migrations/000058_scope_curtailment_external_ref_to_non_terminal.*`
- `server/migrations/000059_scope_curtailment_external_ref_to_non_terminal.*`
- `server/migrations/000062_seed_curtailment_ingest_permission.*`
- `server/internal/infrastructure/mqttclient/`
- `server/migrations/000076_create_curtailment_mqtt_source.*`
- `server/sqlc/queries/curtailment_mqtt.sql`
- `client/src/protoFleet/api/useCurtailmentApi.ts`
- `client/src/protoFleet/features/energy/`
- `client/src/protoFleet/components/PageHeader/CurtailmentPill.tsx`

External background preserved from the v2 planning doc:

- [What Is Demand Response? - Hashrate Index](https://hashrateindex.com/blog/what-is-demand-response/)
- [Bitcoin Mining as a Load Resource in ERCOT - OBM](https://obm.io/blog/ercot-load-resource/)
- [Load Resource Participation in ERCOT - ERCOT.com](https://www.ercot.com/services/programs/load/laar)
- [ERCOT Developer Portal - Market Transaction Messages](https://developer.ercot.com/applications/ews/Market%20Transaction%20Messages/Current%20Operating%20Plan%20(COP)/)
- [OpenADR 2.0b Profile Specification](https://cimug.ucaiug.org/Projects/CIM-OpenADR/Shared%20Documents/Source%20Documents/OpenADR%20Alliance/OpenADR_2_0b_Profile_Specification_v1.0.pdf)
- [OpenADR 3.0 Webinar Slides](https://www.openadr.org/assets/OpenADR%20Webinar%20Nov%202023%20-%20From%202.0%20to%203.0.pdf)
- [OpenADR 3.0 Event resource](https://developer.switchmarket.se/openadr-3/api-reference/events)
- [Voltus Webhooks tutorial](https://api.voltus.co/docs/tutorials/create-an-integration-with-webhooks)
- [Voltus REST Dispatch integration](https://api.voltus.co/docs/tutorials/create-a-rest-dispatch-integration)
- [LuxOS LUXminer Curtail API](https://docs.luxor.tech/firmware/api/luxminer/curtail)
- [OpenLEADR reference implementation](https://github.com/OpenLEADR/openleadr-rs)
