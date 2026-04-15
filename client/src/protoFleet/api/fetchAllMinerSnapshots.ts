import { fleetManagementClient } from "@/protoFleet/api/clients";
import type {
  MinerListFilter,
  MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type MinerListFilterInit = Omit<Partial<MinerListFilter>, "$typeName" | "$unknown">;

/**
 * Paginate through all pages of `ListMinerStateSnapshots` and return a
 * map of `deviceIdentifier → MinerStateSnapshot`.
 *
 * The server caps `page_size` at 1000, so device sets with more members
 * require multiple round-trips. Results are accumulated locally and
 * returned only after every page succeeds — callers never see partial data.
 */
export async function fetchAllMinerSnapshots(
  filter: MinerListFilterInit,
  signal?: AbortSignal,
): Promise<Record<string, MinerStateSnapshot>> {
  const map: Record<string, MinerStateSnapshot> = {};
  let cursor = "";

  do {
    if (signal?.aborted) {
      throw new DOMException("The operation was aborted.", "AbortError");
    }

    const response = await fleetManagementClient.listMinerStateSnapshots(
      { pageSize: 1000, cursor, filter },
      { signal },
    );

    for (const miner of response.miners) {
      map[miner.deviceIdentifier] = miner;
    }

    cursor = response.cursor;
  } while (cursor);

  return map;
}
