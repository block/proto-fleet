import { create } from "@bufbuild/protobuf";

import { fleetManagementClient } from "@/protoFleet/api/clients";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  type MinerListFilter,
  MinerListFilterSchema,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import { pushToast, removeToast, STATUSES, updateToast } from "@/shared/features/toaster";

export type ReparentKind = "rack" | "site";

interface MinerReparentPickerProps {
  kind: ReparentKind;
  // In all-mode this is the visible page only; the full set is
  // resolved via listMinerStateSnapshots before dispatch.
  deviceIdentifiers: string[];
  selectionMode: "subset" | "all";
  currentFilter?: MinerListFilter;
  totalCount?: number;
  // Snapshots keyed by deviceIdentifier — used by the rack guard to
  // detect cross-rack conflicts. Subset mode passes the caller's map;
  // all-mode builds it during resolveAllModeIds.
  miners?: Record<string, MinerStateSnapshot>;
  sourceLabel: string;
  successMessage: (count: number | bigint, target: "site" | "rack") => string;
  onClose: () => void;
  onRefetchMiners?: () => void;
}

const MAX_SNAPSHOT_PAGES = 50;
const SNAPSHOT_PAGE_SIZE = 1000;
const MAX_MINERS = MAX_SNAPSHOT_PAGES * SNAPSHOT_PAGE_SIZE;

// Capacity check for the bulk Add-to-rack path. Server-side
// AddDevicesToDeviceSet doesn't enforce slot count, so an over-fill
// here would persist invisibly until the operator opened the rack
// view. Discounts ids already in the rack — server uses
// `ON CONFLICT DO NOTHING`, so existing members aren't new additions.
// Returns null when the target has room.
const rackOverflowMessage = (rack: DeviceSet, currentMembers: Set<string>, ids: string[]): string | null => {
  const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
  if (!rackInfo) return null;
  const totalSlots = rackInfo.rows * rackInfo.columns;
  if (totalSlots <= 0) return null;
  const newAdditions = ids.filter((id) => !currentMembers.has(id)).length;
  const available = Math.max(0, totalSlots - rack.deviceCount);
  if (newAdditions <= available) return null;
  const label = rack.label || "rack";
  return `Can't add ${newAdditions} miners to "${label}" — only ${available} slot${available === 1 ? "" : "s"} available (${rack.deviceCount}/${totalSlots} full).`;
};

// Paginate listMinerStateSnapshots filtered to the target rack so the
// capacity guard can discount ids that are already members. Capped at
// MAX_MINERS for the same reason as resolveAllModeIds.
const resolveRackMembers = async (rackId: bigint): Promise<Set<string>> => {
  const filter = create(MinerListFilterSchema, { rackIds: [rackId] });
  const members = new Set<string>();
  let cursor = "";
  let exhausted = false;
  for (let i = 0; i < MAX_SNAPSHOT_PAGES; i++) {
    const response = await fleetManagementClient.listMinerStateSnapshots({
      pageSize: SNAPSHOT_PAGE_SIZE,
      cursor,
      filter,
    });
    for (const miner of response.miners) members.add(miner.deviceIdentifier);
    if (!response.cursor) {
      exhausted = true;
      break;
    }
    cursor = response.cursor;
  }
  if (!exhausted) {
    throw new Error(`Target rack has more than ${MAX_MINERS} miners. Refresh the page and retry.`);
  }
  return members;
};

// Group ids that need to move out of a source rack before they can be
// added to the target. `idx_one_rack_per_device` enforces a single
// rack per miner, so a same-batch INSERT against a different rack
// aborts the whole transaction; we orchestrate the remove→add ourselves.
// Returns a map of sourceRackLabel → device ids and any ids whose
// source rack label couldn't be resolved (those skip the remove step
// and fall through to the add — server-side conflict will surface).
const groupBySourceRack = (
  ids: string[],
  miners: Record<string, MinerStateSnapshot> | undefined,
  targetRack: DeviceSet,
  currentMembers: Set<string>,
): { movesByLabel: Map<string, string[]>; unknownSource: string[] } => {
  const movesByLabel = new Map<string, string[]>();
  const unknownSource: string[] = [];
  if (!miners) return { movesByLabel, unknownSource };
  const targetLabel = targetRack.label;
  for (const id of ids) {
    if (currentMembers.has(id)) continue;
    const snapshot = miners[id];
    if (!snapshot) continue;
    const sourceLabel = snapshot.rackLabel;
    if (!sourceLabel || sourceLabel === targetLabel) continue;
    const bucket = movesByLabel.get(sourceLabel) ?? [];
    bucket.push(id);
    movesByLabel.set(sourceLabel, bucket);
  }
  return { movesByLabel, unknownSource };
};

