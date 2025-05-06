import MinerListComponent from "../index";
import { miners } from "@/protoFleet/features/fleetManagement/components/MinerList/stories/mocks";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

export const MinerList = () => {
  return (
    <div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <MinerListComponent title="Miners" miners={miners} />
    </div>
  );
};

export default {
  title: "Components (ProtoFleet)/MinerList",
};
