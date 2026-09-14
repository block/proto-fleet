import { useEffect } from "react";

import Tanks from "./Tanks";
import { tanksFixture } from "./Tanks.fixtures";
import useMinerStore from "@/protoOS/store/useMinerStore";

const TanksStory = ({ count = 2, modules = 16 }: { count?: number; modules?: number }) => {
  useEffect(() => {
    const fixture = tanksFixture(count, modules);
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
  }, [count, modules]);
  return (
    <div className="min-h-screen bg-surface-base p-6 text-text-primary laptop:p-12">
      <div className="mx-auto max-w-[1090px]">
        <Tanks />
      </div>
    </div>
  );
};

export const TwoTanks = () => <TanksStory />;
export const LargerContainer = () => <TanksStory count={6} modules={48} />;

export default { title: "Proto OS/Containers/Tanks", component: Tanks };
