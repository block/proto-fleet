---
title: "Scope alert rules to subsets of miners"
date: 2026-08-06
status: implementing
type: tdd
tracker:
---

# Scope alert rules to subsets of miners

## Context

User-created alert rules (offline / hashrate / temperature) are always
org-wide. The phase-1 user-rules plan
(`docs/plans/2026-07-14-user-created-alert-rules-plan.md`) deliberately
deferred sub-org scoping, and maintenance windows still reject `site`/`group`
scopes for the same underlying reason: alert instances only carry
`organization_id` and `device_id` labels
(`server/internal/domain/alerts/service.go:864`).

The mechanics are cheaper than they look:

- User rules are compiled server-side into raw SQL against the
  `notification_metric_sample` hypertable
  (`server/internal/domain/alerts/user_rules.go`), and that table already has
  a populated `site_id` column and a `device_id` column. Site- and
  device-list scoping are pure predicate changes; no new views, grants, or
  migrations.
- Each user rule is its own Grafana rule with its own `rule_uid`, and channel
  routing (`alert_route_policy`) is keyed on `rule_uid`. Scoping happens at
  firing time, so the webhook handler and `Deliverer` routing logic are
  untouched.
- `RuleConfig` originally round-tripped as JSON through the
  `proto_fleet_config` Grafana annotation. Security review showed Grafana
  copies rule annotations onto every alert instance, so a large scope (up to
  600 ids) replicated per firing device could push a notification batch past
  the webhook's 32 MiB body cap and drop the org's whole batch — configs now
  persist server-side instead (see Design).

For the UI, the curtailment start modal
(`client/src/protoFleet/features/energy/CurtailmentStartModal.tsx:1918-1945`)
already has the exact interaction we want: an "Apply to" section of stacked
`TargetSelectButton` rows (Sites, Miners), each opening a picker modal —
a checkbox site list with a select-all footer, and `MinerSelectionModal`
(the shared `MinerSelectionList` wrapper from
`features/settings/components/Schedules/`). This TDD reuses that screen
pattern and those pickers directly.

## Goals

- Users can scope a user-created alert rule to any union of sites, buildings,
  racks, groups (all evaluated against current membership), and explicitly
  selected miners; unscoped rules stay org-wide.
- "All sites" is a live flag (covers future sites), and the miner picker's
  "select all" round-trips as true org-wide scope — no materialized snapshots.
- The Add/Edit Rule surface is the full-screen two-pane view from the Figma
  design (Details / condition sentence / delivery / Apply to on the left, a
  live summary pane on the right), reusing the curtailment layout components.
- The rules table shows each rule's scope ("All miners", "All sites",
  "2 sites, 1 group, 5 miners").
- Scoped rules fire only for matching miners; placement membership is
  resolved at evaluation time via an owner-privilege view, so moved miners
  join/leave the scope immediately and stale pre-move samples can never fire
  or hold open an alert.

## Non-goals

- Scoping the provisioned default rules (`proto-fleet-rules.yaml`) — they are
  one-copy-per-fleet YAML and cannot be per-org-per-site. Scoped alerts are
  user rules only.
- Unblocking `site`/`group` maintenance-window scopes. That requires site
  labels on *all* rules (including provisioned ones) and carries fingerprint
  churn; separate change once this lands.
- A "matching miners" live preview (`PreviewRule` RPC) — already noted as a
  follow-up in the phase-1 plan.
- Any change to delivery routing, silences, or the pause mechanism.

## Design

### Scope shape (proto)

Add to `proto/alerts/v1/alerts.proto` and a `scope` field on `RuleConfig`
(next free field number):

```proto
message RuleScope {
  repeated int64 site_ids = 1;
  repeated string device_ids = 2;
  repeated int64 building_ids = 3;
  repeated int64 rack_ids = 4;
  repeated int64 group_ids = 5;
  bool all_sites = 6;  // live "any site" (excludes unassigned); supersedes site_ids
}
```

- Absent/empty scope = org-wide (backward compatible with every existing
  rule).
