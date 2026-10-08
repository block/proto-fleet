import { useRef } from "react";
import { runTankMiningAction } from "./runTankMiningAction";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import BulkActionConfirmDialog from "@/protoFleet/features/fleetManagement/components/BulkActions/BulkActionConfirmDialog";
import { variants } from "@/shared/components/Button";

export default function TankMiningDialog({
  tanks,
  mining,
  onClose,
  onRefresh,
  onActionComplete,
}: {
  tanks: InventoryItem[];
  mining: boolean;
  onClose: () => void;
  onRefresh: () => void;
  onActionComplete?: () => void;
}) {
  const submitted = useRef(false);
  const action = mining ? "Start mining" : "Stop mining";
  const submit = () => {
    if (submitted.current) return;
    submitted.current = true;
    const tankIds = tanks.map((tank) => tank.ref!.id);
    onClose();
    void runTankMiningAction(tankIds, mining, onRefresh, onActionComplete);
  };
  return (
    <BulkActionConfirmDialog
      open
      testId="tank-mining-dialog"
      actionConfirmation={{
        title: `${action} on ${tanks.length} ${tanks.length === 1 ? "tank" : "tanks"}?`,
        subtitle: `All modules in ${tanks.length === 1 ? "this tank" : "these tanks"} will ${mining ? "start" : "stop"} hashing.`,
        confirmAction: { title: action, variant: variants.primary },
        testId: "tank-mining-confirm-button",
      }}
      onConfirmation={submit}
      onCancel={() => {
        onClose();
        onActionComplete?.();
      }}
    />
  );
}
