import { fetchAllMinerSnapshots } from "@/protoFleet/api/fetchAllMinerSnapshots";
import type { MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { MinerEligibility } from "@/protoFleet/components/MinerSelectionList";
import { FLEET_VISIBLE_PAIRING_STATUSES } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";
import { isMinerSnapshotIneligible } from "@/protoFleet/features/fleetManagement/utils/minerPlacement";

/** Fetch all miner IDs eligible for a rack by paginating through the fleet API.
 *  Applies the same filter the user had active in MinerSelectionList so "select all"
 *  respects model/subnet filters. Miners in a different rack/building/site are
 *  excluded id-based (matches the list's eligibility predicate) so "select all"
 *  can't pull in ineligible miners even if the assignable-only toggle was off.
 *  Uses the visible pairing set (not PAIRED-only) to stay aligned with the rack
 *  list's fetch — otherwise "select all" would resolve to fewer ids than the
 *  list shows and Save would silently drop a rack's non-paired members. */
export async function fetchAllSelectableMinerIds(
  eligibility: MinerEligibility,
  listFilter?: MinerListFilter,
): Promise<string[]> {
  const filter = listFilter
    ? { ...listFilter, pairingStatuses: FLEET_VISIBLE_PAIRING_STATUSES }
    : { pairingStatuses: FLEET_VISIBLE_PAIRING_STATUSES };
  const snapshots = await fetchAllMinerSnapshots(filter);
  return Object.values(snapshots)
    .filter((m) => !isMinerSnapshotIneligible(m, eligibility))
    .map((m) => m.deviceIdentifier);
}
