import { useEffect } from "react";
import { action } from "storybook/actions";
import MinerListComponent from "../MinerList";
import { miners } from "@/protoFleet/features/fleetManagement/components/MinerList/stories/mocks";
import { useFleetStore } from "@/protoFleet/store";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

export const MinerList = () => {
  const setMiners = useFleetStore((state) => state.fleet.setMiners);

  useEffect(() => {
    setMiners(miners);
  }, [setMiners]);

  const minerIds = miners.map((miner) => miner.deviceIdentifier);

  return (
    <div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <MinerListComponent
        title="Miners"
        minerIds={minerIds}
        onAddMiners={action("onAddMiners")}
      />
    </div>
  );
};

export default {
  title: "Proto Fleet/MinerList",
};
