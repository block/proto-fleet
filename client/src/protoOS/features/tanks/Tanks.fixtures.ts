import {
  asComponentId,
  asContainerId,
  asDeviceId,
  asEntityKey,
  asHashboardId,
  asModuleId,
  asTankId,
  type CommandCapabilities,
  type ContainerTopology,
  type HardwareState,
  type HashboardTelemetry,
  type LevelRollup,
} from "@/protoOS/store/contract";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";

const commands: CommandCapabilities = {
  startStop: "supported",
  powerTarget: "supported",
  locate: "unsupported",
  reboot: "unsupported",
  cooling: "unsupported",
  logs: "unsupported",
  firmwareUpdate: "unsupported",
  poolEditing: "unsupported",
  passwordManagement: "unsupported",
};

export const tanksFixture = (tankCount = 2, moduleCount = 16, cooling = false) => {
  const containerId = asContainerId("container:ct1");
  const plantId = asComponentId(`${containerId}:plant`);
  const topology: ContainerTopology = {
    kind: "container",
    deviceId: asDeviceId("device:ct1"),
    containerId,
    container: {
      id: containerId,
      capabilities: commands,
      tankIds: [],
      controlBoardIds: [],
      fanIds: [],
      plantComponentIds: [plantId],
    },
    tanks: new Map(),
    modules: new Map(),
    hashboards: new Map(),
  };
  const telemetry = createEmptyTelemetryState();
  if (cooling) {
    telemetry.plant.set(plantId, {
      componentId: plantId,
      flowRate: { latest: { value: 1200, units: "L/min" } },
      inletTemperature: { latest: { value: 35, units: "°C" } },
      outletATemperature: { latest: { value: 48, units: "°C" } },
      outletBTemperature: { latest: { value: 42, units: "°C" } },
      tankALiquidLevel: { latest: { value: 480, units: "mm" } },
      tankBLiquidLevel: { latest: { value: 460, units: "mm" } },
    });
  }
  const rollup: LevelRollup = {
    hashrate: { latest: { value: 24000, units: "TH/s" } },
    power: { latest: { value: 338400, units: "W" } },
    efficiency: { latest: { value: 14.1, units: "J/TH" } },
    minimumTemperature: { latest: { value: 68.7, units: "°C" } },
    maximumTemperature: { latest: { value: 80.3, units: "°C" } },
    miningStatus: "mining",
    health: "ok",
    powerCoverage: { kind: "hashboardOnly", includesNonHashboardLoads: false, methods: ["topologyMapping"] },
  };
  for (let number = 1; number <= tankCount; number++) {
    const id = asTankId(`${containerId}:tank:${number}`);
    const tank = {
      id,
      containerId,
      capabilities: commands,
      moduleIds: [] as ReturnType<typeof asModuleId>[],
      label: `Tank ${number}`,
      slot: number,
      moduleLayout: { rows: 2, columns: Math.ceil(moduleCount / 2), numbering: "rowMajor" as const },
    };
    topology.tanks.set(id, tank);
    topology.container.tankIds.push(id);
    telemetry.rollupsByEntity.set(asEntityKey(id), { ...rollup });
    for (let slot = 1; slot <= moduleCount; slot++) {
      const moduleId = asModuleId(`${id}:module:${slot}`);
      const module = {
        id: moduleId,
        profile: "ct1" as const,
        capabilities: { commands },
        tankId: id,
        hashboardIds: [] as ReturnType<typeof asHashboardId>[],
        controlBoardIds: [],
        psuIds: [],
        fanIds: [],
        location: { tankLabel: tank.label, moduleSlot: slot },
      };
      tank.moduleIds.push(moduleId);
      topology.modules.set(moduleId, module);
      telemetry.rollupsByEntity.set(asEntityKey(moduleId), {
        ...rollup,
        hashrate: { latest: { value: 24000 / moduleCount, units: "TH/s" } },
        power: { latest: { value: 338400 / moduleCount, units: "W" } },
      });
      for (let position = 1; position <= 3; position++) {
        const boardId = asHashboardId(`serial:T${number}-M${slot}-HB${position}`);
        module.hashboardIds.push(boardId);
        topology.hashboards.set(boardId, {
          id: boardId,
          identity: { kind: "serial", value: `T${number}-M${slot}-HB${position}` },
          moduleId,
          asicIds: [],
          location: { slot: position },
        });
        const board: HashboardTelemetry = { hashboardId: boardId, health: "ok", miningStatus: "mining" };
        if (slot === 11 && position === 2) board.health = "fault";
        if (slot === 6) {
          board.health = "offline";
          board.miningStatus = "unknown";
        }
        telemetry.hashboards.set(boardId, board);
      }
    }
  }
  const hardware: HardwareState = {
    resource: { status: "ready", updatedAtMs: 1000 },
    topology,
    controlBoards: new Map(),
    hashboards: new Map(
      [...topology.hashboards.values()].map((board) => [
        board.id,
        {
          ref: { id: board.id, kind: "hashboard", owner: { kind: "module", id: board.moduleId } },
          identity: board.identity,
        },
      ]),
    ),
    asics: new Map(),
    psus: new Map(),
    fans: new Map(),
    plant: new Map([
      [
        plantId,
        {
          ref: { id: plantId, kind: "plant", owner: { kind: "container", id: containerId } },
          kind: "plant",
          name: "Plant",
        },
      ],
    ]),
  };
  return { hardware, topology, telemetry };
};
