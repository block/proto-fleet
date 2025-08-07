import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import DeviceWidget from "@/protoFleet/features/fleetManagement/components/ActionBar/DeviceWidget";
import PerformanceWidget from "@/protoFleet/features/fleetManagement/components/ActionBar/PerformanceWidget";
import SettingsWidget from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget";

interface MinerListActionBarProps {
  selectedMiners: string[];
}

const MinerListActionBar = ({ selectedMiners }: MinerListActionBarProps) => {
  return (
    <ActionBar
      className="fixed bottom-4 z-20"
      selectedItems={selectedMiners}
      renderActions={(numberOfItems, setHidden) => (
        <>
          <DeviceWidget selectedMiners={selectedMiners} setHidden={setHidden} />
          <PerformanceWidget
            numberOfMiners={numberOfItems}
            setHidden={setHidden}
          />
          <SettingsWidget
            numberOfMiners={numberOfItems}
            setHidden={setHidden}
          />
        </>
      )}
    />
  );
};

export default MinerListActionBar;
