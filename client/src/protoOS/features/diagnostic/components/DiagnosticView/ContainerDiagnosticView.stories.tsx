import { useEffect } from "react";

import ContainerDiagnosticView from "./ContainerDiagnosticView";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import { asComponentId, asEntityKey, type MetricTelemetry, type MetricUnit } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const reading = (value: number, units: MetricUnit): MetricTelemetry => ({ latest: { value, units } });

const DiagnosticsStory = ({ missingReadings = false, sparseTanks = false, minerStates = false }) => {
  useEffect(() => {
    const fixture = tanksFixture(6);
    const { hardware, telemetry, topology } = fixture;
    const plantId = asComponentId(`${topology.containerId}:plant`);
    if (minerStates) {
      for (const module of topology.modules.values()) {
        for (const id of module.hashboardIds) {
          const board = telemetry.hashboards.get(id)!;
          board.health = "ok";
          board.miningStatus = "mining";
          if (module.location?.moduleSlot === 8) {
            board.health = "offline";
            board.miningStatus = "unknown";
          } else if (module.location?.moduleSlot === 12) {
            board.miningStatus = "stopped";
          } else if (module.location?.moduleSlot === 5 && topology.hashboards.get(id)?.location?.slot === 2) {
            board.miningStatus = "stopped";
          } else if (module.location?.moduleSlot === 2) {
            board.health = "fault";
            board.reportedIssueCount = topology.hashboards.get(id)?.location?.slot === 1 ? 3 : 1;
          }
        }
      }
    }
    telemetry.rollupsByEntity.set(asEntityKey(topology.containerId), { miningStatus: "mining" });
    for (const tank of topology.tanks.values()) {
      telemetry.rollupsByEntity.set(asEntityKey(tank.id), {
        miningStatus: "mining",
        boardCount: 48,
        onlineBoardCount: missingReadings ? undefined : 48,
        averageAsicTemperature: missingReadings ? undefined : reading(65.5, "°C"),
        power: missingReadings ? undefined : reading(48_600, "W"),
      });
      if (sparseTanks && tank.slot === 2) topology.tanks.delete(tank.id);
    }
    telemetry.plant.set(plantId, {
      componentId: plantId,
      ...(missingReadings
        ? {}
        : {
            powered: true,
            heatRejectionActive: true,
            totalPower: reading(124_800, "W"),
            outsideAmbientTemperature: reading(25.5, "°C"),
            inletTemperature: reading(33.3, "°C"),
            outletTemperature: reading(43.3, "°C"),
            outletATemperature: reading(43.3, "°C"),
            outletBTemperature: reading(43.8, "°C"),
            pumpInletPressure: reading(194_430, "Pa"),
            pumpOutletPressure: reading(293_710, "Pa"),
            flowRate: reading(161.3, "L/min"),
            tankALiquidLevel: reading(860, "mm"),
            tankBLiquidLevel: reading(870, "mm"),
          }),
    });
    const fanId = asComponentId(`${topology.containerId}:fan:1`);
    hardware.fans.set(fanId, {
      ref: { id: fanId, kind: "fan", owner: { kind: "container", id: topology.containerId } },
      name: "Fan 1",
    });
    telemetry.fans.set(fanId, {
      componentId: fanId,
      speedPercent: missingReadings ? undefined : reading(85, "%"),
      power: missingReadings ? undefined : reading(3_480, "W"),
    });
    const store = useMinerStore.getState();
    store.resetDeviceData();
    store.systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1-story", product: "CT1" } }));
    store.hardware.replaceHardware(hardware);
    store.telemetry.mergeTelemetry(telemetry);
    return () => useMinerStore.getState().resetDeviceData();
  }, [missingReadings, sparseTanks, minerStates]);

  return (
    <div className="min-h-screen bg-surface-base p-6 text-text-primary laptop:p-10">
      <div className="mx-auto max-w-[1100px]">
        <ContainerDiagnosticView />
      </div>
    </div>
  );
};

export const Default = () => <DiagnosticsStory />;
export const MissingReadings = () => <DiagnosticsStory missingReadings />;
export const SparseTanks = () => <DiagnosticsStory sparseTanks />;
export const MinerStates = () => <DiagnosticsStory minerStates />;

export default { title: "Proto OS/Containers/Systems", component: ContainerDiagnosticView };
