---
title: "Alerts: user-created rules from existing metrics with user-specified constants"
date: 2026-07-14
status: draft
type: plan
tracker: https://github.com/block/proto-fleet/pull/746
---

# Alerts: User-Created Rules from Existing Metrics

## Problem

The production Alerts page (`/settings/alerts`) is read-only for rules: they
are YAML-provisioned into Grafana (`server/monitoring/grafana/provisioning/
alerting/proto-fleet-rules.yaml`), thresholds are hardcoded in the rule SQL,
and `RuleService` exposes only List/Pause/Resume. The "Alerts (Design)"
prototype (`client/src/protoFleet/features/notifications`) demonstrates the
target UX: users create rules by picking a metric template (Offline,
Hashrate, Temperature) and supplying constants — threshold value, unit, and
sustain duration — with a live natural-language summary. This plan covers
graduating exactly that slice to production: **create/edit/delete alerting
rules based on the existing metric templates, parameterized by
user-specified constants.**

## Scope

In scope:

- New `RuleService` RPCs: `CreateRule`, `UpdateRule`, `DeleteRule`.
- Three templates, mirroring the design prototype's picker
  (`AddRuleModal.tsx:58`) and mapping onto today's provisioned rules:
  - **Offline** — constants: sustain duration only (design:
    `OfflineThresholdField`).
  - **Hashrate** — constants: mode (`% of expected` on
    `fleet_device_hashing` | `absolute` on `fleet_device_hashrate_terahash`),
    value, unit (`%` | `TH/s` | `PH/s`), duration. Comparator fixed `<`
    (design: `HashrateThresholdField`).
  - **Temperature** — constants: value, unit (`°C` | `°F`, normalized to °C
    server-side), duration, on `fleet_device_temperature_max_celsius`.
    Comparator fixed `>` (design: `TemperatureThresholdField`).
- Server-side compilation of template + constants into a Grafana alert rule
  (SQL against `notification_metric_sample`, `for:` duration, org labels),
  created via the Grafana provisioning API.
- Client: "Add rule" flow on the production Alerts page, adapted from the
  prototype's `AddRuleModal`; edit/delete for user-created rules only.
- Gated on the existing `alert:manage` permission (catalog description
  updated).

Out of scope (deliberately, per the narrowed ask):

- Sub-org scoping ("Apply to" sites/buildings/racks/groups/miners). Phase 1
  rules are org-wide, consistent with maintenance windows rejecting
  group/site scopes as "not yet supported" (`domain/alerts/service.go:759`).
- Per-rule channel/recipient routing and ticket creation from the prototype
  (`channel_ids`, `recipient_role_ids`, `create_ticket`). Firing rules keep
  today's routing: Alertmanager → notification policies → fleet-api webhook
  → history + org channels.
- Custom PromQL/SQL rules, and the prototype's other templates (pool,
  command_failure, hardware_error, energy).
- "Matching miners" live preview (`sampleMatchingMiners.ts`) — needs a
  telemetry-backed preview RPC; the modal ships with the natural-language
  trigger summary only. A `PreviewRule` RPC is a natural follow-up.
- Editing or deleting the YAML-provisioned default rules; they stay
  read-only (pause/resume via silences, unchanged).

The narrowing is a plan-level decision only: the implementation carries no
"deferred"/"phase 2" comments or TODOs for the out-of-scope items — they
are simply absent.

## Key findings

**A write path exists despite the "no edits" comment.**
`grafana_client.go:77` notes there is no Create/Update/Delete because
Grafana 11.6+ blocks in-place edits — but that restriction applies to
*file-provisioned* rules. Rules created through the provisioning HTTP API
(`POST/PUT/DELETE /api/v1/provisioning/alert-rules[/{uid}]`) with
`X-Disable-Provenance: true` (which our client already sends on writes,
`grafana_client.go:261`) are fully API-editable. User rules therefore live
in their own per-org rule group, disjoint from the provisioned
`proto-fleet-defaults` group. Grafana state is Postgres-backed with a
persistent volume (`docker-compose.alerts.yaml`), so API-created rules
survive container recreation and re-provisioning.

**Rules are template + constants, never raw SQL from the client.** The
server owns a per-template SQL compiler; the proto carries only the enum +
validated numeric constants. This keeps injection surface at zero (all
interpolated values are server-validated numbers; `organization_id` is
taken from the authenticated context, not the request) and matches the
prototype UX where the user never sees a query. The provisioned YAML rules
are the compiler's reference output (e.g. `proto-fleet-rules.yaml:57-101`
is exactly "hashrate, pct_expected, 0.75, 10m").

**Round-tripping for edit.** The `Rule` proto has no threshold fields today;
`ListRules` reconstructs domain rules from Grafana labels/annotations
(`service.go:960`). Rather than parsing constants back out of generated SQL,
the canonical config is stored verbatim in a rule annotation
(`proto_fleet_config`, JSON), and an origin label
(`proto_fleet_origin: user`) plus the existing `organization_id` label mark
which rules are editable and by whom. `ruleVisibleToOrg`
(`service.go:944-958`) already handles org-labeled rules; visibility needs
no change.

## Implementation units

### U1. Proto surface

