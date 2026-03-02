---
id: "0017"
title: "Miner Renaming"
status: spec-ahead
related:
  - specs/client/0004-miner-renaming.md
last-verified: ""
code-refs:
  - server/internal/domain/
  - server/internal/handlers/
  - server/migrations/
  - server/sqlc/queries/
---

# Miner Renaming

> Server-side support for persisting user-defined miner display names.

## Overview

**Purpose**: Allow users to assign custom display names to miners. The backend persists the name.

**Scope**: Storage of the custom name field and the `RenameMiners` API endpoint.

**Audience**: Developers working on the fleet server, domain logic, or database schema.

## Context & Background

Miners currently have no user-defined display name — they are shown to the user as `manufacturer + model` (e.g., "Bitmain Antminer S19"), sourced from the `discovered_device` table. Miner renaming gives operators a custom label to distinguish individual devices beyond the manufacturer/model identity.

Duplicate names are permitted — renaming is advisory, not a unique identifier.

## Architecture

### Components

| Component | Responsibility |
|-----------|---------------|
| `device` table | Stores paired miner records; receives the new `custom_name` column |
| `discovered_device` table | Stores `manufacturer` + `model` — the current display identity, joined at read time |
| sqlc query | Read/write `custom_name` on `device`; join `discovered_device` to resolve the display name with fallback |
| Domain layer | Name generation from config, rename logic |
| Handler | `RenameMiners` RPC in the fleet management service |

### Patterns

- **Display name resolution**: The name returned in miner list responses is `custom_name` when set, falling back to `manufacturer + model` (from `discovered_device`) when `NULL`. This fallback is applied in the domain layer after the query returns the raw nullable `custom_name`. The sort expression for the name field does use SQL-level `COALESCE` so ordering is consistent.
- **Single endpoint for single and bulk**: `RenameMiners` uses `DeviceSelector`, which handles both a specific set of miner IDs and all devices in the fleet.
- **Non-unique names**: Custom names are not enforced as unique at the database level.
- **Atomic writes**: Bulk rename is applied in a single database transaction — all names are persisted or none are.

## Interfaces & Contracts

### Public API

`RenameMiners` RPC in the fleet management service (`proto/fleetmanagement/v1/fleetmanagement.proto`).

### Data Structures

```
RenameMinersRequest {
  device_selector: DeviceSelector  // existing type: specific IDs or all_devices
  name_config:     MinerNameConfig // naming pattern applied to all selected miners
}

MinerNameConfig {
  properties: []NameProperty  // ordered list of properties that form the name
  separator:  string          // separator between properties; one of "-", "_", ".", "" (no separator)
}

NameProperty {
  oneof kind {
    StringAndCounterProperty  string_and_counter
    CounterProperty           counter
    StringProperty            string_value
    FixedValueProperty        fixed_value
    QualifierProperty         qualifier
  }
}

StringAndCounterProperty {
  prefix:        string  // optional
  suffix:        string  // optional
  counter_start: int32
  counter_scale: int32   // number of digits, 1–6 (e.g. 3 → 001, 002, 003)
}

CounterProperty {
  counter_start: int32
  counter_scale: int32   // number of digits, 1–6 (e.g. 3 → 001, 002, 003)
}

StringProperty {
  value: string
}

FixedValueProperty {
  type:            enum { MAC_ADDRESS, SERIAL_NUMBER, WORKER_NAME, MODEL, MANUFACTURER, LOCATION }
  character_count: optional int32         // omitted = all characters; 1–6 = take that many characters
  section:         optional enum { FIRST, LAST }  // which end to take; required when character_count is set, ignored otherwise
}

QualifierProperty {
  type:   enum { BUILDING, RACK, RACK_POSITION }
  prefix: string  // optional
  suffix: string  // optional
}
```

**Notes:**
- `WORKER_NAME` resolves to the worker name of the default pool (priority 0, a hardcoded constant).
- `LOCATION` (fixed value) and `BUILDING`, `RACK`, `RACK_POSITION` (qualifier types) are not yet implemented in the data model and are reserved for future use.

### Validation

**Generated name**:
- Server trims whitespace before storing (`strings.TrimSpace()`); returns `NewInvalidArgumentError` if blank after trim or exceeds 100 characters.

**Input fields** (`buf.validate`):
- `properties`: must be non-empty (`min_items: 1`)
- `separator`: `in: ["-", "_", ".", ""]` (dash, underscore, period, or empty string for no separator)
- `StringProperty.value`: `min_len: 1` (empty string not permitted)
- `counter_start`: `gte: 0`
- `counter_scale`: `gte: 1, lte: 6`
- `character_count` (on `FixedValueProperty`): `gte: 1, lte: 6` when set
- `section` (on `FixedValueProperty`): required when `character_count` is set; ignored otherwise

## Data Flow

### Rename

1. Client sends `device_selector` + `name_config`.
2. Domain resolves the selector to the target device set.
3. Domain sorts the device set by `manufacturer + model` (the default miners table sort) to determine counter assignment order.
4. Domain generates a name per device from `name_config`, assigning counter values in sorted order.
5. Domain persists all names in a single database transaction.
6. Handler returns empty response.

### Error Handling

- Unknown miner ID → not found error.
- `WORKER_NAME` requested for a miner with no pool configured at priority 0 → the worker name segment is omitted from the generated name for that device.

## Data Storage

### Schema

New column on the `device` table (paired/registered devices):

```sql
ALTER TABLE device ADD COLUMN custom_name TEXT;
```

- Nullable: a miner with no custom name has `NULL`.
- No uniqueness constraint: duplicates are permitted.
- Custom name persists until explicitly changed. There is no clear/reset operation.

### Query Updates

The sqlc query selects `device.custom_name` as a raw nullable column. The display name fallback (`custom_name` when non-null, otherwise `manufacturer + ' ' + model`) is applied in the domain layer (`service.go`) after reading the query result. The name sort expression does use SQL-level `COALESCE` so ordering matches the resolved display name. Any future query returning a miner display name to a handler should apply the same fallback in the domain layer.

### Performance

**Counter ordering sort**: Sorting by `manufacturer + model` for counter assignment is done in-memory on the already-fetched device property set — no additional DB query. The existing `idx_discovered_device_sort_model` index on `discovered_device (org_id, model, id)` covers the miner list query; it is not relevant to the rename path.

## Testing

### Strategy

Unit tests for name generation logic. Integration tests against the database for read/write of `custom_name`.

### Known Gaps

- Full integration test (client → handler → domain → DB) to be added.

## Verification

| Command | What it checks |
|---------|----------------|
| `go test ./server/internal/domain/...` | Domain logic |
| `go test ./server/internal/handlers/...` | Handler integration |

## Limitations & Known Issues

- Duplicate names are allowed by design; the backend has no uniqueness enforcement.

## Related Specifications

| Spec | Relationship |
|------|-------------|
| [0004-miner-renaming (client)](../client/0004-miner-renaming.md) | Client-side UI for this feature |

## Changelog

| Date | Author | Change | Reason |
|------|--------|--------|--------|
| 2026-02-26 | Negar | Initial draft | Initial feature development |
| 2026-02-27 | Negar | Correct display name fallback location | Fallback is applied in domain layer, not SQL query; sort expression still uses SQL COALESCE |
