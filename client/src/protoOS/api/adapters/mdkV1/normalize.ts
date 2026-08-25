import type { AsicTelemetrySample, HashboardStatus } from "../types";
import type {
  CoolingConfig,
  CoolingStatusCoolingstatus,
  ErrorListResponse,
  HardwareInfoHardwareinfo,
  HashboardInfo,
  HashboardStatsHashboardstats,
  MiningStatusMiningstatus,
  MiningTargetResponse,
  NetworkInfoNetworkinfo,
  Pool,
  SystemInfoSysteminfo,
  TelemetryData,
  TimeSeriesMetricData,
  TimeSeriesResponse,
  MetricUnit as V1MetricUnit,
} from "@/protoOS/api/generatedApi";
import {
  asAsicId,
  asComponentId,
  asDeviceId,
  type AsicHardware,
  type AsicTelemetry,
  asPoolId,
  asResourceKey,
  type CommandCapabilities,
  type ComponentId,
  type ControlBoardHardware,
  type CoolingMode,
  type DeviceType,
  type FanHardware,
  type FanTelemetry,
  type HardwareState,
  type HashboardHardware,
  type HashboardId,
  type HashboardNode,
  type HashboardTelemetry,
  type LevelRollup,
  type Measurement,
  type MetricTelemetry,
  type MetricUnit,
  type MinerError,
  type MiningTarget,
  type ModuleModel,
  type ModuleNode,
  type NetworkInfoState,
  type OperationalStatus,
  type PoolInfo,
  type PsuHardware,
  type PsuTelemetry,
  ROOT_MODULE_ID,
  ROOT_MODULE_KEY,
  type SystemInfoData,
  type TelemetryState,
  type Topology,
} from "@/protoOS/store/contract";
import type { AsicHardwareData, HashboardHardwareData } from "@/protoOS/store/types";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";
import { getFanId, getHashboardId, getPsuId } from "@/protoOS/store/utils/entityIds";
import { getAsicId } from "@/protoOS/store/utils/getAsicId";

const V1_COMMANDS: CommandCapabilities = {
  startStop: "supported",
  powerTarget: "supported",
  locate: "supported",
  reboot: "supported",
  cooling: "supported",
  logs: "supported",
  firmwareUpdate: "supported",
  poolEditing: "supported",
  passwordManagement: "supported",
};

const ready = () => ({ status: "ready" as const, updatedAtMs: Date.now() });
const moduleOwner = { kind: "module" as const, id: ROOT_MODULE_ID };

export const toCanonicalCoolingStatus = (
  wire: CoolingStatusCoolingstatus | undefined,
): { mode: CoolingMode; fans: FanTelemetry[] } => ({
  mode:
    wire?.fan_mode === "Auto"
      ? "automatic"
      : wire?.fan_mode === "Manual"
        ? "manual"
        : wire?.fan_mode === "Off"
          ? "off"
          : "unknown",
  fans: (wire?.fans ?? []).flatMap((fan) => {
    if (fan.slot === undefined) return [];
    const componentId = getFanId(fan.slot);
    return [
      {
        componentId,
        rpm: fan.rpm === undefined ? undefined : { latest: { value: fan.rpm, units: "RPM" as const } },
        speedPercent:
          fan.percentage === undefined ? undefined : { latest: { value: fan.percentage, units: "%" as const } },
      },
    ];
  }),
});

export const toV1CoolingMode = (mode: CoolingMode): CoolingConfig["mode"] =>
  mode === "automatic" ? "Auto" : mode === "manual" ? "Manual" : mode === "off" ? "Off" : "Unknown";

const toCanonicalUnit = (
  unit: V1MetricUnit | string | undefined,
): { units: MetricUnit; factor: number } | undefined => {
  switch (unit) {
    case "C":
    case "°C":
      return { units: "°C", factor: 1 };
    case "TH/S":
    case "TH/s":
      return { units: "TH/s", factor: 1 };
    case "GH/S":
    case "GH/s":
      return { units: "GH/s", factor: 1 };
    case "MH/S":
    case "MH/s":
      return { units: "GH/s", factor: 0.001 };
    case "kW":
      return { units: "W", factor: 1_000 };
    case "MW":
      return { units: "W", factor: 1_000_000 };
    case "mV":
      return { units: "V", factor: 0.001 };
    case "mA":
      return { units: "A", factor: 0.001 };
    case "GHz":
      return { units: "MHz", factor: 1_000 };
    case "W":
    case "J/TH":
    case "%":
    case "RPM":
    case "V":
    case "A":
    case "MHz":
      return { units: unit, factor: 1 };
    default:
      return undefined;
  }
};

