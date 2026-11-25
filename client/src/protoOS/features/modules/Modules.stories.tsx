import { useEffect } from "react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import ModuleHashboardOverlay from "./ModuleHashboardOverlay";
import Modules from "./Modules";
import ModulesStoryLayout from "./ModulesStoryLayout";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import useMinerStore from "@/protoOS/store/useMinerStore";

const ModulesStory = ({
  tanks = 6,
  modules = 16,
  path = "/modules",
}: {
  tanks?: number;
  modules?: number;
  path?: string;
}) => {
  const ready = useMinerStore((state) => state.hardware.resource.status === "ready");
  useEffect(() => {
    const fixture = tanksFixture(tanks, modules);
    for (const board of fixture.telemetry.hashboards.values())
      board.reportedIssueCount = board.health === "fault" ? 2 : 0;
    const store = useMinerStore.getState();
    store.resetDeviceData();
    store.hardware.replaceHardware(fixture.hardware);
    store.telemetry.mergeTelemetry(fixture.telemetry);
    return () => useMinerStore.getState().resetDeviceData();
  }, [tanks, modules]);
  return ready ? (
    <MemoryRouter initialEntries={[path]}>
      <ModulesStoryLayout>
        <Routes>
          <Route path="/modules" element={<Modules />}>
            <Route path=":tankId/:moduleId" element={<Outlet />}>
              <Route path=":serial" element={<ModuleHashboardOverlay />} />
            </Route>
          </Route>
        </Routes>
      </ModulesStoryLayout>
    </MemoryRouter>
  ) : null;
};

export const AllModules = () => <ModulesStory />;
export const FullContainer = () => <ModulesStory modules={48} />;
export const NeedsAttention = () => <ModulesStory path="/modules?status=attention" />;
export const NoMatches = () => <ModulesStory path="/modules?tank=unavailable" />;
export const Empty = () => <ModulesStory modules={0} />;
export default { title: "Proto OS/Containers/Modules", parameters: { withRouter: false } };
