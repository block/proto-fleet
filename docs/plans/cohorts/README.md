---
title: "Cohorts implementation — overview & shared context"
date: 2026-06-19
status: draft
type: plan
---

# Cohorts implementation — overview

This folder breaks the [Cohorts TDD](../2026-06-19-cohorts-fleet-enforcement-tdd.md) into
**per-phase implementation briefs**, one per file, each meant to be handed to a separate coding agent.

**How to use:** give an agent (1) the phase file it owns, (2) **this README**, and (3) the TDD. The
phase file is the task; this README is the shared context (model, template, scaffolding checklist,
testing, conventions); the TDD is the deep design rationale.

Phases (each gates the next):

```
phase-0 foundations  →  phase-1 lease & visibility  →  phase-2 continuous enforcement  →  phase-3 power
   (compiles, wired)        (MVP: reserve/release)         (firmware/pools/cooling drift)     (power dim)
```

| Phase | Definition of done |
| --- | --- |
| 0 | `cohort` domain compiles, is wired into `fleetd`, CRUD RPCs round-trip; migrations apply up+down. |
| 1 | Reserve/release/extend work; one-cohort-per-device enforced; expiry auto-releases; UI + `fleetcli` verbs; group→cohort bridge. |
| 2 | A cohort's desired firmware/pools/cooling is continuously enforced; drift corrected; release converges to default. |
| 3 | Power becomes a fourth enforced dimension once every plugin implements `GetPowerTarget`. |

> Rollout/canary, site/building baselines, and Memfault delivery are **out of scope** here — see the
> TDD §Work breakdown (Phase 4) for those deferred directions.

## Branch base & CLI

- **Branch off `kkurucz/port-fleet-client-api`** (the `fleetcli` workstream + ported client API) and
  rebase as it advances. Do not branch from `main`.
- The **CLI is the existing `fleetcli`** at `server/cmd/fleetcli/` — extend it (add a `cmd_cohorts.go`
  next to `cmd_groups.go`/`cmd_racks.go`), do **not** create a new binary. See
  `server/docs/fleet-cli-generation.md` for how commands are generated vs hand-written.

## The model (recap)

A **cohort** is a *desired-state cell*: a set of devices + the firmware/config they should run,
optionally owned, optionally time-bounded. The fleet is **partitioned** — every device is in exactly
one cohort, or implicitly the single **global default cohort**. A reconciler drives each device to its
cohort's desired state and corrects drift. A **reservation is a cohort with an owner + expiry.**

Key consequences:
- **Reset = convergence**: releasing/expiring a cohort moves its devices back to the default cohort,
  which changes their desired state; the same loop converges them. No separate reset path.
- **Exclusivity is the model's invariant** — one `UNIQUE(org_id, device_identifier)` on membership.
- **Enforcement is best-effort/optional** — no desired firmware/config ⇒ no enforcement; a channel
  with no registry match for a device's model ⇒ that device is left alone.
- **Groups (`device_set`) are a separate, overlapping, imperative primitive** and are *not* replaced;
  cohorts can be *created from* a group (freeze its current members). See TDD §"Cohorts vs. groups".

## `curtailment` is the structural template

Clone curtailment's layering rather than inventing one. Mapping:

| Curtailment | File | Cohort analog |
| --- | --- | --- |
| `curtailment_event` | `server/migrations/000042_create_curtailment.up.sql:36` | `cohort` |
| `curtailment_target` | `migrations/000042:141` | `cohort_membership` + `device_enforcement_state` |
| device-exclusivity index | `migrations/000072...` | `UNIQUE(org_id, device_identifier)` |
| reconciler (observe→drift→dispatch→escalate) | `server/internal/domain/curtailment/reconciler/reconciler.go` | cohort enforcement reconciler |
| atomic insert of op+members | `server/internal/domain/stores/sqlstores/curtailment.go:102` (`InsertEventWithTargets`) | atomic `CreateCohort`+members |
| command-exclusivity filter | `server/internal/domain/command/curtailment_active_filter.go` | `CohortMembershipFilter` |
| singleton heartbeat (`CHECK(id=1)`) | `migrations/000042:186` | cohort reconciler heartbeat |
| owner from `session.Info` | `server/internal/handlers/curtailment/handler.go:36` | same |

## Domain scaffolding checklist

Adding the `cohort` domain touches these layers in order. Hand-written unless marked *(generated)*.
Run the codegen step after each source change; commit generated output with its source.

1. **Proto** — `proto/cohort/v1/cohort.proto` (package `cohort.v1`). → *(generated)*
   `server/generated/grpc/cohort/v1/*.pb.go` + `.../cohortv1connect/*.connect.go`,
   `client/src/protoFleet/api/generated/cohort/v1/cohort_pb.ts`. **Cmd:** `just gen`.
2. **Migration** — `server/migrations/000078_create_cohort.{up,down}.sql` (+ a seed migration for
   permissions, e.g. `000079_seed_cohort_permissions.{up,down}.sql`). Both up+down required;
   immutable once merged. Applied locally by `just dev` / in tests by `testutil.GetTestDB`.
