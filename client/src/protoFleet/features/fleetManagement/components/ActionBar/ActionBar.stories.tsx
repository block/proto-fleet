import ActionBarComponent from ".";
import DeviceWidget from "@/protoFleet/features/fleetManagement/components/ActionBar/DeviceWidget";
import PerformanceWidget from "@/protoFleet/features/fleetManagement/components/ActionBar/PerformanceWidget";
import SettingsWidget from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

interface ActionBarArgs {
  numberOfMiners: number;
  numberOfActions: number;
}

export const ActionBar = ({
  numberOfMiners,
  numberOfActions,
}: ActionBarArgs) => {
  const renderActions = (
    numberOfItems: number,
    setHidden: (hidden: boolean) => void,
  ) => {
    return [
      <DeviceWidget
        key="device-widget"
        selectedMiners={Array(numberOfMiners).fill("MinerId")}
        setHidden={setHidden}
      />,
      <PerformanceWidget
        key="performance-widget"
        numberOfMiners={numberOfItems}
        setHidden={setHidden}
      />,
      <SettingsWidget
        key="settings-widget"
        numberOfMiners={numberOfItems}
        setHidden={setHidden}
      />,
    ].slice(0, numberOfActions);
  };

  return (
    <div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <ActionBarComponent
        className="fixed right-0 bottom-4 left-0 z-20"
        selectedItems={Array(numberOfMiners).fill("MAC")}
        renderActions={renderActions}
      />
    </div>
  );
};

export default {
  title: "Components (ProtoFleet)/Action Bar",
  args: {
    numberOfMiners: 1,
    numberOfActions: 2,
  },
  argTypes: {
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
    numberOfActions: {
      control: { type: "range", min: 1, max: 3, step: 1 },
    },
  },
};