const resolveAllModeIds = async (
  filter: MinerListFilter,
): Promise<{ ids: string[]; snapshots: Record<string, MinerStateSnapshot> }> => {
  const ids: string[] = [];
  const snapshots: Record<string, MinerStateSnapshot> = {};
  let cursor = "";
  let exhausted = false;
  for (let i = 0; i < MAX_SNAPSHOT_PAGES; i++) {
    const response = await fleetManagementClient.listMinerStateSnapshots({
      pageSize: SNAPSHOT_PAGE_SIZE,
      cursor,
      filter,
    });
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

const MinerReparentPicker = ({
  kind,
  deviceIdentifiers,
  selectionMode,
  currentFilter,
  totalCount,
  miners,
  sourceLabel,
  successMessage,
  onClose,
  onRefetchMiners,
}: MinerReparentPickerProps) => {
  const { reassignDevicesToSite } = useSites();
  const { addDevicesToDeviceSet, getDeviceSet, listRacks, removeDevicesFromDeviceSet } = useDeviceSets();

  const fetchAllRackLabels = () =>
    new Promise<Map<string, bigint>>((resolve, reject) => {
      void listRacks({
        onSuccess: (racks) => {
          const map = new Map<string, bigint>();
          for (const rack of racks) {
            if (rack.label) map.set(rack.label, rack.id);
          }
          resolve(map);
        },
        onError: (msg) => reject(new Error(msg)),
      });
    });

  const removeFromRack = (rackId: bigint, ids: string[]) =>
    new Promise<void>((resolve, reject) => {
      void removeDevicesFromDeviceSet({
        deviceSetId: rackId,
        deviceIdentifiers: ids,
        onSuccess: () => resolve(),
        onError: (msg) => reject(new Error(msg)),
      });
    });

  const fetchRack = (rackId: bigint) =>
    new Promise<DeviceSet>((resolve, reject) => {
      void getDeviceSet({
        deviceSetId: rackId,
        onSuccess: resolve,
        onNotFound: () => reject(new Error("Couldn't find rack.")),
        onError: (msg) => reject(new Error(msg)),
      });
    });

  const dispatchReparent = async (
    targetId: bigint,
    ids: string[],
    minerSnapshots: Record<string, MinerStateSnapshot> | undefined,
  ) => {
    if (kind === "site") {
      void reassignDevicesToSite({
        targetSiteId: targetId,
        deviceIdentifiers: ids,
        onSuccess: (count) => {
          pushToast({ message: successMessage(count, "site"), status: STATUSES.success });
          onRefetchMiners?.();
        },
        onError: (msg) => pushToast({ message: `Couldn't move miners: ${msg}`, status: STATUSES.error }),
      });
      return;
    }
    let rack: DeviceSet;
    let currentMembers: Set<string>;
    try {
      [rack, currentMembers] = await Promise.all([fetchRack(targetId), resolveRackMembers(targetId)]);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Couldn't load rack.";
      pushToast({ message, status: STATUSES.error });
      return;
    }
    const overflow = rackOverflowMessage(rack, currentMembers, ids);
    if (overflow) {
      pushToast({ message: overflow, status: STATUSES.error });
      return;
    }

    // Miners currently in a different rack need to leave that rack
    // first — server enforces one rack per device. Orchestrate the
    // remove-then-add so the picker behaves as a re-parent rather than
    // a duplicate insert.
    const { movesByLabel } = groupBySourceRack(ids, minerSnapshots, rack, currentMembers);
    if (movesByLabel.size > 0) {
      let labelToRackId: Map<string, bigint>;
      try {
        labelToRackId = await fetchAllRackLabels();
      } catch (err) {
        const message = err instanceof Error ? err.message : "Couldn't load racks.";
        pushToast({ message, status: STATUSES.error });
        return;
      }
      const movingToast = pushToast({
        message: `Moving miners from ${movesByLabel.size} other rack${movesByLabel.size === 1 ? "" : "s"}…`,
        status: STATUSES.loading,
        longRunning: true,
      });
      try {
        for (const [sourceLabel, sourceIds] of movesByLabel) {
          const sourceRackId = labelToRackId.get(sourceLabel);
          if (sourceRackId === undefined) continue;
          await removeFromRack(sourceRackId, sourceIds);
        }
      } catch (err) {
        const message = err instanceof Error ? err.message : "Failed to remove miners from current rack.";
        updateToast(movingToast, { message, status: STATUSES.error });
        return;
      }
      removeToast(movingToast);
    }

    void addDevicesToDeviceSet({
      deviceSetId: targetId,
      deviceIdentifiers: ids,
      onSuccess: (count) => {
        pushToast({ message: successMessage(count, "rack"), status: STATUSES.success });
        onRefetchMiners?.();
      },
      onError: (msg) => pushToast({ message: `Couldn't add miners to rack: ${msg}`, status: STATUSES.error }),
    });
  };

  return (
    <ParentPickerModal
      kind={kind}
      show
      selectionMode="single"
      sourceLabel={
        selectionMode === "all" && totalCount !== undefined && totalCount !== deviceIdentifiers.length
          ? `${totalCount} miners`
          : sourceLabel
      }
      onDismiss={onClose}
      onConfirm={async (targetIds) => {
        const targetId = targetIds[0];
        onClose();
        if (targetId === undefined) return;

        if (selectionMode === "all") {
          // Undefined filter = no URL filter params = full fleet.
          const effectiveFilter = currentFilter ?? create(MinerListFilterSchema);
          const loadingToast = pushToast({
            message: "Loading selected miners…",
            status: STATUSES.loading,
            longRunning: true,
          });
          let resolved: { ids: string[]; snapshots: Record<string, MinerStateSnapshot> };
          try {
            resolved = await resolveAllModeIds(effectiveFilter);
          } catch (err) {
            const message =
              err instanceof Error && err.message ? err.message : "Couldn't load selected miners. Try again.";
            updateToast(loadingToast, { message, status: STATUSES.error });
            return;
          }
          removeToast(loadingToast);
          if (resolved.ids.length === 0) {
            pushToast({ message: "No miners selected.", status: STATUSES.queued });
            return;
          }
          void dispatchReparent(targetId, resolved.ids, resolved.snapshots);
          return;
        }

        if (deviceIdentifiers.length === 0) {
          pushToast({ message: "No miners selected.", status: STATUSES.queued });
          return;
        }
        void dispatchReparent(targetId, deviceIdentifiers, miners);
      }}
    />
  );
};

export default MinerReparentPicker;
