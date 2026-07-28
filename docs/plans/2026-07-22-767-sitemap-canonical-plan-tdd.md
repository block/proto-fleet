---
title: "Refactor sitemap import around a canonical resolved plan"
date: 2026-07-22
status: draft
type: tdd
tracker: https://github.com/block/proto-fleet/issues/767
---

# Refactor sitemap import around a canonical resolved plan

## Update 2026-07-23 — single reference-cell model adopted

Superseding the `(*_id, name)` column-pair design below: each parent relationship
is now **one reference cell**. Grammar: blank = unassigned; a bare integer =
existing entity by id; `NAME:x` = a same-import create whose own name/label is x;
anything else = validation error. `resolveReferences` (replacing
`normalizeIDReferences` + `normalizeInferredPlacement`) canonicalizes every cell
in place to a canonical name with implied ancestors filled (a building reference
also fills its site), so resolve/validate/apply read names exclusively. This
deleted the id/name-mismatch errors, `ambiguousBuildingLabels`, the
`desired*ByID` family, and the building name-lookup fork. Export writes the
parent id into the single cell; change-detection comparable rows hold canonical
names.

**Known interim limitation (accepted, to restore in step 7):** because a
building reference canonicalizes to its `(site, name)` pair, two buildings
identical in both `(site, name)` — e.g. two site-less buildings sharing a name,
which the DB permits — are not independently addressable in the name-keyed
interim, and the grid/collision/capacity/miner-move validators would false-positive
on them. Step 7 restores id-precision by keying those validators on
`building_id` via the resolved-plan graph, and re-adds the four id-disambiguation
tests dropped here (see git history of `service_test.go` for the deleted cases).

## Context

The sitemap CSV import planner lives in a single 4,738-line file,
`server/internal/domain/sitemap/service.go`. It grew organically through PR #752
and its review rounds. The recurring class of bug — found repeatedly in review —
is **representation drift**: there is no canonical resolved model, so preview,
validation, commit-token generation, and apply each recompute the "desired"
topology independently from raw parsed rows, and those recomputations disagree.

### Current data flow (the problem)

```
CSV bytes
  → parseSiteMapCSV       → parsedCSV{ sections map[string][]map[string]string }
  → loadSnapshot          → snapshot{ sites, buildings, racks, miners, hiddenRackMembers }
  → buildPlan(parsed, snap, mode):
        normalizeIDReferences(parsed, snap)          // MUTATES rows in place
        normalizeInferredPlacement(parsed, target)   // MUTATES rows in place
        ~30 validate*() calls, each reading parsed.sections[...] + snap
        ~15 desired*() helpers, each REBUILDING the desired graph from rows
        count*() helpers for the preview summary (yet another recompute)
  → commitToken(parsed, mode, plan, snap)            // hashes rows again
  → applyImportPlan(parsed, snap, mode):
        applySiteRows / applyBuildingRows / applyRackRows / applyMinerRows
        each re-deriving desired parents from rows during the write tx
```

Every consumer starts from `[]map[string]string` and re-resolves. The
"desired graph" is recomputed at least six times, by six sets of rules:
`desiredSitesByID`, `desiredBuildingsByID`, `desiredRacksByID`,
`desiredBuildingMap`, `desiredBuildingNameLookup`, `desiredRackMap`,
`desiredBuildingCapacityMap`, `desiredRackGridBuilding`,
`desiredMinerSiteBuilding`, `desiredSiteBuildingIDs`, and the `count*` family.

### Bug classes this has produced (from #752 review + #767 comments)

1. **Row-representation divergence.** Raw DB snapshot rows, exported CSV rows
   with blank inferred columns, parsed rows after escape cleanup, rows after
   ID/name normalization, desired maps for preview, and commit-time resolution
   are six representations that can diverge. Concrete: rack preview counts
   compared raw rack rows instead of export/parse-normalized rows.
2. **Premature authority.** `rack_id` miner rows skipped supplied-parent-ID
   consistency checks because the rack was treated as authoritative too early.
3. **Snapshot-vs-edited-rows divergence.** Rack validators built desired
   building lookups from the pre-import snapshot in some paths and from edited
   BUILDING rows in others (`validateRackGridPositions`,
   capacity/grid validators for renamed/moved buildings).
4. **Mutable-population drift.** Snapshots filtered out UNPAIRED/PENDING/FAILED
   miners, but remove-omitted cascades still mutate those devices. Exported
   rows, omission counts, hidden-resource checks, validation, and apply must
   share one scoped population.
