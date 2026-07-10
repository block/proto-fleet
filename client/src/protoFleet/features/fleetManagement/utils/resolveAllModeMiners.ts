import { fleetManagementClient } from "@/protoFleet/api/clients";
import type {
  MinerListFilter,
  MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// A filtered/scoped "select all" spans pages, but DeviceSet mutations
// (rack/site/building/group assignment) take an explicit device list — the
// device_set/common selectors can't carry the rich MinerListFilter. So we
// resolve the filter to concrete identifiers client-side by paginating the
// snapshot list, bounded so an unfiltered whole-fleet selection can't page
// forever.
export const SNAPSHOT_PAGE_SIZE = 1000;
export const MAX_SNAPSHOT_PAGES = 50;
export const MAX_MINERS = MAX_SNAPSHOT_PAGES * SNAPSHOT_PAGE_SIZE;

/**
 * Paginate listMinerStateSnapshots for `filter` and return every matching
 * device identifier plus its snapshot (snapshots power conflict/placement
 * counting). Throws when the result exceeds MAX_MINERS so callers surface a
 * "filter the list and try again" message instead of silently truncating. On
 * abort it resolves with whatever was accumulated so the caller's
 * `signal.aborted` gate can exit quietly.
 */
export const resolveAllModeIds = async (
  filter: MinerListFilter,
  signal?: AbortSignal,
): Promise<{ ids: string[]; snapshots: Record<string, MinerStateSnapshot> }> => {
  const ids: string[] = [];
  const snapshots: Record<string, MinerStateSnapshot> = {};
  let cursor = "";
  let exhausted = false;
  for (let i = 0; i < MAX_SNAPSHOT_PAGES; i++) {
    let response;
    try {
      response = await fleetManagementClient.listMinerStateSnapshots(
        {
          pageSize: SNAPSHOT_PAGE_SIZE,
          cursor,
          filter,
        },
        { signal },
      );
    } catch (err) {
      // listMinerStateSnapshots rejects on abort; return the partial
      // accumulators so the caller's signal.aborted gate can swallow the
      // early-exit quietly instead of routing to a toast.
      if (signal?.aborted || (err as Error)?.name === "AbortError") {
        return { ids, snapshots };
      }
      throw err;
    }
    if (signal?.aborted) return { ids, snapshots };
    for (const miner of response.miners) {
      ids.push(miner.deviceIdentifier);
      snapshots[miner.deviceIdentifier] = miner;
    }
    if (!response.cursor) {
      exhausted = true;
      break;
    }
    cursor = response.cursor;
  }
  if (!exhausted) {
    throw new Error(`Too many miners selected (over ${MAX_MINERS}). Filter the list and try again.`);
  }
  return { ids, snapshots };
};
