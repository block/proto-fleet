import { Code } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { BREAKER_OUTCOME_UNKNOWN } from "../breakerMessages";
import type { ContainerHistoryRequest } from "../containerHistory";
import type { PoolHistoryRequest } from "../poolHistory";
import type {
  AdapterProbeMatch,
  AdapterRequestOptions,
  ContainerSettings,
  HashboardAction,
  HashboardActionResult,
  HashboardStatus,
  MinerAdapter,
  MiningScope,
  ModuleRunInRequest,
  SystemLogs,
  SystemLogSource,
  TelemetryLevel,
  TimeSeriesQuery,
} from "../types";
import { mapWithConcurrency } from "../utils/async";
import { parseIsoDurationMs } from "../utils/time";
import { createMdkV2Client, type MdkV2Client } from "./connectClient";
import { queryContainerHistory } from "./containerHistory";
import { toCanonicalControllerDiagnostics } from "./controllerDiagnostics";
import { listControllerAuditEvents, readControllerLogs, withDeadline } from "./controllerRecords";
import { createHashboardFirmware } from "./firmware";
import {
  Api,
  type TelemetryServiceQueryTimeSeriesParams,
  V2HashboardIdentifyLEDMode,
  V2PerformanceMode,
  V2PlantWriteConfirmationState,
  V2PLLAlgorithm,
} from "./generated/generatedApi";
import {
  type ContainerSeriesField,
  ct1ContainerId,
  toCanonicalContainer,
  toCanonicalContainerFans,
  toCanonicalContainerTelemetry,
  toCanonicalContainerTimeSeries,
  toCanonicalErrors,
  toCanonicalHashboards,
  toCanonicalHashboardStatus,
  toCanonicalLifecycle,
  toCanonicalMiningTarget,
  toCanonicalNetworkInfo,
  toCanonicalOperationalStatus,
  toCanonicalPoolRecordedShares,
  toCanonicalPools,
  toCanonicalSystemInfo,
} from "./normalize";
import { queryPoolHistory } from "./poolHistory";
import type { CoolingMode, MiningTarget, MiningTargetUpdate } from "@/protoOS/store/contract";
import { getHashboardId } from "@/protoOS/store/utils/entityIds";
import type { PoolInfo } from "@/shared/types/pool";

type QueryTimeSeriesResponse = Awaited<ReturnType<MdkV2Client["telemetryServiceQueryTimeSeries"]>>["data"];
export type { MdkV2Client } from "./connectClient";

/** `connect` reaches Frigate's MDK v2 Connect gateway; `rest` reaches the `/api/v2` gateway. */
export type MdkV2Protocol = "connect" | "rest";

const ACTOR = "proto-os";
const DEFAULT_DEVICE_ID = "ct1";
const HASHBOARD_CONCURRENCY = 8;
const HASHBOARD_TIMESERIES_CACHE_MS = 5 * 60_000;
// A diagnostics poll that stalls past this is reported as unavailable, well
// before the 15-second role observations it would refresh go stale.
export const CONTROLLER_DIAGNOSTICS_TIMEOUT_MS = 8_000;
const SERIES_METRICS: Record<ContainerSeriesField, TelemetryServiceQueryTimeSeriesParams["metric"]> = {
  hashrate: "TIME_SERIES_METRIC_HASHRATE",
  temperature: "TIME_SERIES_METRIC_TEMPERATURE",
  power: "TIME_SERIES_METRIC_POWER",
};

const bucketConfig = (
  interval: string | undefined,
): { size: TelemetryServiceQueryTimeSeriesParams["bucketSize"]; intervalMs: number } => {
  switch (parseIsoDurationMs(interval)) {
    case 60_000:
      return { size: "BUCKET_SIZE_1M", intervalMs: 60_000 };
    case 5 * 60_000:
      return { size: "BUCKET_SIZE_5M", intervalMs: 5 * 60_000 };
    case 15 * 60_000:
    case 30 * 60_000:
      return { size: "BUCKET_SIZE_15M", intervalMs: 15 * 60_000 };
    case 60 * 60_000:
      return { size: "BUCKET_SIZE_1H", intervalMs: 60 * 60_000 };
    case 24 * 60 * 60_000:
      return { size: "BUCKET_SIZE_1D", intervalMs: 24 * 60 * 60_000 };
    default:
      throw new Error(`Unsupported CT1 time-series interval: ${interval ?? "missing"}`);
  }
};

