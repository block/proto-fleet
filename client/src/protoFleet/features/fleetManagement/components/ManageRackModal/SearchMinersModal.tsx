import { useCallback, useRef, useState } from "react";

import type { MinerEligibility, MinerSelectionListHandle } from "@/protoFleet/components/MinerSelectionList";
import MinerSelectionList from "@/protoFleet/components/MinerSelectionList";

import Modal from "@/shared/components/Modal";

interface SearchMinersModalProps {
  show: boolean;
  /** Target rack placement. Drives the "Show assignable only" toggle and the
   *  id-based eligibility filter (miners in another rack/building/site drop out
   *  or render disabled). */
  eligibility: MinerEligibility;
  onDismiss: () => void;
  onConfirm: (selectedMinerId: string) => void;
}

export default function SearchMinersModal({ show, eligibility, onDismiss, onConfirm }: SearchMinersModalProps) {
  const selectionRef = useRef<MinerSelectionListHandle>(null);
  const [hasSelection, setHasSelection] = useState(false);

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
        filterConfig={{
          showTypeFilter: true,
          showSubnetFilter: true,
          showSiteFilter: true,
          showBuildingFilter: true,
          showRackFilter: true,
          showGroupFilter: true,
        }}
        eligibility={eligibility}
        singleSelect
        onSelectionChange={({ selectedItems }) => setHasSelection(selectedItems.length > 0)}
      />
    </Modal>
  );
}
