---
title: "Cohorts — one-pager"
date: 2026-06-19
status: draft
type: plan
---

# Cohorts — one-pager

**TL;DR.** Cohorts make rig **reservation** and fleet-wide **firmware/config enforcement** a
first-class Proto Fleet feature. A *cohort* is a set of devices plus the firmware/config they should
run — optionally owned, optionally time-bounded — that the server continuously keeps the devices
matched to. A **reservation is just a cohort with an owner and an expiry.**

## Problem

~160 mining rigs across dev sites are shared by developers, CI, and AI agents for firmware testing.
There's no coordination: people collide over rigs, rigs are left in unknown firmware/config states
after a test, and there's no cross-site view of what's free. We want atomic reservations, automatic
return to a known-good state on release, and continuous "stay on the target build/config" enforcement —
without standing up new infrastructure.

## The model

- A **cohort** = (a set of devices) + (optional desired firmware) + (optional desired config) +
  (optional owner) + (optional expiry).
- The fleet is **partitioned**: every device is in exactly one cohort, or implicitly the single
  **global default cohort**. Exclusivity is one DB constraint.
- A background **reconciler** drives each device to its cohort's desired state and corrects drift —
  the same pattern the shipped **curtailment** feature already uses for power.
- **Reset = convergence**: releasing/expiring a cohort moves its devices back to the default cohort,
  and the same loop converges them. No separate "reset" path.
- **Best-effort**: a cohort with no desired firmware/config enforces nothing.
- **Groups stay separate**: `device_set` groups remain the overlapping, ad-hoc organizing tool; you
  can *create a cohort from a group* (freeze its current members), but cohorts aren't groups.

## What's being built (phases)

| Phase | Scope |
| --- | --- |
| **0 — Foundations** | `cohort` + `cohort_membership` schema, proto/RPCs, domain/handler, authz, wiring. |
| **1 — Lease & visibility (MVP)** | Atomic reserve/release/extend, one-cohort-per-device, expiry auto-release, command exclusivity (don't let others command your reserved rigs), group→cohort bridge, web UI, `fleetcli` verbs. |
| **2 — Continuous enforcement** | Observe each device's current firmware/config; re-apply on drift (firmware, pools, cooling); reset-on-release via convergence. |
| **3 — Power enforcement** | Add a power/performance-mode getter to the device SDK + plugins; enforce power as a fourth dimension. |

**Deferred (not now):** progressive rollout/canary, site/building-scoped baselines, Memfault delivery.

## Status (as of this review)

- **Phases 0 & 1: implemented and compiling** (~90%). Schema, atomic allocation, exclusivity, expiry
  sweeper, the command filter, the group→cohort bridge, the web feature, and `fleetcli` verbs are all
  in place; cohort unit tests pass.
- **In-flight remediation** before phase 1 is "done": enforce a real admin check on the admin
  override RPCs; make the CLI's "none-available" exit code consistent; move reserve-by-count selection
  server-side; populate the per-member site snapshot; tighten a couple of transaction/authz edges; and
  bring the web UI to full fidelity (create modal + detail page).
- **Phases 2 & 3: planned**, not started.

## Key decisions

- **In-server, not an external CLI** — state lives in Proto Fleet's Postgres; background work runs in
  the existing `fleetd` process. No external cron, no second datastore.
- **Modeled on `curtailment`** — reuse the proven reservation-shaped pattern (owned operation over a
  device scope, reconciler with drift detection, audit, exclusivity filter).
- **The CLI is `fleetcli`** — extended with cohort verbs; agent-friendly (`--json`, stable exit codes).
- **Cross-site is a query**, not a fleet of servers (`device.site_id` column).

## Interfaces

- **Web UI** — reserve rigs, see who has what, browse/release cohorts.
- **`fleetcli`** — `reserve` / `release` / `extend` / `my` / `rigs` / `status` for humans and AI agents.
- **Connect RPCs** — `cohort.v1.CohortService` for any client.

---
*Details: see the [TDD](../2026-06-19-cohorts-fleet-enforcement-tdd.md) and the per-phase briefs in this folder.*