const unsupported = (capability: string): never => {
  throw new Error(`${capability} is not supported by the CT1 MDK v2 API`);
};

const seriesFields = (fields: readonly string[], level: string) => {
  const requested = new Set<ContainerSeriesField>();
  for (const field of fields) {
    if (field === "hashrate" || field === "temperature" || field === "power") {
      requested.add(field);
    } else if (field === "efficiency") {
      requested.add("hashrate");
      requested.add("power");
    } else {
      throw new Error(`Unsupported CT1 ${level} time-series field: ${field}`);
    }
  }
  return requested;
};

const toWireModuleId = (value: string) => {
  const marker = ":module:";
  const markerIndex = value.lastIndexOf(marker);
  return markerIndex === -1 ? value : value.slice(markerIndex + marker.length);
};

// The only codes the controller returns for container-control requests it rejects
// before writing (not primary, unsupported backend, bad request). Any other failure
// may have reached the PLC, so its outcome is unknown until readback.
const BREAKER_REFUSAL_CODES = new Set<number>([Code.InvalidArgument, Code.FailedPrecondition, Code.Unimplemented]);

const breakerWriteError = (error: unknown) => {
  const failure = error as { status?: number; error?: { code?: number; message?: string } } | undefined;
  // Leave authentication failures for the caller's token refresh and retry.
  if (failure?.status === 401 || failure?.error?.code === Code.Unauthenticated) return error;
  const code = failure?.error?.code;
  if (code !== undefined && BREAKER_REFUSAL_CODES.has(code)) {
    const detail = failure?.error?.message?.trim();
    return Object.assign(
      new Error(`The controller refused the request${detail ? `: ${detail}` : ""}. The breaker was not changed.`),
      { cause: error },
    );
  }
  return Object.assign(new Error(BREAKER_OUTCOME_UNKNOWN), { cause: error });
};

const toWireDeviceId = (value: string) => (value.startsWith("device:") ? value.slice("device:".length) : value);

const toWirePerformanceMode = (value: MiningTargetUpdate["performanceMode"]) =>
  value === "maximumHashrate"
    ? V2PerformanceMode.PERFORMANCE_MODE_MAXIMUM_HASHRATE
    : value === "efficiency"
      ? V2PerformanceMode.PERFORMANCE_MODE_EFFICIENCY
      : undefined;

const toWirePllAlgorithm = (value: string | undefined): V2PLLAlgorithm | undefined => {
  if (value === undefined) return undefined;
  const algorithm = value as V2PLLAlgorithm;
  if (Object.values(V2PLLAlgorithm).includes(algorithm)) return algorithm;
  throw new Error(`Unknown CT1 PLL algorithm: ${value}`);
};

export class MdkV2Adapter implements MinerAdapter {
  readonly hashboardFirmware;
  readonly getControllerLogs?: MinerAdapter["getControllerLogs"];
  readonly listAuditEvents?: MinerAdapter["listAuditEvents"];
  private deviceId: string | undefined;
  private readonly hashboardTimeSeriesCache = new Map<string, { expiresAtMs: number; data: QueryTimeSeriesResponse }>();

  constructor(
    private readonly client: MdkV2Client,
    match: AdapterProbeMatch = {},
  ) {
    this.deviceId = match.deviceId;
    this.hashboardFirmware = createHashboardFirmware(client, (params) => this.decorate(params));
    // Only the Connect client can bound the following log stream. The REST
    // client is generated with ListAuditEvents too, but its one caller, Fleet's
    // miner proxy, forwards neither route, so both reads follow the log reader.
    const { systemServiceReadLogTail: readLogTail, systemServiceListAuditEvents: listAuditEvents } = client;
    if (readLogTail && listAuditEvents) {
      this.getControllerLogs = (query, params) =>
        readControllerLogs(readLogTail, client.systemServiceGetSystemStatus, query, this.decorate(params));
      this.listAuditEvents = (query, params) =>
        listControllerAuditEvents(listAuditEvents, query, this.decorate(params));
    }
  }

