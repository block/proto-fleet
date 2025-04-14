import ActionBar from "@/protoFleet/components/ActionBar";
import DeviceWidget from "@/protoFleet/components/ActionBar/DeviceWidget";
import PerformanceWidget from "@/protoFleet/components/ActionBar/PerformanceWidget";
import SettingsWidget from "@/protoFleet/components/ActionBar/SettingsWidget";

interface MinerListActionBarProps {
  selectedMiners: string[];
}

const MinerListActionBar = ({ selectedMiners }: MinerListActionBarProps) => {
  return (
    <ActionBar
      className="fixed right-0 bottom-4 left-0 z-20"
      selectedItems={selectedMiners}
      renderActions={(numberOfItems, setHidden) => (
        <>
          <DeviceWidget numberOfMiners={numberOfItems} setHidden={setHidden} />
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