3. **sqlc** — `server/sqlc/queries/cohort.sql` (annotated `-- name: X :one|:many|:exec`). →
   *(generated)* `server/generated/sqlc/cohort.sql.go`. **Cmd:** `just gen`.
4. **Domain** — `server/internal/domain/cohort/{service.go,audit.go,metrics.go}`,
   `cohort/models/models.go`, `stores/interfaces/cohort.go`, `stores/sqlstores/cohort.go`
   (+ `//go:generate mockgen` → *(generated)* `mocks/`). **Cmd:** `just gen-go` (mockgen).
5. **Handler** — `server/internal/handlers/cohort/{handler.go,translate.go}`.
6. **Authz** — add `PermCohortRead`/`PermCohortManage` + `ResourceCohort` to
   `server/internal/domain/authz/catalog.go` (constants + `catalog` list + `AllPermissions`); update
   `catalog_test.go` (completeness + resource-order tests); seed via the permissions migration.
7. **Wiring** — `server/cmd/fleetd/main.go`: store (~`:441`), service (~`:446`), command filter
   (~`:462`), reconciler Start/Stop (~`:474`, phase 2), `mux.Handle(cohortv1connect...)` (~`:563`),
   service-name registry (~`:159`), `SessionOnlyProcedures` for admin RPCs if any.
8. **Client** — `api/clients.ts` (add `createClient(CohortService, transport)`), a hand-written
   `api/useCohortApi.ts` hook, `features/cohorts/` (pages/components/hooks), and the route trio
   (`routePrefetch.ts` factory+tier, `router.tsx` lazy+route, `config/navItems.ts`).
9. **CLI** — `server/cmd/fleetcli/cmd_cohorts.go` (+ regen `generated_runtime.go` if generated).
10. **Final:** `just gen` (no-op after commit), `go work sync` if deps changed, `just lint`,
    `just format`, the relevant tests (below).

## Testing matrix

| Layer | How | Command |
| --- | --- | --- |
| Domain service | in-memory `fakeStore` (no DB), table tests | `cd server && go test ./internal/domain/cohort/...` |
| Reconciler | `fakeStore` + `fakeDispatcher`, override `now()` for deterministic ticks (see `curtailment/reconciler/reconciler_test.go`) | `cd server && go test ./internal/domain/cohort/reconciler/...` |
| sqlstore / handler | **real Postgres** via `testutil.GetTestDB` / `NewServiceProvider` / `NewInfrastructureProvider` (sets up httptest server + session/auth) | `cd server && DB_PASSWORD=fleet go test ./internal/handlers/cohort/...` |
| Migrations | applied + reversed automatically by `GetTestDB`; verify up+down by hand with `just db-migrate` / reset | `cd server && DB_PASSWORD=fleet go test ./...` |
| E2E | Playwright page objects + specs against **fake rigs** (`E2E_TARGET=fake`) | `just test-e2e-fleet` |
| Local enforcement | `just dev` (brings up fake-proto-rig); set desired FW on a cohort; watch the fake rig receive the command | `just dev` |
| Lint / typecheck | golangci-lint + goimports (server), tsc + eslint (client), buf lint (proto) | `just lint`, `cd client && npm exec --no -- tsc --noEmit` |
| Plugin contract | only for the SDK change in phase 3 | `just test-contract` |

> **Do not mock the DB** where a real Postgres is available (AGENTS.md). Domain/reconciler logic uses
> fakes because that logic is DB-agnostic; persistence (sqlstore) and end-to-end (handler) use the
> real DB via `testutil`.

## Conventions & auto-firing skills

Honor these (they fire on the files each phase touches): `proto-regen` (any `.proto`),
`db-generation-hygiene` (sqlc/migrations → `just gen`), `migration-immutability` (never edit a
merged migration), `client-boundaries` (import rules; no new `console.log`), `go-work-sync`,
`lefthook-bypass-guard` (never `--no-verify`), `plugin-contract-tests` + `asicrs-build` (phase 3),
`plan-conventions` (these docs). AGENTS.md rules: generated code is generated; commit proto+generated
together; migrations immutable (up+down); prepared statements only (sqlc); never commit to `main`.

## Naming

- Tables: `cohort`, `cohort_membership` (+ phase-2 substrate: `device_firmware_state`,
  `device_config_state`, `firmware_release`, `device_enforcement_state`).
- Proto: `cohort.v1.CohortService`. Domain package: `cohort`. Actor: `session.ActorCohort`.
- Permissions: `cohort:read`, `cohort:manage`. Resource: `cohort`.

## Links

- TDD: [`../2026-06-19-cohorts-fleet-enforcement-tdd.md`](../2026-06-19-cohorts-fleet-enforcement-tdd.md)
- Superseded external-CLI proposal: see the TDD's "Supersedes" note.