  /**
   * Build an adapter that owns its transport: constructs the generated MDK v2
   * client for `baseUrl`. The provider resolves miners through this factory so
   * no generated v2 runtime leaks into MinerHostingContext; tests construct the
   * class directly with a mock client.
   */
  static fromConfig(config: { baseUrl: string; protocol: MdkV2Protocol }, match: AdapterProbeMatch = {}): MdkV2Adapter {
    const client: MdkV2Client =
      config.protocol === "rest"
        ? new Api({ baseUrl: config.baseUrl }).api
        : createMdkV2Client(createConnectTransport({ baseUrl: config.baseUrl, useBinaryFormat: true }));
    return new MdkV2Adapter(client, match);
  }

  private containerDeviceId(context?: { deviceId: string }) {
    if (context) this.deviceId ??= toWireDeviceId(context.deviceId);
    return this.deviceId ?? DEFAULT_DEVICE_ID;
  }

  private removeExpiredHashboardTimeSeriesCacheEntries(nowMs: number) {
    for (const [key, entry] of this.hashboardTimeSeriesCache.entries()) {
      if (entry.expiresAtMs <= nowMs) this.hashboardTimeSeriesCache.delete(key);
    }
  }

  decorate(params: AdapterRequestOptions = {}): AdapterRequestOptions {
    return { ...params, headers: { "X-Frigate-Actor": ACTOR, ...params.headers } };
  }

  async getSystemInfo(params: AdapterRequestOptions = {}) {
    const { data: wire } = await this.client.systemServiceGetSystemInfo(this.decorate(params));
    this.deviceId = wire.info?.id ?? this.deviceId;
    return toCanonicalSystemInfo(wire);
  }

  async getNetworkInfo(params: AdapterRequestOptions = {}) {
    const response = await this.client.systemServiceGetSystemInfo(this.decorate(params));
    return toCanonicalNetworkInfo(response.data);
  }

  async getSystemStatus(params: AdapterRequestOptions = {}) {
    const response = await this.client.systemServiceGetSystemStatus(this.decorate(params));
    return toCanonicalLifecycle(response.data);
  }

  async getControllerDiagnostics(params: AdapterRequestOptions = {}) {
    const decorated = this.decorate(params);
    // Each read has its own deadline, so one stalled source cannot hide the
    // other's answer or hold up the next poll.
    const [ha, status] = await Promise.allSettled([
      withDeadline(decorated, CONTROLLER_DIAGNOSTICS_TIMEOUT_MS, (options) =>
        this.client.systemServiceGetHaStatus(options).then(({ data }) => data),
      ),
      withDeadline(decorated, CONTROLLER_DIAGNOSTICS_TIMEOUT_MS, (options) =>
        this.client.systemServiceGetSystemStatus(options).then(({ data }) => data),
      ),
    ]);
    // With neither source there is nothing to diagnose; surface the failure.
    if (ha.status === "rejected" && status.status === "rejected") throw status.reason;
    return toCanonicalControllerDiagnostics(ha, status);
  }

  async getSystemTag(params: AdapterRequestOptions = {}) {
    const { data: wire } = await this.client.systemServiceGetSystemInfo(this.decorate(params));
    return wire.info?.hostname ?? wire.info?.id ?? "";
  }

  async setSystemTag() {
    unsupported("System tags");
  }

  async getMiningStatus(params: AdapterRequestOptions = {}) {
    const { data: wire } = await this.client.miningServiceGetMiningStatus(this.decorate(params));
    return toCanonicalOperationalStatus(wire, ct1ContainerId(this.containerDeviceId()));
  }

  async getErrors(params: AdapterRequestOptions = {}) {
    const decorated = this.decorate(params);
    const [map, health] = await Promise.all([
      this.client.hashboardServiceGetContainerMap(decorated),
      this.client.telemetryServiceGetContainerHealth({}, decorated).catch((error) => {
        // A mixed-version CT1 deployment may not expose plant health yet.
        // Preserve the existing board/PSU errors in that case.
        const status = (error as { status?: number })?.status;
        if (status === 404 || status === 405 || status === 501) return { data: {} };
        throw error;
      }),
    ]);
    return toCanonicalErrors(map.data, this.containerDeviceId(), health.data);
  }

