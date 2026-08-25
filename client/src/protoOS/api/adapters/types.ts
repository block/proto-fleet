import type {
  CoolingMode,
  FanTelemetry,
  HardwareState,
  MinerError,
  MiningTarget,
  MiningTargetUpdate,
  NetworkInfoState,
  OperationalStatus,
  PoolInfo,
  SystemInfoData,
  TelemetryState,
} from "@/protoOS/store/contract";

export interface AdapterRequestOptions {
  secure?: boolean;
  headers?: Record<string, string>;
  signal?: AbortSignal | null;
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
  getMiningTarget(params?: AdapterRequestOptions): Promise<MiningTarget>;
  updateMiningTarget(update: MiningTargetUpdate, params?: AdapterRequestOptions): Promise<MiningTarget>;
  getHardware(
    context: { deviceId: string; profile: "r2" | "ct1" | "unknown" },
    params?: AdapterRequestOptions,
  ): Promise<{ hardware: HardwareState; telemetry: TelemetryState }>;
  getCurrentTelemetry(levels?: TelemetryLevel[], params?: AdapterRequestOptions): Promise<TelemetryState>;
  getTimeSeries(request: TimeSeriesQuery, params?: AdapterRequestOptions): Promise<TelemetryState>;
  getCoolingStatus(params?: AdapterRequestOptions): Promise<{ mode: CoolingMode; fans: FanTelemetry[] }>;
  setCoolingMode(mode: CoolingMode, params?: AdapterRequestOptions): Promise<void>;
}
