import { PerformanceMode as V1PerformanceMode, type Api, type RequestParams } from "@/protoOS/api/generatedApi";
import type { MiningTargetUpdate } from "@/protoOS/store/contract";
import type { MinerAdapter } from "../types";
import {
  normalizeV1Errors,
  normalizeV1Hardware,
  normalizeV1MiningStatus,
  normalizeV1MiningTarget,
  normalizeV1NetworkInfo,
  normalizeV1Pools,
  normalizeV1SystemInfo,
  normalizeV1Telemetry,
  normalizeV1TimeSeries,
} from "./normalize";

export type MdkV1Client = InstanceType<typeof Api>["api"];

export class MdkV1Adapter implements MinerAdapter {
  readonly dialect = "v1" as const;

  constructor(private readonly client: MdkV1Client) {}

  async getSystemInfo(params: RequestParams = {}) {
    const response = await this.client.getSystemInfo(params);
    return normalizeV1SystemInfo(response.data["system-info"] ?? {});
  }

  async getNetworkInfo(params: RequestParams = {}) {
    const response = await this.client.getNetwork(params);
    return normalizeV1NetworkInfo(response.data["network-info"] ?? {});
  }

  async getSystemStatus(params: RequestParams = {}) {
    const response = await this.client.getSystemStatus(params);
    return { onboarded: response.data.onboarded, passwordSet: response.data.password_set };
  }

  async getMiningStatus(params: RequestParams = {}) {
    const response = await this.client.getMiningStatus(params);
    return normalizeV1MiningStatus(response.data["mining-status"] ?? {});
  }

  async getErrors(params: RequestParams = {}) {
    const response = await this.client.getErrors(params);
    return normalizeV1Errors(response.data);
  }

  async getPools(params: RequestParams = {}) {
    const response = await this.client.listPools(params);
    return normalizeV1Pools(response.data.pools ?? []);
  }

  async getMiningTarget(params: RequestParams = {}) {
    const response = await this.client.getMiningTarget(params);
    return normalizeV1MiningTarget(response.data);
  }

  async updateMiningTarget(update: MiningTargetUpdate, params: RequestParams = {}) {
    const response = await this.client.editMiningTarget(
      {
        power_target_watts: update.valueWatts,
        performance_mode:
          update.performanceMode === "maximumHashrate"
            ? V1PerformanceMode.MaximumHashrate
            : update.performanceMode === "efficiency"
              ? V1PerformanceMode.Efficiency
              : undefined,
        balance_bays: update.balanceBays,
        hash_on_disconnect: update.hashOnDisconnect,
      },
      params,
    );
    return normalizeV1MiningTarget(response.data);
  }

  async getHardware(context: { deviceId: string; profile: "r2" | "ct1" | "unknown" }, params: RequestParams = {}) {
    const response = await this.client.getHardware(params);
    return normalizeV1Hardware(response.data["hardware-info"] ?? {}, context);
  }

  async getCurrentTelemetry(query = {}, params: RequestParams = {}) {
    const response = await this.client.getCurrentTelemetry(query, params);
    return normalizeV1Telemetry(response.data);
  }

  async getTimeSeries(request: Parameters<MdkV1Client["getTimeSeries"]>[0], params: RequestParams = {}) {
    const response = await this.client.getTimeSeries(request, params);
    return normalizeV1TimeSeries(response.data);
  }
}