const toCanonicalMeasurement = (
  value: number | null | undefined,
  unit: string | undefined,
): Measurement | undefined => {
  const canonical = toCanonicalUnit(unit);
  if (!canonical || value === undefined) return undefined;
  return { value: value === null ? null : value * canonical.factor, units: canonical.units };
};

const normalizeLatestMetric = (
  value: number | null | undefined,
  unit: string | undefined,
): MetricTelemetry | undefined => {
  const latest = toCanonicalMeasurement(value, unit);
  return latest ? { latest } : undefined;
};

type V1Firmware = {
  name?: string;
  version?: string;
  variant?: string;
  build?: "debug" | "release";
  git_hash?: string;
  image_hash?: string;
};

const toFirmwareInfo = (value: V1Firmware | undefined) =>
  value
    ? {
        name: value.name,
        version: value.version,
        variant: value.variant,
        build: value.build,
        gitHash: value.git_hash,
        imageHash: value.image_hash,
      }
    : undefined;

const toServiceHealth = (name: string | undefined) => {
  if (!name) return "unknown" as const;
  return /tcp connect error: Connection refused|Failed to connect to MinerDataApiClient/.test(name)
    ? ("error" as const)
    : ("running" as const);
};

export const toCanonicalSystemTag = (value: unknown): string => {
  if (typeof value === "string") return value;
  if (typeof value === "object" && value !== null && "tag" in value && typeof value.tag === "string") return value.tag;
  return JSON.stringify(value) ?? "";
};

export const toCanonicalSystemInfo = (wire: SystemInfoSysteminfo): SystemInfoData => {
  const model = wire.model?.toUpperCase();
  const deviceType: DeviceType = model?.startsWith("CU") ? "container" : "rig";
  const moduleModel: ModuleModel = deviceType === "container" ? "ct1" : "r2";
  const deviceIdentity = wire.cb_sn || wire.model || wire.product_name || "unknown";
  const update = wire.sw_update_status;

  return {
    identity: {
      deviceId: asDeviceId(`device:${deviceIdentity}`),
      deviceType,
      mdkVersion: "v1",
      productName: wire.product_name,
      model: wire.model,
      manufacturer: wire.manufacturer,
      serialNumber: wire.cb_sn,
      macAddresses: [],
    },
    moduleModel,
    operatingSystem: toFirmwareInfo(wire.os),
    operatingSystemStatus: wire.os?.status
      ? {
          cpuLoadPercent: wire.os.status.cpu_load_percent,
          memoryFreeKb: wire.os.status.mem_free_kb,
          memoryTotalKb: wire.os.status.mem_total_kb,
          rootFilesystemFreeMb: wire.os.status.rootfs_free_mb,
          rootFilesystemTotalMb: wire.os.status.rootfs_total_mb,
        }
      : undefined,
    webServer: { health: toServiceHealth(wire.web_server?.name), software: toFirmwareInfo(wire.web_server) },
    miningDriver: {
      health: toServiceHealth(wire.mining_driver_sw?.name),
      software: toFirmwareInfo(wire.mining_driver_sw),
    },
    poolInterface: {
      health: toServiceHealth(wire.pool_interface_sw?.name),
      software: toFirmwareInfo(wire.pool_interface_sw),
    },
    firmwareUpdate: update
      ? {
          status:
            update.status === "error"
              ? "failed"
              : update.status === "installed"
                ? "rebootRequired"
                : update.status === "success"
                  ? "complete"
                  : (update.status ?? "unknown"),
          currentVersion: update.current_version,
          newVersion: update.new_version,
          progressPercent: update.progress,
          message: update.message ?? update.error,
          releaseNotes: update.release_notes,
        }
      : undefined,
    uptimeSeconds: wire.uptime_seconds,
  };
};

