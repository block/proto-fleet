import { useCallback, useRef, useState } from "react";

import type { DeviceListItem, MinerSelectionListHandle } from "@/protoFleet/components/MinerSelectionList";
import MinerSelectionList from "@/protoFleet/components/MinerSelectionList";

import Modal from "@/shared/components/Modal";

interface SearchMinersModalProps {
  show: boolean;
  currentRackLabel: string;
  onDismiss: () => void;
  onConfirm: (selectedMinerId: string) => void;
}

export default function SearchMinersModal({ show, currentRackLabel, onDismiss, onConfirm }: SearchMinersModalProps) {
  const selectionRef = useRef<MinerSelectionListHandle>(null);
  const [hasSelection, setHasSelection] = useState(false);

  const isRowDisabled = useCallback(
    (item: DeviceListItem) => !!(item.rackLabel && item.rackLabel !== currentRackLabel),
    [currentRackLabel],
  );

  const handleConfirm = useCallback(() => {
    const selection = selectionRef.current?.getSelection();
    if (!selection || selection.selectedItems.length === 0) return;
    onConfirm(selection.selectedItems[0]);
  }, [onConfirm]);

  if (!show) return null;

  return (
    <Modal
      open={show}
      title="Search miners"
      size="large"
      onDismiss={onDismiss}
      divider={false}
      buttons={[
        {
          text: "Assign",
          variant: "primary",
          disabled: !hasSelection,
          onClick: handleConfirm,
          dismissModalOnClick: false,
        },
      ]}
    >
      <MinerSelectionList
        ref={selectionRef}
        filterConfig={{ showTypeFilter: true, showRackFilter: false, showGroupFilter: false }}
        isRowDisabled={isRowDisabled}
        singleSelect
        onSelectionChange={({ selectedItems }) => setHasSelection(selectedItems.length > 0)}
      />
    </Modal>
  );
}
