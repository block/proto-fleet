import type { PoolRecordedShares } from "../poolHistory";
import type { AsicTelemetrySample, HashboardStatus } from "../types";
import type {
  V2GetContainerHealthResponse,
  V2GetContainerMapResponse,
  V2GetHashboardASICsResponse,
  V2GetMiningStatusResponse,
  V2GetPoolDiagnosticsResponse,
  V2GetSystemInfoResponse,
  V2GetSystemStatusResponse,
  V2GetTargetPowerResponse,
  V2Hashboard,
  V2ListPoolsResponse,
  V2Module,
  V2ModuleLayout,
  V2QueryTimeSeriesResponse,
} from "./generated/generatedApi";
import {
  V2MiningOperatingState,
  V2MiningState,
  V2ModuleNumberingOrder,
  V2PerformanceMode,
  V2PoolConnState,
  V2Severity,
} from "./generated/generatedApi";
import { normalizeModulePsu } from "./modulePsu";
import {
  asAsicId,
  asComponentId,
  asContainerId,
  asDeviceId,
  asEntityKey,
  asHashboardId,
  asModuleId,
  asPoolId,
  asResourceKey,
  asTankId,
  type CommandCapabilities,
  type ComponentId,
  type FanHardware,
  type FanTelemetry,
  type HardwareState,
  type HashboardHardware,
  type HashboardId,
  type HashboardNode,
  type HashboardTelemetry,
  type HealthState,
  type LevelRollup,
  type Measurement,
  type MetricTelemetry,
  type MetricUnit,
  type MinerError,
  type MiningStatus,
  type MiningTarget,
  type ModuleId,
  type ModuleNode,
  type NetworkInfoState,
  type OperationalStatus,
  type PlantHardware,
  type PlantTelemetry,
  type PoolInfo,
  ROOT_MODULE_ID,
  ROOT_MODULE_KEY,
  type SystemInfoData,
  type TankId,
  type TankNode,
  type TelemetrySnapshot,
  type TelemetryState,
} from "@/protoOS/store/contract";
import type { HashboardHardwareData } from "@/protoOS/store/types";
import { CONTAINER_HASHBOARD_COLUMNS } from "@/protoOS/store/utils/containerHashboardLayout";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";
import { efficiencyValue } from "@/protoOS/store/utils/efficiency";