  async getPools(params: AdapterRequestOptions = {}) {
    const decorated = this.decorate(params);
    const [pools, diagnostics] = await Promise.all([
      this.client.poolServiceListPools(decorated),
      this.client.poolServiceGetPoolDiagnostics({}, decorated),
    ]);
    return toCanonicalPools(pools.data, diagnostics.data);
  }

  async getPoolRecordedShares(params: AdapterRequestOptions = {}) {
    try {
      const { data } = await this.client.poolServiceGetPoolDiagnostics(
        { includeLastRecordedShares: true },
        this.decorate(params),
      );
      return toCanonicalPoolRecordedShares(data);
    } catch (error) {
      // Older Frigate versions reject the unknown query field at the REST
      // gateway. Keep the supplemental result absent during a rolling upgrade.
      const status = (error as { status?: number })?.status;
      if (status === 400 || status === 404 || status === 405 || status === 501) return {};
      throw error;
    }
  }

  getPoolHistory(request: PoolHistoryRequest, params: AdapterRequestOptions = {}) {
    return queryPoolHistory(this.client, request, this.decorate(params));
  }

  async getPoolSettings(params: AdapterRequestOptions = {}) {
    const { data } = await this.client.poolServiceListPools(this.decorate(params));
    return (data.pools ?? [])
      .map((pool) => {
        if (!pool.id) throw new Error("Pool identity was not reported");
        return {
          id: pool.id,
          name: pool.name ?? "",
          url: pool.url ?? "",
          username: pool.username ?? "",
          password: "",
          priority: pool.priority ?? 0,
          enabled: pool.enabled ?? false,
        };
      })
      .sort((a, b) => a.priority - b.priority);
  }

  async savePoolSettings(pool: PoolInfo, id?: string, params: AdapterRequestOptions = {}) {
    const { name, url, username, password, priority } = pool;
    if (id) {
      await this.client.poolServiceUpdatePool(
        { poolId: id },
        { pool: { name, url, username }, password: password || undefined, updateMask: "name,url,username" },
        this.decorate(params),
      );
    } else {
      await this.client.poolServiceCreatePool(
        { pool: { name, url, username, priority, enabled: true }, password: password || undefined },
        this.decorate(params),
      );
    }
  }

  async deletePoolSettings(id: string, params: AdapterRequestOptions = {}) {
    await this.client.poolServiceDeletePool({ id }, this.decorate(params));
  }

  async reorderPoolSettings(ids: string[], params: AdapterRequestOptions = {}) {
    const pools = await this.getPoolSettings(params);
    if (
      ids.length !== pools.length ||
      new Set(ids).size !== ids.length ||
      ids.some((id) => !pools.some((p) => p.id === id))
    ) {
      throw new Error("The pool configuration changed. Reload before reordering.");
    }
    const changed = ids.filter((id, priority) => pools.find((pool) => pool.id === id)!.priority !== priority);
    // CT1 rejects duplicate priorities on each write. Park changed entries in
    // unused slots before assigning the final order; preserve credentials and
    // weighted/enabled settings through priority-only field masks.
    const temporaryStart = Math.max(ids.length, ...pools.map((pool) => pool.priority + 1));
    if (temporaryStart + changed.length > 2147483647) throw new Error("Pool priorities exceed the supported range");
    const setPriority = (id: string, priority: number) =>
      this.client.poolServiceUpdatePool(
        { poolId: id },
        { pool: { priority }, updateMask: "priority" },
        this.decorate(params),
      );
    for (const [index, id] of changed.entries()) await setPriority(id, temporaryStart + index);
    for (const id of changed) await setPriority(id, ids.indexOf(id));
  }

