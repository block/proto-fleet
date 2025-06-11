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
      className="sticky right-0 bottom-4 left-0 z-20 w-full"
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
