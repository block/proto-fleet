import type { AdapterRequestOptions, MinerAdapter, TelemetryLevel, TimeSeriesQuery } from "../types";
import {
  toCanonicalCoolingStatus,
  toCanonicalErrors,
  toCanonicalHardware,
  toCanonicalHardwareTelemetry,
  toCanonicalMiningStatus,
  toCanonicalMiningTarget,
  toCanonicalNetworkInfo,
  toCanonicalPools,
  toCanonicalSystemInfo,
  toCanonicalSystemTag,
  toCanonicalTelemetry,
  toCanonicalTimeSeries,
  toV1CoolingMode,
} from "./normalize";
import { type Api, type TimeSeriesRequest, PerformanceMode as V1PerformanceMode } from "@/protoOS/api/generatedApi";
import type { CoolingMode, MiningTargetUpdate } from "@/protoOS/store/contract";

export type MdkV1Client = InstanceType<typeof Api>["api"];

export class MdkV1Adapter implements MinerAdapter {
  constructor(private readonly client: MdkV1Client) {}

  async getSystemInfo(params: AdapterRequestOptions = {}) {
    const response = await this.client.getSystemInfo(params);
    return toCanonicalSystemInfo(response.data["system-info"] ?? {});
  }

  async getNetworkInfo(params: AdapterRequestOptions = {}) {
    const response = await this.client.getNetwork(params);
    return toCanonicalNetworkInfo(response.data["network-info"] ?? {});
  }

  async getSystemStatus(params: AdapterRequestOptions = {}) {
    const response = await this.client.getSystemStatus(params);
    return { onboarded: response.data.onboarded, passwordSet: response.data.password_set };
  }

  async getSystemTag(params: AdapterRequestOptions = {}) {
    const response = await this.client.getSystemTag(params);
    return toCanonicalSystemTag(response.data);
  }

  async setSystemTag(value: string, params: AdapterRequestOptions = {}) {
    await this.client.putSystemTag(value, params);
  }

  async getMiningStatus(params: AdapterRequestOptions = {}) {
    const response = await this.client.getMiningStatus(params);
    return toCanonicalMiningStatus(response.data["mining-status"] ?? {});
  }

  async getErrors(params: AdapterRequestOptions = {}) {
    const response = await this.client.getErrors(params);
    return toCanonicalErrors(response.data);
  }

  async getPools(params: AdapterRequestOptions = {}) {
    const response = await this.client.listPools(params);
    return toCanonicalPools(response.data.pools ?? []);
  }

  async getMiningTarget(params: AdapterRequestOptions = {}) {
    const response = await this.client.getMiningTarget(params);
    return toCanonicalMiningTarget(response.data);
  }

  async updateMiningTarget(update: MiningTargetUpdate, params: AdapterRequestOptions = {}) {
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

  async getHardware(
    context: { deviceId: string; profile: "r2" | "ct1" | "unknown" },
    params: AdapterRequestOptions = {},
  ) {
    const response = await this.client.getHardware(params);
    const wire = response.data["hardware-info"] ?? {};
    return { hardware: toCanonicalHardware(wire, context), telemetry: toCanonicalHardwareTelemetry(wire) };
  }

  async getCurrentTelemetry(levels: TelemetryLevel[] = [], params: AdapterRequestOptions = {}) {
    const response = await this.client.getCurrentTelemetry({ level: levels.length ? levels : undefined }, params);
    return toCanonicalTelemetry(response.data);
  }

  async getTimeSeries(request: TimeSeriesQuery, params: AdapterRequestOptions = {}) {
    const needsHashboardIdentity =
      request.levels.some((level) => level.type === "asic") &&
      !request.levels.some((level) => level.type === "hashboard");
    const levels = needsHashboardIdentity
      ? [...request.levels, { type: "hashboard" as const, fields: ["hashrate" as const] }]
      : request.levels;
    const wireRequest: TimeSeriesRequest = {
      start_time: request.startTime,
      duration: request.duration,
      end_time: request.endTime,
      interval: request.interval,
      levels: levels as TimeSeriesRequest["levels"],
      aggregation: request.aggregation,
    };
    const response = await this.client.getTimeSeries(wireRequest, params);
    return toCanonicalTimeSeries(response.data, {
      startTimeMs: Date.parse(request.startTime),
      interval: request.interval,
    });
  }

  async getCoolingStatus(params: AdapterRequestOptions = {}) {
    const response = await this.client.getCooling(params);
    return toCanonicalCoolingStatus(response.data["cooling-status"]);
  }

  async setCoolingMode(mode: CoolingMode, params: AdapterRequestOptions = {}) {
    await this.client.setCoolingMode({ mode: toV1CoolingMode(mode) }, params);
  }
}
