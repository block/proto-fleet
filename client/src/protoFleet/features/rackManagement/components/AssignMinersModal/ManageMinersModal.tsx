import { useCallback } from "react";

import { type MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerPickerModal from "@/protoFleet/components/MinerPickerModal";
import { type DeviceListItem } from "@/protoFleet/components/MinerSelectionList";

interface ManageMinersModalProps {
  show: boolean;
  currentRackMiners: string[];
  currentRackLabel: string;
  maxSlots: number;
  onDismiss: () => void;
  onConfirm: (selectedIds: string[], allSelected: boolean, filter?: MinerListFilter) => void;
}

// Rack-context wrapper around the shared MinerPickerModal. Preserves
// the original ManageMinersModal contract (rack-slot cap +
// disable-rows-from-other-racks predicate) so AssignMinersModal needs
// no change while the underlying picker gets reused by the /fleet
// "Add miners" flows.
const ManageMinersModal = ({
  show,
  currentRackMiners,
  currentRackLabel,
  maxSlots,
  onDismiss,
  onConfirm,
}: ManageMinersModalProps) => {
  const isRowDisabled = useCallback(
    (item: DeviceListItem) => !!(item.rackLabel && item.rackLabel !== currentRackLabel),
    [currentRackLabel],
  );

  return (
    <MinerPickerModal
      show={show}
      initialSelectedIds={currentRackMiners}
      isRowDisabled={isRowDisabled}
      maxSelection={maxSlots}
      // Preserves the original ManageMinersModal copy verbatim — the
      // rack-slot cap mentions the available slots so the operator
      // knows how many to deselect.
      buildOverflowMessage={(count, max) =>
        `Cannot add ${count} miners with only ${max} available slots. Deselect some miners or update your rack settings.`
      }
      onDismiss={onDismiss}
      onConfirm={(payload) => onConfirm(payload.selectedIds, payload.allSelected, payload.filter)}
    />
  );
};

export default ManageMinersModal;