- Multiple dimensions set = union, matching curtailment semantics ("these
  miners and the miners in these placements").
- `all_sites` is the true-"all" representation: the pickers never materialize
  today's id list, so sites created later are covered and >100-site orgs can
  still save. The miner picker's "select all" maps to org-wide (empty scope)
  and round-trips as such.
- Deliberately *not* `MinerListFilter`/`DeviceSelector`: those express
  predicates (model, firmware, hashrate ranges) that the placement view does
  not carry. A narrow message keeps the contract honest.

### Server

- `models.go`: add `Scope` to the domain `RuleConfig`. Configs persist in a
  new `alert_rule_config (org_id, rule_uid, config JSONB)` table (migration
  `000135`) following the `alert_route_policy` lifecycle: rows written before
  rule create (inert without the rule, cleaned up if the create provably
  failed), upserted before the update PUT (restored on PUT failure), deleted
  with the rule. Reads overlay store rows in `attachConfigs` (fail-closed,
  like routing). New and updated rules carry no config annotation at all, but
  rules created before the table (the annotation flow shipped in v0.2.9 behind
  the alerts beta flag) still hold their config only in the legacy
  `proto_fleet_config` annotation: reads fall back to it when the store has no
  row (validated, scope-less so it cannot bloat alert instances), and the
  rule's first successful update migrates it into a store row and strips the
  annotation.
- Validation (Create/Update in `service.go`): site ids must be org-owned
  (site store lookup, formatted server-side as int64); device ids must match
  the existing `deviceIDPattern` / `maxDeviceIDLength` used by
  maintenance-window device scopes, capped at 500; cap sites at 100.
- Placement resolution: migration `000134` adds `fleet_device_placement`, an
  owner-privilege view over `device` / `device_set_membership` /
  `device_set_rack` (one row per device × group; rack-derived building wins),
  with a `GRANT SELECT` + smoke check in `run-fleet.sh` following the
  `fleet_pollable_device_presence` pattern.
- SQL compilation (`user_rules.go`): every template gains an optional scope
  filter in WHERE,

  ```sql
  AND device_id IN (
    SELECT device_id FROM fleet_device_placement
    WHERE org_id = 7
      AND (device_id IN ('a-1') OR site_id IN (12) OR building_id IN (3) OR rack_id IN (4) OR group_id IN (5))
  )
  ```

  Both dimensions go through the semijoin so only CURRENT membership of LIVE
  devices matches at eval time — filtering rows on emit-time `site_id` would
  let up-to-10-minute stale pre-move samples fire or hold open an alert, and
  a direct device-id filter would let a soft-deleted miner's retained samples
  (deletion does not purge them) keep matching for the eval window. The planner evaluates
  the subquery once as a hashed subplan (verified against dev TimescaleDB —
  the `last()` + join pushdown landmine documented in
  `proto-fleet-system-rules.yaml` does not trip for this shape). Scoped rules
  also add a `site_id` column resolved from `fleet_device_placement` (current
  placement, not the sample's emit-time stamp, which goes stale after a move
  and never refreshes for an offline miner) so instances carry
  the site label; unscoped rules compile byte-identical to today, so existing
  rules see zero label/fingerprint churn. The injection contract holds:
  placement ids are server-formatted integers, device ids are regex-validated
  before quoting.
- Webhook handler (`handlers/alertmanagerwebhook/handler.go`): unchanged —
  scoped rules never carry a config annotation (legacy annotations are
  scope-less), so there is nothing scope-sized for Grafana to replicate onto
  instances. Labels store as-is, so `site_id` lands
  in `notification_history.labels`.
- No changes to `deliver.go`, routing, or Grafana provisioning.

### Client

- Types + mappers: `RuleScope { siteIds: string[]; deviceIds: string[] }` in
  `features/alerts/types`, proto mapping in `alertsApi.ts`.
- New `useAlertScope()` hook cloned from `useDeliveryRouting.ts` (same
  `reset(scope)` / `toRuleScope()` contract; no validate/sessionKey — an empty
  scope is valid org-wide and the pickers remount per open) so the create and
  edit flows share scope state the way they share routing.
- `AddRuleModal.tsx` is the full-screen two-pane view from the Figma design
  (node 532:41644), built on `FullScreenTwoPaneModal` like curtailment:
  header with close + Save, left pane sections Details (Name + Type),
  condition sentence rows, "Then send notification to" (`DeliveryPicker`,
  create only; the design's Notify dropdown is deliberately omitted), and
  "Apply to" with five `TargetSelectButton` rows — Sites, Buildings, Racks,
  Groups, Miners — opening the Schedules pickers, gated per permission like
  `ScheduleModal` (site:read for sites/buildings, rack:read for racks/groups).
  The right pane is the design's summary panel: the trigger sentence plus a
  live "Applies to" description. `SiteSelectionModal` gains an optional
  `onSaveSelection`/`initialAllSelected` pair so "All sites" persists as the
  live flag. Pickers stack above the full-screen modal, curtailment-style.
  Unlike delivery (create-only), scope is editable in edit mode since it
  lives on `RuleConfig` and flows through `UpdateRule`.
- `RulesSection.tsx`: add an "Applies to" column formatted like
  `MaintenanceWindowsSection.formatTarget` ("All miners", "2 sites",
  "5 miners", "2 sites and 5 miners"). Counts only — name resolution would
  add a sites fetch to the table for little gain; the edit modal's pickers
  show names.

### Behavior notes

- `site_id` on `notification_metric_sample` is stamped at emit time from
  `device.site_id`, so site scope tracks reassignment automatically with
  sample-level latency.
- A rule scoped only to since-deleted sites/miners silently matches nothing.
  The scope column keeps the scope visible as counts (name/liveness
  resolution would need a sites fetch); update validation tolerates stored
  site ids so the dead reference never blocks unrelated edits. Active
  revalidation is out of scope.

## Alternatives considered

- **Rich filter scope (`MinerListFilter` / `DeviceSelector`).** Rejected:
  most filter dimensions don't exist on the metrics table, so the UI would
  promise scoping the SQL can't deliver. Revisit if a placement view lands.
- **Resolve sites to device-id lists at save time.** Rejected: snapshots go
  stale as miners move/pair; live `site_id` predicate is strictly better.
- **Group/device-set scoping now.** Deferred: needs an owner-privilege
  membership view, a `run-fleet.sh` grant + smoke check, and a semijoin that
  must dodge the documented TimescaleDB planner bug. Curtailment's
  `deviceSet` scope type similarly ships without a picker today.
- **Extract a shared ApplyTo component from `CurtailmentStartModal`.**
  Rejected per repo convention (scoped fixes over global refactors): the
  reusable parts (`TargetSelectButton`, the picker modals) are already
  shared; the section itself is ~20 lines of layout.
- **Scope-aware delivery routing (label matchers in `routeAlerts`).** Not
  needed: scoping at firing time keeps routing keyed on `rule_uid` and
  preserves its fail-open/fail-closed semantics.

## Risks

- **Query cost.** Migration `000120` dropped the
  `(metric, organization_id, device_id, time)` index, so scope predicates
  ride `(metric, time DESC)` + chunk exclusion. Fine at 7-day retention /
  1h chunks; benchmark with a representative fleet before raising scope
  caps.
- **Alert identity churn.** Avoided for existing rules (unscoped compile is
  unchanged). Toggling scope on an existing rule changes its instance labels
  → fingerprints → `notification_active` keys; a firing alert re-fires under
  the new identity. Acceptable and user-initiated; document in the UI copy
  if it proves confusing.
- **Rule-count pressure.** Scoping encourages one-rule-per-subset;
  `maxUserRulesPerOrg = 50` is the knob to bump if orgs hit it.
- **Silent empty scope** (deleted sites/miners): the counts-only scope
  column shows that a scope exists but not that it is dead; a `PreviewRule`
  RPC is the durable fix.
- **Rollback:** revert is safe — the scope field simply stops being read;
  already-compiled scoped rules keep their predicates in Grafana until
  edited or deleted, and can be cleared by setting scope back to org-wide.

## Test plan

- **Server unit:** golden SQL tests in `user_rules` for site-only,
  device-only, union, and empty scopes, asserting the unscoped output is
  byte-identical to today; validation tests for cross-org site ids, invalid
  device ids, and caps.
- **Server integration** (`DB_PASSWORD=fleet`, `-p 1`): create/update/list a
  scoped rule end-to-end; assert the compiled Grafana rule and the
  round-tripped scope; webhook handler test asserting `site_id` label lands
  in history.
- **Manual against `just dev`:** scoped rule fires for a matching fake
  miner and stays silent for a non-matching one (drive
  `fake-antminer` offline in/out of the scoped site).
- **Playwright E2E** (`client/e2eTests/protoFleet/spec/alerts.spec.ts`):
  create a rule via the Apply-to pickers, verify the scope column, edit the
  scope, verify persistence — as a follow-up PR per the E2E convention.
