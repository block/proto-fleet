---
title: "RefreshMiners RPC + row & bulk refresh actions"
date: 2026-06-11
status: draft
type: tdd
---

# RefreshMiners RPC + row & bulk refresh actions

## Context

When a field tech remediates a miner error, the change takes up to ~2
minutes to surface in ProtoFleet because three pollers compound:

- Server scheduler `FetchInterval` ≤ 10s
  (`server/internal/domain/telemetry/config.go`).
- Status writer flush 1s (`StatusFlushInterval`).
- Client list poll 60s (`client/src/protoFleet/constants/polling.ts`).

`ListMinerStateSnapshots` reads live `device_status`
(`server/sqlc/queries/device.sql:464`) — not the 60s rollup — so once a
fresh status flushes, the next list call sees it. The bottleneck is
"how do we force a fresh fetch and flush right now."

This PR introduces a `RefreshMiners(device_ids[])` Connect-RPC and
wires it to both the single-row action menu and the bulk-action toolbar
that already exists for firmware / reboot / pool / etc. A follow-up
PR (separate TDD) reuses this RPC inside the status modal for live
updates.

## Goals

- A field tech can refresh one miner from the row-action menu and see
  the updated row within ~1–2s of the plugin fetch completing.
- A field tech can select N miners (existing `ActionBar`) and run
  "Refresh status" against the selection; progress is reported with the
  same toast UX used by firmware/reboot batches.
- The new RPC is the single server entry point — list row, bulk action,
  and (in the follow-up PR) the status modal all funnel through it.
- No regressions to the scheduled telemetry path or the 60s rollup.

## Non-goals

- Server-push, SSE, websockets.
- Lowering the global 60s client list poll.
- Changing the 60s state-snapshot rollup interval.
- Modal-side polling (lives in the follow-up TDD).
- A general-purpose priority queue in the scheduler.

## Architecture today (verified)

- **Telemetry collection.** Workers run `processDevice`
  (`server/internal/domain/telemetry/service.go:549`) — fetches metrics,
  derives status (or fetches explicitly), polls errors, sends to
  `statusResults`. Already context-safe and handles auth remediation +
  failed-device bookkeeping.
- **Status writer.** `statusWriterRoutine` batches at 1s intervals
  (`service.go:608-619`) and broadcasts changes.
- **`inFlight sync.Map`** (`service.go:236-239`) serializes against
  workers and the status-polling routine — refresh can claim the same
  primitive.
- **List query.** `ListMinerStateSnapshots` reads live `device_status`
  joined with discovered/device/pairing/site
  (`server/sqlc/queries/device.sql:464`).
- **Row-action menu** ships in PR #412 — insertion point is
  `client/src/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu.tsx`.
- **Multi-select & bulk bar.** Selection state lives in
  `ScopedMinerListBody` (`MinerList.tsx:290-299`); the `ActionBar`
  component renders `MinerActionsMenu` when selection is non-empty.
- **Bulk command pattern.** Existing bulk actions go through
  `useMinerCommand` (`api/useMinerCommand.ts:258-288`) with a server
  `batchIdentifier` and `StreamCommandBatchUpdates` streaming progress
  back. Real-time toast tracks success/failure counts.
- **`useFleet`** (`api/useFleet.ts`) holds page-level miner data as
  `Record<string, MinerStateSnapshot>` and uses protobuf `equals()` to
  suppress no-op re-renders.

## Design

### Proto

Add to `proto/fleetmanagement/v1/fleetmanagement.proto`:

```proto
rpc RefreshMiners(RefreshMinersRequest) returns (RefreshMinersResponse) {}

message RefreshMinersRequest {
  // 1..=50 device identifiers. Bulk selections larger than 50 are
  // chunked client-side; see "Client — bulk action" below.
  repeated string device_ids = 1;
}

message RefreshMinersResponse {
  // Fresh snapshots for devices whose collection succeeded.
  // Same MinerStateSnapshot type used by ListMinerStateSnapshots so
  // the client can merge by device_id without translation.
  repeated MinerStateSnapshot snapshots = 1;
  // Per-device failures. Devices that succeeded do not appear here.
  map<string, string> errors = 2;
}
```

