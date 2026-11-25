import { useEffect } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";

import { moduleFixture, type ModuleFixtureState } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import ListContentLayout from "@/protoOS/components/ContentLayout/ListContentLayout";
import HashboardTemperatureWrapper from "@/protoOS/features/diagnostic/components/HashboardTemperature/HashboardTemperatureWrapper";
import Modules from "@/protoOS/features/modules/Modules";
import useMinerStore from "@/protoOS/store/useMinerStore";

const ModuleStory = ({
  state = "healthy",
  table = false,
  tankLabel,
}: {
  state?: ModuleFixtureState;
  table?: boolean;
  tankLabel?: string;
}) => {
  const ready = useMinerStore((state) => state.hardware.resource.status === "ready");
  useEffect(() => {
    const fixture = moduleFixture(state);
    if (tankLabel && fixture.hardware.topology?.kind === "container") {
      for (const tank of fixture.hardware.topology.tanks.values()) tank.label = tankLabel;
    }
    const store = useMinerStore.getState();
    store.resetDeviceData();
    store.systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1-story", product: "CT1" } }));
    store.hardware.replaceHardware(fixture.hardware);
    store.telemetry.mergeTelemetry(fixture.telemetry);
    return () => useMinerStore.getState().resetDeviceData();
  }, [state, tankLabel]);
  if (!ready) return null;
  const modulesPage = (
    <ListContentLayout className="h-screen bg-surface-base text-text-primary">
      <Modules />
    </ListContentLayout>
  );
  return (
    <MemoryRouter initialEntries={[`/modules/1/1${table ? "/T1-M1-HB1" : ""}`]}>
      <Routes>
        <Route path="/modules" element={modulesPage} />
        <Route path="/modules/:tankId/:moduleId" element={modulesPage} />
        <Route path="/modules/:tankId/:moduleId/:serial" element={<HashboardTemperatureWrapper />} />
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
export const LongTankLabel = () => (
  <ModuleStory tankLabel="North container immersion cooling tank beside the service entrance" />
);
export const LongTankLabelAsicTable = () => (
  <ModuleStory table tankLabel="North container immersion cooling tank beside the service entrance" />
);

export default { title: "Proto OS/Containers/Module modal", parameters: { withRouter: false } };
