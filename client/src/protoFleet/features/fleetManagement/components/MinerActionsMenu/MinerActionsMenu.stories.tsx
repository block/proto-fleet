import { useState } from "react";
import { action } from "storybook/actions";
import MinerActionsMenuComponent from ".";

export const MinerActionsMenu = () => {
  const [selectedMiners] = useState(["miner-1", "miner-2", "miner-3"]);

  return (
    <div className="flex h-screen items-center justify-center bg-grayscale-gray-87">
      <MinerActionsMenuComponent
        selectedMiners={selectedMiners}
        selectionMode="subset"
        onActionStart={action("Action started")}
        onActionComplete={action("Action completed")}
      />
    </div>
  );
};

export default {
  title: "Proto Fleet/Miner Actions Menu",
  component: MinerActionsMenuComponent,
};