5. **Ordering-sensitive uniqueness collisions.** `UpdateSite` → `uk_site_org_name`
   on name swaps/rotations; `moveBuildingsToSite` + `UpdateBuilding` →
   `uk_building_site_name`; `UpdateCollection` → `uk_device_collection_org_type_label`
   on rack label swaps. Currently rejected in preview per entity type; the
   canonical plan should generate collision-safe two-phase ops or explicitly
   model unsupported cases once.
6. **Preflight/lock TOCTOU race.** Remove-omitted site deletion does hidden-resource
   preflight (`validateOmittedSiteDeleteImpacts`) *before* `deleteOmittedSites`
   acquires delete locks. A concurrent curtailment-profile or infrastructure-device
   create can land in the window and be silently cascade-deleted.

## Goals

- One **canonical resolved plan** built exactly once from `(parsed, snapshot, mode)`.
- Preview counts, all validation, commit-token, and apply consume **that same
  plan** — no consumer re-resolves from raw rows.
- Names are create/reference sugar; **stable IDs are primary identity** for
  existing entities, resolved once during graph construction.
- One explicitly-scoped mutable population, shared by export, omission counts,
  hidden-resource checks, validation, and apply.
- Identity swaps/rotations become **collision-safe two-phase operations**
  (rename via reserved temp name) within the existing single transaction.
- Hidden-resource safety re-checked **after** acquiring delete locks, inside the
  commit transaction, before destructive deletes.
- **No behavior change** for the happy path and all currently-covered error
  cases — the existing 2,885-line test suite must stay green.

## Non-goals

- No proto/API surface change (`ImportSiteMapCsvRequest/Response`, error shapes,
  change-summary operations stay identical).
- No CSV format change (headers, section markers, escape rules unchanged).
- No new supported operations beyond making today's rejected transient-collision
  cases succeed via two-phase. Anything `ensureSupportedCommitPlan` rejects today
  stays rejected unless explicitly listed below.
- Not fixing #778 (interactive "Manage racks" reparent over-capacity ordering
  bug). It shares a bug *class* and the building-capacity invariant with this
  work (see below) but its fix is a client-side commit-staging decision on a live
  RPC flow, not a planner change.

## Design: the canonical resolved plan

### Core types (new file: `resolved.go`)

```go
// nodeAction is the resolved verb for one entity, derived once.
type nodeAction int
const (
    actionNone nodeAction = iota // present, unchanged
    actionCreate
    actionUpdate                 // field-only change, no identity collision
    actionRename                 // identity (name/label) change — may need two-phase
    actionMove                   // parent change (buildings→site, miners→placement)
    actionUnassign               // miner detached (remove-omitted)
    actionDelete                 // site/building/rack removed (remove-omitted)
)

type resolvedSite struct {
    id       *int64            // nil ⇒ create
    name     string            // desired final name
    action   nodeAction
    prevName string            // for rename collision detection
    rowNum   int               // provenance for error messages
    // ...remaining site fields
}

type resolvedBuilding struct {
    id       *int64
    site     *resolvedSite     // resolved parent pointer, never a re-lookup
    name     string
    action   nodeAction
    // layout: aisles, racksPerAisle, etc.
    rowNum   int
}

type resolvedRack struct {
    id       *int64
    building *resolvedBuilding // nil ⇒ rack sits directly under site (see memory: miner placement model)
    site     *resolvedSite
    label    string
    action   nodeAction
    // zone, rows, cols, cooling, orderIndex, aisleIndex, positionInAisle
    rowNum   int
}

type resolvedMiner struct {
    deviceID string            // stable identity, always present
    name     string
    rack     *resolvedRack     // resolved placement, pointers not names
    building *resolvedBuilding
    site     *resolvedSite
    rackRow, rackCol string
    action   nodeAction
    rowNum   int
}

// resolvedPlan is the single source of truth.
type resolvedPlan struct {
    mode       pb.OmissionMode
    sites      []*resolvedSite
    buildings  []*resolvedBuilding
    racks      []*resolvedRack
    miners     []*resolvedMiner
    population minerPopulation // the one scoped mutable set (goal 4)

    // derived, all consumed downstream — never recomputed:
    omissions  *pb.OmissionCounts
    errors     []*pb.ImportValidationError
    warnings   []string
    changes    []*pb.ImportChangeSummary
}
```

