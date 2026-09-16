import type { ContainerHistoryRequest, ContainerHistoryResult } from "./containerHistory";
import type { ChangePasswordRequest } from "./mdkV1";
import type {
  CoolingMode,
  FanTelemetry,
  HardwareState,
  Measurement,
  MinerAdapterId,
  MinerError,
  MiningTarget,
  MiningTargetUpdate,
  NetworkInfoState,
  OperationalStatus,
  PoolInfo,
  SystemInfoData,
  TelemetrySnapshot,
  TelemetryState,
} from "@/protoOS/store/contract";
import type { AsicHardwareData, HashboardHardwareData } from "@/protoOS/store/types";
import type { PoolInfo as PoolFormInput } from "@/shared/types/pool";

export interface AdapterRequestOptions {
  headers?: Record<string, string>;
  signal?: AbortSignal | null;
}

export interface AdapterProbeMatch {
  deviceId?: string;
  product?: string;
  reportedVersion?: string;
}

export interface MiningScope {
  identifiers?: string[];
  modules?: Array<{ tank: number; module: number }>;
}

export interface ModuleRunInRequest {
  taskId?: string;
  duration?: string;
  sampleInterval?: string;
  cooldownDuration?: string;
  boardPowerTargetWatts?: number;
  stopMiningOnFinish?: boolean;
}

export interface ModuleRunInResult {
  taskId?: string;
  moduleId?: string;
  status?: string;
  voltageLeaderMac?: string;
}

export interface ContainerSettings {
  supported: boolean;
  controls: Array<{ name: string; description?: string; enabled?: boolean }>;
  setpoints: Array<{ name: string; description?: string; value?: number; unit?: string; min?: number; max?: number }>;
}

export interface SavedPoolSettings extends PoolFormInput {
  id: string;
  enabled: boolean;
}

interface MinerAdapterDefinitionBase<Match extends AdapterProbeMatch = AdapterProbeMatch> {
  id: MinerAdapterId;
  displayName: string;
  probe: {
    path: string;
    parseResponse(body: unknown): Match | undefined;
  };
}

export type MinerAdapterDefinition<Match extends AdapterProbeMatch = AdapterProbeMatch> =
  | (MinerAdapterDefinitionBase<Match> & {
      support: "supported";
      create(match: Match): MinerAdapter;
    })
  | (MinerAdapterDefinitionBase<Match> & {
      support: "probe-only";
    });

export type AdapterResolution =
  | { status: "probing"; adapterId: undefined; adapter: undefined }
  | { status: "ready"; adapterId: MinerAdapterId; adapter: MinerAdapter }
  | {
      status: "unsupported";
      adapterId: MinerAdapterId | undefined;
      adapter: undefined;
      match?: AdapterProbeMatch;
    }
  // A probe errored or the miner was unreachable, so identification is
  // inconclusive rather than a confirmed "unsupported miner". Recoverable: the
  // caller can re-probe. Distinct from "unsupported", which is terminal.
  | { status: "unreachable"; adapterId: undefined; adapter: undefined };

/** Access/refresh token pair returned by the miner auth endpoints. */
export interface AuthTokens {
  accessToken: string;
  refreshToken: string;
}

/** Which log stream to fetch from the miner. Mirrors the v1 logs source union. */
export type SystemLogSource = "os" | "pool_sw" | "miner_sw" | "miner_web_server";

/** Canonical system-logs payload, mirroring the v1 logs envelope. */
export interface SystemLogs {
  content?: string[];
  lines?: number;
  source?: string;
}

/** One ASIC's latest telemetry sample, in canonical units. */
export interface AsicTelemetrySample {
  asicId: string;
  hashrate?: Measurement;
  temperature?: Measurement;
  voltage?: Measurement;
  frequency?: Measurement;
  passRate?: Measurement;
}

/**
 * Normalized per-hashboard status: the ASIC identities/positions and their
 * latest telemetry, with all v1 field-name and unit knowledge already applied.
 */
export interface HashboardStatus {
  serial: string;
  asicIds: string[];
  asics: AsicHardwareData[];
  asicTelemetry: AsicTelemetrySample[];
}

export type TelemetryLevel = "miner" | "hashboard" | "psu" | "asic";

export type TimeSeriesLevel =
  | { type: "miner"; fields: ("hashrate" | "temperature" | "power" | "efficiency")[] }
  | {
      type: "hashboard";
      fields: ("hashrate" | "temperature" | "inletTemp" | "outletTemp" | "power" | "efficiency")[];
      indexes?: number[];
    }
  | { type: "asic"; fields: ("hashrate" | "temperature")[]; indexes?: number[] }
  | {
      type: "psu";
      fields: (
        | "outputVoltage"
        | "outputCurrent"
        | "outputPower"
        | "inputVoltage"
        | "inputCurrent"
        | "inputPower"
        | "hotspotTemp"
        | "ambientTemp"
        | "averageTemp"
      )[];
      indexes?: number[];
    };

export interface TimeSeriesQuery {
  startTime: string;
  duration?: string;
  endTime?: string;
  interval?: string;
  levels: TimeSeriesLevel[];
  aggregation?: "mean" | "avg" | "min" | "max" | "last" | "sum" | "count";
}

