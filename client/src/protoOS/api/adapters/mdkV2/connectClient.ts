import {
  type DescEnum,
  type DescMessage,
  type DescMethodUnary,
  fromJson,
  type JsonObject,
  type JsonValue,
  toJson,
} from "@bufbuild/protobuf";
import { Code, ConnectError, type Transport } from "@connectrpc/connect";

import type { AdapterRequestOptions, MiningScope } from "../types";
import type {
  PoolServiceGetPoolDiagnosticsParams,
  PoolServiceUpdatePoolBody,
  TelemetryServiceQueryShareQualityHistoryParams,
  TelemetryServiceQueryTimeSeriesParams,
  V2ClearModuleFixedVoltageResponse,
  V2ClearPowerTargetScopedRequest,
  V2ClearPowerTargetScopedResponse,
  V2CreatePoolRequest,
  V2CreatePoolResponse,
  V2DeletePoolResponse,
  V2GetContainerControlsResponse,
  V2GetContainerHealthResponse,
  V2GetContainerMapResponse,
  V2GetHashboardASICsResponse,
  V2GetMiningStatusResponse,
  V2GetPoolDiagnosticsResponse,
  V2GetSystemInfoResponse,
  V2GetSystemStatusResponse,
  V2GetTargetPowerResponse,
  V2HashboardRunInConfig,
  V2ListPoolsResponse,
  V2QueryShareQualityHistoryResponse,
  V2QueryTimeSeriesResponse,
  V2SetContainerControlResponse,
  V2SetContainerSetpointResponse,
  V2SetIdentifyLEDRequest,
  V2SetIdentifyLEDResponse,
  V2SetIsolationResponse,
  V2SetModuleFixedVoltageResponse,
  V2SetPowerTargetScopedRequest,
  V2SetPowerTargetScopedResponse,
  V2SetTargetPowerRequest,
  V2SetTargetPowerResponse,
  V2StartMiningScopedResponse,
  V2StartModuleRunInResponse,
  V2StopMiningScopedResponse,
  V2UpdatePoolResponse,
} from "./generated/generatedApi";
import { HashboardService } from "./generated/proto/mdk/v2/hashboard_pb";
import { MiningService } from "./generated/proto/mdk/v2/mining_pb";
import { PoolService } from "./generated/proto/mdk/v2/pool_pb";
import { PowerService } from "./generated/proto/mdk/v2/power_pb";
import { RunInService } from "./generated/proto/mdk/v2/runin_pb";
import { SystemService } from "./generated/proto/mdk/v2/system_pb";
import { TelemetryService } from "./generated/proto/mdk/v2/telemetry_pb";

type HttpResponse<T> = Promise<{ data: T }>;

