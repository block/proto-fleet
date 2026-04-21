import { useState } from "react";

import MinerSelectionList from "@/protoFleet/components/MinerSelectionList";
import Modal from "@/shared/components/Modal";

interface MinerSelectionModalProps {
  open: boolean;
  selectedMinerIds: string[];
  onDismiss: () => void;
  onSave: (minerIds: string[]) => void;
}

const MinerSelectionModal = ({ open, selectedMinerIds, onDismiss, onSave }: MinerSelectionModalProps) => {
  const [draftSelection, setDraftSelection] = useState<string[]>(selectedMinerIds);

  if (!open) {
    return null;
  }

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title="Select miners"
      size="large"
      className="flex !h-[calc(100vh-(--spacing(32)))] max-h-[calc(100vh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col overflow-hidden"
      divider={false}
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: () => onSave(draftSelection),
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col gap-4">
        <MinerSelectionList
          key={selectedMinerIds.join(",")}
          initialSelectedItems={selectedMinerIds}
          onSelectionChange={({ selectedItems }) => setDraftSelection(selectedItems)}
        />
      </div>
    </Modal>
  );
};

export default MinerSelectionModal;