export const toCanonicalNetworkInfo = (wire: NetworkInfoNetworkinfo): Pick<NetworkInfoState, "interfaces"> => ({
  interfaces: new Map([
    [
      "primary",
      {
        id: "primary",
        hostname: wire.hostname,
        ipAddress: wire.ip,
        macAddress: wire.mac,
        gateway: wire.gateway,
        netmask: wire.netmask,
        dhcp: wire.dhcp,
        isPrimary: true,
      },
    ],
  ]),
});

const miningStatus = (status: MiningStatusMiningstatus["status"]): OperationalStatus["miningStatus"] => {
  switch (status) {
    case "Uninitialized":
      return "uninitialized";
    case "PoweringOn":
      return "poweringOn";
    case "Mining":
      return "mining";
    case "DegradedMining":
      return "degradedMining";
    case "PoweringOff":
      return "poweringOff";
    case "Stopped":
      return "stopped";
    case "Curtailed":
      return "curtailed";
    case "NoPools":
      return "noPools";
    case "Error":
      return "error";
    default:
      return "unknown";
  }
};

export const toCanonicalMiningStatus = (wire: MiningStatusMiningstatus): OperationalStatus => ({
  owner: moduleOwner,
  resource: ready(),
  miningStatus: miningStatus(wire.status),
  miningUptime: wire.mining_uptime_s === undefined ? undefined : { value: wire.mining_uptime_s, units: "s" },
  rebootUptime: wire.reboot_uptime_s === undefined ? undefined : { value: wire.reboot_uptime_s, units: "s" },
  hardwareErrorCount: wire.hw_errors,
});

export const toCanonicalPools = (wire: Pool[]): PoolInfo[] =>
  wire
    .map((pool, index) => ({
      id: asPoolId(`pool:${pool.id ?? index}`),
      name: pool.name,
      url: pool.url,
      username: pool.user,
      priority: pool.priority ?? index,
      status: pool.status,
      acceptedShares: pool.accepted,
      rejectedShares: pool.rejected,
      invalidShares: pool.invalid,
      difficulty: pool.current_difficulty,
      lastShareAtMs: pool.last_share_time === undefined ? undefined : pool.last_share_time * 1_000,
      bestShare: pool.best_difficulty_share,
      blocks: pool.blocks_seen,
    }))
    .sort((left, right) => left.priority - right.priority);

export const toCanonicalMiningTarget = (wire: MiningTargetResponse): MiningTarget => ({
  moduleId: ROOT_MODULE_ID,
  resource: ready(),
  valueWatts: wire.power_target_watts,
  defaultWatts: wire.default_power_target_watts,
  performanceMode:
    wire.performance_mode === "MaximumHashrate"
      ? "maximumHashrate"
      : wire.performance_mode === "Efficiency"
        ? "efficiency"
        : undefined,
  boundsWatts:
    wire.power_target_min_watts === undefined && wire.power_target_max_watts === undefined
      ? undefined
      : { min: wire.power_target_min_watts ?? 0, max: wire.power_target_max_watts ?? 0 },
  balanceBays: wire.balance_bays,
  hashOnDisconnect: wire.hash_on_disconnect,
});

export const toCanonicalErrors = (wire: ErrorListResponse): MinerError[] =>
  wire.map((error, index) => {
    const source = error.source ?? "rig";
    const slot = error.slot;
    const component =
      source === "rig" || source === "hashboard" || slot === undefined
        ? undefined
        : {
            id: source === "fan" ? getFanId(slot) : getPsuId(slot),
            kind: source as "fan" | "psu",
            owner: moduleOwner,
          };
    return {
      id: `${error.error_code ?? "unknown"}:${error.timestamp ?? "none"}:${source}:${slot ?? "none"}:${index}`,
      code: error.error_code ?? "UNKNOWN",
      message: error.message ?? "Unknown miner error",
      owner: moduleOwner,
      source: { kind: source, slot },
      component,
      severity: "critical",
      timestampMs: error.timestamp === undefined ? undefined : error.timestamp * 1_000,
    };
  });

