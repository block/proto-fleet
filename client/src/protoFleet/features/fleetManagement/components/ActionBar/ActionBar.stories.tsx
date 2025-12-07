import ActionBarComponent from ".";
import MinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

interface ActionBarArgs {
  numberOfMiners: number;
}

export const ActionBar = ({ numberOfMiners }: ActionBarArgs) => {
  const selectedMiners = Array(numberOfMiners).fill("MinerId");

  return (
    <div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <ActionBarComponent
        className="fixed right-0 bottom-4 left-0 z-20"
        selectedItems={selectedMiners}
        selectionMode="subset"
        renderActions={(setHidden) => (
          <MinerActionsMenu
            selectedMiners={selectedMiners}
            selectionMode="subset"
            onActionStart={() => setHidden(true)}
            onActionComplete={() => setHidden(false)}
          />
        )}
      />
    </div>
  );
};

export default {
  title: "Proto Fleet/Action Bar",
  args: {
    numberOfMiners: 1,
  },
  argTypes: {
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
  },
};
