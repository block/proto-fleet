import { poolInfoToPoolConfig, poolInfoToTestConnection } from "../../poolAdapters";
import type {
  AdapterRequestOptions,
  HashboardStatus,
  MinerAdapter,
  SystemLogs,
  SystemLogSource,
  TelemetryLevel,
  TimeSeriesQuery,
} from "../types";
import {
  toCanonicalCoolingStatus,
  toCanonicalErrors,
  toCanonicalHardware,
  toCanonicalHardwareTelemetry,
  toCanonicalHashboards,
  toCanonicalHashboardStatus,
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
import {
  type Api,
  type ChangePasswordRequest,
  type GetCurrentTelemetryParams,
  type LocateSystemParams,
  type TimeSeriesRequest,
  PerformanceMode as V1PerformanceMode,
} from "@/protoOS/api/generatedApi";
import type { CoolingMode, MiningTargetUpdate } from "@/protoOS/store/contract";
import type { PoolInfo } from "@/shared/types/pool";

export type MdkV1Client = InstanceType<typeof Api>["api"];

const EMPTY_BODY_JSON_ERROR = "Unexpected end of JSON input";

const hasEmptyBodyJsonError = (value: unknown): boolean => {
  if (typeof value !== "object" || value === null) return false;

  const candidate = value as { message?: unknown; error?: unknown };
  if (typeof candidate.message === "string" && candidate.message.includes(EMPTY_BODY_JSON_ERROR)) return true;

  const nested = candidate.error;
  return (
    typeof nested === "object" &&
    nested !== null &&
    typeof (nested as { message?: unknown }).message === "string" &&
    (nested as { message: string }).message.includes(EMPTY_BODY_JSON_ERROR)
  );
};

const runEmptyBodyCommand = async (request: () => Promise<unknown>): Promise<void> => {
  try {
    const response = await request();
    if (hasEmptyBodyJsonError(response)) return;
  } catch (error: unknown) {
    if (hasEmptyBodyJsonError(error)) return;
    throw error;
  }
};

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

  async getHashboards(params: AdapterRequestOptions = {}) {
    const response = await this.client.getAllHashboards(params);
    return toCanonicalHashboards(response.data["hashboards-info"] ?? []);
  }

  async getHashboardStatus(serials: string[], params: AdapterRequestOptions = {}): Promise<HashboardStatus[]> {
    return Promise.all(
      serials.map(async (serial) => {
        const response = await this.client.getHashboardStatus({ hbSn: serial }, params);
        return toCanonicalHashboardStatus(serial, response.data["hashboard-stats"] ?? {});
      }),
    );
  }

  async login(password: string, params: AdapterRequestOptions = {}) {
    const response = await this.client.login({ password }, params);
    return { accessToken: response.data.access_token, refreshToken: response.data.refresh_token };
  }

  async refreshAccessToken(refreshToken: string, params: AdapterRequestOptions = {}) {
    const response = await this.client.refreshToken({ refresh_token: refreshToken }, params);
    return { accessToken: response.data.access_token };
  }

  async getSystemLogs(
    query: { lines: number; source?: SystemLogSource },
    params: AdapterRequestOptions = {},
  ): Promise<SystemLogs | undefined> {
    const response = await this.client.getSystemLogs(query, params);
    return response.data["logs"];
  }

  async getCurrentTelemetry(levels: TelemetryLevel[] = [], params: AdapterRequestOptions = {}) {
    // The MDK firmware expects `level` as a single comma-joined value
    // (`level=miner,hashboard`), i.e. OpenAPI explode:false. Sending the array
    // the generated type advertises serializes as repeated `level=` params and
    // the device rejects it with 400 "Query deserialize error: duplicate field
    // `level`". Join manually; the cast is required because the generated type
    // does not match the firmware's actual query contract. Verified against a
    // real rig: the comma form returns miner/hashboards/psus/asics.
    const level = levels.length ? (levels.join(",") as unknown as GetCurrentTelemetryParams["level"]) : undefined;
    const response = await this.client.getCurrentTelemetry({ level }, params);
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

  async startMining(params: AdapterRequestOptions = {}) {
    await this.client.startMining(params);
  }

  async stopMining(params: AdapterRequestOptions = {}) {
    await this.client.stopMining(params);
  }

  async rebootSystem(params: AdapterRequestOptions = {}) {
    await this.client.rebootSystem(params);
  }

  async startFirmwareUpdate(params: AdapterRequestOptions = {}) {
    await runEmptyBodyCommand(() => this.client.postUpdateSystem(params));
  }

  async checkForFirmwareUpdate(params: AdapterRequestOptions = {}) {
    await runEmptyBodyCommand(() => this.client.updateCheck(params));
  }

  async locateSystem(options: { enable?: boolean; ledOnTime?: number }, params: AdapterRequestOptions = {}) {
    const query: LocateSystemParams = {};
    if (options.enable !== undefined) query.enable = options.enable;
    if (options.ledOnTime !== undefined) query.led_on_time = options.ledOnTime;
    await this.client.locateSystem(query, params);
  }

  async createPools(pools: PoolInfo[], params: AdapterRequestOptions = {}) {
    await this.client.createPools(pools.map(poolInfoToPoolConfig), params);
  }

  async editPool(poolId: number, pool: PoolInfo, params: AdapterRequestOptions = {}) {
    await this.client.editPool({ id: poolId }, poolInfoToPoolConfig(pool), params);
  }

  async testPoolConnection(pool: PoolInfo, params: AdapterRequestOptions = {}) {
    await this.client.testPoolConnection(poolInfoToTestConnection(pool), params);
  }

  async setPassword(password: string, params: AdapterRequestOptions = {}) {
    await this.client.setPassword({ password }, params);
  }

  async changePassword(request: ChangePasswordRequest, params: AdapterRequestOptions = {}) {
    await this.client.changePassword(request, params);
  }
}
