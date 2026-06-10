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

// Rack-context wrapper around MinerPickerModal — adds the slot cap +
// disable-rows-from-other-racks predicate.
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
      buildOverflowMessage={(count, max) =>
        `Cannot add ${count} miners with only ${max} available slots. Deselect some miners or update your rack settings.`
      }
      onDismiss={onDismiss}
      onConfirm={(payload) => onConfirm(payload.selectedIds, payload.allSelected, payload.filter)}
    />
  );
};

export default ManageMinersModal;
