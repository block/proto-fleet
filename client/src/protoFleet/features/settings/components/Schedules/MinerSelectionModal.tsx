import { useRef, useState } from "react";

import MinerSelectionList, { type MinerSelectionListHandle } from "@/protoFleet/components/MinerSelectionList";
import type { SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import Modal from "@/shared/components/Modal";

interface MinerSelectionModalProps {
  open: boolean;
  selectedMinerIds: string[];
  // Soft default from the topbar SitePicker; forwarded to MinerSelectionList.
  scope?: SiteFilterFields;
  onDismiss: () => void;
  onSave: (minerIds: string[]) => void;
}

const MinerSelectionModal = ({ open, selectedMinerIds, scope, onDismiss, onSave }: MinerSelectionModalProps) => {
  const selectionRef = useRef<MinerSelectionListHandle>(null);
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
          onClick: () => onSave(selectionRef.current?.getSelection().selectedItems ?? draftSelection),
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col gap-4">
        <MinerSelectionList
          ref={selectionRef}
          key={selectedMinerIds.join(",")}
          initialSelectedItems={selectedMinerIds}
          scope={scope}
          onSelectionChange={({ selectedItems }) => setDraftSelection(selectedItems)}
        />
      </div>
    </Modal>
  );
};

export default MinerSelectionModal;
