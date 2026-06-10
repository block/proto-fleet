import { create } from "@bufbuild/protobuf";

import { fleetManagementClient } from "@/protoFleet/api/clients";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  type MinerListFilter,
  MinerListFilterSchema,
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
// view and saw "deviceCount > totalSlots". Returns null when the
// target has room; otherwise returns the operator-facing message.
const rackOverflowMessage = (rack: DeviceSet, addCount: number): string | null => {
  const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
  if (!rackInfo) return null;
  const totalSlots = rackInfo.rows * rackInfo.columns;
  if (totalSlots <= 0) return null;
  const available = Math.max(0, totalSlots - rack.deviceCount);
  if (addCount <= available) return null;
  const label = rack.label || "rack";
  return `Can't add ${addCount} miners to "${label}" — only ${available} slot${available === 1 ? "" : "s"} available (${rack.deviceCount}/${totalSlots} full).`;
};

const resolveAllModeIds = async (filter: MinerListFilter): Promise<string[]> => {
  const collected: string[] = [];
  let cursor = "";
  let exhausted = false;
  for (let i = 0; i < MAX_SNAPSHOT_PAGES; i++) {
    const response = await fleetManagementClient.listMinerStateSnapshots({
      pageSize: SNAPSHOT_PAGE_SIZE,
      cursor,
      filter,
    });
    for (const miner of response.miners) collected.push(miner.deviceIdentifier);
    if (!response.cursor) {
      exhausted = true;
      break;
    }
    cursor = response.cursor;
  }
  if (!exhausted) {
    throw new Error(`Too many miners selected (over ${MAX_MINERS}). Filter the list and try again.`);
  }
  return collected;
};

const MinerReparentPicker = ({
  kind,
  deviceIdentifiers,
  selectionMode,
  currentFilter,
  totalCount,
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

  const dispatchReparent = async (targetId: bigint, ids: string[]) => {
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
    try {
      rack = await fetchRack(targetId);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Couldn't load rack.";
      pushToast({ message, status: STATUSES.error });
      return;
    }
    const overflow = rackOverflowMessage(rack, ids.length);
    if (overflow) {
      pushToast({ message: overflow, status: STATUSES.error });
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
          let ids: string[];
          try {
            ids = await resolveAllModeIds(effectiveFilter);
          } catch (err) {
            const message =
              err instanceof Error && err.message ? err.message : "Couldn't load selected miners. Try again.";
            updateToast(loadingToast, { message, status: STATUSES.error });
            return;
          }
          removeToast(loadingToast);
          if (ids.length === 0) {
            pushToast({ message: "No miners selected.", status: STATUSES.queued });
            return;
          }
          void dispatchReparent(targetId, ids);
          return;
        }

        if (deviceIdentifiers.length === 0) {
          pushToast({ message: "No miners selected.", status: STATUSES.queued });
          return;
        }
        void dispatchReparent(targetId, deviceIdentifiers);
      }}
    />
  );
};

export default MinerReparentPicker;
