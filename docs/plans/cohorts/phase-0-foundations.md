---
title: "Cohorts Phase 0 — foundations (proto, schema, domain skeleton)"
date: 2026-06-19
status: draft
type: plan
---

# Phase 0 — Foundations

> Read [`README.md`](./README.md) first (model, curtailment template, scaffolding checklist, testing
> matrix, conventions). Deep design is in the [TDD](../2026-06-19-cohorts-fleet-enforcement-tdd.md).

## Context & prerequisites

Stand up the `cohort` domain end-to-end as a **compiling, wired, CRUD-only** vertical — the skeleton
every later phase builds on. No enforcement, no client UI, no CLI yet. Branch off
`kkurucz/port-fleet-client-api` (see README §Branch base).

This phase is pure scaffolding; the cleanest reference is the **`sites`** domain (smallest complete
vertical) for shape, and **`curtailment`** migration `000042` for the table conventions.

## Scope

**In:** the `cohort.v1.CohortService` proto; the `cohort` + `cohort_membership` tables + permission
seed; sqlc queries; the domain service/models/store; the handler; authz catalog entries; `main.go`
wiring; basic CRUD RPCs (`CreateCohort`, `GetCohort`, `ListCohorts`, `DeleteCohort`) returning real
data.

**Out:** allocation semantics, the partition *enforcement path*, expiry, the command filter, the
group→cohort bridge, membership-move authz, enforcement, client, CLI (all phase 1+). Define the table
constraints now (incl. the `UNIQUE` partition index) but the *behaviors* that exercise them land in
phase 1.

## Files to create / modify

**Proto** (`proto-regen` → `just gen`)
- `proto/cohort/v1/cohort.proto` — `CohortService` + messages. Define the full message set now
  (`Cohort`, `CohortMember`, and request/response types per TDD §Interfaces) even though only CRUD is
  wired this phase. *Clone-from:* `proto/sites/v1/sites.proto`, `proto/curtailment/v1/curtailment.proto`.

**Migrations** (`db-generation-hygiene`, `migration-immutability`)
- `server/migrations/000078_create_cohort.{up,down}.sql` — `cohort` + `cohort_membership` per
  TDD §Data model: typed columns + nullable owner/expiry + nullable desired firmware/config; the
  `UNIQUE(org_id, device_identifier)` partition on membership; `is_default` partial-unique-per-org;
  owner-active + expiry indexes; `update_updated_at_column()` trigger; FK `ON DELETE RESTRICT` to org.
  Seed exactly one `is_default` cohort per existing org. *Clone-from:* `000042_create_curtailment.up.sql`
  (typed-vs-JSONB, trigger, partial indexes), `000043_create_site_table.up.sql` (org-scoped table shape).
- `server/migrations/000079_seed_cohort_permissions.{up,down}.sql` — insert `cohort:read`/`cohort:manage`
  `ON CONFLICT DO NOTHING`. *Clone-from:* `000062_seed_curtailment_ingest_permission.up.sql`.
- Re-confirm `000078`/`000079` are still the next free numbers before writing (`ls server/migrations/`).

**sqlc** (`just gen`)
- `server/sqlc/queries/cohort.sql` — CRUD + membership insert/delete/list + "resolve a device's
  effective cohort (membership row, else default)" + "list default-cohort devices (no membership row)".
  *Clone-from:* `server/sqlc/queries/device_set.sql`, `curtailment.sql`.

**Domain**
- `server/internal/domain/cohort/models/models.go` — `Cohort`, `CohortMember`, params structs, a typed
  `CohortState` string. *Clone-from:* `curtailment/models/models.go`, `sites/models/models.go`.
- `server/internal/domain/cohort/service.go` — `NewService(store, opts...)` + CRUD methods.
- `server/internal/domain/cohort/{audit.go,metrics.go}` — activity-log emit + NoOp metrics.
  *Clone-from:* `curtailment/audit.go`, `curtailment/metrics.go`.
- `server/internal/domain/stores/interfaces/cohort.go` — `CohortStore` interface.
- `server/internal/domain/stores/sqlstores/cohort.go` — sqlc-backed impl (+ `//go:generate mockgen`).
  *Clone-from:* `stores/interfaces/curtailment.go`, `stores/sqlstores/curtailment.go`.

**Handler**
- `server/internal/handlers/cohort/{handler.go,translate.go}` — assert
  `cohortv1connect.CohortServiceHandler`; gate via `middleware.RequirePermission`; proto↔domain in
  `translate.go`. *Clone-from:* `handlers/curtailment/{handler.go,translate.go}`, `handlers/sites/`.

**Authz**
- `server/internal/domain/authz/catalog.go` — `PermCohortRead`/`PermCohortManage`, `ResourceCohort`,
  catalog entries, `AllPermissions`. `catalog_test.go` — add to completeness + resource-order tests.

**Wiring**
- `server/cmd/fleetd/main.go` — imports; `cohortStore := sqlstores.NewSQLCohortStore(conn)`;
  `cohortSvc := cohortDomain.NewService(cohortStore, cohortDomain.WithAuditLogger(activitySvc))`;
  `mux.Handle(cohortv1connect.NewCohortServiceHandler(cohortHandler.NewHandler(cohortSvc), li))`; add
  to the service-name registry. (Filter/reconciler come in phases 1/2.)

## Key implementation notes

- Default cohort is **sparse**: do not write membership rows for it; "no membership row" ⇒ default.
  The seed creates the `cohort` row (`is_default=true`, `owner_user_id NULL`); its members are
  computed (`device LEFT JOIN cohort_membership WHERE membership IS NULL`).
- Put the `UNIQUE(org_id, device_identifier)` constraint on `cohort_membership` now even though
  allocation lands in phase 1 — it's the partition invariant and later code relies on it.
- Keep `desired_firmware_*` / `desired_config_jsonb` columns nullable and **unused** this phase.
- Owner identity comes from `session.Info` via `RequirePermission` — see `handlers/curtailment/handler.go:36`.

## Acceptance criteria

- `just gen` produces the generated Go/TS/sqlc and is a **no-op** when re-run after commit.
- `000078` and `000079` apply cleanly **up and down**.
- `CreateCohort` → `GetCohort`/`ListCohorts` round-trips real rows; `DeleteCohort` soft-deletes.
- The authz catalog completeness test passes with the new permissions.
- `just dev` boots `fleetd` with the new service registered.

## Verification

```bash
just gen && git status            # generated files present; re-run no-op
just lint
cd server && DB_PASSWORD=fleet go test ./internal/domain/authz/... ./internal/handlers/cohort/...
just dev                          # boots; CohortService reachable
```

## Open questions

- Final RPC set/shape for `CohortService` — settle the full proto now (later phases add behavior, not
  new messages, ideally). Validate against TDD §Interfaces.
- Whether `DeleteCohort` and `ReleaseCohort` are distinct or one RPC (release = members→default +
  soft-delete). Lean: keep `DeleteCohort` (admin/empty) minimal here; full `ReleaseCohort` in phase 1.
