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

// `idx_one_rack_per_device` enforces a single rack per miner. Adding a
// miner from rack A to rack B would violate that unique index and abort
// the whole batch. Surface a specific error before dispatch, listing
// the conflicting rack labels we can identify from snapshots in scope.
const crossRackConflictMessage = (
  ids: string[],
  miners: Record<string, MinerStateSnapshot> | undefined,
  targetRack: DeviceSet,
  currentMembers: Set<string>,
): string | null => {
  if (!miners) return null;
  const targetLabel = targetRack.label || "rack";
  const conflictRackLabels = new Set<string>();
  let conflictCount = 0;
  for (const id of ids) {
    if (currentMembers.has(id)) continue;
    const snapshot = miners[id];
    if (!snapshot) continue;
    const rackLabel = snapshot.rackLabel;
    if (rackLabel && rackLabel !== targetLabel) {
      conflictCount += 1;
      conflictRackLabels.add(rackLabel);
    }
  }
  if (conflictCount === 0) return null;
  const labels = Array.from(conflictRackLabels).slice(0, 3).join(", ");
  const more = conflictRackLabels.size > 3 ? ` and ${conflictRackLabels.size - 3} other rack(s)` : "";
  return `${conflictCount} of the selected miners are already in ${labels}${more}. Remove them from their current rack before adding to "${targetLabel}".`;
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
  const { addDevicesToDeviceSet, getDeviceSet } = useDeviceSets();

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
    const crossRack = crossRackConflictMessage(ids, minerSnapshots, rack, currentMembers);
    if (crossRack) {
      pushToast({ message: crossRack, status: STATUSES.error });
      return;
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
