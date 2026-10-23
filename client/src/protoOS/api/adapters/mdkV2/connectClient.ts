import { type DescMessage, fromJson, type JsonValue, type MessageShape, toJson } from "@bufbuild/protobuf";
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import type { AdapterRequestOptions, MiningScope } from "../types";
import type {
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
  V2StartMiningScopedResponse,
  V2StartModuleRunInResponse,
  V2StopMiningScopedResponse,
  V2UpdatePoolResponse,
} from "./generated/generatedApi";
import {
  GetContainerMapResponseSchema,
  GetHashboardASICsRequestSchema,
  GetHashboardASICsResponseSchema,
  HashboardService,
  SetIdentifyLEDRequestSchema,
  SetIdentifyLEDResponseSchema,
} from "./generated/proto/mdk/v2/hashboard_pb";
import {
  ClearModuleFixedVoltageRequestSchema,
  ClearModuleFixedVoltageResponseSchema,
  ClearPowerTargetScopedRequestSchema,
  ClearPowerTargetScopedResponseSchema,
  GetMiningStatusResponseSchema,
  GetTargetPowerResponseSchema,
  MiningService,
  SetIsolationRequestSchema,
  SetIsolationResponseSchema,
  SetModuleFixedVoltageRequestSchema,
  SetModuleFixedVoltageResponseSchema,
  SetPowerTargetScopedRequestSchema,
  SetPowerTargetScopedResponseSchema,
  SetTargetPowerRequestSchema,
  SetTargetPowerResponseSchema,
  StartMiningResponseSchema,
  StartMiningScopedRequestSchema,
  StartMiningScopedResponseSchema,
  StopMiningResponseSchema,
  StopMiningScopedRequestSchema,
  StopMiningScopedResponseSchema,
} from "./generated/proto/mdk/v2/mining_pb";
import {
  CreatePoolRequestSchema,
  CreatePoolResponseSchema,
  DeletePoolRequestSchema,
  DeletePoolResponseSchema,
  GetPoolDiagnosticsRequestSchema,
  GetPoolDiagnosticsResponseSchema,
  ListPoolsResponseSchema,
  PoolService,
  UpdatePoolRequestSchema,
  UpdatePoolResponseSchema,
} from "./generated/proto/mdk/v2/pool_pb";
import {
  GetContainerControlsResponseSchema,
  PowerService,
  SetContainerControlRequestSchema,
  SetContainerControlResponseSchema,
  SetContainerSetpointRequestSchema,
  SetContainerSetpointResponseSchema,
} from "./generated/proto/mdk/v2/power_pb";
import {
  RunInService,
  StartModuleRunInRequestSchema,
  StartModuleRunInResponseSchema,
} from "./generated/proto/mdk/v2/runin_pb";
import {
  GetSystemInfoResponseSchema,
  GetSystemStatusResponseSchema,
  SystemService,
} from "./generated/proto/mdk/v2/system_pb";
import {
  GetContainerHealthResponseSchema,
  QueryShareQualityHistoryRequestSchema,
  QueryShareQualityHistoryResponseSchema,
  QueryTimeSeriesRequestSchema,
  QueryTimeSeriesResponseSchema,
  TelemetryService,
} from "./generated/proto/mdk/v2/telemetry_pb";

type HttpResponse<T> = Promise<{ data: T }>;

export interface MdkV2Client {
  systemServiceGetSystemInfo(params?: AdapterRequestOptions): HttpResponse<V2GetSystemInfoResponse>;
  systemServiceGetSystemStatus(params?: AdapterRequestOptions): HttpResponse<V2GetSystemStatusResponse>;
  miningServiceGetMiningStatus(params?: AdapterRequestOptions): HttpResponse<V2GetMiningStatusResponse>;
  miningServiceGetTargetPower(params?: AdapterRequestOptions): HttpResponse<V2GetTargetPowerResponse>;
  miningServiceSetTargetPower(data: unknown, params?: AdapterRequestOptions): HttpResponse<unknown>;
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
    query?: TelemetryServiceQueryShareQualityHistoryParams | { includeLastRecordedShares?: boolean },
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

const callOptions = (params: AdapterRequestOptions = {}) => ({
  headers: params.headers,
  signal: params.signal ?? undefined,
});

const statusByCode: Partial<Record<Code, number>> = {
  [Code.InvalidArgument]: 400,
  [Code.Unauthenticated]: 401,
  [Code.PermissionDenied]: 403,
  [Code.NotFound]: 404,
  [Code.ResourceExhausted]: 429,
  [Code.FailedPrecondition]: 412,
  [Code.Aborted]: 409,
  [Code.Unimplemented]: 501,
  [Code.Internal]: 500,
  [Code.Unavailable]: 503,
  [Code.DeadlineExceeded]: 504,
};

const withHttpStatus = (error: unknown) => {
  const connect = ConnectError.from(error);
  const status = statusByCode[connect.code];
  return status === undefined ? connect : Object.assign(connect, { status });
};

const cleanJson = (value: unknown): unknown => {
  if (value === undefined) return undefined;
  if (Array.isArray(value)) return value.map(cleanJson).filter((item) => item !== undefined);
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value)
        .map(([key, item]) => [key, cleanJson(item)] as const)
        .filter(([, item]) => item !== undefined),
    );
  }
  return value;
};