type NormalizeHardwareOptions = { deviceId: string; profile: ModuleModel };

export const toCanonicalHardware = (
  wire: HardwareInfoHardwareinfo,
  { deviceId, profile }: NormalizeHardwareOptions,
): HardwareState & { topology: Topology } => {
  const hashboards = new Map<HashboardId, HashboardHardware>();
  const hashboardNodes = new Map<HashboardId, HashboardNode>();
  const asics = new Map<ReturnType<typeof asAsicId>, AsicHardware>();
  const psus = new Map<ComponentId, PsuHardware>();
  const fans = new Map<ComponentId, FanHardware>();
  const controlBoards = new Map<ComponentId, ControlBoardHardware>();

  for (const board of wire["hashboards-info"] ?? []) {
    if (!board.hb_sn) continue;
    const id = getHashboardId(board.hb_sn);
    const ref = { id, kind: "hashboard" as const, owner: moduleOwner };
    hashboards.set(id, {
      ref,
      identity: { kind: "serial", value: board.hb_sn },
      boardModel: board.board,
      apiVersion: board.api_version,
      chipId: board.chip_id,
      miningAsic: board.mining_asic,
      miningAsicCount: board.mining_asic_count,
      temperatureSensorCount: board.temp_sensor_count,
      ecLogsPath: board.ec_logs_path,
      firmware: toFirmwareInfo(board.firmware),
      bootloader: toFirmwareInfo(board.bootloader),
    });
    hashboardNodes.set(id, {
      id,
      identity: { kind: "serial", value: board.hb_sn },
      moduleId: ROOT_MODULE_ID,
      asicIds: [],
      location: {
        slot: board.slot,
        bay: board.slot === undefined ? undefined : Math.floor((board.slot - 1) / 3) + 1,
        port: board.port,
      },
    });
  }

  for (const psu of wire["psus-info"] ?? []) {
    if (psu.slot === undefined) continue;
    const id = getPsuId(psu.slot);
    psus.set(id, {
      ref: { id, kind: "psu", owner: moduleOwner },
      slot: psu.slot,
      serialNumber: psu.psu_sn,
      manufacturer: psu.manufacturer,
      model: psu.model,
      hardwareRevision: psu.hw_revision,
      firmware: {
        application: psu.firmware?.app_version ? { version: psu.firmware.app_version } : undefined,
        bootloader: psu.firmware?.bootloader_version ? { version: psu.firmware.bootloader_version } : undefined,
      },
    });
  }

  for (const fan of wire["fans-info"] ?? []) {
    if (fan.slot === undefined) continue;
    const id = getFanId(fan.slot);
    fans.set(id, { ref: { id, kind: "fan", owner: moduleOwner }, slot: fan.slot, name: fan.name });
  }

  const cb = wire["cb-info"];
  if (cb) {
    const id = asComponentId(`${ROOT_MODULE_ID}:controlBoard:1`);
    controlBoards.set(id, {
      ref: { id, kind: "controlBoard", owner: moduleOwner },
      role: "primary",
      serialNumber: cb.serial_number,
      boardId: cb.board_id,
      machineName: cb.machine_name,
      firmware: toFirmwareInfo(cb.firmware),
      cpu: cb.mpu
        ? {
            modelName: cb.mpu.model_name,
            architecture: cb.mpu.cpu_architecture?.toString(),
            hardware: cb.mpu.hardware,
            revision: cb.mpu.revision,
            implementer: cb.mpu.cpu_implementer,
            part: cb.mpu.cpu_part,
            variant: cb.mpu.cpu_variant,
          }
        : undefined,
    });
  }

  const moduleNode: ModuleNode = {
    id: ROOT_MODULE_ID,
    profile,
    capabilities: { commands: V1_COMMANDS },
    hashboardIds: [...hashboardNodes.keys()],
    controlBoardIds: [...controlBoards.keys()],
    psuIds: [...psus.keys()],
    fanIds: [...fans.keys()],
  };
  const modules = new Map([[ROOT_MODULE_ID, moduleNode]]);
  const normalizedDeviceId = asDeviceId(deviceId);
  const topology: Topology =
    profile === "ct1"
      ? {
          kind: "directModule",
          deviceId: normalizedDeviceId,
          moduleId: ROOT_MODULE_ID,
          modules,
          hashboards: hashboardNodes,
        }
      : {
          kind: "rig",
          deviceId: normalizedDeviceId,
          capabilities: V1_COMMANDS,
          rootModuleId: ROOT_MODULE_ID,
          modules,
          hashboards: hashboardNodes,
        };

  return { resource: ready(), topology, controlBoards, hashboards, asics, psus, fans };
};

