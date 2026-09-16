import { useEffect } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";

import { moduleFixture, type ModuleFixtureState } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import HashboardTemperatureWrapper from "@/protoOS/features/diagnostic/components/HashboardTemperature/HashboardTemperatureWrapper";
import Tanks from "@/protoOS/features/tanks/Tanks";
import useMinerStore from "@/protoOS/store/useMinerStore";

const ModuleStory = ({ state = "healthy", table = false }: { state?: ModuleFixtureState; table?: boolean }) => {
  const ready = useMinerStore((state) => state.hardware.resource.status === "ready");
  useEffect(() => {
    const fixture = moduleFixture(state);
    const store = useMinerStore.getState();
    store.resetDeviceData();
    store.systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1-story", product: "CT1" } }));
    store.hardware.replaceHardware(fixture.hardware);
    store.telemetry.mergeTelemetry(fixture.telemetry);
    return () => useMinerStore.getState().resetDeviceData();
  }, [state]);
  if (!ready) return null;
  return (
    <MemoryRouter initialEntries={[`/tanks/1/modules/1${table ? "/T1-M1-HB1" : ""}`]}>
      <Routes>
        <Route path="/tanks/:tankId" element={<Tanks />} />
        <Route path="/tanks/:tankId/modules/:moduleId" element={<Tanks />} />
        <Route path="/tanks/:tankId/modules/:moduleId/:serial" element={<HashboardTemperatureWrapper />} />
      </Routes>
    </MemoryRouter>
  );
};

export const Healthy = () => <ModuleStory />;
export const NeedsAttention = () => <ModuleStory state="attention" />;
export const MissingBoard = () => <ModuleStory state="missingBoard" />;
export const NoPsuData = () => <ModuleStory state="noPsu" />;
export const PartialPsuData = () => <ModuleStory state="partialPsu" />;
export const Ct1AsicTable = () => <ModuleStory table />;

export default { title: "Proto OS/Containers/Module modal", parameters: { withRouter: false } };
