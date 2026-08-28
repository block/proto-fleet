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
  type Api,
  type ChangePasswordRequest,
  type GetCurrentTelemetryParams,
  type LocateSystemParams,
  type TimeSeriesRequest,
  PerformanceMode as V1PerformanceMode,
} from "./generated/generatedApi";
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

// The generated client throws the raw HttpResponse on non-2xx and attaches the
// empty-body SyntaxError to `error`. A genuine failure (e.g. 503) with an empty
// body must NOT be mistaken for the legitimate 2xx-empty-body success case, so
// only treat the parse error as success when the response is not a failed one.
const isFailedResponse = (value: unknown): boolean => {
  if (typeof value !== "object" || value === null) return false;

  const candidate = value as { ok?: unknown; status?: unknown };
  if (candidate.ok === false) return true;
  return typeof candidate.status === "number" && candidate.status >= 400;
};

const runEmptyBodyCommand = async (request: () => Promise<unknown>): Promise<void> => {
  try {
    const response = await request();
    if (hasEmptyBodyJsonError(response)) return;
  } catch (error: unknown) {
    if (isFailedResponse(error)) throw error;
    if (hasEmptyBodyJsonError(error)) return;
    throw error;
  }
};

export class MdkV1Adapter implements MinerAdapter {
  /**
   * @param client   The generated MDK v1 HTTP client.
   * @param getAccessToken Supplies the current bearer token so the adapter can
   *   decorate its own requests. Omitted (or returning "") when no client-side
   *   credential applies — e.g. fleet mode, where the minerproxy owns auth, or
   *   pre-login public calls. The provider carries the credential here.
   */
  constructor(
    private readonly client: MdkV1Client,
    private readonly getAccessToken?: () => string,
  ) {}

  // v1 gates nearly every endpoint on a bearer token exchanged from the miner
  // password. Attach it to every request that has one. The public endpoints
  // (system/status, pairing) are decorated too, but the firmware ignores the
  // header there (verified: a bad bearer on /system/status still returns 200),
  // so it's harmless. Only login/refresh — which establish the credential —
  // must stay undecorated, to avoid sending a stale bearer. Caller-supplied
  // headers win over ours.
  decorate(params: AdapterRequestOptions = {}): AdapterRequestOptions {
    const token = this.getAccessToken?.();
    if (!token) return params;
    return { ...params, headers: { Authorization: `Bearer ${token}`, ...params.headers } };
  }

  async getSystemInfo(params: AdapterRequestOptions = {}) {
    const response = await this.client.getSystemInfo(this.decorate(params));
    return toCanonicalSystemInfo(response.data["system-info"] ?? {});
  }

  async getNetworkInfo(params: AdapterRequestOptions = {}) {
    const response = await this.client.getNetwork(this.decorate(params));
    return toCanonicalNetworkInfo(response.data["network-info"] ?? {});
  }

  async getSystemStatus(params: AdapterRequestOptions = {}) {
    const response = await this.client.getSystemStatus(this.decorate(params));
    return { onboarded: response.data.onboarded, passwordSet: response.data.password_set };
  }

  async getSystemTag(params: AdapterRequestOptions = {}) {
    const response = await this.client.getSystemTag(this.decorate(params));
    return toCanonicalSystemTag(response.data);
  }

  async setSystemTag(value: string, params: AdapterRequestOptions = {}) {
    await this.client.putSystemTag(value, this.decorate(params));
  }

  async getMiningStatus(params: AdapterRequestOptions = {}) {
    const response = await this.client.getMiningStatus(this.decorate(params));
    return toCanonicalMiningStatus(response.data["mining-status"] ?? {});
  }

  async getErrors(params: AdapterRequestOptions = {}) {
    const response = await this.client.getErrors(this.decorate(params));
    return toCanonicalErrors(response.data);
  }

  async getPools(params: AdapterRequestOptions = {}) {
    const response = await this.client.listPools(this.decorate(params));
    return toCanonicalPools(response.data.pools ?? []);
  }

