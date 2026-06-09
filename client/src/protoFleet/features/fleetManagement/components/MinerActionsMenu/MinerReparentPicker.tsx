import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export type ReparentKind = "rack" | "site";

interface MinerReparentPickerProps {
  kind: ReparentKind;
  deviceIdentifiers: string[];
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

const MinerReparentPicker = ({
  kind,
  deviceIdentifiers,
  sourceLabel,
  successMessage,
  onClose,
  onRefetchMiners,
}: MinerReparentPickerProps) => {
  const { reassignDevicesToSite } = useSites();
  const { addDevicesToDeviceSet } = useDeviceSets();

  return (
    <ParentPickerModal
      kind={kind}
      show
      selectionMode="single"
      sourceLabel={sourceLabel}
      onDismiss={onClose}
      onConfirm={(targetIds) => {
        const targetId = targetIds[0];
        onClose();
        if (targetId === undefined) return;
        if (deviceIdentifiers.length === 0) {
          pushToast({ message: "No miners selected.", status: STATUSES.queued });
          return;
        }
        if (kind === "site") {
          void reassignDevicesToSite({
            targetSiteId: targetId,
            deviceIdentifiers,
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
          deviceIdentifiers,
          onSuccess: (count) => {
            pushToast({ message: successMessage(count, "rack"), status: STATUSES.success });
            onRefetchMiners?.();
          },
          onError: (msg) => pushToast({ message: `Couldn't add miners to rack: ${msg}`, status: STATUSES.error }),
        });
      }}
    />
  );
};

export default MinerReparentPicker;