Nodes hold **resolved parent pointers**, not names or repeated ID lookups. Once
`resolvePlan` links `resolvedRack.building → *resolvedBuilding`, every validator
and the applier follow the pointer instead of re-deriving from
`row[fieldBuilding]` against a freshly-built map. This structurally eliminates
bug classes 1–3.

### Resolution pipeline (`resolvePlan`)

```
resolvePlan(parsed, snapshot, mode) → *resolvedPlan
  1. scopePopulation(snapshot, mode)      // one mutable-miner set (goal 4)
  2. resolveSites(parsed.SITE, snapshot)  // ID-first identity, name is sugar
  3. resolveBuildings(...) linking → sites
  4. resolveRacks(...)     linking → buildings/sites
  5. resolveMiners(...)    linking → racks/buildings/sites
       - fold normalizeIDReferences + normalizeInferredPlacement into resolution
         so ID/name consistency errors are produced here, once, against the
         resolved parent pointer (fixes bug class 2: no premature rack authority)
  6. classifyActions()                    // sets nodeAction per node from id/prev
  7. validate(plan)                       // all validators read resolved nodes
  8. computeOmissions(plan)               // from population + node presence
  9. computeChanges(plan)                 // preview counts from nodeAction, not count*()
```

Steps 7–9 replace ~30 `validate*` + ~15 `desired*` + ~12 `count*` functions.
Validators are rewritten to take typed nodes; the *rules* (constraint messages,
row numbers, ordering) are preserved verbatim — this is a representation change,
not a policy change.

### Two-phase identity operations (goal 5, answers "is it still safe?")

Apply already runs in one `transactor.RunInTx`. Identity changes that would trip
a unique constraint mid-transaction (name swaps/rotations across sites,
building move+rename, rack label swaps) are emitted by the resolved plan as an
ordered op sequence:

```
Phase A: rename each colliding entity to a reserved temp label
Phase B: apply the real renames/moves (target names now free)
Phase C: (temp labels already consumed by B)
```

**Safety invariant:** all three phases run inside the *same* transaction. Unique
constraints are non-deferred and checked per statement — that is exactly why the
transient collision exists today — but a failure at any statement rolls the
whole transaction back. The temp label is only ever visible inside the
uncommitted tx and is never observable by another connection. So two-phase
removes the false collision **without weakening atomicity**: either the full
final topology commits or nothing does. Requirements:

- Temp labels must be provably unique and outside the valid user namespace
  (e.g. a reserved prefix + node id) so Phase A cannot itself collide.
- The plan orders phases deterministically (sorted by node id) so the commit
  token and apply agree.
- Cases genuinely unsupportable in one tx are modeled as explicit plan errors
  **before token generation** (replacing today's scattered per-entity rejects in
  `validateSiteRenameTargets` / `validateBuildingMoveRenameTargets` /
  `validateRackRenameTargets`).

### Locked hidden-resource re-validation (goal 6)

Keep the preview-time `validateOmittedSiteDeleteImpacts` check (fast feedback),
but move the **authoritative** check inside `deleteOmittedSites`, *after*
`LockSiteForWrite` / `LockBuildingsBySiteForWrite` /
`LockInfrastructureDevicesBySiteForWrite`, before the destructive cascade. If a
concurrent create landed in the preview→lock window, the locked re-check fails
the transaction (rolls back) instead of silently deleting hidden resources. This
is the "commit-time apply rehydrates and locks live state before writes"
requirement from the issue.

### Shared building-capacity invariant (issue bullet 5, coupling to #778)

Sitemap import currently maintains its **own copy** of the building rack-capacity
invariant (`validateBuildingRackCapacity`, `service.go:3540`, backed by
`desiredBuildingCapacityMap`) — separate from the authoritative guard in the
buildings domain (`buildings/service.go:600-611`, in `AssignRacksToBuilding`).
Both enforce the same rule: `existing + net-new members ≤ aisles × racks_per_aisle`.
Two independent copies of one invariant is exactly the duplication the issue's
"reuse or extract domain validation helpers" bullet targets.

As part of routing validation through the resolved plan (step 3), extract a
single capacity helper — e.g. `buildings.RackCapacityFits(building, resultingCount)`
or a small `gridCapacity`/net-membership function in the buildings domain — and
have the sitemap capacity validator call it against the resolved final graph
instead of reimplementing the arithmetic. The resolved plan already knows each
building's final rack membership (via `resolvedRack.building` pointers), so it
passes the *net-final* count, not an intermediate one.