Permissions (`server/internal/handlers/middleware/rpc_permissions.go`):
gate at the same scope as `ListMinerStateSnapshots` (read-equivalent —
refresh re-polls existing devices, does not mutate device state).

### Why unary, not streaming

Existing bulk actions use `StreamCommandBatchUpdates` because each
device command can take seconds-to-minutes and the user needs progress.
Refresh is bounded by plugin RTT (sub-second typical) and we cap at 50
ids per request. Unary keeps the server simpler — no `batchIdentifier`,
no streaming lifecycle, no in-memory batch state. The client batches
large selections itself and updates a toast tally between batches.

### Server handler

New `RefreshMiners` in
`server/internal/handlers/fleetmanagement/handler.go`:

1. Validate `len(device_ids) >= 1 && <= 50` →
   `connect.CodeInvalidArgument` otherwise.
2. Apply existing org/permission scoping. For ids the caller cannot
   see, return them as `errors[id] = "not found"` to avoid leaking
   existence.
3. Per-device debounce: in-process LRU keyed by device_id, 2s TTL. If a
   refresh ran for that device within the window, skip re-collect and
   return the latest row directly. Reason: two tabs or rapid clicks
   shouldn't double plugin load.
4. Fan out to a goroutine pool capped at `min(len(device_ids), 10)`:
   - Look up `models.Device` via `deviceStore`.
   - Call `TelemetryService.RefreshDevice(ctx, device)` (see below).
   - Read the post-refresh row via a new store method
     `GetMinerStateSnapshot(ctx, deviceID)` — the single-row equivalent
     of the list query.
5. Assemble `RefreshMinersResponse{snapshots, errors}`.

Timeouts: request ctx capped at 8s; per-device ctx 5s.

### TelemetryService — `RefreshDevice`

```go
func (s *TelemetryService) RefreshDevice(ctx context.Context, device models.Device) error
```

1. Try to claim `device.ID` in `inFlight`. If already claimed, wait
   ≤2s for the in-flight collection to release, then return — the
   handler will read the already-fresh row.
2. Call `s.processDevice(ctx, device)` — same path as workers. All
   auth remediation, error polling, metrics writes happen identically.
3. Trigger a synchronous flush. Add `FlushStatusNow(ctx) error` to the
   status-writer routine: signals an immediate flush via an internal
   channel and waits on a per-call done channel.
   - Considered (and rejected): bypass batching for refresh writes
     with a direct single-row write. Rejected because it duplicates
     the broadcaster + `lastKnownStatuses` wiring that the writer
     owns.
4. Release `inFlight` in a `defer`.

Failures: plugin unreachable / auth → `processDevice` already records
the bookkeeping; `RefreshDevice` propagates the error so the handler
records it per-device. Context deadline → propagate `ctx.Err()`.

### Client — shared hook

`client/src/protoFleet/api/useRefreshMiners.ts`:

```ts
useRefreshMiners(): {
  refreshMiners: (deviceIds: string[]) => Promise<RefreshResult>;
  refreshing: Set<string>; // device ids in-flight, for UI spinners
}
```

The hook handles the 50-id server cap by chunking large arrays into
batches and running them with a small concurrency limit (e.g. 3
parallel batches). It aggregates `snapshots[]` and `errors{}` from
each batch into a single `RefreshResult`. Callers don't need to know
about the cap.

### Client — `useFleet.mergeMiners`

Add `mergeMiners(snapshots: MinerStateSnapshot[])` to `useFleet`. It
upserts into the existing local map and uses the same protobuf
`equals()` short-circuit so no-op merges don't re-render. This is the
single merge point used by both row and bulk actions (and by the modal
in the follow-up PR).

### Client — row action

In `SingleMinerActionsMenu.tsx`, add "Refresh status":

- Click → `refreshMiners([device.id])` → on success
  `mergeMiners(result.snapshots)`; on failure show a toast with the
  per-device error message.
