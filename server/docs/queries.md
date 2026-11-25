# Error Query SQL Reference

This document explains the SQL queries in `sqlc/queries/errors.sql` for reviewers.

## Table of Contents
- [Deduplication & CRUD](#deduplication--crud)
- [Query Errors (Main Query)](#query-errors-main-query)
- [Device Pagination](#device-pagination)
- [Component Pagination](#component-pagination)

---

## Deduplication & CRUD

### UpsertDeviceErrors

Persists one device's error snapshot in a single `INSERT ... ON CONFLICT` statement. The partial unique index `idx_errors_open_dedup` (`NULLS NOT DISTINCT`, `WHERE closed_at IS NULL`) allows one open occurrence per key:

```
Dedup Key = (org_id, device_id, miner_error, component_id, component_type)
```

- Open reports insert or update the open row. Unchanged rows are rewritten only once `last_seen_at` is older than the heartbeat cutoff; `last_seen_at` never moves backwards.
- Detail changes, including `summary` and vendor attributes in `extra`, write immediately. Reports containing changing live readings may still write every poll; heartbeat coalescing saves writes only while all compared details stay unchanged.
- If `CloseStaleErrors` closes the row first, the conflict disappears and the report inserts a new occurrence.
- Reports carrying `closed_at` close the matching open row and write its latest details, or insert a closed row when none is open.
- Partial polls call `RefreshOpenErrorsLastSeenByDevice` separately so omitted open rows stay fresh.
- The stale closer requires error age beyond the staleness threshold plus the refresh heartbeat, while device status must still fall within the original staleness threshold. A skipped heartbeat cannot turn a still-reported error into evidence of absence when polling stops.
- Callers must deduplicate reports by key first: `ON CONFLICT` cannot update one row twice in a statement.
- Returns whether the device exists in the organization. The statement is not retried; the next poll replays it.

### GetErrorByErrorID

Fetches a single error by its external ULID. JOINs with `device` to return `device_identifier` alongside the error fields.

---

## Query Errors (Main Query)

The core query that powers all error list views. Supports:
- **AND filter logic**: all provided filters must match
- **6 filter types**: device, device_type, severity, miner_error, component_type, component_id
- **Cursor pagination** on `(severity, last_seen_at, error_id)`

> **TODO**: Add CASE statement to support OR logic via `use_or_logic` parameter.

### Filter Logic Pattern

```sql
-- All provided filters must match (AND logic)
AND (sqlc.narg('device_filter') IS NULL OR device IN (...))
AND (sqlc.narg('severity_filter') IS NULL OR severity IN (...))
...
```

**Why this pattern?**
- `sqlc.narg('filter') IS NULL` checks if the filter is provided
- `sqlc.slice('values')` expands to the IN clause values
- `NULL OR condition` = `condition`, so missing filters are ignored
- All provided filters must match for an error to be returned

### Cursor Pagination

Sort order: `severity ASC, last_seen_at DESC, error_id DESC`

```sql
AND (
    cursor_severity IS NULL                    -- No cursor = first page
    OR severity > cursor_severity              -- Worse severity
    OR (severity = cursor AND last_seen < cursor)  -- Same severity, older
    OR (severity = cursor AND last_seen = cursor AND error_id < cursor)
)
```

**Why this compound cursor?**
- Severity alone isn't unique (many errors can be CRITICAL)
- Adding last_seen_at still isn't unique (errors can share timestamps)
- error_id (ULID) guarantees uniqueness

---

## Device Pagination

For `ResultViewDevice`, we paginate by **device** not by error.

### QueryDeviceIDsWithErrors

Returns paginated list of devices that have errors:

```sql
SELECT device_id, device_identifier, MIN(severity) as worst_severity
GROUP BY device_id, device_identifier
HAVING (cursor conditions on worst_severity, device_id)
ORDER BY worst_severity ASC, device_id ASC
```

**Key points**:
- `MIN(severity)` gets worst severity per device (lower = worse)
- Returns both `device_id` (int64, for cursor) and `device_identifier` (string, for re-filtering)
- HAVING clause filters after GROUP BY to enable cursor on aggregated `worst_severity`

### Two-Query Pattern

The service uses this query in a two-step process:
1. Get paginated device keys (this query)
2. Fetch ALL errors for those specific devices using `QueryErrors` with device filter

This ensures each device shows all its errors, not just page_size errors total.

---

## Component Pagination

For `ResultViewComponent`, we paginate by **(device_id, component_type, component_id)** tuples.

### QueryComponentKeysWithErrors

```sql
SELECT device_id, device_identifier, component_type, component_id, MIN(severity) as worst_severity
GROUP BY device_id, device_identifier, component_type, component_id
HAVING (cursor conditions)
ORDER BY worst_severity ASC, device_id ASC, component_type ASC, component_id ASC
```

**Handling NULL component_id**:
Device-level errors have `component_id = NULL`. The HAVING clause handles this:

```sql
OR (severity = cursor AND device_id = cursor AND component_type = cursor AND (
    component_id > cursor_component_id
    OR (cursor_component_id IS NULL AND component_id IS NOT NULL)
))
```

This condition assumes that NULL component_ids sort first. The `ORDER BY` uses
`component_id ASC`, and PostgreSQL sorts NULL last for that order. Thus a page
boundary inside a `(device_id, component_type)` group that has both NULL and
non-NULL component_ids can skip or repeat rows.

### CountComponentsWithErrors

Uses a subquery pattern because `COUNT(DISTINCT device_id, component_type, component_id)` isn't valid SQL:

```sql
SELECT COUNT(*) FROM (
    SELECT DISTINCT device_id, component_type, component_id
    FROM errors ...
) as component_count
```

---

## Index Usage

The `errors` table has these indexes. All are in the 1000 baseline, except `idx_errors_open_dedup`, which migration 001115 adds:
- `idx_errors_dedup` on `(org_id, device_id, miner_error, component_id, component_type)` - deduplication lookup
- `idx_errors_open_dedup` UNIQUE on `(org_id, device_id, miner_error, component_id, component_type) NULLS NOT DISTINCT WHERE closed_at IS NULL` - one open row per error key
- `idx_errors_org_severity` on `(org_id, severity)` - severity-based filtering
- `idx_errors_org_last_seen` on `(org_id, last_seen_at DESC)` - time-based filtering and sorting
- `idx_errors_org_site_last_seen` on `(org_id, site_id, last_seen_at DESC)` - site-scoped time filtering
- `idx_errors_pagination` on `(org_id, last_seen_at DESC, id DESC)` - cursor pagination
- `idx_errors_open` on `(org_id, closed_at, severity)` - filtering open errors
- `idx_errors_device_open` on `(device_id) WHERE closed_at IS NULL` - open errors for one device
- `idx_errors_org_component` on `(org_id, component_id, component_type)` - component filtering
- `idx_errors_org_miner_error` on `(org_id, miner_error)` - error-code filtering

The `error_id` column has a UNIQUE constraint (implicit index). JOINs with `device` and `discovered_device` use their primary keys.
