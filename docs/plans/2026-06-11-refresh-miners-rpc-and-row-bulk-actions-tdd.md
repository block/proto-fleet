---
title: "RefreshMiners RPC + row refresh action"
date: 2026-06-11
status: draft
type: tdd
---

# RefreshMiners RPC + row refresh action

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
wires it to the single-row action menu. Follow-up PRs can reuse this
RPC inside the status modal for live updates and after status-changing
commands to shorten the time rows spend in a pending state.

## Goals

- A field tech can refresh one miner from the row-action menu and see
  the updated row within ~1–2s of the plugin fetch completing.
- The new RPC is the single server entry point for explicit device
  refreshes — row refresh in this PR, status modal and post-command
  verification in follow-up PRs.
- No regressions to the scheduled telemetry path or the 60s rollup.

## Non-goals

- Server-push, SSE, websockets.
- Lowering the global 60s client list poll.
- Changing the 60s state-snapshot rollup interval.
- Modal-side polling (lives in the follow-up TDD).
- Bulk/action-bar refresh. V1 only exposes a row action; the RPC still
  accepts up to 50 explicit ids so follow-up callers can reuse it.
- Post-command verification refreshes after sleep / wake / reboot /
  pool / firmware actions.
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
- **Status-changing command actions.** Existing bulk actions go through
  `useMinerCommand` (`api/useMinerCommand.ts:258-288`) with a server
  `batchIdentifier` and `StreamCommandBatchUpdates` streaming progress
  back. The row status cell keeps showing a pending spinner until the
  normal 60s list poll observes the expected status; this PR does not
  change that behavior.
- **`useFleet`** (`api/useFleet.ts`) holds page-level miner data as
  `Record<string, MinerStateSnapshot>` and uses protobuf `equals()` to
  suppress no-op re-renders.

## Design

### Proto

Add to `proto/fleetmanagement/v1/fleetmanagement.proto`:

```proto
rpc RefreshMiners(RefreshMinersRequest) returns (RefreshMinersResponse) {}

message RefreshMinersRequest {
  // 1..=50 explicit device identifiers.
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
no streaming lifecycle, no in-memory batch state.

### Server handler

New `RefreshMiners` in
`server/internal/handlers/fleetmanagement/handler.go`:

1. Validate `len(device_ids) >= 1 && <= 50` →
   `connect.CodeInvalidArgument` otherwise.
2. Apply existing org/permission scoping. For ids the caller cannot
   see, return them as `errors[id] = "not found"` to avoid leaking
   existence.
3. Fan out to a goroutine pool capped at `min(len(device_ids), 10)`:
   - Look up `models.Device` via `deviceStore`.
   - Call `TelemetryService.RefreshDevice(ctx, device)` (see below).
   - Read the post-refresh row via a new store method
     `GetMinerStateSnapshot(ctx, deviceID)` — the single-row equivalent
     of the list query.
4. Assemble `RefreshMinersResponse{snapshots, errors}`.

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

V1 row refresh calls the hook with exactly one id. The hook still
validates `deviceIds.length > 0 && <= 50` so callers receive a clear
client-side error before the RPC would reject the request.

### Client — `useFleet.mergeMiners`

Add `mergeMiners(snapshots: MinerStateSnapshot[])` to `useFleet`. It
upserts into the existing local map and uses the same protobuf
`equals()` short-circuit so no-op merges don't re-render. This is the
single merge point used by row refresh in this PR and by follow-up
modal / post-command refresh loops.

### Client — row action

In `SingleMinerActionsMenu.tsx`, add "Refresh":

- Click → `refreshMiners([device.id])` → on success
  `mergeMiners(result.snapshots)`; on failure show a toast with the
  per-device error message.
- Disable the item with a small spinner while in-flight (`refreshing`
  Set from the hook).
- While in-flight, render the status cell with a spinner and render
  telemetry cells as skeleton bars so the operator can see the row is
  waiting on a fresh miner read.
- After success, the item can be clicked again; v1 relies on the
  in-flight disabled state and telemetry `inFlight` guard rather than a
  separate cache or throttling layer.

## Test plan

**Server**

- `RefreshMiners` with empty `device_ids` → `InvalidArgument`.
- `RefreshMiners` with 51 ids → `InvalidArgument`.
- Mixed-result request (one healthy, one unreachable, one not in org) →
  one `snapshots[]` entry, two `errors{}` entries; "not in org" is
  surfaced as `"not found"` (no existence leak).
- Concurrent `RefreshDevice` for the same id (worker already in
  flight) → only one `processDevice` runs; the second returns after
  the first releases `inFlight`.
- Auth failure path → pairing advances to `AUTHENTICATION_NEEDED`,
  per-device error reported in response.
- `FlushStatusNow` is invoked exactly once per `RefreshDevice` call,
  including under context cancellation.

**Client**

- `useRefreshMiners` rejects empty arrays and >50 ids before calling
  the RPC.
- `useRefreshMiners` returns the unary RPC's `snapshots` and `errors`
  for a valid single-id request.
- Row action click → `mergeMiners` called with returned snapshot →
  next render of the row reflects new status (protobuf `equals()`
  prevents re-render when unchanged).

**E2E (`just test-e2e-fleet`)**

- Single-row refresh updates a row before the next 60s list poll.

## Risks and tradeoffs

- **Plugin load.** V1 row refresh adds one explicit plugin fetch per
  click. The existing `inFlight` guard prevents duplicate concurrent
  fetches inside one process; v1 does not add cross-process throttling
  or distributed lock.
- **In-flight contention.** Refreshing a device the scheduler just
  picked up costs one bounded wait, not a duplicate fetch.
- **Snapshot rollup divergence.** The 60s `fleetStateSnapshotRoutine`
  is untouched; refresh writes only to `device_status`, not the rollup
  table.
- **Permission scope.** Treating refresh as read-equivalent is the
  proposed default; reviewers may want it gated as a write. The
  decision lives in `rpc_permissions.go` and flips without proto
  churn.
- **Unary vs streaming.** If a future requirement makes refreshes slow
  or introduces bulk refresh, we may want a parallel streaming RPC. The
  unary RPC stays useful for single-row and modal use cases.

## Follow-up

- Live status modal: separate TDD at
  `docs/plans/2026-06-11-status-modal-live-refresh-tdd.md`. Reuses
  `RefreshMiners` on a ~10s interval while open.
- Post-command verification refreshes: after `StreamCommandBatchUpdates`
  reports success for status-changing actions (sleep, wake, reboot,
  pool assignment, firmware), start a short client-side loop over the
  successful device ids:
  - call `RefreshMiners` every ~2s for ids that have not reached
    `hasReachedExpectedStatus(action, snapshot.device_status)`;
  - merge returned snapshots through `useFleet.mergeMiners`;
  - remove ids from the loop as soon as their row reaches the expected
    status;
  - stop the loop after a bounded timeout (for example 30–60s) and let
    the existing stale batch cleanup handle stragglers.
- Bulk/action-bar refresh: not in v1. If added later, keep explicit
  ids and a client cap; do not introduce empty-array or implicit
  "all miners" semantics.
