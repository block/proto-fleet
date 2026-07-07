import { useCallback, useRef, useState } from "react";

import { type MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerSelectionList, {
  type MinerEligibility,
  type MinerSelectionListHandle,
} from "@/protoFleet/components/MinerSelectionList";

import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";

interface ManageMinersModalProps {
  show: boolean;
  currentRackMiners: string[];
  /** Target rack placement. Drives the "Show assignable only" toggle and the
   *  id-based eligibility filter. */
  eligibility: MinerEligibility;
  maxSlots: number;
  onDismiss: () => void;
  /** `reassignedItems` is the subset of the explicit selection that is currently
   *  assigned elsewhere, so the caller can confirm the reparent (empty when
   *  `allSelected`, since that path is pre-filtered to assignable miners). */
  onConfirm: (
    selectedIds: string[],
    allSelected: boolean,
    filter: MinerListFilter | undefined,
    reassignedItems: string[],
  ) => void;
}

export default function ManageMinersModal({
  show,
  currentRackMiners,
  eligibility,
  maxSlots,
  onDismiss,
  onConfirm,
}: ManageMinersModalProps) {
  const selectionRef = useRef<MinerSelectionListHandle>(null);
  const [overflowError, setOverflowError] = useState("");

  const handleContinue = useCallback(() => {
    const selection = selectionRef.current?.getSelection();
    if (!selection) return;

    const { selectedItems, allSelected, filter, reassignedItems } = selection;

    // Only validate overflow for explicit selections. When allSelected is true,
    // the parent resolves the full selectable list via server pagination and
    // validates overflow after resolution.
    if (!allSelected && selectedItems.length > maxSlots) {
      setOverflowError(
        `Cannot add ${selectedItems.length} miners with only ${maxSlots} available slots. Deselect some miners or update your rack settings.`,
      );
      return;
    }

    onConfirm(selectedItems, allSelected, allSelected ? filter : undefined, reassignedItems);
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
          filterConfig={{
            showTypeFilter: true,
            showSubnetFilter: true,
            showSiteFilter: true,
            showBuildingFilter: true,
            showRackFilter: true,
            showGroupFilter: true,
          }}
          initialSelectedItems={currentRackMiners}
          eligibility={eligibility}
        />
      </div>
    </Modal>
  );
}