`proto/alerts/v1/alerts.proto`: add `CreateRule`/`UpdateRule`/`DeleteRule`
to `RuleService`; add a `RuleConfig` message — `template`, `name`,
`duration_seconds`, and a oneof: `OfflineConfig{}` (empty),
`HashrateConfig{mode, value, unit}`, `TemperatureConfig{max_celsius}`.
Extend `Rule` with `config` (populated for user rules) and `origin`
(`PROVISIONED | USER`) so the client can gate row actions and prefill the
edit modal. Severity is fixed per template, derived server-side, and not
part of `RuleConfig` (decided; matches the provisioned defaults).
Regenerate via `just gen` (proto-regen skill).

### U2. Grafana client write methods

`domain/alerts/grafana_client.go`: `CreateAlertRule`, `UpdateAlertRule`,
`DeleteAlertRule` against `/api/v1/provisioning/alert-rules`, plus
folder ensure-or-resolve via the folders API: one dedicated folder per org
(decided; server-managed UID derived from the org id), keeping the
provisioned "Proto Fleet" folder untouched and making rule ownership
visible in Grafana itself. Server-generated rule UIDs (`pfu-<random>`);
per-org rule group `proto-fleet-user-<org>` at 30s interval. Update the `grafana_client.go:77` comment to state the
provisioned-vs-API distinction.

### U3. Domain: compiler, validation, guards

`domain/alerts/`: a template compiler mapping `RuleConfig` → Grafana rule
body (rawSql per template with `WHERE organization_id = $org` added — user
rules must not fire for other orgs' devices, unlike shared defaults which
group by org), `for:` duration, labels (`organization_id`,
`proto_fleet_origin: user`, `severity`, `template`, `rule_group`), and
annotations (`summary`/`description` generated in the style of
`formatRuleSummary`, `proto_fleet_config` JSON). Golden tests: compiled
output for each template matches the provisioned YAML shape.

Validation (server-side, mirrored client-side): name required/length-capped;
duration 60s–24h; temperature 0–150 °C after unit conversion; hashrate pct
in (0, 100], absolute > 0 with unit normalization to TH/s; per-org rule
quota (e.g. 50) to bound Grafana evaluation load. Update/Delete guard:
target rule must carry `proto_fleet_origin: user` AND the caller's
`organization_id` label — provisioned rules and other orgs' rules are
NotFound. `grafanaRuleToDomain` learns to surface `config` from the
annotation and `origin` from the label.

### U4. Handler + permissions

`handlers/alerts/handler.go`: three new RPCs gated on `PermAlertManage`
(consistent with existing mutations). `catalog.go:171`: extend the
`alert:manage` description to include creating, editing, and deleting alert
rules. Authz tests for read-only callers and cross-org attempts.

### U5. Client UI

`client/src/protoFleet/features/alerts/`: adapt (copy, don't import — the
prototype feature is demo-backed) the prototype's `AddRuleModal` skeleton
and the three threshold fields into the production feature, wired to the
new RPCs via `alertsApi.ts`/`useAlerts`. No Zustand: the prototype's
`notificationsStore` does not come along — state stays in the production
feature's existing pattern (local component state + `useAlerts`/
`AlertsContext`). `RulesSection`: "Add rule" button
(alert:manage), Edit/Delete row actions for `origin == USER` rows only, an
origin column/badge ("Default" vs "Custom"), and copy update — rules are no
longer purely "managed by ops" (`RulesSection.tsx:158-174`). Right-pane
preview reduced to the trigger-summary sentence + severity (no synthetic
miner sample). Unit conversion display logic reused from the prototype's
`UNIT_TO_SECONDS` helpers.

### U6. Tests, docs, verification

Handler + domain unit tests (compile goldens, validation table, guard
matrix); an integration test against real Grafana in the alerts compose
profile (`just dev-alerts`): create → appears in ListRules with config →
edit → delete; verify a created rule actually fires end-to-end into
`notification_history` via the fake-rig stack. Client component tests for
the modal's per-template state. Playwright coverage per the
proto-fleet-playwright-e2e skill once the feature is merged. Update
`docs/alerts-metrics.md` cross-reference and this plan's status.

## Risks

- **Grafana provisioning-API drift**: the API-created-rule path depends on
  provenance semantics of the pinned Grafana version; the integration test
  in U6 is the guard. A Grafana upgrade that re-blocks disabled-provenance
  writes would strand the feature — pin and test.
- **Routing regressions**: user rules must carry labels that satisfy
  `notification-policies.yaml` routing and the org-visibility filter;
  a mislabeled rule is invisible or noisy. Covered by the guard matrix +
  e2e fire test.
- **Evaluation load**: each rule is a 30s-interval SQL query against the
  hypertable; the per-org quota bounds this, but the quota number should be
  validated against TimescaleDB headroom in the alerts profile.
- **Flapping**: user-chosen short durations flap on transient dips (the
  provisioned hashrate rule deliberately uses 10m, `proto-fleet-rules.yaml:89`).
  Mitigated by the 60s duration floor and per-template default durations
  from the prototype (offline 30m, hashrate/temperature 20m).
- **Pause/resume interaction**: pause silences match on rule labels
  (`[proto-fleet:rule-paused]` marker); editing a paused user rule must not
  orphan its silence — U3 keeps label identity (uid) stable across updates.

## Decisions

- Severity is fixed per template, not user-selectable. The proto's
  `RuleConfig` leaves room to add it compatibly later.
- User rules live in a dedicated per-org Grafana folder, not the
  provisioned "Proto Fleet" folder.
- No Zustand anywhere in the implementation; the production alerts
  feature's hook/context state pattern is the model.
- Out-of-scope items are not referenced in code (no deferral comments or
  TODOs).
