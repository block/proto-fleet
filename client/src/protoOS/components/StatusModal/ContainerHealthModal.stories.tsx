import { useEffect } from "react";
import StatusModal from "./StatusModal";
import { moduleFixture } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import type { ContainerTopology } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const HealthStory = ({ moduleOnly = false, unknown = false, healthOnly = false }) => {
  const topology = useMinerStore((state) => state.hardware.topology);
  useEffect(() => {
    const fixture = moduleFixture();
    const module = [...fixture.hardware.topology!.modules.values()][0];
    const board = fixture.hardware.hashboards.get(module.hashboardIds[0])!;
    const psu = fixture.hardware.psus.get(module.psuIds[0])!;
    if (unknown) fixture.telemetry.hashboards.clear();
    if (healthOnly) fixture.telemetry.psus.get(psu.ref.id)!.health = "fault";
    const store = useMinerStore.getState();
    store.resetDeviceData();
    store.systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1-story", product: "CT1" } }));
    store.hardware.replaceHardware(fixture.hardware);
    store.telemetry.mergeTelemetry(fixture.telemetry);
    if (!unknown && !healthOnly)
      store.minerStatus.setErrors([
        {
          id: "board",
          code: "HASHRAIL",
          message: "Hashrail fault",
          severity: "critical",
          component: board.ref,
          owner: board.ref.owner,
        },
        {
          id: "psu",
          code: "PSU_TEMPERATURE",
          message: "PSU over temperature",
          severity: "critical",
          component: psu.ref,
          owner: psu.ref.owner,
        },
      ]);
    return () => useMinerStore.getState().resetDeviceData();
  }, [unknown, healthOnly]);
  if (topology?.kind !== "container") return null;
  const tank = [...(topology as ContainerTopology).tanks.values()][0];
  return (
    <StatusModal
      scope={moduleOnly ? { kind: "module", id: tank.moduleIds[0] } : { kind: "tank", id: tank.id }}
      onClose={() => {}}
    />
  );
};

export const TankHealth = () => <HealthStory />;
export const ModuleHealth = () => <HealthStory moduleOnly />;
export const MissingReadings = () => <HealthStory unknown />;
export const FaultWithoutDetails = () => <HealthStory healthOnly />;

export default { title: "Proto OS/Containers/Health drill-down" };