export const toCanonicalHardwareTelemetry = (wire: HardwareInfoHardwareinfo): TelemetryState => {
  const state = createEmptyTelemetryState();
  state.resourceByScope.set(asResourceKey("telemetry:fan-calibration:module:root"), ready());
  for (const fan of wire["fans-info"] ?? []) {
    if (fan.slot === undefined) continue;
    const componentId = getFanId(fan.slot);
    state.fans.set(componentId, {
      componentId,
      minimumRpm: fan.min_rpm === undefined ? undefined : { latest: { value: fan.min_rpm, units: "RPM" } },
      maximumRpm: fan.max_rpm === undefined ? undefined : { latest: { value: fan.max_rpm, units: "RPM" } },
    });
  }
  return state;
};

type WireLatestMetric = { value?: number | null; unit?: string };
type WireSeriesMetric = TimeSeriesMetricData & { unit?: string };
const isLatestMetric = (value: unknown): value is WireLatestMetric =>
  typeof value === "object" && value !== null && "value" in value;
const isSeriesMetric = (value: unknown): value is WireSeriesMetric =>
  typeof value === "object" && value !== null && "values" in value;

const applyLatestMetrics = (target: LevelRollup | HashboardTelemetry, source: Record<string, unknown>) => {
  for (const field of ["hashrate", "temperature", "power", "efficiency"] as const) {
    const metric = source[field];
    if (!isLatestMetric(metric)) continue;
    const latest = toCanonicalMeasurement(metric.value, metric.unit);
    if (latest) target[field] = { ...(target[field] ?? {}), latest };
  }
};

export const toCanonicalTelemetry = (wire: TelemetryData): TelemetryState => {
  const state = createEmptyTelemetryState();
  const timestampMs = Date.parse(wire.timestamp);
  state.resourceByScope.set(asResourceKey("telemetry:current:module:root"), {
    status: "ready",
    updatedAtMs: Number.isNaN(timestampMs) ? Date.now() : timestampMs,
  });

  if (wire.miner) {
    const rollup: LevelRollup = {};
    applyLatestMetrics(rollup, wire.miner as unknown as Record<string, unknown>);
    state.rollupsByEntity.set(ROOT_MODULE_KEY, rollup);
  }

  for (const board of wire.hashboards ?? []) {
    if (!board.serial_number) continue;
    const id = getHashboardId(board.serial_number);
    const normalized: HashboardTelemetry = { hashboardId: id };
    applyLatestMetrics(normalized, board as unknown as Record<string, unknown>);
    const temp = board.temperature;
    if (temp) {
      normalized.temperature = normalizeLatestMetric(temp.average, temp.unit);
      normalized.inletTemperature = normalizeLatestMetric(temp.inlet, temp.unit);
      normalized.outletTemperature = normalizeLatestMetric(temp.outlet, temp.unit);
    }
    state.hashboards.set(id, normalized);

    const asicCount = Math.max(
      board.asics?.hashrate?.values?.length ?? 0,
      board.asics?.temperature?.values?.length ?? 0,
    );
    const temperatures: number[] = [];
    for (let index = 0; index < asicCount; index += 1) {
      const asicId = getAsicId(board.serial_number, index);
      const asic: AsicTelemetry = { asicId };
      const hashrate = toCanonicalMeasurement(board.asics?.hashrate?.values?.[index], board.asics?.hashrate?.unit);
      const temperature = toCanonicalMeasurement(
        board.asics?.temperature?.values?.[index],
        board.asics?.temperature?.unit,
      );
      if (hashrate) asic.hashrate = { latest: hashrate };
      if (temperature) {
        asic.temperature = { latest: temperature };
        if (temperature.value !== null) temperatures.push(temperature.value);
      }
      state.asics.set(asicId, asic);
    }
    if (temperatures.length) {
      normalized.averageAsicTemperature = {
        latest: { value: temperatures.reduce((sum, value) => sum + value, 0) / temperatures.length, units: "°C" },
      };
      normalized.maximumAsicTemperature = { latest: { value: Math.max(...temperatures), units: "°C" } };
    }
  }

  for (const psu of wire.psus ?? []) {
    // MDK v1 telemetry indexes physical PSU positions from zero, while hardware
    // inventory exposes the corresponding physical slots from one.
    const slot = psu.index + 1;
    const id = getPsuId(slot);
    const normalized: PsuTelemetry = { componentId: id };
    normalized.inputVoltage = normalizeLatestMetric(psu.voltage?.input, psu.voltage?.unit);
    normalized.outputVoltage = normalizeLatestMetric(psu.voltage?.output, psu.voltage?.unit);
    normalized.inputCurrent = normalizeLatestMetric(psu.current?.input, psu.current?.unit);
    normalized.outputCurrent = normalizeLatestMetric(psu.current?.output, psu.current?.unit);
    normalized.inputPower = normalizeLatestMetric(psu.power?.input, psu.power?.unit);
    normalized.outputPower = normalizeLatestMetric(psu.power?.output, psu.power?.unit);
    normalized.ambientTemperature = normalizeLatestMetric(psu.temperature?.ambient, psu.temperature?.unit);
    normalized.averageTemperature = normalizeLatestMetric(psu.temperature?.average, psu.temperature?.unit);
    normalized.hotspotTemperature = normalizeLatestMetric(psu.temperature?.hotspot, psu.temperature?.unit);
    state.psus.set(id, normalized);
  }

  return state;
};

