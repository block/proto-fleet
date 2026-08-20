import type { GetCurrentTelemetryParams, RequestParams, TimeSeriesRequest } from "@/protoOS/api/generatedApi";
import type {
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

export interface MinerAdapter {
  getSystemInfo(params?: RequestParams): Promise<SystemInfoData>;
  getNetworkInfo(params?: RequestParams): Promise<Pick<NetworkInfoState, "interfaces">>;
  getSystemStatus(params?: RequestParams): Promise<{
    onboarded?: boolean;
    passwordSet?: boolean;
  }>;
  getMiningStatus(params?: RequestParams): Promise<OperationalStatus>;
  getErrors(params?: RequestParams): Promise<MinerError[]>;
  getPools(params?: RequestParams): Promise<PoolInfo[]>;
  getMiningTarget(params?: RequestParams): Promise<MiningTarget>;
  updateMiningTarget(update: MiningTargetUpdate, params?: RequestParams): Promise<MiningTarget>;
  getHardware(
    context: { deviceId: string; profile: "r2" | "ct1" | "unknown" },
    params?: RequestParams,
  ): Promise<HardwareState>;
  getCurrentTelemetry(query?: GetCurrentTelemetryParams, params?: RequestParams): Promise<TelemetryState>;
  getTimeSeries(request: TimeSeriesRequest, params?: RequestParams): Promise<TelemetryState>;
}