  async getContainerSettings(params: AdapterRequestOptions = {}): Promise<ContainerSettings> {
    const { data } = await this.client.powerServiceGetContainerControls(this.decorate(params));
    return {
      supported: data.supported ?? false,
      controls: (data.controls ?? []).flatMap((control) => {
        if (!control.name) return [];
        return [
          {
            name: control.name,
            description: control.description,
            enabled: control.stateKnown ? (control.enabled ?? false) : undefined,
          },
        ];
      }),
      setpoints: (data.setpoints ?? []).flatMap((setting) => {
        if (!setting.name) return [];
        return [
          {
            name: setting.name,
            description: setting.description,
            value: setting.value,
            unit: setting.unit,
            min: setting.boundsKnown ? setting.min : undefined,
            max: setting.boundsKnown ? setting.max : undefined,
          },
        ];
      }),
      ...(data.mainCircuitBreakerSupported === true
        ? { mainCircuitBreaker: { closed: data.mainCircuitBreakerClosed } }
        : {}),
    };
  }

  private async containerState(deviceId: string, params: AdapterRequestOptions) {
    const decorated = this.decorate(params);
    const [map, status, telemetry] = await Promise.all([
      this.client.hashboardServiceGetContainerMap(decorated),
      this.client.miningServiceGetMiningStatus(decorated),
      this.client.telemetryServiceGetContainerHealth({}, decorated),
    ]);
    return toCanonicalContainer(map.data, status.data, telemetry.data, deviceId);
  }

  async getMiningTarget(params: AdapterRequestOptions = {}): Promise<MiningTarget> {
    const response = await this.client.miningServiceGetTargetPower(this.decorate(params));
    return toCanonicalMiningTarget(response.data);
  }

  async updateMiningTarget(update: MiningTargetUpdate, params: AdapterRequestOptions = {}) {
    const valueWatts = update.valueWatts;
    if (valueWatts === undefined) throw new Error("A CT1 mining target requires a power value");
    await this.client.miningServiceSetTargetPower(
      {
        watts: valueWatts,
        performanceMode: toWirePerformanceMode(update.performanceMode),
        balanceBays: update.balanceBays,
        hashOnDisconnect: update.hashOnDisconnect,
      },
      this.decorate(params),
    );
    return this.getMiningTarget(params);
  }

  async setScopedPowerTarget(
    scope: MiningScope,
    watts: number,
    pllAlgorithm?: string,
    params: AdapterRequestOptions = {},
  ) {
    await this.client.miningServiceSetPowerTargetScoped(
      { scope, watts, pllAlgorithm: toWirePllAlgorithm(pllAlgorithm) },
      this.decorate(params),
    );
  }

  async clearScopedPowerTarget(scope: MiningScope, params: AdapterRequestOptions = {}) {
    await this.client.miningServiceClearPowerTargetScoped({ scope }, this.decorate(params));
  }

  async runHashboardAction(
    action: HashboardAction,
    identifiers: string[],
    params: AdapterRequestOptions = {},
  ): Promise<HashboardActionResult> {
    if (!identifiers.length || identifiers.some((id) => !id.trim())) throw new Error("No hashboards selected.");
    const scope = { identifiers: [...new Set(identifiers)] };
    const options = this.decorate(params);
    if (action === "identify") {
      const { data } = await this.client.hashboardServiceSetIdentifyLed(
        {
          scope,
          mode: V2HashboardIdentifyLEDMode.HASHBOARD_IDENTIFY_LED_MODE_BLINK,
        },
        options,
      );
      return { accepted: data.boardsUpdated ?? 0, failures: [] };
    }
    const { data } =
      action === "start"
        ? await this.client.miningServiceStartMiningScoped({ scope }, options)
        : action === "stop"
          ? await this.client.miningServiceStopMiningScoped({ scope }, options)
          : await this.client.miningServiceSetIsolation({ scope, isolated: action === "isolate" }, options);
    const results = data.results ?? [];
    return {
      accepted: results.filter((result) => result.applied === true).length,
      failures: results
        .filter((result) => result.applied !== true)
        .map((result) => ({
          identifier: result.macAddress ?? "Unknown board",
          message: result.message || "Command was not applied.",
        })),
    };
  }