const parseDurationMs = (duration: string | undefined): number | undefined => {
  if (!duration) return undefined;
  const match = /^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$/.exec(duration);
  if (!match) return undefined;
  const durationMs = (Number(match[1] ?? 0) * 60 * 60 + Number(match[2] ?? 0) * 60 + Number(match[3] ?? 0)) * 1_000;
  return durationMs > 0 ? durationMs : undefined;
};

const toCanonicalSeries = (
  metric: WireSeriesMetric,
  startTimeMs: number,
  bucketSizeMs: number,
): MetricTelemetry | undefined => {
  const canonical = toCanonicalUnit(metric.unit);
  if (!canonical || !metric.values) return undefined;
  const measurement = (value: number | undefined): Measurement | undefined =>
    value === undefined ? undefined : { value: value * canonical.factor, units: canonical.units };
  return {
    timeSeries: {
      bucketSizeMs,
      units: canonical.units,
      points: metric.values.map((value, index) => ({
        timestampMs: startTimeMs + index * bucketSizeMs,
        value: value === null ? null : value * canonical.factor,
      })),
      aggregates: metric.aggregates
        ? {
            min: measurement(metric.aggregates.min),
            avg: measurement(metric.aggregates.avg),
            max: measurement(metric.aggregates.max),
          }
        : undefined,
    },
  };
};

const applySeriesMetrics = (
  target: LevelRollup | HashboardTelemetry,
  source: Record<string, unknown>,
  startTimeMs: number,
  bucketSizeMs: number,
) => {
  for (const field of ["hashrate", "temperature", "power", "efficiency"] as const) {
    const metric = source[field];
    if (!isSeriesMetric(metric)) continue;
    const normalized = toCanonicalSeries(metric, startTimeMs, bucketSizeMs);
    if (normalized) target[field] = normalized;
  }
};

