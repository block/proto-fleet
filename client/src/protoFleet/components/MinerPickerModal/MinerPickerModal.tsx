import { useCallback, useRef, useState } from "react";

import { type MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerSelectionList, {
  type DeviceListItem,
  type MinerSelectionListHandle,
} from "@/protoFleet/components/MinerSelectionList";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";

// Result returned to the caller when the operator confirms the
// selection. `allSelected` + `filter` mirror MinerSelectionList's
// "select all" semantics — when true, the caller must paginate the
// fleet API with the supplied filter to resolve the full ID set since
// the visible page only carried the current page's IDs.
export interface MinerPickerConfirmPayload {
  selectedIds: string[];
  allSelected: boolean;
  filter?: MinerListFilter;
}

interface MinerPickerModalProps {
  show: boolean;
  // Modal title — defaults to "Select miners". Callers can specialize
  // (e.g. "Add miners to North") so the picker reads naturally inside
  // the calling flow.
  title?: string;
  // Confirm button copy. Defaults to "Continue" to match the existing
  // ManageMinersModal wording; callers wiring to a terminal action
  // (e.g. assigning to a site) may prefer "Add" or "Confirm".
  confirmButtonText?: string;
  // Pre-checked IDs when the modal opens. Empty means "nothing
  // selected" — the picker preserves the existing
  // ManageMinersModal behavior of pre-checking the current member set.
  initialSelectedIds?: string[];
  // Optional per-row disable predicate (e.g. "miner is in another
  // rack"). Rows match against the rendered DeviceListItem, so this
  // also serves as the disable-reason hook.
  isRowDisabled?: (item: DeviceListItem) => boolean;
  // Hard cap on the number of items the caller can accept. When the
  // operator confirms with more rows selected than `maxSelection`, the
  // picker surfaces a Callout with `overflowMessage` and short-circuits
  // the confirm. Skipped entirely when undefined (the new Add-miners
  // flows have no slot-count ceiling).
  maxSelection?: number;
  // Message rendered inside the overflow Callout. The picker passes the
  // selected count into the renderer so callers can quote it back.
  buildOverflowMessage?: (selectedCount: number, max: number) => string;
  // Default overflow message used when buildOverflowMessage is omitted.
  overflowMessage?: string;
  onDismiss: () => void;
  onConfirm: (payload: MinerPickerConfirmPayload) => void;
}

// Generic miner picker — same Modal + MinerSelectionList shell that
// previously lived inside rackManagement/.../ManageMinersModal,
// hoisted to the shared components/ tree so reassignment flows from
// the /fleet ellipsis menus (Add miners → site / building) can reuse
// it. Rack-specific behavior (slot-count cap, current-rack disable
// predicate) now ships as props so the picker stays domain-agnostic.
const MinerPickerModal = ({
  show,
  title = "Select miners",
  confirmButtonText = "Continue",
  initialSelectedIds,
  isRowDisabled,
  maxSelection,
  buildOverflowMessage,
  overflowMessage,
  onDismiss,
  onConfirm,
}: MinerPickerModalProps) => {
  const selectionRef = useRef<MinerSelectionListHandle>(null);
  const [overflowError, setOverflowError] = useState("");

  const handleContinue = useCallback(() => {
    const selection = selectionRef.current?.getSelection();
    if (!selection) return;
    const { selectedItems, allSelected, filter } = selection;

    if (!allSelected && maxSelection !== undefined && selectedItems.length > maxSelection) {
      // Mirror the existing ManageMinersModal copy when no message is
      // supplied — keeps the rack flow byte-for-byte compatible while
      // letting new callers customize via buildOverflowMessage.
      const message =
        buildOverflowMessage?.(selectedItems.length, maxSelection) ??
        overflowMessage ??
        `Cannot select more than ${maxSelection} miners. Deselect some miners.`;
      setOverflowError(message);
      return;
    }

    onConfirm({ selectedIds: selectedItems, allSelected, filter: allSelected ? filter : undefined });
  }, [buildOverflowMessage, maxSelection, onConfirm, overflowMessage]);

  if (!show) return null;

  return (
    <Modal
      open={show}
      title={title}
      size="large"
      className="flex !h-[calc(100vh-(--spacing(32)))] max-h-[calc(100vh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col overflow-hidden"
      onDismiss={onDismiss}
      divider={false}
      buttons={[
        {
          text: confirmButtonText,
          variant: "primary",
          onClick: handleContinue,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col">
        {overflowError ? (
          <Callout className="mb-4 shrink-0" intent="danger" prefixIcon={<Alert />} title={overflowError} />
        ) : null}
        <MinerSelectionList
          ref={selectionRef}
          filterConfig={{ showTypeFilter: true, showRackFilter: false, showGroupFilter: false }}
          initialSelectedItems={initialSelectedIds}
          isRowDisabled={isRowDisabled}
        />
      </div>
    </Modal>
  );
};

export default MinerPickerModal;
