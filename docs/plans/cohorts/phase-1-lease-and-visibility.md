---
title: "Cohorts Phase 1 — lease & visibility (MVP)"
date: 2026-06-19
status: draft
type: plan
---

# Phase 1 — Lease & visibility (MVP)

> Read [`README.md`](./README.md) first. Deep design: [TDD](../2026-06-19-cohorts-fleet-enforcement-tdd.md)
> §Lifecycle & operations, §Exclusivity & authorization, §Interfaces.

## Context & prerequisites

**Prerequisite:** Phase 0 merged (the `cohort` domain compiles, CRUD RPCs round-trip, tables exist).
Branch off `kkurucz/port-fleet-client-api`.

This phase makes cohorts *useful* without continuous enforcement: reserve a set of rigs atomically,
see who has what, release/expire back to the default cohort, and keep one cohort per device. Desired
firmware/config may be *recorded* on a cohort but is only applied via existing one-shot deploy
commands — the continuous reconciler is phase 2. This is the largest phase (server + client + CLI); it
can be split into PRs along those seams.

## Scope

**Server (in):** atomic all-or-nothing allocation; mutable membership (add/remove) with authz-on-move;
`ReleaseCohort` (members→default, soft-delete); expiry sweeper; `CohortMembershipFilter`; audit;
visibility (`GetMyCohorts`, `ListCohorts`, `ListDevices` with effective cohort + state); the
**group→cohort bridge** (`CreateCohort(source_device_set_id)`).
**Client (in):** `features/cohorts/` (list, create incl. from-group, detail, my, device view), the api
client + hook, and the route trio.
**CLI (in):** `fleetcli` cohort verbs.

**Out:** continuous firmware/config drift correction + the substrate (phase 2); power (phase 3).

## Files to create / modify

### Server
- `server/internal/domain/cohort/service.go` — add: `Reserve` (atomic), `AddDevices`/`RemoveDevices`,
  `ReleaseCohort`, `Extend`, `GetMyCohorts`, `ListDevices`, and `CreateCohort(source_device_set_id)`.
  Inject the device-set/`collection` store + a `DeviceIdentifierResolver` for the bridge.
- `server/internal/domain/stores/sqlstores/cohort.go` (+ interface + sqlc) — `InsertCohortWithMembers`
  (one tx), `ListDefaultCohortDevices` (candidates), `ListActiveOwnedCohortsForUser`, membership
  move/delete, expiry sweep query. *Clone-from:* `sqlstores/curtailment.go:102` (`InsertEventWithTargets`).
- `server/internal/domain/command/cohort_membership_filter.go` — `CohortMembershipFilter`
  (blocks commands on an owned-cohort device from a non-owner). *Clone-from:*
  `command/curtailment_active_filter.go`.
- `server/internal/domain/session/models.go` — add `ActorCohort` (beside `ActorCurtailment`); add to
  the `RequirePermission` allowlist + `ActorType.Valid()`.
- `server/internal/handlers/cohort/{handler.go,translate.go}` — wire the new RPCs; map the allocation
  unique-violation → Connect `AlreadyExists`.
- `server/cmd/fleetd/main.go` — `commandSvc.RegisterFilter(commandDomain.NewCohortMembershipFilter(cohortStore))`
  (~`:462`); add `cohortSvc.SweepExpired(cleanupCtx)` to the cleanup ticker loop (~`:248`).

### Client (`client-boundaries`)
- `client/src/protoFleet/api/clients.ts` — `createClient(CohortService, transport)` + export.
- `client/src/protoFleet/api/useCohortApi.ts` — hand-written hook (create/reserve/release/extend/
  add/remove/list/my/listDevices). *Clone-from:* `api/useDeviceSets.ts`, `api/useCurtailmentApi.ts`.
- `client/src/protoFleet/features/cohorts/` — `CohortsPage` (list), create modal (incl. "from group"),
  `CohortDetailPage`/my, device view. *Clone-from:* `features/groupManagement/`, `features/sites/`.
