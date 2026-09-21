import { useEffect } from "react";

import Tanks from "./Tanks";
import { tanksFixture } from "./Tanks.fixtures";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import useMinerStore from "@/protoOS/store/useMinerStore";

const TanksStory = ({
  count = 2,
  modules = 16,
  cooling = false,
}: {
  count?: number;
  modules?: number;
  cooling?: boolean;
}) => {
  useEffect(() => {
    const fixture = tanksFixture(count, modules, cooling);
    useMinerStore.getState().resetDeviceData();
    useMinerStore
      .getState()
      .systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1-story", product: "CT1" } }));
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
  }, [count, modules, cooling]);
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
export const PlantReadings = () => <TanksStory count={6} cooling />;

export default { title: "Proto OS/Containers/Tanks", component: Tanks };