export const toCanonicalTimeSeries = (
  wire: TimeSeriesResponse,
  options: { startTimeMs?: number; interval?: string } = {},
): TelemetryState => {
  const state = createEmptyTelemetryState();
  const responseStartTimeMs = Date.parse(wire.meta?.start_time ?? "");
  const safeStartTimeMs = Number.isNaN(responseStartTimeMs) ? (options.startTimeMs ?? Date.now()) : responseStartTimeMs;
  const bucketSizeMs = parseDurationMs(wire.meta?.interval) ?? parseDurationMs(options.interval);
  if (bucketSizeMs === undefined)
    throw new Error("MDK v1 time-series response did not provide a valid positive interval");
  state.resourceByScope.set(asResourceKey("telemetry:history:module:root"), ready());

  type TimeSeriesHashboard = NonNullable<NonNullable<TimeSeriesResponse["data"]>["hashboards"]>[number];
  const boardByIndex = new Map<number, TimeSeriesHashboard>();
  const ambiguousBoardIndexes = new Set<number>();
  for (const board of wire.data?.hashboards ?? []) {
    if (board.index === undefined || !board.serial_number || ambiguousBoardIndexes.has(board.index)) continue;
    if (boardByIndex.has(board.index)) {
      boardByIndex.delete(board.index);
      ambiguousBoardIndexes.add(board.index);
    } else {
      boardByIndex.set(board.index, board);
    }
  }

  if (wire.data?.miner) {
    const rollup: LevelRollup = {};
    applySeriesMetrics(rollup, wire.data.miner, safeStartTimeMs, bucketSizeMs);
    state.rollupsByEntity.set(ROOT_MODULE_KEY, rollup);
  }

  for (const board of wire.data?.hashboards ?? []) {
    if (!board.serial_number) continue;
    const id = getHashboardId(board.serial_number);
    const normalized: HashboardTelemetry = { hashboardId: id };
    applySeriesMetrics(normalized, board, safeStartTimeMs, bucketSizeMs);
    state.hashboards.set(id, normalized);
  }

  for (const asicWire of wire.data?.asics ?? []) {
    const board = boardByIndex.get(asicWire.hashboard_index);
    if (!board?.serial_number) continue;
    const id = getAsicId(board.serial_number, asicWire.index);
    const normalized: AsicTelemetry = { asicId: id };
    for (const field of ["hashrate", "temperature"] as const) {
      const metric = asicWire[field];
      if (isSeriesMetric(metric)) normalized[field] = toCanonicalSeries(metric, safeStartTimeMs, bucketSizeMs);
    }
    state.asics.set(id, normalized);
  }

  return state;
};

// The v1 firmware lays hashboards out three-to-a-bay; the store carries the bay
// so components never re-derive it. Slots are 1-based (bay 1 = slots 1-3).
const bayForSlot = (slot: number) => Math.floor((slot - 1) / 3) + 1;

export const toCanonicalHashboards = (wire: HashboardInfo[]): HashboardHardwareData[] =>
  wire
    .filter(
      (hb): hb is HashboardInfo & { hb_sn: string; slot: number } =>
        Boolean(hb.hb_sn) && hb.slot !== undefined && hb.slot > 0,
    )
    .map((hb) => ({
      serial: hb.hb_sn,
      slot: hb.slot,
      board: hb.board,
      bay: bayForSlot(hb.slot),
      // Populated later by the hashboard-status endpoint (see toCanonicalHashboardStatus).
      asicIds: [],
    }));

export const toCanonicalHashboardStatus = (serial: string, wire: HashboardStatsHashboardstats): HashboardStatus => {
  const asics = wire.asics ?? [];
  const asicIds = asics.filter((asic) => asic.index !== undefined).map((asic) => getAsicId(serial, asic.index!));

  const asicHardware: AsicHardwareData[] = [];
  const asicTelemetry: AsicTelemetrySample[] = [];
  for (const asic of asics) {
    if (asic.index === undefined || asic.row === undefined || asic.column === undefined) continue;
    const id = getAsicId(serial, asic.index);
    asicHardware.push({ id, hashboardSerial: serial, index: asic.index, row: asic.row, column: asic.column });
    const sample: AsicTelemetrySample = { asicId: id };
    if (asic.voltage_mv !== undefined && asic.voltage_mv !== null) {
      sample.voltage = { value: asic.voltage_mv / 1000, units: "V" };
    }
    if (asic.freq_mhz !== undefined && asic.freq_mhz !== null) {
      sample.frequency = { value: asic.freq_mhz, units: "MHz" };
    }
    asicTelemetry.push(sample);
  }

  return { serial, asicIds, asics: asicHardware, asicTelemetry };
};