- Route trio: `routePrefetch.ts` (factory + tier), `router.tsx` (lazy + route), `config/navItems.ts`
  (`/cohorts`, perm `cohort:read`). Follow the runbook atop `routePrefetch.ts`.

### CLI
- `server/cmd/fleetcli/cmd_cohorts.go` — verbs `reserve`, `release`, `extend`, `my`, `rigs`, `status`
  over the RPCs; `--json` + exit codes (`0` ok / `1` err / `2` `AlreadyExists`). Regenerate
  `generated_runtime.go` if the surface is codegen'd (see open question). *Clone-from:* `cmd_groups.go`,
  `cmd_racks.go`. `swufetch` (PR/release→`.swu`) stays client-side and passes a concrete `firmware_file_id`.

## Key implementation notes

- **Atomic allocation:** selector pre-filters to default-cohort devices matching `--product`/`--site`;
  one tx inserts the cohort + bulk-inserts membership; a `UNIQUE(org_id, device_identifier)` violation
  rolls back the whole tx → `AlreadyExists` → CLI exit 2. Defense-in-depth = selector + the index
  (exactly as curtailment's `runSelector` + `InsertEventWithTargets` + the `000072` index).
- **Authz-on-move is the lease:** pulling a device *from default* is free (needs `cohort:manage`);
  moving one *out of an owned cohort* requires owner or admin. Enforce in the service before the move.
- **Release/expiry = membership→default, no reset code.** `ReleaseCohort` and `SweepExpired` both just
  delete membership rows (devices fall back to default) and soft-delete the cohort. Phase 2's reconciler
  later converges those devices to the default's desired state.
- **Group→cohort bridge:** `CreateCohort(source_device_set_id)` resolves the group's *current* members
  (via the `collection`/device_set store) and freezes them into the new cohort. This is also the
  concrete `ScopeDeviceSets` resolution curtailment left unimplemented (`curtailment/service.go:1134`).
  After creation the cohort owns its membership — later group edits don't touch it.
- **Filter:** `CohortMembershipFilter` must let the owner (and `ActorCohort` self-traffic) through and
  skip others; mirror the skip-reason shape from `curtailment_active_filter.go`.

## Acceptance criteria

- `reserve --count N` allocates N atomically or exits `2` (allocates nothing) when short.
- Two concurrent reserves cannot both claim the same device (the `UNIQUE` index holds).
- `release`/expiry returns devices to the default cohort; `extend` bumps `expires_at`.
- A non-owner's command against an owned-cohort device is filtered/blocked.
- UI: create a cohort (incl. from a group), view who-has-what, release.
- `fleetcli reserve|release|extend|my|rigs|status` work headless with `--json` + correct exit codes.

## Verification

```bash
# Server unit (fakeStore) + integration (real DB)
cd server && go test ./internal/domain/cohort/...
cd server && DB_PASSWORD=fleet go test ./internal/handlers/cohort/...
# Client
cd client && npm exec --no -- tsc --noEmit && npm run lint
# E2E (new spec + page object)
just test-e2e-fleet                         # add client/e2eTests/protoFleet/spec/cohorts.spec.ts
# CLI smoke
cd server && go test ./cmd/fleetcli/...
just lint
```

## Open questions

- **CLI codegen:** confirm via `server/docs/fleet-cli-generation.md` whether adding `CohortService`
  auto-generates commands (`generated_runtime.go`) or whether `cmd_cohorts.go` is hand-authored like
  `cmd_groups.go`. Shapes how much CLI work this phase is.
- **Soft vs hard lease:** does `CohortMembershipFilter` *block* a non-owner's command or only warn/audit?
  Lean: block (it's a lease), with admin override. Confirm with the team.
- **Allocation selector knobs:** which device attributes are filterable at reserve time
  (`--product`, `--site`, model, status)? Align with `fleetcli`'s existing miner filters.
