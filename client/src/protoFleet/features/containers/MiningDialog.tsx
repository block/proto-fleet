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
  const noun = items.length === 1 ? kind : `${kind}s`;
  let affectedItems = items.map((item) => item.name).join(", ");
  if (kind === "tank") {
    const tankSelection = items.length === 1 ? "this tank" : "these tanks";
    affectedItems = `All modules in ${tankSelection}`;
  }
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
        title: `${action} on ${items.length} ${noun}?`,
        subtitle: `${affectedItems} will ${mining ? "start" : "stop"} hashing.`,
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
