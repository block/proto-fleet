import type { MinerAdapter } from "../types";
import {
  toCanonicalErrors,
  toCanonicalHardware,
  toCanonicalMiningStatus,
  toCanonicalMiningTarget,
  toCanonicalNetworkInfo,
  toCanonicalPools,
  toCanonicalSystemInfo,
  toCanonicalTelemetry,
  toCanonicalTimeSeries,
} from "./normalize";
import { type Api, type RequestParams, PerformanceMode as V1PerformanceMode } from "@/protoOS/api/generatedApi";
import type { MiningTargetUpdate } from "@/protoOS/store/contract";

export type MdkV1Client = InstanceType<typeof Api>["api"];

export class MdkV1Adapter implements MinerAdapter {
  constructor(private readonly client: MdkV1Client) {}

  async getSystemInfo(params: RequestParams = {}) {
    const response = await this.client.getSystemInfo(params);
    return toCanonicalSystemInfo(response.data["system-info"] ?? {});
  }

  async getNetworkInfo(params: RequestParams = {}) {
    const response = await this.client.getNetwork(params);
    return toCanonicalNetworkInfo(response.data["network-info"] ?? {});
  }

  async getSystemStatus(params: RequestParams = {}) {
    const response = await this.client.getSystemStatus(params);
    return { onboarded: response.data.onboarded, passwordSet: response.data.password_set };
  }

  async getMiningStatus(params: RequestParams = {}) {
    const response = await this.client.getMiningStatus(params);
    return toCanonicalMiningStatus(response.data["mining-status"] ?? {});
  }

  async getErrors(params: RequestParams = {}) {
    const response = await this.client.getErrors(params);
    return toCanonicalErrors(response.data);
  }

  async getPools(params: RequestParams = {}) {
    const response = await this.client.listPools(params);
    return toCanonicalPools(response.data.pools ?? []);
  }

  async getMiningTarget(params: RequestParams = {}) {
    const response = await this.client.getMiningTarget(params);
    return toCanonicalMiningTarget(response.data);
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
    return toCanonicalMiningTarget(response.data);
  }

  async getHardware(context: { deviceId: string; profile: "r2" | "ct1" | "unknown" }, params: RequestParams = {}) {
    const response = await this.client.getHardware(params);
    return toCanonicalHardware(response.data["hardware-info"] ?? {}, context);
  }

  async getCurrentTelemetry(query = {}, params: RequestParams = {}) {
    const response = await this.client.getCurrentTelemetry(query, params);
    return toCanonicalTelemetry(response.data);
  }

  async getTimeSeries(request: Parameters<MdkV1Client["getTimeSeries"]>[0], params: RequestParams = {}) {
    const response = await this.client.getTimeSeries(request, params);
    return toCanonicalTimeSeries(response.data);
  }
}
