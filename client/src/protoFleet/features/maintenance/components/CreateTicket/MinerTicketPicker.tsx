import { useMemo, useRef } from "react";
import { create } from "@bufbuild/protobuf";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerSelectionList, { type MinerSelectionListHandle } from "@/protoFleet/components/MinerSelectionList";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface MinerTicketPickerProps {
  selected?: string;
  manageableSiteIds: string[];
  onSelect: (identifier: string) => void;
  onDismiss: () => void;
}
const MinerTicketPicker = ({ selected, manageableSiteIds, onSelect, onDismiss }: MinerTicketPickerProps) => {
  const ref = useRef<MinerSelectionListHandle>(null);
  const initialFilter = useMemo(
    () => create(MinerListFilterSchema, { siteIds: manageableSiteIds.map(BigInt), includeUnassigned: true }),
    [manageableSiteIds],
  );
  return (
    <Modal
      open
      title="Select miner"
      onDismiss={onDismiss}
      buttons={[
        { text: "Cancel", variant: variants.secondary, onClick: onDismiss, dismissModalOnClick: false },
        {
          text: "Use selected miner",
          variant: variants.primary,
          onClick: () => {
            const identifier = ref.current?.getSelection().selectedItems[0];
            if (identifier) onSelect(identifier);
          },
          dismissModalOnClick: false,
        },
      ]}
    >
      <MinerSelectionList
        ref={ref}
        singleSelect
        initialSelectedItems={selected ? [selected] : []}
        initialFilter={initialFilter}
        disableFilteredSelectAll
        showSelectAllFooter={false}
        filterConfig={{ showTypeFilter: true, showSiteFilter: false, showBuildingFilter: true, showRackFilter: true }}
      />
    </Modal>
  );
};
export default MinerTicketPicker;