  async startScopedMining(scope: MiningScope, params: AdapterRequestOptions = {}) {
    await this.client.miningServiceStartMiningScoped({ scope }, this.decorate(params));
  }

  async stopScopedMining(scope: MiningScope, params: AdapterRequestOptions = {}) {
    await this.client.miningServiceStopMiningScoped({ scope }, this.decorate(params));
  }

  async setModuleFixedVoltage(moduleId: string, millivolts: number, params: AdapterRequestOptions = {}) {
    await this.client.miningServiceSetModuleFixedVoltage(
      { moduleId: toWireModuleId(moduleId) },
      { millivolts },
      this.decorate(params),
    );
  }

  async clearModuleFixedVoltage(moduleId: string, params: AdapterRequestOptions = {}) {
    await this.client.miningServiceClearModuleFixedVoltage(
      { moduleId: toWireModuleId(moduleId) },
      this.decorate(params),
    );
  }

  async setContainerControl(name: string, enabled: boolean, params: AdapterRequestOptions = {}) {
    const { data } = await this.client.powerServiceSetContainerControl({ name }, { enabled }, this.decorate(params));
    this.checkContainerWriteConfirmation(data.confirmation);
  }

  async setContainerSetpoint(name: string, value: number, params: AdapterRequestOptions = {}) {
    if (!Number.isFinite(value) || !Number.isFinite(Math.fround(value))) throw new Error("Enter a finite number");
    const { data } = await this.client.powerServiceSetContainerSetpoint({ name }, { value }, this.decorate(params));
    this.checkContainerWriteConfirmation(data.confirmation);
  }

  async setMainCircuitBreakerState(closed: boolean, params: AdapterRequestOptions = {}) {
    let readback: boolean;
    try {
      const { data } = await this.client.powerServiceSetMainCircuitBreakerState({ closed }, this.decorate(params));
      // A plain proto3 bool: JSON gateways omit it when false.
      readback = data.closed ?? false;
    } catch (error) {
      if (params.signal?.aborted) throw error;
      throw breakerWriteError(error);
    }
    if (readback !== closed) {
      throw new Error(
        `The controller reports the breaker ${readback ? "closed" : "open"}. Check the current state before trying again.`,
      );
    }
  }

  private checkContainerWriteConfirmation(confirmation?: { state?: V2PlantWriteConfirmationState }) {
    if (!confirmation || confirmation.state === V2PlantWriteConfirmationState.PLANT_WRITE_CONFIRMATION_STATE_CONFIRMED)
      return;
    if (confirmation.state === V2PlantWriteConfirmationState.PLANT_WRITE_CONFIRMATION_STATE_MISMATCH) {
      throw new Error(
        "Controller readback did not match the requested change. Check the current setting before trying again.",
      );
    }
    throw new Error(
      "The command was sent, but controller readback is unavailable. Check the current setting before trying again.",
    );
  }

  async startModuleRunIn(moduleId: string, request: ModuleRunInRequest, params: AdapterRequestOptions = {}) {
    const { taskId, duration, boardPowerTargetWatts, ...config } = request;
    if (!duration || !boardPowerTargetWatts || !Number.isFinite(boardPowerTargetWatts) || boardPowerTargetWatts <= 0) {
      throw new Error("A module run-in requires a duration and positive per-hashboard power target");
    }
    const response = await this.client.runInServiceStartModuleRunIn(
      { moduleId: toWireModuleId(moduleId), taskId },
      {
        ...config,
        tuningSweep: {
          dwellDuration: duration,
          powerTarget: {
            startPowerTargetWatts: boardPowerTargetWatts,
            endPowerTargetWatts: boardPowerTargetWatts,
            stepPowerTargetWatts: 1,
          },
        },
      },
      this.decorate(params),
    );
    return {
      taskId: response.data.taskId,
      moduleId: response.data.moduleId,
      status: response.data.status,
      voltageLeaderMac: response.data.voltageLeaderMac,
    };
  }

  async getHardware(
    context: { deviceId: string; profile: "r2" | "ct1" | "unknown" },
    params: AdapterRequestOptions = {},
  ) {
    return this.containerState(this.containerDeviceId(context), params);
  }

