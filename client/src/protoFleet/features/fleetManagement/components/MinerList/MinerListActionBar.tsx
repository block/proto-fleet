import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import MinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu";

interface MinerListActionBarProps {
  selectedMiners: string[];
}

const MinerListActionBar = ({ selectedMiners }: MinerListActionBarProps) => {
  return (
    <ActionBar
      className="fixed bottom-4 z-20"
      selectedItems={selectedMiners}
      renderActions={(setHidden) => (
        <MinerActionsMenu
          selectedMiners={selectedMiners}
          onActionStart={() => setHidden(true)}
          onActionComplete={() => setHidden(false)}
        />
      )}
    />
  );
};

export default MinerListActionBar;
