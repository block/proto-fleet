import { useCallback, useRef, useState } from "react";

import { type MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerSelectionList, {
  type DeviceListItem,
  type MinerSelectionListHandle,
} from "@/protoFleet/components/MinerSelectionList";

import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";

interface ManageMinersModalProps {
  show: boolean;
  currentRackMiners: string[];
  currentRackLabel: string;
  maxSlots: number;
  onDismiss: () => void;
  onConfirm: (selectedIds: string[], allSelected: boolean, filter?: MinerListFilter) => void;
}

export default function ManageMinersModal({
  show,
  currentRackMiners,
  currentRackLabel,
  maxSlots,
  onDismiss,
  onConfirm,
}: ManageMinersModalProps) {
  const selectionRef = useRef<MinerSelectionListHandle>(null);
  const [overflowError, setOverflowError] = useState("");

  const isRowDisabled = useCallback(
    (item: DeviceListItem) => !!(item.rackLabel && item.rackLabel !== currentRackLabel),
    [currentRackLabel],
  );

  const handleContinue = useCallback(() => {
    const selection = selectionRef.current?.getSelection();
    if (!selection) return;

    const { selectedItems, allSelected, filter } = selection;

    // Only validate overflow for explicit selections. When allSelected is true,
    // the parent resolves the full selectable list via server pagination and
    // validates overflow after resolution.
    if (!allSelected && selectedItems.length > maxSlots) {
      setOverflowError(
        `Cannot add ${selectedItems.length} miners with only ${maxSlots} available slots. Deselect some miners or update your rack settings.`,
      );
      return;
    }

    onConfirm(selectedItems, allSelected, allSelected ? filter : undefined);
  }, [maxSlots, onConfirm]);

  if (!show) return null;

  return (
    <Modal
      open={show}
      title="Select miners"
      size="large"
      className="flex !h-[calc(100vh-(--spacing(32)))] max-h-[calc(100vh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col overflow-hidden"
      onDismiss={onDismiss}
      divider={false}
      buttons={[
        {
          text: "Continue",
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
          initialSelectedItems={currentRackMiners}
          isRowDisabled={isRowDisabled}
        />
      </div>
    </Modal>
  );
}
