import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import MinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu";
import { type SelectionMode } from "@/shared/components/List";

interface MinerListActionBarProps {
  selectedMiners: string[];
  onClearSelection?: () => void;
  selectionMode: SelectionMode;
  totalCount?: number;
}

const MinerListActionBar = ({
  selectedMiners,
  onClearSelection,
  selectionMode,
  totalCount,
}: MinerListActionBarProps) => {
  return (
    <ActionBar
      className="fixed bottom-4 z-20"
      selectedItems={selectedMiners}
      selectionMode={selectionMode}
      totalCount={totalCount}
      onClose={onClearSelection}
      renderActions={(setHidden) => (
        <MinerActionsMenu
          selectedMiners={selectedMiners}
          selectionMode={selectionMode}
          totalCount={totalCount}
          onActionStart={() => setHidden(true)}
          onActionComplete={() => setHidden(false)}
        />
      )}
    />
  );
};

export default MinerListActionBar;
