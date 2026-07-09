import { useRef, useState } from "react";

import type { MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerSelectionList, {
  type FilterConfig,
  type MinerSelectionListHandle,
} from "@/protoFleet/components/MinerSelectionList";
import type { SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import Modal from "@/shared/components/Modal";

export interface MinerSelectionValue {
  selectedMinerIds: string[];
  allSelected: boolean;
  totalMiners: number | undefined;
  filter?: MinerListFilter;
}

interface MinerSelectionModalProps {
  open: boolean;
  allMinersSelected?: boolean;
  selectedMinerIds: string[];
  // Soft default from the topbar SitePicker; forwarded to MinerSelectionList.
  scope?: SiteFilterFields;
  // Forwarded to MinerSelectionList to hide facets whose list RPCs the caller
  // can't call (e.g. rack/group filters need rack:read).
  filterConfig?: FilterConfig;
  onDismiss: () => void;
  onSave: (selection: MinerSelectionValue) => void;
}

const MinerSelectionModal = ({
  open,
  allMinersSelected = false,
  selectedMinerIds,
  scope,
  filterConfig,
  onDismiss,
  onSave,
}: MinerSelectionModalProps) => {
  const selectionRef = useRef<MinerSelectionListHandle>(null);
  const [draftSelection, setDraftSelection] = useState<MinerSelectionValue>({
    selectedMinerIds,
    allSelected: allMinersSelected,
    totalMiners: undefined,
  });

  if (!open) {
    return null;
  }

  const getSelectionValue = (): MinerSelectionValue => {
    const selection = selectionRef.current?.getSelection();
    if (!selection) {
      return draftSelection;
    }

    return {
      selectedMinerIds: selection.selectedItems,
      allSelected: selection.allSelected,
      totalMiners: selection.totalMiners,
      filter: selection.filter,
    };
  };

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title="Select miners"
      size="large"
      className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col"
      divider={false}
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: () => onSave(getSelectionValue()),
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col gap-4">
        <MinerSelectionList
          ref={selectionRef}
          key={`${allMinersSelected ? "all" : "subset"}:${selectedMinerIds.join(",")}`}
          initialAllSelected={allMinersSelected}
          initialSelectedItems={selectedMinerIds}
          disableFilteredSelectAll
          scope={scope}
          filterConfig={filterConfig}
          onSelectionChange={({ selectedItems, allSelected, totalMiners }) =>
            setDraftSelection({ selectedMinerIds: selectedItems, allSelected, totalMiners })
          }
        />
      </div>
    </Modal>
  );
};

export default MinerSelectionModal;
