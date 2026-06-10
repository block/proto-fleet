import { useCallback, useRef, useState } from "react";

import { type MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerSelectionList, {
  type DeviceListItem,
  type MinerSelectionListHandle,
} from "@/protoFleet/components/MinerSelectionList";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";

// When `allSelected` is true the caller must paginate the fleet API
// with `filter` to resolve the full ID set (`selectedIds` only holds
// the visible page).
export interface MinerPickerConfirmPayload {
  selectedIds: string[];
  allSelected: boolean;
  filter?: MinerListFilter;
}

interface MinerPickerModalProps {
  show: boolean;
  title?: string;
  confirmButtonText?: string;
  initialSelectedIds?: string[];
  isRowDisabled?: (item: DeviceListItem) => boolean;
  maxSelection?: number;
  buildOverflowMessage?: (selectedCount: number, max: number) => string;
  overflowMessage?: string;
  onDismiss: () => void;
  onConfirm: (payload: MinerPickerConfirmPayload) => void;
}

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