  async getMiningTarget(params: AdapterRequestOptions = {}) {
    const response = await this.client.getMiningTarget(this.decorate(params));
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
      this.decorate(params),
    );
    return toCanonicalMiningTarget(response.data);
  }

  async getHardware(
    context: { deviceId: string; profile: "r2" | "ct1" | "unknown" },
    params: AdapterRequestOptions = {},
  ) {
    const response = await this.client.getHardware(this.decorate(params));
    const wire = response.data["hardware-info"] ?? {};
    return { hardware: toCanonicalHardware(wire, context), telemetry: toCanonicalHardwareTelemetry(wire) };
  }

  async getHashboards(params: AdapterRequestOptions = {}) {
    const response = await this.client.getAllHashboards(this.decorate(params));
    return toCanonicalHashboards(response.data["hashboards-info"] ?? []);
  }

  async getHashboardStatus(serials: string[], params: AdapterRequestOptions = {}): Promise<HashboardStatus[]> {
    const decorated = this.decorate(params);
    return Promise.all(
      serials.map(async (serial) => {
        const response = await this.client.getHashboardStatus({ hbSn: serial }, decorated);
        return toCanonicalHashboardStatus(serial, response.data["hashboard-stats"] ?? {});
      }),
    );
  }

  async login(password: string, params: AdapterRequestOptions = {}) {
    // Establishes the credential — must not carry a (possibly stale) bearer.
    const response = await this.client.login({ password }, params);
    return { accessToken: response.data.access_token, refreshToken: response.data.refresh_token };
  }

  async refreshAccessToken(refreshToken: string, params: AdapterRequestOptions = {}) {
    // Renews the credential — undecorated for the same reason as login.
    const response = await this.client.refreshToken({ refresh_token: refreshToken }, params);
    return { accessToken: response.data.access_token };
  }

  async getSystemLogs(
    query: { lines: number; source?: SystemLogSource },
    params: AdapterRequestOptions = {},
  ): Promise<SystemLogs | undefined> {
    const response = await this.client.getSystemLogs(query, this.decorate(params));
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
    const response = await this.client.getCurrentTelemetry({ level }, this.decorate(params));
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
    const response = await this.client.getTimeSeries(wireRequest, this.decorate(params));
    return toCanonicalTimeSeries(response.data, {
      startTimeMs: Date.parse(request.startTime),
      interval: request.interval,
    });
  }

  async getCoolingStatus(params: AdapterRequestOptions = {}) {
    const response = await this.client.getCooling(this.decorate(params));
    return toCanonicalCoolingStatus(response.data["cooling-status"]);
  }

  async setCoolingMode(mode: CoolingMode, params: AdapterRequestOptions = {}) {
    await this.client.setCoolingMode({ mode: toV1CoolingMode(mode) }, this.decorate(params));
  }

  async startMining(params: AdapterRequestOptions = {}) {
    await this.client.startMining(this.decorate(params));
  }

  async stopMining(params: AdapterRequestOptions = {}) {
    await this.client.stopMining(this.decorate(params));
  }

  async rebootSystem(params: AdapterRequestOptions = {}) {
    await this.client.rebootSystem(this.decorate(params));
  }

  async startFirmwareUpdate(params: AdapterRequestOptions = {}) {
    await runEmptyBodyCommand(() => this.client.postUpdateSystem(this.decorate(params)));
  }

  async checkForFirmwareUpdate(params: AdapterRequestOptions = {}) {
    await runEmptyBodyCommand(() => this.client.updateCheck(this.decorate(params)));
  }

  async locateSystem(options: { enable?: boolean; ledOnTime?: number }, params: AdapterRequestOptions = {}) {
    const query: LocateSystemParams = {};
    if (options.enable !== undefined) query.enable = options.enable;
    if (options.ledOnTime !== undefined) query.led_on_time = options.ledOnTime;
    await this.client.locateSystem(query, this.decorate(params));
  }

  async createPools(pools: PoolInfo[], params: AdapterRequestOptions = {}) {
    await this.client.createPools(pools.map(poolInfoToPoolConfig), this.decorate(params));
  }

  async editPool(poolId: number, pool: PoolInfo, params: AdapterRequestOptions = {}) {
    await this.client.editPool({ id: poolId }, poolInfoToPoolConfig(pool), this.decorate(params));
  }

  async testPoolConnection(pool: PoolInfo, params: AdapterRequestOptions = {}) {
    await this.client.testPoolConnection(poolInfoToTestConnection(pool), this.decorate(params));
  }

  async setPassword(password: string, params: AdapterRequestOptions = {}) {
    await this.client.setPassword({ password }, this.decorate(params));
  }

  async changePassword(request: ChangePasswordRequest, params: AdapterRequestOptions = {}) {
    await this.client.changePassword(request, this.decorate(params));
  }
}