export interface MinerAdapter {
  /**
   * Merge this adapter's own request decoration (auth headers, actor
   * attribution, …) into `params`. Each generation owns what its firmware
   * needs: MDK v1 adds `Authorization: Bearer <token>`; the CT1 MDK v2 API is
   * unauthenticated on-LAN and adds only an `X-Frigate-Actor` attribution
   * header. Adapters apply this to their own requests, so callers pass plain
   * options and never build auth headers themselves. Caller-supplied headers
   * take precedence over the adapter's defaults.
   */
  decorate(params?: AdapterRequestOptions): AdapterRequestOptions;
  getSystemInfo(params?: AdapterRequestOptions): Promise<SystemInfoData>;
  getNetworkInfo(params?: AdapterRequestOptions): Promise<Pick<NetworkInfoState, "interfaces">>;
  getSystemStatus(params?: AdapterRequestOptions): Promise<{
    onboarded?: boolean;
    passwordSet?: boolean;
  }>;
  getSystemTag(params?: AdapterRequestOptions): Promise<string>;
  setSystemTag(value: string, params?: AdapterRequestOptions): Promise<void>;
  getMiningStatus(params?: AdapterRequestOptions): Promise<OperationalStatus>;
  getErrors(params?: AdapterRequestOptions): Promise<MinerError[]>;
  getPools(params?: AdapterRequestOptions): Promise<PoolInfo[]>;
  getPoolSettings?(params?: AdapterRequestOptions): Promise<SavedPoolSettings[]>;
  savePoolSettings?(pool: PoolFormInput, id?: string, params?: AdapterRequestOptions): Promise<void>;
  deletePoolSettings?(id: string, params?: AdapterRequestOptions): Promise<void>;
  reorderPoolSettings?(ids: string[], params?: AdapterRequestOptions): Promise<void>;
  getContainerSettings?(params?: AdapterRequestOptions): Promise<ContainerSettings>;
  getMiningTarget(params?: AdapterRequestOptions): Promise<MiningTarget>;
  updateMiningTarget(update: MiningTargetUpdate, params?: AdapterRequestOptions): Promise<MiningTarget>;
  setScopedPowerTarget?(
    scope: MiningScope,
    watts: number,
    pllAlgorithm?: string,
    params?: AdapterRequestOptions,
  ): Promise<void>;
  clearScopedPowerTarget?(scope: MiningScope, params?: AdapterRequestOptions): Promise<void>;
  startScopedMining?(scope: MiningScope, params?: AdapterRequestOptions): Promise<void>;
  stopScopedMining?(scope: MiningScope, params?: AdapterRequestOptions): Promise<void>;
  setModuleFixedVoltage?(moduleId: string, millivolts: number, params?: AdapterRequestOptions): Promise<void>;
  clearModuleFixedVoltage?(moduleId: string, params?: AdapterRequestOptions): Promise<void>;
  setContainerControl?(name: string, enabled: boolean, params?: AdapterRequestOptions): Promise<void>;
  setContainerSetpoint?(name: string, value: number, params?: AdapterRequestOptions): Promise<void>;
  startModuleRunIn?(
    moduleId: string,
    request: ModuleRunInRequest,
    params?: AdapterRequestOptions,
  ): Promise<ModuleRunInResult>;
  getHardware(
    context: { deviceId: string; profile: "r2" | "ct1" | "unknown" },
    params?: AdapterRequestOptions,
  ): Promise<{ hardware: HardwareState; telemetry: TelemetryState }>;
  getHashboards(params?: AdapterRequestOptions): Promise<HashboardHardwareData[]>;
  getHashboardStatus(serials: string[], params?: AdapterRequestOptions): Promise<HashboardStatus[]>;
  login(password: string, params?: AdapterRequestOptions): Promise<AuthTokens>;
  refreshAccessToken(refreshToken: string, params?: AdapterRequestOptions): Promise<{ accessToken: string }>;
  getSystemLogs(
    query: { lines: number; source?: SystemLogSource },
    params?: AdapterRequestOptions,
  ): Promise<SystemLogs | undefined>;
  getCurrentTelemetry(levels?: TelemetryLevel[], params?: AdapterRequestOptions): Promise<TelemetrySnapshot>;
  getTimeSeries(request: TimeSeriesQuery, params?: AdapterRequestOptions): Promise<TelemetryState>;
  /** Container and plant histories without per-hashboard requests. Unsupported adapters omit this capability. */
  getContainerHistory?(
    request: ContainerHistoryRequest,
    params?: AdapterRequestOptions,
  ): Promise<ContainerHistoryResult>;
  getCoolingStatus(params?: AdapterRequestOptions): Promise<{ mode: CoolingMode; fans: FanTelemetry[] }>;
  setCoolingMode(mode: CoolingMode, params?: AdapterRequestOptions): Promise<void>;

  // Commands. Payloads are generation-agnostic except where noted: pools take the
  // shared PoolInfo form shape (the adapter maps it to its wire format), locate and
  // setPassword take plain options, and start/stop/reboot are parameterless.
  // changePassword still forwards the v1 request body — a trivial two-field
  // payload not worth its own canonical type today.
  startMining(params?: AdapterRequestOptions): Promise<void>;
  stopMining(params?: AdapterRequestOptions): Promise<void>;
  rebootSystem(params?: AdapterRequestOptions): Promise<void>;
  startFirmwareUpdate(params?: AdapterRequestOptions): Promise<void>;
  checkForFirmwareUpdate(params?: AdapterRequestOptions): Promise<void>;
  locateSystem(options: { enable?: boolean; ledOnTime?: number }, params?: AdapterRequestOptions): Promise<void>;
  createPools(pools: PoolFormInput[], params?: AdapterRequestOptions): Promise<void>;
  editPool(poolId: number, pool: PoolFormInput, params?: AdapterRequestOptions): Promise<void>;
  testPoolConnection(pool: PoolFormInput, params?: AdapterRequestOptions): Promise<void>;
  setPassword(password: string, params?: AdapterRequestOptions): Promise<void>;
  changePassword(request: ChangePasswordRequest, params?: AdapterRequestOptions): Promise<void>;
}
