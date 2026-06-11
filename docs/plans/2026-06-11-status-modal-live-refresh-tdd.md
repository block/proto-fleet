---
title: "Status modal live refresh"
date: 2026-06-11
status: draft
type: tdd
---

# Status modal live refresh

## Context

`ProtoFleetStatusModal`
(`client/src/protoFleet/components/StatusModal/StatusModal.tsx`) shows a
single miner's status — current state, error details — but it does not
poll. The `miner` prop is a `MinerStateSnapshot` snapshotted from the
parent Fleet list at modal-open time. `useDeviceErrors`
(`api/useDeviceErrors.ts`) is also fetched once on open and only
refetches when the device id changes.

Operator-observed failure: open the modal on an erroring miner →
on-device remediation clears the error → modal never reflects the
change. The user must close and reopen.

This PR makes the modal reflect changes within ~10 seconds by reusing
the `RefreshMiners` RPC introduced in the predecessor TDD.

**Depends on:**
[`2026-06-11-refresh-miners-rpc-and-row-bulk-actions-tdd.md`](./2026-06-11-refresh-miners-rpc-and-row-bulk-actions-tdd.md).
That PR must land first so `RefreshMiners`, `useRefreshMiners`, and
`useFleet.mergeMiners` exist.

## Goals

- Status modal reflects underlying miner state changes within ~10s of
  the change being observable to the server.
- The list row beneath the modal stays in sync — closing the modal
  shouldn't show a "snap" to fresher data.
- Polling stops when the user can't see it (tab hidden) and after a
  reasonable idle ceiling.
- One immediate fetch on modal open so users don't wait 10s for the
  first refresh.

## Non-goals

- Server-push / SSE.
- Adding modal polling for any modal other than `ProtoFleetStatusModal`.
- Reducing the global 60s list poll.
- A new RPC. This PR is purely client-side once the predecessor lands.

## Architecture today (verified)

- Modal receives `miner: MinerStateSnapshot` from the parent list
  (`StatusModal.tsx`).
- `useDeviceErrors(modalDeviceIds)` fetches errors when `isVisible &&
  deviceId` changes (`useDeviceErrors.ts:25`); it exposes a manual
  `refetch()`.
- After the predecessor PR lands:
  - `useRefreshMiners()` returns `{ refreshMiners, refreshing }`.
  - `useFleet.mergeMiners(snapshots[])` upserts into the page map with
    protobuf `equals()` short-circuiting.

## Design

### Poll lifecycle inside the modal

When the modal mounts with `isVisible && deviceId`:

1. **Initial fetch.** Fire one `refreshMiners([deviceId])` immediately
   on open. Call `useDeviceErrors.refetch()` on the same tick.
2. **Recurring tick.** Every 10s thereafter, repeat both calls.
3. **Visibility gating.** If `document.visibilityState !== "visible"`,
   suspend the tick. Resume on `visibilitychange` back to "visible"
   and immediately run one fetch (don't wait for the next interval).
4. **Idle ceiling.** Track user interaction inside the modal
   (`mousemove`, `keydown`, `click`, `scroll`). After 10 minutes
   without any interaction, pause the poll and render a "Paused — click
   to resume" affordance. Clicking it resumes the loop with an
   immediate fetch.
5. **Cleanup.** On modal close / unmount, clear the interval and any
   visibility listeners.

The poll lives in a small dedicated hook
`client/src/protoFleet/components/StatusModal/useModalLiveRefresh.ts`
so the lifecycle logic is testable in isolation and StatusModal stays
declarative.

### Merging results

Each tick:

- `refreshMiners` returns `{ snapshots, errors }`. Call
  `mergeMiners(snapshots)` so the parent's `useFleet` map updates. The
  list row beneath the modal stays consistent.
- The modal itself reads its `miner` from the parent map (by id)
  rather than from the snapshot it was opened with — so updates from
  `mergeMiners` flow into the modal on the same re-render.
- `useDeviceErrors.refetch()` updates error state independently. The
  hook already skips re-render when error ids haven't changed.

### Picking the latest miner from the map

Modal currently receives `miner` as a prop. Change the prop to
`deviceId: string` and have the modal look up the snapshot from
`useFleet`'s map via the existing outlet-context shape. Falls back to
the original snapshot for one render if the map hasn't repopulated
(rare; only on a back-to-back open).

### Error handling

- If `refreshMiners` returns the device id in `errors`, surface a
  subtle inline banner in the modal ("Couldn't refresh — last updated
  Xs ago"). Keep the last good snapshot visible.
- Successive failures don't compound — the next tick retries.
- If the modal's device goes missing entirely (e.g. unpaired in
  another tab), the modal closes with a toast.

### What does *not* change

- `useFleet`'s 60s list poll is untouched. The modal poll is additive.
- The status modal's error UI, layout, and existing actions stay the
  same.
- Other modals (firmware, pool config, etc.) are not affected.

## Test plan

**Component (`StatusModal.test.tsx` + `useModalLiveRefresh.test.ts`)**

- Mount with a visible modal → `refreshMiners` invoked immediately
  with `[deviceId]`.
- After 10s, `refreshMiners` invoked again.
- `document.visibilityState=hidden` → no tick fires.
- Back to "visible" → immediate fetch, then resume 10s cadence.
- 10 minutes of no interaction → poll pauses, "Paused" affordance
  renders; click resumes with immediate fetch.
- Unmount → interval cleared; no further calls observed.
- `refreshMiners` resolves with an error for the device → inline
  banner renders; previous snapshot remains visible; next tick clears
  the banner on success.

**Integration with `useFleet`**

- Modal tick merges returned snapshot into parent map; closing the
  modal does not "snap" the row to fresher data because the row was
  already updated.

**E2E (`just test-e2e-fleet`)**

- Open modal on an erroring miner; mutate fixture to clear the error;
  assert the modal reflects the cleared state within ~12s without
  user action.
- Open modal, switch tab away, mutate fixture, switch back → modal
  reflects change immediately on visibility restore.

## Risks and tradeoffs

- **Plugin load per open modal.** One modal open for 10 minutes = ~60
  fetches against one device. Visibility + idle gating bound the
  worst case. Per-device server-side debounce (2s, from predecessor
  PR) prevents two simultaneously open modals from doubling load.
- **Prop change is a small API break inside the client.** Switching
  `StatusModal` from `miner` to `deviceId` requires touching all call
  sites. Limited blast radius; all under `protoFleet/`.
- **Idle ceiling chosen by intuition.** 10 minutes feels like a
  reasonable bound for "tech actively watching this miner." If
  telemetry shows real users sitting on modals for longer, lift the
  ceiling; if they don't, lower it.
- **Tick cadence is fixed.** Not adaptive to backend load. If we ever
  see plugin saturation from many concurrent modals, the predecessor
  PR's debounce already caps redundant work; further mitigation
  (server-driven hint in the response) is an option but not in scope.
- **Future push channel.** If we later add SSE/websocket for live
  miner state, this hook becomes redundant and can be deleted without
  touching the RPC.