  async getHashboards(params: AdapterRequestOptions = {}) {
    const response = await this.client.hashboardServiceGetContainerMap(this.decorate(params));
    return toCanonicalHashboards(response.data);
  }

  async getHashboardStatus(identifiers: string[], params: AdapterRequestOptions = {}): Promise<HashboardStatus[]> {
    const decorated = this.decorate(params);
    return mapWithConcurrency(
      identifiers,
      HASHBOARD_CONCURRENCY,
      async (identifier) =>
        toCanonicalHashboardStatus(
          identifier,
          (await this.client.hashboardServiceGetHashboardAsiCs({ identifier }, decorated)).data,
        ),
      params.signal ?? undefined,
    );
  }

  async login() {
    return unsupported("Password login");
  }

  async refreshAccessToken() {
    return unsupported("Bearer-token refresh");
  }

  async getSystemLogs(
    _query: { lines: number; source?: SystemLogSource },
    _params: AdapterRequestOptions = {},
  ): Promise<SystemLogs | undefined> {
    return undefined;
  }

  async getCurrentTelemetry(levels: TelemetryLevel[] = [], params: AdapterRequestOptions = {}) {
    if (!levels.length || levels.some((level) => level !== "miner")) {
      return (await this.containerState(this.containerDeviceId(), params)).telemetry;
    }
    const decorated = this.decorate(params);
    const [status, telemetry] = await Promise.all([
      this.client.miningServiceGetMiningStatus(decorated),
      this.client.telemetryServiceGetContainerHealth({}, decorated),
    ]);
    return toCanonicalContainerTelemetry(status.data, telemetry.data, this.containerDeviceId());
  }

