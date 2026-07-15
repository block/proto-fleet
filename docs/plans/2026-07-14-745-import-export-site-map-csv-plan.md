---
title: Import / Export Site Map (CSV)
date: 2026-07-14
status: draft
type: plan
tracker: https://github.com/block/proto-fleet/issues/745
---

# Import / Export Site Map (CSV)

## Summary

Let an org **export** its site map — every Site, Building, Rack, and Miner
placement — as a deterministic multi-section CSV, and
**re-import** an edited copy to reconcile the fleet against the sheet after a
validated dry-run.

The sheet describes the desired placement/config. What happens to entities that
exist in the fleet but are **omitted** from the sheet is not hardcoded — the
importer detects omissions and **asks the operator**, in one dialog before the
dry-run, whether to **remove** the omitted entries or **leave them in place**.
The choice applies uniformly to sites, buildings, racks, and miners. Infrastructure
devices are out of scope for v1 while that domain is still evolving. This lets an
operator drive full setup/restructure from the sheet without the UI, while never
silently deleting anything.

Groups remain out of scope for v1 (many-to-many; doesn't fit a flat row).

## Decisions locked for this workspace

Confirmed before drafting. Several supersede the original ticket (merge-only,
single-flat-sheet, no infra):

1. **Delivery:** full feature (export + import) on one branch, sequenced commits.
2. **Omission policy is an explicit, uniform per-import choice.** After upload,
   if any entity in the fleet is absent from the sheet, a dialog (before the
   dry-run summary) offers one choice applied to sites/buildings/racks/miners:
   **Remove omitted** or **Leave omitted in place**. Present-in-sheet entities are
   always created (if new) or updated (if existing) regardless of the choice.
3. **Multi-section CSV, one section per entity type:** `SITE`, `BUILDING`, `RACK`,
   `MINER`. Removes denormalized-metadata + entity-only-row complexity.
4. **What "Remove" means per entity** (removal never violates fleet invariants):
   - Site / Building / Rack → **delete** (soft, with cascade).
   - Miner → **unassign** (clear rack/slot, building, site); miner stays paired.
     *Never unpaired/deleted — miners come from pairing only.*
   - Infra → out of scope for v1.
   - "Leave in place" → the omitted entity is untouched (merge behavior).
5. **Present-row semantics:** sites/buildings/racks upsert (create if new; update
   placement/layout fields if existing). Miners: apply placement and `name`
   updates. Unknown miner rows → validation error (never minted/created). Miner
   identity/context fields (`serial_number`, `mac_address`, `ip_address`) are
   read-only and error if changed for a known `device_identifier`.
6. **Import permissions:** composite of existing perms — site manage, building
   manage, `PermRackManage`, and miner placement permissions. If the
   chosen plan (including any removals) needs a perm the caller lacks → rejected
   outright, not trimmed.
7. **`commit_token`:** hash of the parsed plan **including the omission choice**;
   on confirm the server re-validates against live state and rejects on drift.
8. **`driver_config` stays UI-only** — never in the CSV.
9. **`worker_name` and miner `model` are excluded from the CSV.** Miner `name`
   is exported and writable for bulk rename workflows. `serial_number`,
   `mac_address`, and `ip_address` are exported for matching but read-only on
   import.
10. **No hard row cap** in v1.

## Prior art (reuse, don't reinvent)

| Concern | Existing code | Reuse |
|---|---|---|
| Streaming CSV export | `server/internal/domain/fleetmanagement/export_csv.go` `ExportMinerListCsv` | BOM, `sanitizeCSVField`, `formatDecimal`, chunked `send()` |
| Deterministic ordering | `stores/sqlstores/device_sort.go` + `device_query_fragments.go` | name-expr `ORDER BY (expr) ASC NULLS LAST, id ASC` |
| Source-agnostic import | `server/internal/domain/fleetimport/{types,importer}.go` | extend `ImportData`; feed `Importer` |
| 2-phase dry-run flow | `server/internal/domain/foremanimport/` | dry-run/commit split, `NewlyAssignedCount` idempotency |
| Create/update RPCs | `CreateSite`, `CreateBuilding`, `SaveRack`, `AssignRacksToBuilding`, `AssignDevicesTo{Site,Building,Rack}`, `RenameMiners` | commit orchestration |
| Delete/unassign RPCs | `DeleteSite`, `DeleteBuilding`, `DeleteDeviceSet` (rack); `AssignDevicesTo{Site,Building,Rack}` with target unset | apply "Remove" |
| Browser download | `client/src/shared/utils/utility.ts` `downloadBlob`, `getFileName` | verbatim |
| Streaming export hook | `client/src/protoFleet/api/useExportMinerListCsv.ts` | clone for site map |
| Import modal UX | `client/src/protoFleet/features/onboarding/.../Miners` | upload → dialog → dry-run → confirm |

### Data-model facts that constrain the design

- **Miners** (`device`): `device_identifier VARCHAR(36) UNIQUE`, `serial_number UNIQUE`,
  `mac_address`, optional `site_id`/`building_id` FKs. Rack membership via
  `device_set_membership` (unique per device where type=rack); slot via
  `rack_slot(row,col)`, 0-indexed. Placement is not a strict tree. IP address
  lives on discovered state and is read-only for this import.
- **Racks** = `device_set` (type=rack) + `device_set_rack` (rows, columns, zone,
  cooling_type, order_index, aisle_index, position_in_aisle, optional site_id/building_id).
  Label unique per `(org, type)`.
- **Sites**: name unique per org. **Buildings**: name unique per site (FK to site).
- **Deletes are soft** and cascade predictably; none refuse on non-empty children.
  `DeleteSite` cascades buildings → racks → device unassignment → infra soft-delete.
  No server-side reconciliation endpoint exists — we compute the diff ourselves.

## CSV format

Multi-section single file. Each section: a marker row `# SECTION: <NAME>`, then a
header row, then data rows. Section order fixed: SITE, BUILDING, RACK, MINER.

```
# SECTION: SITE
site

# SECTION: BUILDING
site, building, aisles, racks_per_aisle

# SECTION: RACK
site, building, rack, zone, rows, columns, order_index, aisle_index, position_in_aisle

# SECTION: MINER
device_identifier, serial_number, name, ip_address, mac_address,
  site, building, rack, rack_row, rack_col
```

- Keys: SITE by `site` (per org); BUILDING by `(site, building)`; RACK by `rack`
  label (unique per org+type; `site`/`building` are its placement); MINER by
  `device_identifier`.
- Read-only-on-import miner columns (exported for readability/matching, rejected if edited):
  `serial_number`, `ip_address`, `mac_address`. Miner `name` is writable.
- Enums as human strings: `order_index` =
  `BOTTOM_LEFT|TOP_LEFT|BOTTOM_RIGHT|TOP_RIGHT`. `rack_row`/`rack_col` 0-indexed.
- Miner rows with `rack` set leave `site` and `building` blank on export; import
  derives site/building from the rack and rejects nonblank mismatches.
- Dedicated sections → **no denormalization, no entity-only rows**.

### Determinism (hard requirement)

Same site map → byte-identical CSV every time.

- Ordering: SITE by name; BUILDING by (site, name); RACK by (site, building, label);
  MINER by (site, building, rack, rack_row, rack_col, device_identifier). Reuse
  name-expr + id-tiebreaker from `device_sort.go`.
- Fixed column/section order, no locale formatting, UTF-8 BOM + `sanitizeCSVField`
  from `export_csv.go`.
- **Acceptance:** export twice from one snapshot → identical bytes; export a known
  fixture → golden-file match.

## Export

New streaming RPC `ExportSiteMapCsv` (mirrors `ExportMinerListCsv`), in a new
`sitemap/v1` proto package.

- Service emits the four sections in order from scoped queries (`site`, `building`,
  `device_set`+`device_set_rack`, miner placement join),
  chunked server-stream, BOM on first chunk only.
- Perm: read/export perm in the spirit of `PermMinerExportCSV` (reuse vs. new
  `PermSiteMapExport` — open question).
- Client: `useExportSiteMapCsv.ts` cloned from `useExportMinerListCsv.ts`;
  `downloadBlob(blob, getFileName("proto-fleet-site-map"))`.

## Import (reconcile with chosen omission policy)

### Flow

1. **Upload** CSV (raw bytes; server is source of truth).
2. **Parse + validate + detect omissions.** Server returns validation errors (if
   any) and a per-section **omission count** (entities in the fleet absent from the
   sheet, for sites/buildings/racks/miners). No writes, no `commit_token` yet.
3. **Omission dialog** (only if omissions exist): "N sites, M buildings, K racks,
   P miners in your fleet are not in this sheet." → choose **Remove omitted** or
   **Leave in place** (one choice, all four types).
4. **Dry-run** with the chosen `omission_mode` → consolidated diff + `commit_token`
   (token binds the mode). UI renders the summary; destructive rows visually distinct.
5. **Confirm** replays with `commit_token`. Server re-validates against live state,
   rejects on drift, else **commits atomically**.

RPC `ImportSiteMap(csv_bytes, omission_mode, dry_run, commit_token)` in `sitemap/v1`,
with `omission_mode` ∈ `{UNSPECIFIED, REMOVE_OMITTED, LEAVE_IN_PLACE}`. A dry-run
with `UNSPECIFIED` and omissions present returns `omission_choice_required=true` +
counts and withholds the token; a dry-run with a concrete mode returns the full
plan + token.

### Reconciliation model

| Entity | new in sheet | exists in both | omitted + **Remove** | omitted + **Leave** |
|---|---|---|---|---|
| Site | create | update metadata | **delete** (cascades) | untouched |
| Building | create | update metadata | **delete** (cascades) | untouched |
| Rack | create | update dims/placement | **delete** (cascades) | untouched |
| Miner | *error* (never minted) | placement + rename | **unassign** (stays paired) | untouched |
| Infra | out of scope for v1 | out of scope for v1 | out of scope for v1 | out of scope for v1 |

Apply order in the single transaction (creates/updates before removals; children
before parents on delete so cascade counts stay legible):
1. Upsert sites → buildings → racks.
2. Apply present miners' placement/rename.
3. If **Remove**: unassign omitted miners (rack → building → site).
4. If **Remove**: delete omitted racks → buildings → sites (bottom-up).

### Validation layers (all pass before commit)

1. **Structural** — section markers + headers present, recognized columns, valid
   encoding/delimiter, no ragged rows, enums parse, numerics parse & in range
   (rack rows/cols > 0, aisles 0–100, `fan_count` matches `device_kind`).
2. **Referential** — every miner key resolves to an existing miner **in this org**
   (unknown → error); every infra `(site,name)` resolves; a rack/building's named
   parent is present in the sheet or fleet.
3. **Uniqueness** — duplicate site name / `(site,building)` / rack label / miner /
   `(site,name)` infra → error; **slot collisions** → error.
4. **Placement consistency** — slot within declared rack dims; a rack's building
   belongs to the rack's site; miner's rack/building/site mutually consistent.
5. **Authorization** — caller holds every perm the chosen plan implies (incl.
   delete perms when **Remove** is picked). Missing → whole plan rejected.

### Dry-run summary (consolidated)

Grouped and **consolidated by operation**, not per-entity enumeration — e.g.
"150 miners unassigned from Rack A", not 150 lines:

- **Creates:** N sites / buildings / racks (named).
- **Metadata updates:** counts per type + before→after for changed fields.
- **Removals (only if Remove chosen):** deletes with cascade rollups
  ("Site X → 3 buildings, 12 racks, 400 miners unassigned"); miner unassignments
  consolidated by source.
- **Moves:** consolidated per source→target ("150 miners: Rack A → Rack B").
- **Renames:** count of miner names changing.
- **Errors (block):** every validation failure with row number + reason.
- **Warnings (allow):** large blast radius.

### Atomicity & idempotency

Whole import commits in one transaction (or a saga that rolls back on any failure).
Re-running the same file with the same omission choice is a no-op — reconciliation
compares against current state (mirrors `fleetimport`'s `NewlyAssignedCount`).
`commit_token` drift re-validation guards state changing between preview and commit.

### Client

Import modal modeled on the Foreman `Miners` flow: upload → (server parse/validate)
→ **omission dialog** (Remove vs Leave, shown only when omissions exist) →
consolidated dry-run summary → **Confirm import**. New hook `useImportSiteMapCsv.ts`
threads `omission_mode` through the two dry-run phases and the commit call.

## UI placement

- **Export site map** / **Import site map** in the **Fleet page header** (final
  home TBD — may move to a Sites/Settings area).

## Proposed commit sequence (one branch)

1. **proto + regen** — `sitemap/v1`: `ExportSiteMapCsv`, `ImportSiteMap`,
   `omission_mode` enum, omission-preview + dry-run diff + `commit_token` messages.
2. **export service + handler** — five-section emit, determinism, perm gate;
   golden-file + twice-identical tests.
3. **export client** — `useExportSiteMapCsv` + Fleet header action.
4. **CSV parser** — bytes → sectioned desired-state model; unit tests on fixtures.
5. **validation + omission detection** — validation layers + per-section omission
   counts; table-driven tests.
6. **reconcile planner** — desired vs live under a chosen `omission_mode` →
   consolidated diff + `commit_token`.
7. **commit orchestration** — transactional upsert/unassign/delete over the RPCs;
   idempotency + atomicity (forced-failure rollback) + cascade-ordering tests.
8. **import client** — modal, omission dialog, dry-run summary (destructive styling).
9. **E2E** — export → re-import zero-change no-op (both modes); a Remove case that
   deletes an omitted rack; Playwright per `proto-fleet-playwright-e2e`.

## Out of scope for v1

- Groups. Creating/deleting miners or infra. Inferred renames (changed name =
  different entity). `worker_name` and `driver_config` in the CSV. Infra removal
  on omission (exempt — see follow-up).

## Follow-ups

- **Infra placement parity ([#748](https://github.com/block/proto-fleet/issues/748)).** Give `infrastructure_device`
  real `site_id`/`building_id` foreign keys (like `device`) instead of the current
  mandatory `site_id` + free-form `building_name`. Then infra can be
  unassigned/placed identically to miners and rejoin the uniform omission policy
  (decision #4), removing the v1 exemption.

## Open questions

- **Export perm:** reuse `PermMinerExportCSV` or new `PermSiteMapExport`?
- **Omission choice granularity:** one global choice for all sections (planned), or
  per-section toggles in the dialog later?
- **Blast-radius guard:** consolidated dry-run + single confirm only, or add an
  empty-file reject / threshold override for large deletions?
- **Section syntax:** `# SECTION:` marker rows vs. a leading `section` column —
  verify an Excel/Sheets round-trip preserves whichever we pick.

## Acceptance criteria

- [ ] Export byte-identical for an unchanged site map (golden-file).
- [ ] Export round-trips through import as a zero-change no-op dry-run (both modes).
- [ ] Omissions trigger the choice dialog before dry-run; "Leave" touches nothing
      omitted, "Remove" deletes omitted sites/buildings/racks (with cascades) and
      unassigns omitted miners. Infra is never removed on omission.
- [ ] Import rejects unknown miners, duplicate rows, and slot collisions with
      row-cited errors.
- [ ] Dry-run consolidates changes (e.g. "150 miners unassigned from Rack A") and
      accurately previews creates / updates / removals / moves before any write.
- [ ] Import commits atomically; a forced mid-import failure leaves the map unchanged.
- [ ] Infra devices export and re-import (placement) without touching `driver_config`.