export interface MdkV2Client {
  systemServiceGetSystemInfo(params?: AdapterRequestOptions): HttpResponse<V2GetSystemInfoResponse>;
  systemServiceGetSystemStatus(params?: AdapterRequestOptions): HttpResponse<V2GetSystemStatusResponse>;
  miningServiceGetMiningStatus(params?: AdapterRequestOptions): HttpResponse<V2GetMiningStatusResponse>;
  miningServiceGetTargetPower(params?: AdapterRequestOptions): HttpResponse<V2GetTargetPowerResponse>;
  miningServiceSetTargetPower(
    data: V2SetTargetPowerRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2SetTargetPowerResponse>;
  miningServiceSetPowerTargetScoped(
    data: V2SetPowerTargetScopedRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2SetPowerTargetScopedResponse>;
  miningServiceClearPowerTargetScoped(
    data: V2ClearPowerTargetScopedRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ClearPowerTargetScopedResponse>;
  miningServiceStartMiningScoped(
    data: { scope: MiningScope },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2StartMiningScopedResponse>;
  miningServiceStopMiningScoped(
    data: { scope: MiningScope },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2StopMiningScopedResponse>;
  miningServiceSetIsolation(
    data: { scope: MiningScope; isolated: boolean },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2SetIsolationResponse>;
  miningServiceSetModuleFixedVoltage(
    path: { moduleId: string },
    data: { millivolts: number },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2SetModuleFixedVoltageResponse>;
  miningServiceClearModuleFixedVoltage(
    path: { moduleId: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ClearModuleFixedVoltageResponse>;
  miningServiceStartMining(params?: AdapterRequestOptions): HttpResponse<unknown>;
  miningServiceStopMining(params?: AdapterRequestOptions): HttpResponse<unknown>;
  hashboardServiceGetContainerMap(params?: AdapterRequestOptions): HttpResponse<V2GetContainerMapResponse>;
  hashboardServiceSetIdentifyLed(
    data: V2SetIdentifyLEDRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2SetIdentifyLEDResponse>;
  hashboardServiceGetHashboardAsiCs(
    path: { identifier: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetHashboardASICsResponse>;
  poolServiceListPools(params?: AdapterRequestOptions): HttpResponse<V2ListPoolsResponse>;
  poolServiceGetPoolDiagnostics(
    query?: PoolServiceGetPoolDiagnosticsParams,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetPoolDiagnosticsResponse>;
  poolServiceCreatePool(data: V2CreatePoolRequest, params?: AdapterRequestOptions): HttpResponse<V2CreatePoolResponse>;
  poolServiceUpdatePool(
    path: { poolId: string },
    data: PoolServiceUpdatePoolBody,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2UpdatePoolResponse>;
  poolServiceDeletePool(path: { id: string }, params?: AdapterRequestOptions): HttpResponse<V2DeletePoolResponse>;
  powerServiceGetContainerControls(params?: AdapterRequestOptions): HttpResponse<V2GetContainerControlsResponse>;
  powerServiceSetContainerControl(
    path: { name: string },
    data: { enabled: boolean },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2SetContainerControlResponse>;
  powerServiceSetContainerSetpoint(
    path: { name: string },
    data: { value: number },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2SetContainerSetpointResponse>;
  runInServiceStartModuleRunIn(
    path: { moduleId: string; taskId?: string },
    data: V2HashboardRunInConfig,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2StartModuleRunInResponse>;
  telemetryServiceGetContainerHealth(params?: AdapterRequestOptions): HttpResponse<V2GetContainerHealthResponse>;
  telemetryServiceQueryTimeSeries(
    query: TelemetryServiceQueryTimeSeriesParams,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2QueryTimeSeriesResponse>;
  telemetryServiceQueryShareQualityHistory(
    query: TelemetryServiceQueryShareQualityHistoryParams,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2QueryShareQualityHistoryResponse>;
}

// Same mapping as grpc-gateway's HTTPStatusFromCode, which callers were written against.
const httpStatusByCode: Record<Code, number> = {
  [Code.Canceled]: 499,
  [Code.Unknown]: 500,
  [Code.InvalidArgument]: 400,
  [Code.DeadlineExceeded]: 504,
  [Code.NotFound]: 404,
  [Code.AlreadyExists]: 409,
  [Code.PermissionDenied]: 403,
  [Code.ResourceExhausted]: 429,
  [Code.FailedPrecondition]: 400,
  [Code.Aborted]: 409,
  [Code.OutOfRange]: 400,
  [Code.Unimplemented]: 501,
  [Code.Internal]: 500,
  [Code.Unavailable]: 503,
  [Code.DataLoss]: 500,
  [Code.Unauthenticated]: 401,
};

/** Rejects with the REST client's shape: an HTTP `status` plus the gateway's `{ code, message }` body on `error`. */
const toGatewayError = (error: unknown) => {
  const connectError = ConnectError.from(error);
  return Object.assign(connectError, {
    status: httpStatusByCode[connectError.code],
    error: { code: connectError.code, message: connectError.rawMessage },
  });
};

const withoutUndefined = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(withoutUndefined).filter((item) => item !== undefined);
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value)
        .map(([key, item]) => [key, withoutUndefined(item)] as const)
        .filter(([, item]) => item !== undefined),
    );
  }
  return value;
};

const isJsonObject = (value: JsonValue): value is JsonObject =>
  typeof value === "object" && value !== null && !Array.isArray(value);

// Binary responses keep enum numbers the bundled schema doesn't define, and toJson emits those as
// numbers. The REST gateway always sent names, so callers get `<SHARED_PREFIX><number>` instead.
const enumName = (desc: DescEnum, value: JsonValue) => {
  if (typeof value !== "number") return value;
  const prefix = desc.sharedPrefix ? desc.values[0].name.slice(0, desc.sharedPrefix.length) : "";
  return `${prefix}${value}`;
};

const nameUnknownEnums = (desc: DescMessage, json: JsonValue): void => {
  // Well-known types have their own JSON forms (Timestamp strings, Struct objects) and no enums to name.
  if (desc.typeName.startsWith("google.protobuf.") || !isJsonObject(json)) return;
  for (const field of desc.fields) {
    const value = json[field.jsonName];
    if (value === undefined || (!field.enum && !field.message)) continue;
    const nameValue = (item: JsonValue) => {
      if (field.enum) return enumName(field.enum, item);
      if (field.message) nameUnknownEnums(field.message, item);
      return item;
    };
    if (field.fieldKind === "list" && Array.isArray(value)) json[field.jsonName] = value.map(nameValue);
    else if (field.fieldKind === "map" && isJsonObject(value))
      json[field.jsonName] = Object.fromEntries(Object.entries(value).map(([key, item]) => [key, nameValue(item)]));
    else json[field.jsonName] = nameValue(value);
  }
};

const unary = async <I extends DescMessage, O extends DescMessage, T>(
  transport: Transport,
  method: DescMethodUnary<I, O>,
  request: unknown,
  params: AdapterRequestOptions = {},
): HttpResponse<T> => {
  // Unknown fields throw so a request shaped for the REST path cannot silently lose data.
  const input = fromJson(method.input, withoutUndefined(request) as JsonValue);
  const signal = params.signal ?? undefined;
  try {
    const response = await transport.unary(method, signal, undefined, params.headers, input);
    const data = toJson(method.output, response.message, { alwaysEmitImplicit: true });
    nameUnknownEnums(method.output, data);
    return { data: data as T };
  } catch (error) {
    // fetch rejects with the abort reason; callers use that to ignore cancellations.
    if (signal?.aborted) throw signal.reason;
    throw toGatewayError(error);
  }
};

const timeRange = <Q extends { "range.start"?: string; "range.end"?: string }>({
  "range.start": start,
  "range.end": end,
  ...query
}: Q) => ({ ...query, range: { start, end } });

export const createMdkV2Client = (transport: Transport): MdkV2Client => {
  const system = SystemService.method;
  const mining = MiningService.method;
  const hashboard = HashboardService.method;
  const pool = PoolService.method;
  const power = PowerService.method;
  const runIn = RunInService.method;
  const telemetry = TelemetryService.method;

  return {
    systemServiceGetSystemInfo: (params) => unary(transport, system.getSystemInfo, {}, params),
    systemServiceGetSystemStatus: (params) => unary(transport, system.getSystemStatus, {}, params),
    miningServiceGetMiningStatus: (params) => unary(transport, mining.getMiningStatus, {}, params),
    miningServiceGetTargetPower: (params) => unary(transport, mining.getTargetPower, {}, params),
    miningServiceSetTargetPower: (data, params) => unary(transport, mining.setTargetPower, data, params),
    miningServiceSetPowerTargetScoped: (data, params) => unary(transport, mining.setPowerTargetScoped, data, params),
    miningServiceClearPowerTargetScoped: (data, params) =>
      unary(transport, mining.clearPowerTargetScoped, data, params),
    miningServiceStartMiningScoped: (data, params) => unary(transport, mining.startMiningScoped, data, params),
    miningServiceStopMiningScoped: (data, params) => unary(transport, mining.stopMiningScoped, data, params),
    miningServiceSetIsolation: (data, params) => unary(transport, mining.setIsolation, data, params),
    miningServiceSetModuleFixedVoltage: ({ moduleId }, data, params) =>
      unary(transport, mining.setModuleFixedVoltage, { ...data, moduleId }, params),
    miningServiceClearModuleFixedVoltage: ({ moduleId }, params) =>
      unary(transport, mining.clearModuleFixedVoltage, { moduleId }, params),
    miningServiceStartMining: (params) => unary(transport, mining.startMining, {}, params),
    miningServiceStopMining: (params) => unary(transport, mining.stopMining, {}, params),
    hashboardServiceGetContainerMap: (params) => unary(transport, hashboard.getContainerMap, {}, params),
    hashboardServiceSetIdentifyLed: (data, params) => unary(transport, hashboard.setIdentifyLED, data, params),
    hashboardServiceGetHashboardAsiCs: ({ identifier }, params) =>
      unary(transport, hashboard.getHashboardASICs, { identifier }, params),
    poolServiceListPools: (params) => unary(transport, pool.listPools, {}, params),
    poolServiceGetPoolDiagnostics: (query = {}, params) => unary(transport, pool.getPoolDiagnostics, query, params),
    poolServiceCreatePool: (data, params) => unary(transport, pool.createPool, data, params),
    poolServiceUpdatePool: ({ poolId }, data, params) =>
      unary(transport, pool.updatePool, { ...data, pool: { ...data.pool, id: poolId } }, params),
    poolServiceDeletePool: ({ id }, params) => unary(transport, pool.deletePool, { id }, params),
    powerServiceGetContainerControls: (params) => unary(transport, power.getContainerControls, {}, params),
    powerServiceSetContainerControl: ({ name }, data, params) =>
      unary(transport, power.setContainerControl, { ...data, name }, params),
    powerServiceSetContainerSetpoint: ({ name }, data, params) =>
      unary(transport, power.setContainerSetpoint, { ...data, name }, params),
    runInServiceStartModuleRunIn: ({ moduleId, taskId }, config, params) =>
      unary(transport, runIn.startModuleRunIn, { moduleId, taskId, config }, params),
    telemetryServiceGetContainerHealth: (params) => unary(transport, telemetry.getContainerHealth, {}, params),
    telemetryServiceQueryTimeSeries: (query, params) =>
      unary(transport, telemetry.queryTimeSeries, timeRange(query), params),
    telemetryServiceQueryShareQualityHistory: (query, params) =>
      unary(transport, telemetry.queryShareQualityHistory, timeRange(query), params),
  };
};