- Disable the item with a small spinner while in-flight (`refreshing`
  Set from the hook).
- After success, keep the item disabled for 2s (matches server
  debounce) to prevent button-mashing.

### Client — bulk action

In `MinerActionsMenu` / `ActionBar` flow, add "Refresh status (N
selected)":

- Click → resolve the selection to a concrete id list. For
  `selectionMode === "all"` the client requests the filtered id list
  using the same selector pattern existing bulk actions use, then runs
  the same chunked refresh.
- Toast UX modeled on existing batch toasts: a single toast that
  updates `succeeded / total` and `failed / total` as each chunk
  resolves. No `batchIdentifier` is involved — the toast is purely
  client-side and updates between unary RPC responses.
- On any failures, the toast offers a "Retry failures" action that
  re-invokes the hook with only the failed ids.
- Selection is preserved until the toast is dismissed (consistent with
  existing bulk actions).

Hard cap: the bulk action declines to run for selections > 500 with a
toast explaining the cap. Rationale: at 50/request and 3 in parallel,
500 ids = ~3–4 plugin-RTT cycles, well bounded; beyond that the user
should narrow the filter.

## Test plan

**Server**

- `RefreshMiners` with empty `device_ids` → `InvalidArgument`.
- `RefreshMiners` with 51 ids → `InvalidArgument`.
- Mixed-result request (one healthy, one unreachable, one not in org) →
  one `snapshots[]` entry, two `errors{}` entries; "not in org" is
  surfaced as `"not found"` (no existence leak).
- Two back-to-back refreshes for the same device within 2s → second
  call returns latest row without invoking `processDevice` a second
  time (assert via call counter on a fake `MinerGetter`).
- Concurrent `RefreshDevice` for the same id (worker already in
  flight) → only one `processDevice` runs; the second returns after
  the first releases `inFlight`.
- Auth failure path → pairing advances to `AUTHENTICATION_NEEDED`,
  per-device error reported in response.
- `FlushStatusNow` is invoked exactly once per `RefreshDevice` call,
  including under context cancellation.

**Client**

- `useRefreshMiners` chunks a 120-id selection into 50/50/20 with
  bounded parallelism; aggregated result preserves order-independent
  `snapshots` and `errors`.
- Row action click → `mergeMiners` called with returned snapshot →
  next render of the row reflects new status (protobuf `equals()`
  prevents re-render when unchanged).
- Bulk action click → toast appears, updates tally as chunks resolve,
  offers "Retry failures" on any error, dismiss clears selection.
- Bulk action declines selections > 500 with a clear toast.

**E2E (`just test-e2e-fleet`)**

- Single-row refresh updates a row before the next 60s list poll.
- Bulk refresh against ≥ 60 selected miners completes, toast shows
  final tally, list rows reflect updated statuses.

## Risks and tradeoffs

- **Plugin load.** Worst-case bulk: 500 ids × one plugin fetch each at
  ≤ 3 parallel batches of 50. Plugins already absorb worse during
  scheduler bursts. Per-device debounce prevents accidental double-load
  from rapid retries.
- **In-flight contention.** Refreshing a device the scheduler just
  picked up costs one bounded wait, not a duplicate fetch.
- **Snapshot rollup divergence.** The 60s `fleetStateSnapshotRoutine`
  is untouched; refresh writes only to `device_status`, not the rollup
  table.
- **Permission scope.** Treating refresh as read-equivalent is the
  proposed default; reviewers may want it gated as a write. The
  decision lives in `rpc_permissions.go` and flips without proto
  churn.
- **Unary vs streaming.** If a future requirement makes individual
  refreshes slow (e.g. plugins added with multi-second handshakes), we
  may want to switch the bulk path to streaming. The unary RPC stays
  useful for single-row and modal use cases; a parallel streaming RPC
  could be added without removing it.

## Follow-up

- Live status modal: separate TDD at
  `docs/plans/2026-06-11-status-modal-live-refresh-tdd.md`. Reuses
  `RefreshMiners` on a ~10s interval while open.