  async getTimeSeries(request: TimeSeriesQuery, params: AdapterRequestOptions = {}) {
    const aggregation = request.aggregation ?? "mean";
    if (!["mean", "avg", "min", "max"].includes(aggregation)) {
      throw new Error(`Unsupported CT1 time-series aggregation: ${aggregation}`);
    }
    const containerFields = new Set<ContainerSeriesField>();
    const hashboardLevels: { fields: Set<ContainerSeriesField>; indexes: number[] }[] = [];
    for (const level of request.levels) {
      if (level.type === "miner") {
        for (const field of seriesFields(level.fields, level.type)) containerFields.add(field);
      } else if (level.type === "hashboard") {
        if (level.indexes === undefined) {
          throw new Error("CT1 hashboard time-series queries require explicit zero-based indexes");
        }
        hashboardLevels.push({ fields: seriesFields(level.fields, level.type), indexes: level.indexes });
      } else {
        unsupported(`${level.type} time-series telemetry`);
      }
    }

    const startMs = Date.parse(request.startTime);
    if (Number.isNaN(startMs)) throw new Error(`Invalid CT1 time-series start time: ${request.startTime}`);
    const windowMs = parseIsoDurationMs(request.duration);
    if (request.duration && windowMs === undefined) {
      throw new Error(`Invalid CT1 time-series duration: ${request.duration}`);
    }
    const end = request.endTime ?? (windowMs === undefined ? undefined : new Date(startMs + windowMs).toISOString());
    const bucket = bucketConfig(request.interval);
    const decorated = this.decorate(params);
    const bucketedBoundary = (value: string | undefined) => {
      if (!value) return undefined;
      const valueMs = Date.parse(value);
      return Number.isNaN(valueMs)
        ? value
        : new Date(Math.floor(valueMs / bucket.intervalMs) * bucket.intervalMs).toISOString();
    };
    const hashboardCacheKey = (query: TelemetryServiceQueryTimeSeriesParams) => {
      if (!query.identifier) return undefined;

      const slidingWindow = request.duration !== undefined && request.endTime === undefined;
      return JSON.stringify({
        deviceId: this.containerDeviceId(),
        identifier: query.identifier,
        metric: query.metric,
        bucketSize: query.bucketSize,
        aggregation,
        duration: slidingWindow ? request.duration : undefined,
        startTime: slidingWindow ? bucketedBoundary(query["range.start"]) : query["range.start"],
        endTime: slidingWindow ? bucketedBoundary(query["range.end"]) : query["range.end"],
      });
    };
    const queryTimeSeries = async (query: TelemetryServiceQueryTimeSeriesParams) => {
      const now = Date.now();
      this.removeExpiredHashboardTimeSeriesCacheEntries(now);

      const cacheKey = hashboardCacheKey(query);
      const cached = cacheKey ? this.hashboardTimeSeriesCache.get(cacheKey) : undefined;
      if (cached && cached.expiresAtMs > now) return cached.data;

      const response = await this.client.telemetryServiceQueryTimeSeries(query, decorated);
      if (cacheKey) {
        this.hashboardTimeSeriesCache.set(cacheKey, {
          expiresAtMs: now + HASHBOARD_TIMESERIES_CACHE_MS,
          data: response.data,
        });
      }
      return response.data;
    };
    const queryFields = async (fields: Set<ContainerSeriesField>, identifier?: string) =>
      Object.fromEntries(
        await Promise.all(
          [...fields].map(async (field) => {
            const query = {
              metric: SERIES_METRICS[field],
              "range.start": request.startTime,
              "range.end": end,
              bucketSize: bucket.size,
              identifier,
            };
            return [field, await queryTimeSeries(query)] as const;
          }),
        ),
      );
    const containerResponses = await queryFields(containerFields);
    let hashboardResponses: {
      hashboardId: ReturnType<typeof getHashboardId>;
      responses: Awaited<ReturnType<typeof queryFields>>;
    }[] = [];
    if (hashboardLevels.length) {
      const map = await this.client.hashboardServiceGetContainerMap(decorated);
      const selected = toCanonicalHashboards(map.data).flatMap((board, index) => {
        const fields = new Set<ContainerSeriesField>();
        for (const level of hashboardLevels) {
          if (level.indexes.includes(index)) {
            for (const field of level.fields) fields.add(field);
          }
        }
        return fields.size ? [{ identifier: board.serial, hashboardId: getHashboardId(board.serial), fields }] : [];
      });
      hashboardResponses = await mapWithConcurrency(
        selected,
        HASHBOARD_CONCURRENCY,
        async (board) => ({
          hashboardId: board.hashboardId,
          responses: await queryFields(board.fields, board.identifier),
        }),
        params.signal ?? undefined,
      );
    }
    return toCanonicalContainerTimeSeries(
      containerResponses,
      this.containerDeviceId(),
      bucket.intervalMs,
      aggregation as "mean" | "avg" | "min" | "max",
      hashboardResponses,
    );
  }

  getContainerHistory(request: ContainerHistoryRequest, params: AdapterRequestOptions = {}) {
    return queryContainerHistory(this.client, request, this.decorate(params));
  }

  async getCoolingStatus(params: AdapterRequestOptions = {}) {
    const response = await this.client.telemetryServiceGetContainerHealth({}, this.decorate(params));
    return { mode: "unknown" as CoolingMode, fans: toCanonicalContainerFans(response.data, this.containerDeviceId()) };
  }

  async setCoolingMode() {
    unsupported("Cooling control");
  }

  async startMining(params: AdapterRequestOptions = {}) {
    await this.client.miningServiceStartMining(this.decorate(params));
  }

  async stopMining(params: AdapterRequestOptions = {}) {
    await this.client.miningServiceStopMining(this.decorate(params));
  }

  async rebootSystem() {
    unsupported("System reboot");
  }

  async startFirmwareUpdate() {
    unsupported("Firmware updates");
  }

  async checkForFirmwareUpdate() {
    // The boot shell checks on every supported miner. CT1 has no equivalent
    // endpoint, so this capability is an intentional no-op.
  }

  async locateSystem() {
    unsupported("Locate");
  }

  async createPools(_pools: PoolInfo[]) {
    unsupported("Pool editing");
  }

  async editPool(_poolId: number, _pool: PoolInfo) {
    unsupported("Pool editing");
  }

  async testPoolConnection(_pool: PoolInfo) {
    unsupported("Pool connection tests");
  }

  async setPassword() {
    unsupported("Password management");
  }

  async changePassword() {
    unsupported("Password management");
  }
}