export const CT1_COMMANDS: CommandCapabilities = {
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

const ready = (updatedAtMs = Date.now()) => ({ status: "ready" as const, updatedAtMs });
const numeric = (value: string | number | undefined): number | undefined => {
  if (value === undefined) return undefined;
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
};
const latest = <U extends Measurement["units"]>(value: number | undefined, units: U) =>
  value === undefined ? undefined : { latest: { value, units } };
const average = (values: number[]) =>
  values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : undefined;

const efficiencyMetric = (
  power: number | undefined,
  hashrate: number | undefined,
  reported?: number,
): MetricTelemetry => {
  // Explicit null clears an earlier reading when a later snapshot lacks data.
  return { latest: { value: efficiencyValue(power, hashrate, reported), units: "J/TH" } };
};

export const ct1DeviceId = (value: string | undefined) => asDeviceId(`device:${value || "ct1"}`);
export const ct1ContainerId = (value: string | undefined) => asContainerId(`container:${value || "ct1"}`);
const tankId = (containerId: string, tank: number) => asTankId(`${containerId}:tank:${tank}`);
const moduleId = (containerId: string, tank: number, module: number, reported?: string) =>
  asModuleId(reported ? `${containerId}:module:${reported}` : `${containerId}:tank:${tank}:module:${module}`);
const hashboardId = (board: V2Hashboard) =>
  asHashboardId(board.macAddress ? `mac:${board.macAddress}` : `serial:${board.serial || "unknown"}`);
const asicId = (boardId: HashboardId, index: number) => asAsicId(`${boardId}:asic:${index}`);

const miningStatus = (value: string | undefined, mining?: boolean): MiningStatus => {
  switch (value) {
    case V2MiningOperatingState.MINING_OPERATING_STATE_UNINITIALIZED:
      return "uninitialized";
    case V2MiningOperatingState.MINING_OPERATING_STATE_POWERING_ON:
      return "poweringOn";
    case V2MiningOperatingState.MINING_OPERATING_STATE_MINING:
      return "mining";
    case V2MiningOperatingState.MINING_OPERATING_STATE_DEGRADED_MINING:
      return "degradedMining";
    case V2MiningOperatingState.MINING_OPERATING_STATE_POWERING_OFF:
      return "poweringOff";
    case V2MiningOperatingState.MINING_OPERATING_STATE_STOPPED:
      return "stopped";
    case V2MiningOperatingState.MINING_OPERATING_STATE_NO_POOLS:
      return "noPools";
    case V2MiningOperatingState.MINING_OPERATING_STATE_ERROR:
      return "error";
    default:
      return mining === undefined ? "unknown" : mining ? "mining" : "stopped";
  }
};

const healthState = (board: V2Hashboard): HealthState => {
  if (board.isolated) return "offline";
  // Rails/clocks are only required while hashing. Idle boards can power them
  // down, and disconnected boards cannot report verified live indicators.
  // Explicit errors and warnings still take precedence over inactive states.
  if (
    board.miningState === V2MiningState.MINING_STATE_ERROR ||
    board.health?.errors?.length ||
    (board.miningState === V2MiningState.MINING_STATE_MINING &&
      (board.health?.hashrailOk === false || board.health?.refclksOk === false))
  )
    return "fault";
  if (board.health?.warnings?.length) return "degraded";
  if (!board.miningState) return "unknown";
  if (
    board.miningState === V2MiningState.MINING_STATE_MINING ||
    board.miningState === V2MiningState.MINING_STATE_IDLE ||
    board.miningState === V2MiningState.MINING_STATE_READY
  )
    return "ok";
  if (board.miningState === V2MiningState.MINING_STATE_DISCONNECTED) return "offline";
  return "degraded";
};

export const toCanonicalSystemInfo = (wire: V2GetSystemInfoResponse): SystemInfoData => {
  const info = wire.info ?? {};
  return {
    identity: {
      deviceId: ct1DeviceId(info.id),
      deviceType: "container",
      adapterId: "mdk-v2",
      productName: info.product,
      model: info.product,
      manufacturer: info.manufacturer,
      serialNumber: info.controlBoard?.serialNumber ?? info.id,
      macAddresses: [],
    },
    moduleModel: "ct1",
    controlBoard: info.controlBoard
      ? {
          model: info.controlBoard.boardModel,
          serialNumber: info.controlBoard.serialNumber,
          machineName: info.controlBoard.machineName,
        }
      : undefined,
    operatingSystem: info.osBuild
      ? {
          name: info.osBuild.name,
          version: info.osBuild.version ?? info.version,
          gitHash: info.osBuild.gitHash,
          variant: info.osBuild.variant,
        }
      : { version: info.version },
    controlBoardSoftware: info.controlBoard
      ? { name: info.controlBoard.machineName, version: info.version }
      : undefined,
    webServer: { health: "running" },
    miningDriver: { health: "running" },
    poolInterface: { health: "running" },
    uptimeSeconds: numeric(info.uptimeSeconds),
  };
};

export const toCanonicalNetworkInfo = (wire: V2GetSystemInfoResponse): Pick<NetworkInfoState, "interfaces"> => ({
  interfaces: new Map(
    (wire.info?.ipAddresses ?? []).map((ipAddress, index) => [
      `ip:${index}`,
      {
        id: `ip:${index}`,
        hostname: wire.info?.hostname,
        ipAddress,
        isPrimary: index === 0,
      },
    ]),
  ),
});

export const toCanonicalOperationalStatus = (
  wire: V2GetMiningStatusResponse,
  containerId: string,
): OperationalStatus => {
  const uptimeSeconds = numeric(wire.uptimeSeconds);
  return {
    owner: { kind: "container", id: asContainerId(containerId) },
    resource: ready(),
    miningStatus: miningStatus(wire.operatingState, wire.mining),
    miningUptime: uptimeSeconds === undefined ? undefined : { value: uptimeSeconds, units: "s" },
    hardwareErrorCount: wire.boardsByState?.[V2MiningState.MINING_STATE_ERROR] ?? 0,
  };
};

const poolStatus = (enabled: boolean | undefined, state: V2PoolConnState | undefined, isActive?: boolean) => {
  if (enabled === false) return "Disabled";
  if (isActive === true) return "Active";
  switch (state) {
    case V2PoolConnState.POOL_CONN_STATE_ACTIVE:
      return "Active";
    case V2PoolConnState.POOL_CONN_STATE_IDLE:
      return "Idle";
    case V2PoolConnState.POOL_CONN_STATE_DEAD:
      return "Dead";
    default:
      return "Unknown";
  }
};

export const toCanonicalPools = (
  wire: V2ListPoolsResponse,
  diagnosticsWire: V2GetPoolDiagnosticsResponse,
): PoolInfo[] => {
  const diagnosticsById = new Map(
    (diagnosticsWire.pools ?? []).flatMap((diagnostics) =>
      diagnostics.poolId === undefined ? [] : [[diagnostics.poolId, diagnostics] as const],
    ),
  );
  return (wire.pools ?? [])
    .map((pool, index) => ({
      id: asPoolId(`pool:${pool.id ?? index}`),
      name: pool.name,
      url: pool.url,
      username: pool.username,
      priority: pool.priority ?? index,
      status: poolStatus(
        pool.enabled,
        pool.id === undefined ? undefined : diagnosticsById.get(pool.id)?.connState,
        pool.id === undefined ? undefined : diagnosticsById.get(pool.id)?.isActive,
      ),
      difficulty: pool.id === undefined ? undefined : diagnosticsById.get(pool.id)?.currentDifficulty,
    }))
    .sort((left, right) => left.priority - right.priority);
};

/** An absent result is unavailable, including when an older Frigate ignores the opt-in query. */
export const toCanonicalPoolRecordedShares = (wire: V2GetPoolDiagnosticsResponse): PoolRecordedShares => {
  const byPool: PoolRecordedShares = {};
  for (const result of wire.lastRecordedShares ?? []) {
    if (!result.poolId) continue;
    const resultAtMs = result.resultAt ? Date.parse(result.resultAt) : NaN;
    byPool[asPoolId(`pool:${result.poolId}`)] = {
      resultAtMs: Number.isFinite(resultAtMs) ? resultAtMs : undefined,
      assignedDifficulty: result.assignedDifficulty,
      outcome:
        result.accepted === true
          ? "accepted"
          : result.rejectReason === "submit_error"
            ? "submitError"
            : result.accepted === false
              ? "rejected"
              : "unknown",
    };
  }
  return byPool;
};

type BoardEntry = {
  board: V2Hashboard;
  tank: number;
  module: number;
  slot?: number;
  tankId: TankId;
  moduleId: ModuleId;
  hashboardId: HashboardId;
  powerFeedId?: string;
};

const canonicalModuleId = (containerId: string, tank: number, module: V2Module) => {
  const reported = module.slots?.find((slot) => slot.hashboard?.moduleId)?.hashboard?.moduleId;
  return moduleId(containerId, tank, module.id!, reported);
};

const flattenBoards = (wire: V2GetContainerMapResponse, containerId: string): BoardEntry[] => {
  const boards: BoardEntry[] = [];
  for (const tank of wire.containerMap?.tanks ?? []) {
    const tankNumber = tank.id;
    if (tankNumber === undefined) continue;
    for (const module of tank.modules ?? []) {
      const moduleNumber = module.id;
      if (moduleNumber === undefined) continue;
      const normalizedModuleId = canonicalModuleId(containerId, tankNumber, module);
      for (const slot of module.slots ?? []) {
        const board = slot.hashboard;
        if (!board || (!board.macAddress && !board.serial)) continue;
        boards.push({
          board,
          tank: tankNumber,
          module: moduleNumber,
          slot: numeric(board.location?.hashboardPosition),
          tankId: tankId(containerId, tankNumber),
          moduleId: normalizedModuleId,
          hashboardId: hashboardId(board),
          powerFeedId: slot.powerFeedId ?? board.location?.powerFeedId,
        });
      }
    }
  }
  return boards;
};

const boardRollup = (boards: BoardEntry[], power?: number): LevelRollup => {
  const metric = (value: number | undefined, units: MetricUnit): MetricTelemetry => ({
    latest: { value: value ?? null, units },
  });
  const sum = (read: (entry: BoardEntry) => number | undefined) => {
    if (!boards.length) return undefined;
    let total = 0;
    for (const board of boards) {
      const value = read(board);
      if (value === undefined || !Number.isFinite(value)) return undefined;
      total += value;
    }
    return total;
  };
  const temps = boards.flatMap(({ board }) => board.health?.temperatures ?? []).filter(Number.isFinite);
  const asicTemps = boards.flatMap(({ board }) => {
    const value = board.health?.averageAsicTemperatureC;
    return typeof value === "number" && Number.isFinite(value) ? [value] : [];
  });
  const hashrate = sum(({ board }) => board.health?.hashrate);
  const boardHealth = boards.map(({ board }) => healthState(board));
  const onlineBoardCount = boards.filter(
    ({ board }) => !board.isolated && board.miningState === V2MiningState.MINING_STATE_MINING,
  ).length;
  return {
    hashrate: metric(hashrate, "TH/s"),
    power: metric(power, "W"),
    efficiency: efficiencyMetric(power, hashrate),
    temperature: metric(average(temps), "°C"),
    averageAsicTemperature: metric(average(asicTemps), "°C"),
    minimumTemperature: metric(temps.length ? Math.min(...temps) : undefined, "°C"),
    maximumTemperature: metric(temps.length ? Math.max(...temps) : undefined, "°C"),
    miningStatus: onlineBoardCount === 0 ? "stopped" : onlineBoardCount === boards.length ? "mining" : "degradedMining",
    health:
      boards.length === 0
        ? "offline"
        : boardHealth.includes("fault")
          ? "fault"
          : boardHealth.includes("degraded")
            ? "degraded"
            : onlineBoardCount === boards.length
              ? "ok"
              : onlineBoardCount === 0
                ? "offline"
                : "degraded",
    boardCount: boards.length,
    onlineBoardCount,
    powerCoverage: {
      kind: power === undefined ? "unknown" : "complete",
      includesNonHashboardLoads: true,
      methods: ["topologyMapping"],
    },
  };
};

const groupBoards = <K>(boards: BoardEntry[], key: (entry: BoardEntry) => K) => {
  const grouped = new Map<K, BoardEntry[]>();
  for (const entry of boards) {
    const groupKey = key(entry);
    const entries = grouped.get(groupKey);
    if (entries) {
      entries.push(entry);
    } else {
      grouped.set(groupKey, [entry]);
    }
  }
  return grouped;
};

const toHashboardTelemetry = (entry: BoardEntry): HashboardTelemetry => ({
  isolated: entry.board.isolated ?? false,
  hashboardId: entry.hashboardId,
  health: healthState(entry.board),
  miningStatus: boardMiningStatus(entry.board),
  reportedIssueCount: entry.board.health
    ? (entry.board.health.errors?.length ?? 0) + (entry.board.health.warnings?.length ?? 0)
    : null,
  hashrate: latest(entry.board.health?.hashrate, "TH/s"),
  power: latest(entry.board.health?.power, "W"),
  efficiency: efficiencyMetric(entry.board.health?.power, entry.board.health?.hashrate),
  temperature: latest(average(entry.board.health?.temperatures ?? []), "°C"),
  averageAsicTemperature: latest(entry.board.health?.averageAsicTemperatureC, "°C"),
});

const boardMiningStatus = (board: V2Hashboard): MiningStatus => {
  if (board.isolated || board.miningState === V2MiningState.MINING_STATE_DISCONNECTED) return "unknown";
  switch (board.miningState) {
    case V2MiningState.MINING_STATE_MINING:
      return "mining";
    case V2MiningState.MINING_STATE_ERROR:
      return "error";
    case V2MiningState.MINING_STATE_IDLE:
    case V2MiningState.MINING_STATE_READY:
      return "stopped";
    case V2MiningState.MINING_STATE_INIT:
    case V2MiningState.MINING_STATE_STARTUP:
    case V2MiningState.MINING_STATE_IDENTIFYING:
    case V2MiningState.MINING_STATE_CONFIGURING:
      return "poweringOn";
    default:
      return "unknown";
  }
};

const plantComponents = (containerId: string, wire: V2GetContainerHealthResponse) => {
  const owner = { kind: "container" as const, id: asContainerId(containerId) };
  const hardware = new Map<ComponentId, PlantHardware>();
  const telemetry = new Map<ComponentId, PlantTelemetry>();
  const fanHardware = new Map<ComponentId, FanHardware>();
  const fanTelemetry = new Map<ComponentId, FanTelemetry>();
  const faultedComponents = new Set((wire.faults ?? []).map((fault) => fault.componentId).filter(Boolean));
  const addPlantComponent = (id: ComponentId, kind: PlantHardware["kind"], name: string, value: PlantTelemetry) => {
    hardware.set(id, { ref: { id, kind: "plant", owner }, kind, name });
    telemetry.set(id, value);
  };
  const plantId = asComponentId(`${containerId}:plant`);
  const plant = wire.plant;
  // Missing or invalid sensors in a successful snapshot must clear the previous reading.
  const sensorReading = (value: unknown, units: MetricUnit): MetricTelemetry => ({
    latest: { value: typeof value === "number" && Number.isFinite(value) ? value : null, units },
  });
  addPlantComponent(plantId, "plant", "Plant", {
    componentId: plantId,
    operatingState: plant?.operatingState,
    powered: plant?.powered,
    inletTemperature: sensorReading(plant?.inletTemperatureC, "°C"),
    outletTemperature: sensorReading(plant?.outletTemperatureC, "°C"),
    outletATemperature: sensorReading(plant?.outletATemperatureC, "°C"),
    outletBTemperature: sensorReading(plant?.outletBTemperatureC, "°C"),
    tankALiquidLevel: sensorReading(plant?.tankALiquidLevelMm, "mm"),
    tankBLiquidLevel: sensorReading(plant?.tankBLiquidLevelMm, "mm"),
    ambientTemperature: sensorReading(plant?.ambientTemperatureC, "°C"),
    insideAmbientTemperature: sensorReading(plant?.insideAmbientTemperatureC, "°C"),
    outsideAmbientTemperature: sensorReading(plant?.outsideAmbientTemperatureC ?? plant?.ambientTemperatureC, "°C"),
    dryCoolerInletTemperature: sensorReading(plant?.dryCoolerInletTemperatureC, "°C"),
    totalPower: sensorReading(plant?.totalPowerW, "W"),
    heatRejectionPower: latest(plant?.heatRejectionPowerW, "W"),
    facilityPower: sensorReading(plant?.facilityPowerW, "W"),
    powerFactor: latest(plant?.powerFactor, "ratio"),
    pue: latest(plant?.pue, "ratio"),
    flowRate: sensorReading(plant?.flowRateLpm, "L/min"),
    coolantPumpSpeed: sensorReading(plant?.coolantPumpSpeedHz, "Hz"),
    pumpInletPressure: sensorReading(plant?.pumpInletPressurePa, "Pa"),
    pumpOutletPressure: sensorReading(plant?.pumpOutletPressurePa, "Pa"),
    pumpPressureDelta: sensorReading(plant?.pumpPressureDiffPa, "Pa"),
    heatRejectionInletPressure: sensorReading(plant?.heatRejectionInletPressurePa, "Pa"),
    heatRejectionOutletPressure: sensorReading(plant?.heatRejectionOutletPressurePa, "Pa"),
    heatRejectionPressureDelta: sensorReading(plant?.heatRejectionPressureDiffPa, "Pa"),
    coolantPumpActive: plant?.coolantPumpActive,
    heatRejectionActive: plant?.heatRejectionActive,
    alarmActive: plant?.alarmActive,
    criticalAlarmActive: plant?.criticalAlarmActive,
    buzzerActive: plant?.buzzerActive,
    controls: (wire.controls ?? []).flatMap((control) =>
      control.name
        ? [
            {
              name: control.name,
              description: control.description,
              enabled: control.enabled,
              stateKnown: control.stateKnown === true,
            },
          ]
        : [],
    ),
    setpoints: (wire.setpoints ?? []).flatMap((setpoint) =>
      setpoint.name
        ? [
            {
              name: setpoint.name,
              description: setpoint.description,
              value: setpoint.value,
              unit: setpoint.unit,
              minimum: setpoint.min,
              maximum: setpoint.max,
            },
          ]
        : [],
    ),
  });
  for (const phase of wire.phases ?? []) {
    if (!phase.phase) continue;
    const id = asComponentId(`${containerId}:phase:${phase.phase}`);
    addPlantComponent(id, "phase", phase.phase, {
      componentId: id,
      phase: phase.phase,
      totalPower: latest(phase.powerW, "W"),
      voltage: latest(phase.voltageV, "V"),
      current: latest(phase.currentA, "A"),
    });
  }
  for (const [index, distribution] of (wire.powerDistributions ?? []).entries()) {
    const reportedId = distribution.componentId ?? `distribution-${index + 1}`;
    const id = asComponentId(`${containerId}:distribution:${reportedId}`);
    addPlantComponent(id, "distribution", reportedId, {
      componentId: id,
      health: faultedComponents.has(distribution.componentId) ? "fault" : "ok",
      activeFeedCount: distribution.activeFeedCount,
      totalPower: sensorReading(distribution.activePowerW, "W"),
      voltageAb: latest(distribution.voltageAbV, "V"),
      voltageBc: latest(distribution.voltageBcV, "V"),
      voltageCa: latest(distribution.voltageCaV, "V"),
      currentA: latest(distribution.currentAA, "A"),
      currentB: latest(distribution.currentBA, "A"),
      currentC: latest(distribution.currentCA, "A"),
      activeEnergy: latest(distribution.activeEnergyKwh, "kWh"),
    });
  }
  for (const [index, feed] of (wire.powerFeeds ?? []).entries()) {
    const reportedId = feed.powerFeedId ?? `feed-${index + 1}`;
    const id = asComponentId(`${containerId}:power-feed:${reportedId}`);
    addPlantComponent(id, "powerFeed", reportedId, {
      componentId: id,
      operatingState: feed.state,
      powered: feed.active,
      health: feed.faulted || faultedComponents.has(feed.powerFeedId) ? "fault" : feed.active ? "ok" : "offline",
      phase: feed.phase,
      totalPower: latest(feed.powerW, "W"),
      voltage: latest(feed.voltageV, "V"),
      current: latest(feed.currentA, "A"),
      voltageAb: latest(feed.voltageAbV, "V"),
      voltageBc: latest(feed.voltageBcV, "V"),
      voltageCa: latest(feed.voltageCaV, "V"),
      currentA: latest(feed.currentAA, "A"),
      currentB: latest(feed.currentBA, "A"),
      currentC: latest(feed.currentCA, "A"),
    });
  }
  for (const [index, fan] of (wire.coolingFans ?? []).entries()) {
    const reportedId = fan.componentId ?? `fan-${index + 1}`;
    const id = asComponentId(`${containerId}:fan:${reportedId}`);
    fanHardware.set(id, { ref: { id, kind: "fan", owner }, name: reportedId });
    fanTelemetry.set(id, {
      componentId: id,
      power: latest(fan.powerW, "W"),
      speedPercent: sensorReading(fan.speedPercent, "%"),
      health: fan.alarmActive || faultedComponents.has(fan.componentId) ? "fault" : "ok",
    });
  }
  return { hardware, telemetry, fanHardware, fanTelemetry };
};

export const toCanonicalContainerFans = (wire: V2GetContainerHealthResponse, deviceId: string): FanTelemetry[] => [
  ...plantComponents(ct1ContainerId(deviceId), wire).fanTelemetry.values(),
];

const canonicalModuleLayout = (layout: V2ModuleLayout | undefined): TankNode["moduleLayout"] => {
  if (!layout) return undefined;
  const { rows, columns, numbering } = layout;
  if (!rows || !columns || !Number.isSafeInteger(rows) || !Number.isSafeInteger(columns)) return undefined;
  if (rows <= 0 || columns <= 0) return undefined;
  if (numbering === undefined || numbering === V2ModuleNumberingOrder.MODULE_NUMBERING_ORDER_UNSPECIFIED)
    return undefined;
  return {
    rows,
    columns,
    numbering: numbering === V2ModuleNumberingOrder.MODULE_NUMBERING_ORDER_COLUMN_MAJOR ? "columnMajor" : "rowMajor",
  };
};

export const toCanonicalContainer = (
  mapWire: V2GetContainerMapResponse | undefined,
  statusWire: V2GetMiningStatusResponse,
  telemetryWire: V2GetContainerHealthResponse,
  deviceIdValue: string,
): { hardware: HardwareState; telemetry: TelemetrySnapshot } => {
  const normalizedDeviceId = ct1DeviceId(deviceIdValue);
  const containerId = ct1ContainerId(deviceIdValue);
  const boards = flattenBoards(mapWire ?? {}, containerId);
  const hashboards = new Map<HashboardId, HashboardHardware>();
  const hashboardNodes = new Map<HashboardId, HashboardNode>();
  const modules = new Map<ModuleId, ModuleNode>();
  const tanks = new Map<TankId, TankNode>();

  for (const wireTank of mapWire?.containerMap?.tanks ?? []) {
    if (wireTank.id === undefined) continue;
    const id = tankId(containerId, wireTank.id);
    const tankModuleIds: ModuleId[] = [];
    for (const wireModule of wireTank.modules ?? []) {
      if (wireModule.id === undefined) continue;
      const normalizedModuleId = canonicalModuleId(containerId, wireTank.id, wireModule);
      tankModuleIds.push(normalizedModuleId);
      modules.set(normalizedModuleId, {
        id: normalizedModuleId,
        profile: "ct1",
        capabilities: { commands: CT1_COMMANDS },
        tankId: id,
        hashboardIds: [],
        controlBoardIds: [],
        psuIds: [],
        fanIds: [],
        location: { tankLabel: `Tank ${wireTank.id}`, moduleSlot: wireModule.id },
      });
    }
    tanks.set(id, {
      id,
      containerId,
      capabilities: CT1_COMMANDS,
      moduleIds: tankModuleIds,
      label: `Tank ${wireTank.id}`,
      slot: wireTank.id,
      moduleLayout: canonicalModuleLayout(wireTank.moduleLayout),
    });
  }

  for (const entry of boards) {
    const owner = { kind: "module" as const, id: entry.moduleId };
    const identity = entry.board.macAddress
      ? ({ kind: "mac", value: entry.board.macAddress } as const)
      : ({ kind: "serial", value: entry.board.serial! } as const);
    hashboards.set(entry.hashboardId, {
      ref: { id: entry.hashboardId, kind: "hashboard", owner },
      identity,
      serialNumber: entry.board.serial,
      boardModel: entry.board.hwRevision,
      chipId: entry.board.chipId,
      miningAsic: "unknown",
      miningAsicCount: numeric(entry.board.observedAsicCount ?? entry.board.expectedAsicCount),
      temperatureSensorCount: entry.board.health?.temperatures?.length,
      firmware: { version: entry.board.firmwareVersion, gitHash: entry.board.firmwareGitHash },
    });
    hashboardNodes.set(entry.hashboardId, {
      id: entry.hashboardId,
      identity,
      moduleId: entry.moduleId,
      asicIds: [],
      location: { slot: entry.slot },
      powerFeedId: entry.powerFeedId,
    });
    modules.get(entry.moduleId)?.hashboardIds.push(entry.hashboardId);
  }

  const plant = plantComponents(containerId, telemetryWire);
  const boardsByTank = groupBoards(boards, (entry) => entry.tankId);
  const boardsByModule = groupBoards(boards, (entry) => entry.moduleId);
  const hardware: HardwareState = {
    resource: ready(),
    topology: {
      kind: "container",
      deviceId: normalizedDeviceId,
      containerId,
      container: {
        id: containerId,
        capabilities: CT1_COMMANDS,
        tankIds: [...tanks.keys()],
        controlBoardIds: [],
        fanIds: [...plant.fanHardware.keys()],
        plantComponentIds: [...plant.hardware.keys()],
      },
      tanks,
      modules,
      hashboards: hashboardNodes,
    },
    controlBoards: new Map(),
    hashboards,
    asics: new Map(),
    psus: new Map(),
    fans: plant.fanHardware,
    plant: plant.hardware,
  };

  const telemetry: TelemetrySnapshot = createEmptyTelemetryState();
  for (const [id, moduleBoards] of boardsByModule) {
    const psu = normalizeModulePsu(
      id,
      moduleBoards.map((entry) => entry.board),
    );
    if (!psu) continue;
    hardware.psus.set(psu.id, psu.hardware);
    telemetry.psus.set(psu.id, psu.telemetry);
    modules.get(id)?.psuIds.push(psu.id);
  }
  if (mapWire !== undefined) {
    telemetry.containerModuleRollups = { containerId };
    telemetry.modulePsus = { moduleIds: [...modules.keys()], hardware: hardware.psus };
  }
  // Boards share a PSU: use its normalized, freshest input reading once per
  // module. A missing module reading makes the aggregate unknown, not partial.
  const moduleInputPower = (moduleIds: ModuleId[]): number | undefined => {
    if (!moduleIds.length) return undefined;
    let total = 0;
    for (const id of moduleIds) {
      const psuIds = modules.get(id)?.psuIds;
      if (!psuIds?.length) return undefined;
      for (const psuId of psuIds) {
        const value = telemetry.psus.get(psuId)?.inputPower?.latest?.value;
        if (value == null || !Number.isFinite(value)) return undefined;
        total += value;
      }
    }
    return total;
  };
  const timestampMs = Date.parse(telemetryWire.timestamp ?? "");
  telemetry.resourceByScope.set(
    asResourceKey(`telemetry:current:${containerId}`),
    ready(Number.isNaN(timestampMs) ? Date.now() : timestampMs),
  );
  telemetry.plant = plant.telemetry;
  telemetry.containerPlant = { containerId, hardware: plant.hardware };
  telemetry.fans = plant.fanTelemetry;
  const boardSummary = mapWire === undefined ? undefined : boardRollup(boards);
  const containerRollup: LevelRollup = {
    ...boardSummary,
    // Home requests the bulk map: its board health is the live device rate.
    // Mining status reports trailing accepted-share hashrate instead. Missing
    // map health must remain unknown rather than falling back to that rate.
    hashrate: boardSummary?.hashrate ?? latest(statusWire.hashrate, "TH/s"),
    power: latest(statusWire.power, "W"),
    // Container mining power comes from firmware; its coverage is independent
    // of the module PSU input totals.
    powerCoverage: { kind: "unknown", includesNonHashboardLoads: false, methods: ["firmwareAggregate"] },
    efficiency: efficiencyMetric(statusWire.power, statusWire.hashrate, statusWire.efficiency),
    temperature: latest(statusWire.averageAsicTemperatureC ?? statusWire.averageHashboardTemperatureC, "°C"),
    miningStatus: miningStatus(statusWire.operatingState, statusWire.mining),
    boardCount: statusWire.totalBoards ?? boardSummary?.boardCount,
    onlineBoardCount: statusWire.activeBoards ?? boardSummary?.onlineBoardCount,
  };
  telemetry.rollupsByEntity.set(asEntityKey(containerId), containerRollup);
  // Existing rig-oriented pages still read the root rollup. Keep that shared
  // compatibility projection until the container views select their explicit
  // container/tank entities directly.
  telemetry.rollupsByEntity.set(ROOT_MODULE_KEY, containerRollup);
  for (const [id, tank] of tanks) {
    telemetry.rollupsByEntity.set(
      asEntityKey(id),
      boardRollup(boardsByTank.get(id) ?? [], moduleInputPower(tank.moduleIds)),
    );
  }
  for (const [id] of modules) {
    telemetry.rollupsByEntity.set(asEntityKey(id), boardRollup(boardsByModule.get(id) ?? [], moduleInputPower([id])));
  }
  for (const entry of boards) {
    telemetry.hashboards.set(entry.hashboardId, toHashboardTelemetry(entry));
  }
  return { hardware, telemetry };
};

/** Active health arrays and plant alarms are snapshots; lastShutdownError is latched history. */
export const toCanonicalErrors = (
  wire: V2GetContainerMapResponse,
  deviceId: string,
  health: V2GetContainerHealthResponse = {},
): MinerError[] => {
  const containerId = ct1ContainerId(deviceId);
  const boards = flattenBoards(wire, containerId);
  const errors: MinerError[] = [];
  for (const entry of boards) {
    const owner = { kind: "module" as const, id: entry.moduleId };
    const component = { kind: "hashboard" as const, id: entry.hashboardId, owner };
    for (const [severity, issues] of [
      ["critical", entry.board.health?.errors],
      ["warning", entry.board.health?.warnings],
    ] as const) {
      for (const [index, issue] of (issues ?? []).entries()) {
        const code = issue.cause ?? "HASHBOARD_ERROR_CAUSE_UNKNOWN_ERROR";
        const cause = code
          .replace(/^HASHBOARD_ERROR_CAUSE_/, "")
          .replaceAll("_", " ")
          .toLowerCase();
        const details = [cause.charAt(0).toUpperCase() + cause.slice(1)];
        if (issue.asic?.id !== undefined) details.push(`ASIC ${issue.asic.id}`);
        if (issue.asic?.temperature !== undefined) details.push(`${issue.asic.temperature} °C`);
        if (issue.asic?.voltage !== undefined) details.push(`${issue.asic.voltage} V`);
        if (issue.asic?.voltageDomain) details.push(issue.asic.voltageDomain.replace(/^HASHBOARD_VOLTAGE_DOMAIN_/, ""));
        if (issue.stack !== undefined) details.push(`Stack ${issue.stack} (firmware index)`);
        // Firmware timestamps are not specified as wall-clock time by the API.
        errors.push({
          id: `${entry.hashboardId}:${severity}:${code}:${index}`,
          code,
          message: details.join(" · "),
          owner,
          component,
          source: { kind: "hashboard", slot: entry.slot },
          severity,
        });
      }
    }
  }
  for (const [moduleId, entries] of groupBoards(boards, (entry) => entry.moduleId)) {
    const psu = normalizeModulePsu(
      moduleId,
      entries.map((entry) => entry.board),
    );
    if (psu) errors.push(...psu.errors);
  }
  for (const [index, alarm] of (health.alarms ?? []).entries()) {
    const severity =
      alarm.severity === V2Severity.SEVERITY_CRITICAL || alarm.severity === V2Severity.SEVERITY_ERROR
        ? "critical"
        : alarm.severity === V2Severity.SEVERITY_WARNING
          ? "warning"
          : "info";
    const severityLabel =
      alarm.severity === V2Severity.SEVERITY_CRITICAL
        ? "Critical"
        : alarm.severity === V2Severity.SEVERITY_ERROR
          ? "Error"
          : alarm.severity === V2Severity.SEVERITY_WARNING
            ? "Warning"
            : alarm.severity === V2Severity.SEVERITY_INFO
              ? "Info"
              : "Severity unknown";
    const code = alarm.id || "PLANT_ALARM";
    const name = alarm.name?.trim() || alarm.id || "Plant alarm";
    errors.push({
      id: `${containerId}:alarm:${alarm.id || `${alarm.componentId || name}:${index}`}`,
      code,
      message: `${severityLabel}: ${name}${alarm.componentId ? ` · Component ${alarm.componentId}` : ""}`,
      owner: { kind: "container", id: containerId },
      severity,
    });
  }
  return errors;
};

export const toCanonicalContainerTelemetry = (
  statusWire: V2GetMiningStatusResponse,
  telemetryWire: V2GetContainerHealthResponse,
  deviceIdValue: string,
) => toCanonicalContainer(undefined, statusWire, telemetryWire, deviceIdValue).telemetry;

export const toCanonicalHashboards = (wire: V2GetContainerMapResponse): HashboardHardwareData[] =>
  flattenBoards(wire, "container:ct1").map(({ board, slot, module, hashboardId: id }) => ({
    serial: board.macAddress ?? board.serial ?? id,
    slot,
    bay: module,
    board: board.hwRevision,
    chipId: board.chipId,
    miningAsicCount: numeric(board.observedAsicCount ?? board.expectedAsicCount),
    firmware: { version: board.firmwareVersion, gitHash: board.firmwareGitHash },
    asicIds: [],
  }));

export const toCanonicalHashboardStatus = (identifier: string, wire: V2GetHashboardASICsResponse): HashboardStatus => {
  const id = asHashboardId(identifier.includes(":") ? `mac:${identifier}` : `serial:${identifier}`);
  const positions = new Map((wire.layout?.entries ?? []).map((entry) => [entry.index, entry]));
  // Some APIs report a stack as one long row. Wrap its display only; ASIC
  // indices and the wire's electrical stack identities remain unchanged.
  const entries = wire.layout?.entries ?? [];
  const columns = CONTAINER_HASHBOARD_COLUMNS;
  const rowsPerStack = Math.ceil((wire.layout?.asicsPerStack ?? columns) / columns);
  const wrapStacks =
    entries.some((entry) => (entry.x ?? 0) >= columns) ||
    (rowsPerStack > 1 &&
      entries.length > 0 &&
      entries.every(
        (entry) =>
          entry.stackId !== undefined &&
          entry.stackIndex !== undefined &&
          entry.x === entry.stackIndex &&
          entry.y === entry.stackId,
      ));
  const asicIds: string[] = [];
  const asics: HashboardStatus["asics"] = [];
  const samples: AsicTelemetrySample[] = [];
  for (const asic of wire.asics ?? []) {
    if (asic.index === undefined) continue;
    const normalizedId = asicId(id, asic.index);
    const position = positions.get(asic.index);
    asicIds.push(normalizedId);
    asics.push({
      id: normalizedId,
      hashboardSerial: identifier,
      index: asic.index,
      row:
        wrapStacks && position?.x !== undefined && position.y !== undefined
          ? position.y * rowsPerStack + Math.floor(position.x / columns)
          : position?.y,
      column: wrapStacks && position?.x !== undefined ? position.x % columns : position?.x,
    });
    samples.push({
      asicId: normalizedId,
      hashrate: asic.hashrateGhs10 === undefined ? undefined : { value: asic.hashrateGhs10 / 10, units: "GH/s" },
      temperature: asic.temperatureDc === undefined ? undefined : { value: asic.temperatureDc / 10, units: "°C" },
      voltage: asic.voltageMv === undefined ? undefined : { value: asic.voltageMv / 1_000, units: "V" },
      frequency: asic.frequencyMhz === undefined ? undefined : { value: asic.frequencyMhz, units: "MHz" },
      passRate: asic.passRateBp === undefined ? undefined : { value: asic.passRateBp / 100, units: "%" },
    });
  }
  return {
    serial: identifier,
    asicIds,
    asics,
    asicTelemetry: samples,
  };
};

export type ContainerSeriesField = "hashrate" | "temperature" | "power";

const SERIES_UNITS: Record<ContainerSeriesField, MetricUnit> = {
  hashrate: "TH/s",
  temperature: "°C",
  power: "W",
};

const seriesConversion = (wireUnit: string | undefined, field: ContainerSeriesField) => {
  const normalized = wireUnit?.trim().toLowerCase().replace(/\s/g, "");
  if (field === "hashrate") {
    if (normalized === "gh/s" || normalized === "ghs") return { factor: 1 / 1_000, units: "TH/s" as const };
    if (normalized === "h/s" || normalized === "hs") return { factor: 1 / 1_000_000_000_000, units: "TH/s" as const };
  }
  if (field === "power" && normalized === "kw") return { factor: 1_000, units: "W" as const };
  return { factor: 1, units: SERIES_UNITS[field] };
};

const averagePointValues = (points: { value: number | null; sampleCount?: number }[]) => {
  const values = points.filter((point): point is typeof point & { value: number } => point.value !== null);
  if (!values.length) return undefined;
  const weighted = values.filter(
    (point): point is typeof point & { value: number; sampleCount: number } =>
      point.sampleCount !== undefined && point.sampleCount > 0,
  );
  const sampleCount = weighted.reduce((sum, point) => sum + point.sampleCount, 0);
  return weighted.length === values.length && sampleCount > 0
    ? weighted.reduce((sum, point) => sum + point.value * point.sampleCount, 0) / sampleCount
    : values.reduce((sum, point) => sum + point.value, 0) / values.length;
};

const toSeriesMetric = (
  wire: V2QueryTimeSeriesResponse | undefined,
  field: ContainerSeriesField,
  bucketSizeMs?: number,
  aggregation: "mean" | "avg" | "min" | "max" = "mean",
): MetricTelemetry | undefined => {
  if (!wire) return undefined;
  const { factor, units } = seriesConversion(wire.unit, field);
  const points = (wire.buckets ?? []).flatMap((bucket) => {
    const timestampMs = Date.parse(bucket.time ?? "");
    if (Number.isNaN(timestampMs)) return [];
    const count = numeric(bucket.count);
    const rawValue = aggregation === "min" ? bucket.min : aggregation === "max" ? bucket.max : bucket.avg;
    return [
      {
        timestampMs,
        value: rawValue === undefined ? null : rawValue * factor,
        min: bucket.min === undefined ? undefined : bucket.min * factor,
        max: bucket.max === undefined ? undefined : bucket.max * factor,
        sampleCount: count,
      },
    ];
  });
  const values = points.map((point) => point.value).filter((value): value is number => value !== null);
  const minimums = points.map((point) => point.min ?? point.value).filter((value): value is number => value !== null);
  const maximums = points.map((point) => point.max ?? point.value).filter((value): value is number => value !== null);
  const average = averagePointValues(points);
  return {
    timeSeries: {
      bucketSizeMs,
      units,
      points,
      aggregates: values.length
        ? {
            min: { value: Math.min(...minimums), units },
            avg: { value: average!, units },
            max: { value: Math.max(...maximums), units },
          }
        : undefined,
    },
  };
};

const efficiencySeries = (
  power: MetricTelemetry | undefined,
  hashrate: MetricTelemetry | undefined,
): MetricTelemetry | undefined => {
  const powerPoints = power?.timeSeries?.points;
  const hashratePoints = hashrate?.timeSeries?.points;
  if (!powerPoints || !hashratePoints) return undefined;
  const hashrateByTime = new Map(hashratePoints.map((point) => [point.timestampMs, point]));
  const points = powerPoints.map((point) => {
    const hashratePoint = hashrateByTime.get(point.timestampMs);
    const hashrateValue = hashratePoint?.value;
    const sampleCount =
      point.sampleCount === undefined || hashratePoint?.sampleCount === undefined
        ? undefined
        : Math.min(point.sampleCount, hashratePoint.sampleCount);
    return {
      timestampMs: point.timestampMs,
      value: point.value === null || !hashrateValue ? null : point.value / hashrateValue,
      sampleCount,
    };
  });
  const values = points.map((point) => point.value).filter((value): value is number => value !== null);
  return {
    timeSeries: {
      bucketSizeMs: power.timeSeries?.bucketSizeMs,
      units: "J/TH",
      points,
      aggregates: values.length
        ? {
            min: { value: Math.min(...values), units: "J/TH" },
            avg: { value: averagePointValues(points)!, units: "J/TH" },
            max: { value: Math.max(...values), units: "J/TH" },
          }
        : undefined,
    },
  };
};

export const toCanonicalContainerTimeSeries = (
  responses: Partial<Record<ContainerSeriesField, V2QueryTimeSeriesResponse>>,
  containerId: string,
  bucketSizeMs?: number,
  aggregation: "mean" | "avg" | "min" | "max" = "mean",
  hashboards: {
    hashboardId: HashboardId;
    responses: Partial<Record<ContainerSeriesField, V2QueryTimeSeriesResponse>>;
  }[] = [],
): TelemetryState => {
  const state = createEmptyTelemetryState();
  const normalizeSeries = (source: Partial<Record<ContainerSeriesField, V2QueryTimeSeriesResponse>>) => {
    const rollup: LevelRollup = {
      hashrate: toSeriesMetric(source.hashrate, "hashrate", bucketSizeMs, aggregation),
      temperature: toSeriesMetric(source.temperature, "temperature", bucketSizeMs, aggregation),
      power: toSeriesMetric(source.power, "power", bucketSizeMs, aggregation),
    };
    rollup.efficiency = efficiencySeries(rollup.power, rollup.hashrate);
    return rollup;
  };
  const containerKey = asEntityKey(ct1ContainerId(containerId));
  state.resourceByScope.set(asResourceKey(`telemetry:history:${containerKey}`), ready());
  if (Object.keys(responses).length) {
    const rollup = normalizeSeries(responses);
    state.rollupsByEntity.set(containerKey, rollup);
    state.rollupsByEntity.set(ROOT_MODULE_KEY, rollup);
  }
  for (const board of hashboards) {
    state.hashboards.set(board.hashboardId, { hashboardId: board.hashboardId, ...normalizeSeries(board.responses) });
  }
  return state;
};

export const toCanonicalMiningTarget = (wire: V2GetTargetPowerResponse): MiningTarget => ({
  moduleId: ROOT_MODULE_ID,
  resource: ready(),
  valueWatts: wire.watts,
  defaultWatts: wire.defaultWatts,
  performanceMode:
    wire.performanceMode === V2PerformanceMode.PERFORMANCE_MODE_MAXIMUM_HASHRATE
      ? "maximumHashrate"
      : wire.performanceMode === V2PerformanceMode.PERFORMANCE_MODE_EFFICIENCY
        ? "efficiency"
        : undefined,
  boundsWatts:
    wire.minimumWatts === undefined && wire.maximumWatts === undefined
      ? undefined
      : { min: wire.minimumWatts ?? 0, max: wire.maximumWatts ?? 0 },
  balanceBays: wire.balanceBays,
  hashOnDisconnect: wire.hashOnDisconnect,
});

export const toCanonicalLifecycle = (_wire: V2GetSystemStatusResponse) => ({
  onboarded: true,
  passwordSet: true,
});
