import { useEffect } from "react";
import MinerListComponent from "../MinerList";
import { miners } from "@/protoFleet/features/fleetManagement/components/MinerList/stories/mocks";
import { useFleetStore } from "@/protoFleet/features/fleetManagement/store/useFleetStore";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

export const MinerList = () => {
  const { setMiners } = useFleetStore();

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
        onFilterChange={() => {}}
      />
    </div>
  );
};

export default {
  title: "Components (ProtoFleet)/MinerList",
};
