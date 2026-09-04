import { useRef } from "react";
import MinerSelectionList, { type MinerSelectionListHandle } from "@/protoFleet/components/MinerSelectionList";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface MinerTicketPickerProps {
  selected?: string;
  onSelect: (identifier: string) => void;
  onDismiss: () => void;
}
const MinerTicketPicker = ({ selected, onSelect, onDismiss }: MinerTicketPickerProps) => {
  const ref = useRef<MinerSelectionListHandle>(null);
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
        disableFilteredSelectAll
        showSelectAllFooter={false}
        filterConfig={{ showTypeFilter: true, showSiteFilter: true, showBuildingFilter: true, showRackFilter: true }}
      />
    </Modal>
  );
};
export default MinerTicketPicker;
