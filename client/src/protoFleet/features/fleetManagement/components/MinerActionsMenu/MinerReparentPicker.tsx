import { create } from "@bufbuild/protobuf";

import { fleetManagementClient } from "@/protoFleet/api/clients";
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
  // Explicit selection ids. In all-mode this is the visible page only;
  // the full set is resolved via listMinerStateSnapshots before dispatch.
  deviceIdentifiers: string[];
  selectionMode: "subset" | "all";
  // Required when selectionMode === "all" so the snapshot pagination
  // hits the same filtered set the user sees.
  currentFilter?: MinerListFilter;
  // All-mode display total. Surfaces in the picker title so the
  // operator sees how many miners the action will affect.
  totalCount?: number;
  // Display string for the source — "12 miners" / "Miner foo". Surfaces
  // in the picker title and toast messages.
  sourceLabel: string;
  // Toast template used for success messaging — bulk wants the count
  // returned by the RPC, single-row wants the miner's name. Caller
  // picks; we don't try to derive.
  successMessage: (count: number | bigint, target: "site" | "rack") => string;
  onClose: () => void;
  onRefetchMiners?: () => void;
}

// Snapshot pagination cap mirrors FleetGroupActionsMenu — 50 pages of
// 1000 covers any realistic fleet cohort. We throw past the cap rather
// than silently truncating; the caller surfaces the message via toast.
const MAX_SNAPSHOT_PAGES = 50;
const SNAPSHOT_PAGE_SIZE = 1000;
const MAX_MINERS = MAX_SNAPSHOT_PAGES * SNAPSHOT_PAGE_SIZE;

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
  const { addDevicesToDeviceSet } = useDeviceSets();

  const dispatchReparent = (targetId: bigint, ids: string[]) => {
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
          // `parseFilterFromURL` returns undefined when the URL has no
          // filter params — i.e. the operator selected all miners on an
          // unfiltered miners page, which is the full fleet. Substitute
          // an empty filter (matches everything) rather than bailing.
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
            // `resolveAllModeIds` throws a specific message when the
            // selection exceeds MAX_MINERS so the operator sees the
            // real cause; generic RPC failures land in the same toast.
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
          dispatchReparent(targetId, ids);
          return;
        }

        if (deviceIdentifiers.length === 0) {
          pushToast({ message: "No miners selected.", status: STATUSES.queued });
          return;
        }
        dispatchReparent(targetId, deviceIdentifiers);
      }}
    />
  );
};

export default MinerReparentPicker;
