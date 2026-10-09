import { useRef } from "react";
import { runMiningAction } from "./runMiningAction";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import BulkActionConfirmDialog from "@/protoFleet/features/fleetManagement/components/BulkActions/BulkActionConfirmDialog";
import { variants } from "@/shared/components/Button";

export default function MiningDialog({
  items,
  kind,
  mining,
  onClose,
  onRefresh,
  onActionComplete,
}: {
  items: InventoryItem[];
  kind: "tank" | "module";
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
    const ids = items.map((item) => item.ref!.id);
    onClose();
    void runMiningAction(ids, kind, mining, onRefresh, onActionComplete);
  };
  return (
    <BulkActionConfirmDialog
      open
      testId={`${kind}-mining-dialog`}
      actionConfirmation={{
        title: `${action} on ${items.length} ${items.length === 1 ? kind : `${kind}s`}?`,
        subtitle:
          kind === "tank"
            ? `All modules in ${items.length === 1 ? "this tank" : "these tanks"} will ${mining ? "start" : "stop"} hashing.`
            : `${items.map((module) => module.name).join(", ")} will ${mining ? "start" : "stop"} hashing.`,
        confirmAction: { title: action, variant: variants.primary },
        testId: `${kind}-mining-confirm-button`,
      }}
      onConfirmation={submit}
      onCancel={() => {
        onClose();
        onActionComplete?.();
      }}
    />
  );
}