**Coupling to #778 (informational, out of scope here):** #778 is a false
over-capacity rejection in the interactive "Manage racks" reparent flow, caused
by the buildings guard being evaluated at commit time against *intermediate*
membership (removals staged to Save, reparents committed on Continue). Its fix is
a client commit-staging decision, not part of this refactor. But it enforces the
*same* invariant this plan extracts. Extracting one canonical
net-final-membership helper here gives #778 a single correct implementation to
reuse rather than maintaining a third copy — so keep the helper's signature
capacity-agnostic about *how* the resulting count was derived (import graph vs.
live reparent working set), taking only `(building, resultingRackCount)`.

### Commit token

`commitToken` hashes the resolved plan (scoped population fingerprint + resolved
node set + mode) instead of raw rows. Same dry-run/commit token contract; token
is now derived from the same structure that apply will execute, so a token can
never validate a topology that apply resolves differently.

## Work breakdown (incremental, each step compiles + tests green)

Even though the end state is a full rewrite, land it in reviewable steps on this
branch (`issue-767`), each independently green:

1. **Introduce `resolved.go` types + `resolvePlan`** producing the graph, with
   `classifyActions`. Not yet wired — pure addition. Unit-test resolution
   directly (ID-first identity, name sugar, inferred placement, scoped
   population).
2. **Route preview counts** (`computeChanges`) through the plan; delete the
   `count*` family. Assert change summaries byte-identical on the existing corpus.
3. **Route validation** through the plan node-by-node; delete `desired*` helpers
   and per-row validators as each is replaced. This is the largest step — do it
   validator-group by validator-group (uniqueness, placement targets, capacity/
   grid, slot collisions) so each sub-commit stays green.
4. **Route apply** through the resolved op list, including two-phase identity
   ops. Replace `applySiteRows/applyBuildingRows/applyRackRows/applyMinerRows`
   row-walkers with plan-driven appliers.
5. **Locked hidden-resource re-validation** in `deleteOmittedSites` /
   building / rack delete paths.
6. **Commit token from plan**; verify token stability against current tokens for
   unchanged inputs (may intentionally change — see Risks).
7. **Delete dead code** (`normalizeIDReferences`, `normalizeInferredPlacement`,
   `snapshotForOmissionMode`, the `desired*`/`count*` families, row-identity
   helpers). Target: `service.go` shrinks substantially; new `resolved.go`
   carries the model.

## Testing strategy

- **Characterization first.** Before refactoring, capture current behavior as a
  golden corpus: for a matrix of CSV inputs × omission modes, snapshot the full
  `ImportSiteMapCsvResponse` (errors, warnings, changes, omission counts,
  commit token). Re-run after each step; diffs must be explained (only the token
  may change, and only in step 6).
- **Regression tests for every #752/#767 bug** listed in Context, asserted
  against the resolved plan so they cannot silently regress:
  - rack preview count uses normalized rows,
  - rack_id parent consistency enforced,
  - renamed/moved building visible to rack grid/capacity validators,
  - UNPAIRED/PENDING/FAILED miners in scoped population under remove-omitted.
- **New two-phase tests:** site name rotation (A→B→C→A), building move+rename
  into occupied target name, rack label swap — each now *commits* and produces
  the correct final topology; assert no temp label leaks post-commit.
- **New race test:** concurrent curtailment-profile/infra-device create between
  preflight and lock is rejected by the locked re-check (rolls back, nothing
  deleted).
- Keep `go test -race -count=1 ./internal/domain/sitemap`. DB-backed integration
  tests for the two-phase/lock paths where a real transaction is needed.

## Risks & mitigations

- **Behavior drift during rewrite.** Mitigated by the golden corpus gate on every
  step and by preserving validator messages/row numbers verbatim.
- **Commit-token churn.** Changing the token derivation invalidates in-flight
  dry-run tokens at deploy time; acceptable (users re-run dry-run) but call it out
  in the PR and land token change as its own commit.
- **Two-phase temp-label collisions.** Mitigated by reserved out-of-namespace
  prefix + node id; add a validator asserting no user label uses the reserved
  prefix.
- **Big diff review fatigue.** Mitigated by the 7-step breakdown; each step is
  independently reviewable and green. Consider stacking as separate PRs off this
  branch if the single diff is too large.

## Open questions

- Should step 6 (token from plan) ship in this PR or defer, given it forces
  in-flight dry-run tokens to invalidate on deploy?
- Are there operations currently rejected by `ensureSupportedCommitPlan` that we
  should opportunistically support now that the plan models them cleanly, or hold
  the line on non-goals?