const request = <Desc extends DescMessage>(schema: Desc, value: unknown = {}) =>
  fromJson(schema, cleanJson(value) as JsonValue, { ignoreUnknownFields: true });

const response = <Desc extends DescMessage, T>(schema: Desc, value: MessageShape<Desc>) => ({
  data: toJson(schema, value, { alwaysEmitImplicit: true }) as T,
});

const unary = async <Desc extends DescMessage, T>(schema: Desc, call: () => Promise<MessageShape<Desc>>) => {
  try {
    return response<Desc, T>(schema, await call());
  } catch (error) {
    throw withHttpStatus(error);
  }
};

const timeRangeQuery = (query: { "range.start"?: string; "range.end"?: string }) => ({
  ...query,
  range: { start: query["range.start"], end: query["range.end"] },
  "range.start": undefined,
  "range.end": undefined,
});

export const createMdkV2Client = (baseUrl: string): MdkV2Client => {
  const transport = createConnectTransport({ baseUrl, useBinaryFormat: true });
  const system = createClient(SystemService, transport);
  const mining = createClient(MiningService, transport);
  const hashboard = createClient(HashboardService, transport);
  const pool = createClient(PoolService, transport);
  const power = createClient(PowerService, transport);
  const runIn = createClient(RunInService, transport);
  const telemetry = createClient(TelemetryService, transport);

  return {
    systemServiceGetSystemInfo: (params = {}) =>
      unary(GetSystemInfoResponseSchema, () => system.getSystemInfo({}, callOptions(params))),
    systemServiceGetSystemStatus: (params = {}) =>
      unary(GetSystemStatusResponseSchema, () => system.getSystemStatus({}, callOptions(params))),
    miningServiceGetMiningStatus: (params = {}) =>
      unary(GetMiningStatusResponseSchema, () => mining.getMiningStatus({}, callOptions(params))),
    miningServiceGetTargetPower: (params = {}) =>
      unary(GetTargetPowerResponseSchema, () => mining.getTargetPower({}, callOptions(params))),
    miningServiceSetTargetPower: (data, params = {}) =>
      unary(SetTargetPowerResponseSchema, () =>
        mining.setTargetPower(request(SetTargetPowerRequestSchema, data), callOptions(params)),
      ),
    miningServiceSetPowerTargetScoped: (data, params = {}) =>
      unary(SetPowerTargetScopedResponseSchema, () =>
        mining.setPowerTargetScoped(request(SetPowerTargetScopedRequestSchema, data), callOptions(params)),
      ),
    miningServiceClearPowerTargetScoped: (data, params = {}) =>
      unary(ClearPowerTargetScopedResponseSchema, () =>
        mining.clearPowerTargetScoped(request(ClearPowerTargetScopedRequestSchema, data), callOptions(params)),
      ),
    miningServiceStartMiningScoped: (data, params = {}) =>
      unary(StartMiningScopedResponseSchema, () =>
        mining.startMiningScoped(request(StartMiningScopedRequestSchema, data), callOptions(params)),
      ),
    miningServiceStopMiningScoped: (data, params = {}) =>
      unary(StopMiningScopedResponseSchema, () =>
        mining.stopMiningScoped(request(StopMiningScopedRequestSchema, data), callOptions(params)),
      ),
    miningServiceSetIsolation: (data, params = {}) =>
      unary(SetIsolationResponseSchema, () =>
        mining.setIsolation(request(SetIsolationRequestSchema, data), callOptions(params)),
      ),
    miningServiceSetModuleFixedVoltage: ({ moduleId }, data, params = {}) =>
      unary(SetModuleFixedVoltageResponseSchema, () =>
        mining.setModuleFixedVoltage(
          request(SetModuleFixedVoltageRequestSchema, { ...data, moduleId }),
          callOptions(params),
        ),
      ),
    miningServiceClearModuleFixedVoltage: ({ moduleId }, params = {}) =>
      unary(ClearModuleFixedVoltageResponseSchema, () =>
        mining.clearModuleFixedVoltage(
          request(ClearModuleFixedVoltageRequestSchema, { moduleId }),
          callOptions(params),
        ),
      ),
    miningServiceStartMining: (params = {}) =>
      unary(StartMiningResponseSchema, () => mining.startMining({}, callOptions(params))),
    miningServiceStopMining: (params = {}) =>
      unary(StopMiningResponseSchema, () => mining.stopMining({}, callOptions(params))),
    hashboardServiceGetContainerMap: (params = {}) =>
      unary(GetContainerMapResponseSchema, () => hashboard.getContainerMap({}, callOptions(params))),
    hashboardServiceSetIdentifyLed: (data, params = {}) =>
      unary(SetIdentifyLEDResponseSchema, () =>
        hashboard.setIdentifyLED(request(SetIdentifyLEDRequestSchema, data), callOptions(params)),
      ),
    hashboardServiceGetHashboardAsiCs: ({ identifier }, params = {}) =>
      unary(GetHashboardASICsResponseSchema, () =>
        hashboard.getHashboardASICs(request(GetHashboardASICsRequestSchema, { identifier }), callOptions(params)),
      ),
    poolServiceListPools: (params = {}) =>
      unary(ListPoolsResponseSchema, () => pool.listPools({}, callOptions(params))),
    poolServiceGetPoolDiagnostics: (query = {}, params = {}) =>
      unary(GetPoolDiagnosticsResponseSchema, () =>
        pool.getPoolDiagnostics(request(GetPoolDiagnosticsRequestSchema, query), callOptions(params)),
      ),
    poolServiceCreatePool: (data, params = {}) =>
      unary(CreatePoolResponseSchema, () =>
        pool.createPool(request(CreatePoolRequestSchema, data), callOptions(params)),
      ),
    poolServiceUpdatePool: ({ poolId }, data, params = {}) =>
      unary(UpdatePoolResponseSchema, () =>
        pool.updatePool(
          request(UpdatePoolRequestSchema, {
            ...data,
            pool: { ...data.pool, id: poolId },
          }),
          callOptions(params),
        ),
      ),
    poolServiceDeletePool: ({ id }, params = {}) =>
      unary(DeletePoolResponseSchema, () =>
        pool.deletePool(request(DeletePoolRequestSchema, { id }), callOptions(params)),
      ),
    powerServiceGetContainerControls: (params = {}) =>
      unary(GetContainerControlsResponseSchema, () => power.getContainerControls({}, callOptions(params))),
    powerServiceSetContainerControl: ({ name }, data, params = {}) =>
      unary(SetContainerControlResponseSchema, () =>
        power.setContainerControl(request(SetContainerControlRequestSchema, { ...data, name }), callOptions(params)),
      ),
    powerServiceSetContainerSetpoint: ({ name }, data, params = {}) =>
      unary(SetContainerSetpointResponseSchema, () =>
        power.setContainerSetpoint(request(SetContainerSetpointRequestSchema, { ...data, name }), callOptions(params)),
      ),
    runInServiceStartModuleRunIn: ({ moduleId, taskId }, data, params = {}) =>
      unary(StartModuleRunInResponseSchema, () =>
        runIn.startModuleRunIn(
          request(StartModuleRunInRequestSchema, { ...data, moduleId, taskId }),
          callOptions(params),
        ),
      ),
    telemetryServiceGetContainerHealth: (params = {}) =>
      unary(GetContainerHealthResponseSchema, () => telemetry.getContainerHealth({}, callOptions(params))),
    telemetryServiceQueryTimeSeries: (query, params = {}) =>
      unary(QueryTimeSeriesResponseSchema, () =>
        telemetry.queryTimeSeries(request(QueryTimeSeriesRequestSchema, timeRangeQuery(query)), callOptions(params)),
      ),
    telemetryServiceQueryShareQualityHistory: (query, params = {}) =>
      unary(QueryShareQualityHistoryResponseSchema, () =>
        telemetry.queryShareQualityHistory(
          request(QueryShareQualityHistoryRequestSchema, timeRangeQuery(query)),
          callOptions(params),
        ),
      ),
  };
};
