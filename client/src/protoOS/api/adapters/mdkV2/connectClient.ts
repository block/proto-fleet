import {
  type DescEnum,
  type DescMessage,
  type DescMethodServerStreaming,
  type DescMethodUnary,
  fromJson,
  type JsonObject,
  type JsonValue,
  toJson,
} from "@bufbuild/protobuf";
import { Code, ConnectError, type Transport } from "@connectrpc/connect";
import { createAsyncIterable } from "@connectrpc/connect/protocol";

import type { AdapterRequestOptions, MiningScope } from "../types";
import type { FeedMappingClient } from "./feedMapping";
import type {
  FirmwareServiceWriteFirmwareArtifactUploadChunkBody,
  HashboardServiceGetHashboardEventsParams,
  HashboardServiceListRecoveryEvidenceParams,
  MiningGroupServiceDeleteMiningGroupParams,
  MiningGroupServiceReplaceMiningGroupPoolsBody,
  MiningGroupServiceUpdateMiningGroupBody,
  MiningGroupServiceUpdateMiningGroupIntentBody,
  PoolServiceGetPoolDiagnosticsParams,
  PoolServiceUpdatePoolBody,
  SystemServiceGetLogsParams,
  SystemServiceListAuditEventsParams,
  TelemetryServiceGetContainerHealthParams,
  TelemetryServiceQueryShareQualityHistoryParams,
  TelemetryServiceQueryTimeSeriesParams,
  V2AbortFirmwareArtifactUploadResponse,
  V2ActivateMiningGroupsRequest,
  V2ActivateMiningGroupsResponse,
  V2BeginFirmwareArtifactUploadRequest,
  V2BeginFirmwareArtifactUploadResponse,
  V2CancelFirmwareUpdateResponse,
  V2ClearModuleFixedVoltageResponse,
  V2ClearPowerTargetScopedRequest,
  V2ClearPowerTargetScopedResponse,
  V2CommitFirmwareArtifactUploadResponse,
  V2CreateMiningGroupRequest,
  V2CreateMiningGroupResponse,
  V2CreatePoolRequest,
  V2CreatePoolResponse,
  V2DeleteMiningGroupResponse,
  V2DeletePoolResponse,
  V2GetActivePowerRecoveryCycleResponse,
  V2GetContainerControlsResponse,
  V2GetContainerHealthResponse,
  V2GetContainerMapResponse,
  V2GetFirmwareUpdateResponse,
  V2GetHashboardASICsResponse,
  V2GetHashboardEventsResponse,
  V2GetHAStatusResponse,
  V2GetLogsResponse,
  V2GetMiningGroupCapabilityResponse,
  V2GetMiningGroupOperationResponse,
  V2GetMiningGroupPoolDiagnosticsResponse,
  V2GetMiningGroupResponse,
  V2GetMiningStatusResponse,
  V2GetPoolDiagnosticsResponse,
  V2GetRecoveryEvidenceResponse,
  V2GetSystemInfoResponse,
  V2GetSystemStatusResponse,
  V2GetTargetPowerResponse,
  V2HashboardRunInConfig,
  V2ListAuditEventsResponse,
  V2ListFirmwareUpdatesResponse,
  V2ListMiningGroupMembershipsResponse,
  V2ListMiningGroupPoolsResponse,
  V2ListMiningGroupsResponse,
  V2ListModuleVoltageSettingsResponse,
  V2ListPoolsResponse,
  V2ListPowerFeedsResponse,
  V2ListRecoveryEvidenceResponse,
  V2MuteContainerAlarmRequest,
  V2MuteContainerAlarmResponse,
  V2QueryShareQualityHistoryResponse,
  V2QueryTimeSeriesResponse,
  V2ReplaceMiningGroupPoolsResponse,
  V2RequestMiningGroupTransferRequest,
  V2RequestMiningGroupTransferResponse,
  V2ResetContainerAlarmRequest,
  V2ResetContainerAlarmResponse,
  V2RunDryCoolerFanReverseRequest,
  V2RunDryCoolerFanReverseResponse,
  V2SetContainerControlResponse,
  V2SetContainerSetpointResponse,
  V2SetIdentifyLEDRequest,
  V2SetIdentifyLEDResponse,
  V2SetIsolationResponse,
  V2SetMainCircuitBreakerStateRequest,
  V2SetMainCircuitBreakerStateResponse,
  V2SetModuleFixedVoltageResponse,
  V2SetPowerFeedStateResponse,
  V2SetPowerTargetScopedRequest,
  V2SetPowerTargetScopedResponse,
  V2SetTargetPowerRequest,
  V2SetTargetPowerResponse,
  V2StartFirmwareUpdateRequest,
  V2StartFirmwareUpdateResponse,
  V2StartMiningScopedResponse,
  V2StartModuleRunInResponse,
  V2StopMiningScopedResponse,
  V2StreamASICHealthResponse,
  V2UpdateMiningGroupIntentResponse,
  V2UpdateMiningGroupResponse,
  V2UpdatePoolResponse,
  V2WriteFirmwareArtifactUploadChunkResponse,
} from "./generated/generatedApi";
import { FeedMappingService } from "./generated/proto/mdk/v2/feed_mapping_pb";
import { FirmwareService } from "./generated/proto/mdk/v2/firmware_pb";
import { HashboardService } from "./generated/proto/mdk/v2/hashboard_pb";
import { MiningGroupService } from "./generated/proto/mdk/v2/mining_group_pb";
import { MiningService } from "./generated/proto/mdk/v2/mining_pb";
import { PoolService } from "./generated/proto/mdk/v2/pool_pb";
import { PowerService } from "./generated/proto/mdk/v2/power_pb";
import { RunInService } from "./generated/proto/mdk/v2/runin_pb";
import { SystemService } from "./generated/proto/mdk/v2/system_pb";
import { TelemetryService } from "./generated/proto/mdk/v2/telemetry_pb";

type HttpResponse<T> = Promise<{ data: T }>;

/**
 * GetLogs sends the controller's retained tail and then follows new entries
 * until the client disconnects, so a bounded read stops on its own.
 * `interruption` is set when the stream failed after it opened, ran past the
 * deadline, or paused before the controller's newest matching entry arrived.
 */
export interface LogTailRead {
  entries: V2GetLogsResponse[];
  /** Why the read stopped before the tail was known to be finished. */
  interruption?: string;
  /** The stream never opened, so an empty tail and a slow controller look the same. */
  noResponse?: true;
}

export interface MdkV2Client {
  /** The Fleet REST proxy does not expose commissioning RPCs. */
  feedMappingClient?(): FeedMappingClient;
  miningGroupServiceGetMiningGroupPoolDiagnostics(
    path: { groupId: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetMiningGroupPoolDiagnosticsResponse>;
  miningGroupServiceUpdateMiningGroup(
    path: { id: string },
    data: MiningGroupServiceUpdateMiningGroupBody,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2UpdateMiningGroupResponse>;
  miningGroupServiceDeleteMiningGroup(
    query: MiningGroupServiceDeleteMiningGroupParams,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2DeleteMiningGroupResponse>;
  miningGroupServiceCreateMiningGroup(
    data: V2CreateMiningGroupRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2CreateMiningGroupResponse>;
  miningGroupServiceActivateMiningGroups(
    data: V2ActivateMiningGroupsRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ActivateMiningGroupsResponse>;
  miningGroupServiceReplaceMiningGroupPools(
    path: { groupId: string },
    data: MiningGroupServiceReplaceMiningGroupPoolsBody,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ReplaceMiningGroupPoolsResponse>;
  miningGroupServiceUpdateMiningGroupIntent(
    path: { id: string },
    data: MiningGroupServiceUpdateMiningGroupIntentBody,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2UpdateMiningGroupIntentResponse>;
  miningGroupServiceRequestMiningGroupTransfer(
    data: V2RequestMiningGroupTransferRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2RequestMiningGroupTransferResponse>;
  miningGroupServiceGetMiningGroupOperation(
    path: { id: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetMiningGroupOperationResponse>;
  miningGroupServiceGetMiningGroupCapability(
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetMiningGroupCapabilityResponse>;
  miningGroupServiceListMiningGroups(params?: AdapterRequestOptions): HttpResponse<V2ListMiningGroupsResponse>;
  miningGroupServiceListMiningGroupMemberships(
    query: { groupId?: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ListMiningGroupMembershipsResponse>;
  miningGroupServiceListMiningGroupPools(
    path: { groupId: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ListMiningGroupPoolsResponse>;
  miningGroupServiceGetMiningGroup(
    path: { id: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetMiningGroupResponse>;
  /** Bounded GetLogs read. Connect only: the REST client cannot end a following stream. */
  systemServiceReadLogTail?(query: SystemServiceGetLogsParams, params?: AdapterRequestOptions): Promise<LogTailRead>;
  systemServiceListAuditEvents?(
    query: SystemServiceListAuditEventsParams,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ListAuditEventsResponse>;
  firmwareServiceBeginFirmwareArtifactUpload(
    data: V2BeginFirmwareArtifactUploadRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2BeginFirmwareArtifactUploadResponse>;
  firmwareServiceWriteFirmwareArtifactUploadChunk(
    path: { uploadId: string },
    data: FirmwareServiceWriteFirmwareArtifactUploadChunkBody,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2WriteFirmwareArtifactUploadChunkResponse>;
  firmwareServiceCommitFirmwareArtifactUpload(
    path: { uploadId: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2CommitFirmwareArtifactUploadResponse>;
  firmwareServiceAbortFirmwareArtifactUpload(
    path: { uploadId: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2AbortFirmwareArtifactUploadResponse>;
  firmwareServiceStartFirmwareUpdate(
    data: V2StartFirmwareUpdateRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2StartFirmwareUpdateResponse>;
  firmwareServiceGetFirmwareUpdate(
    path: { taskId: string; serverId?: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetFirmwareUpdateResponse>;
  firmwareServiceListFirmwareUpdates(
    query?: { localOnly?: boolean },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ListFirmwareUpdatesResponse>;
  firmwareServiceCancelFirmwareUpdate(
    path: { taskId: string; serverId?: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2CancelFirmwareUpdateResponse>;

  systemServiceGetSystemInfo(params?: AdapterRequestOptions): HttpResponse<V2GetSystemInfoResponse>;
  systemServiceGetSystemStatus(params?: AdapterRequestOptions): HttpResponse<V2GetSystemStatusResponse>;
  systemServiceGetHaStatus(params?: AdapterRequestOptions): HttpResponse<V2GetHAStatusResponse>;
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
  miningServiceListModuleVoltageSettings(
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ListModuleVoltageSettingsResponse>;
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
  /** One board's past events within a window the controller defaults and caps. */
  hashboardServiceGetHashboardEvents?(
    query: HashboardServiceGetHashboardEventsParams,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetHashboardEventsResponse>;
  /** Recovery evidence summaries, newest first. CT1 only. */
  hashboardServiceListRecoveryEvidence?(
    query?: HashboardServiceListRecoveryEvidenceParams,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ListRecoveryEvidenceResponse>;
  /** One recovery evidence snapshot with its bounded JSON payload. CT1 only. */
  hashboardServiceGetRecoveryEvidence?(
    path: { evidenceId: string },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetRecoveryEvidenceResponse>;
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
  powerServiceListPowerFeeds(params?: AdapterRequestOptions): HttpResponse<V2ListPowerFeedsResponse>;
  powerServiceSetPowerFeedState(
    path: { powerFeedId: string },
    data: { disabled: boolean },
    params?: AdapterRequestOptions,
  ): HttpResponse<V2SetPowerFeedStateResponse>;
  powerServiceGetActivePowerRecoveryCycle(
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetActivePowerRecoveryCycleResponse>;
  powerServiceSetMainCircuitBreakerState(
    data: V2SetMainCircuitBreakerStateRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2SetMainCircuitBreakerStateResponse>;
  powerServiceMuteContainerAlarm(
    data: V2MuteContainerAlarmRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2MuteContainerAlarmResponse>;
  powerServiceResetContainerAlarm(
    data: V2ResetContainerAlarmRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2ResetContainerAlarmResponse>;
  powerServiceRunDryCoolerFanReverse(
    data: V2RunDryCoolerFanReverseRequest,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2RunDryCoolerFanReverseResponse>;
  runInServiceStartModuleRunIn(
    path: { moduleId: string; taskId?: string },
    data: V2HashboardRunInConfig,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2StartModuleRunInResponse>;
  /** Follows one board's ASIC readings until `params.signal` aborts. Connect only: the REST client cannot follow a stream. */
  telemetryServiceFollowAsicHealth?(
    path: { identifier: string },
    params?: AdapterRequestOptions,
  ): AsyncIterable<V2StreamASICHealthResponse>;
  telemetryServiceGetContainerHealth(
    query?: TelemetryServiceGetContainerHealthParams,
    params?: AdapterRequestOptions,
  ): HttpResponse<V2GetContainerHealthResponse>;
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

/**
 * Rejects with the REST client's shape: an HTTP `status` plus the gateway's `{ code, message }` body on `error`.
 * `message` is the server's text without ConnectError's `[code]` prefix, which callers would otherwise display.
 */
const toGatewayError = (error: unknown) => {
  const connectError = ConnectError.from(error);
  return Object.assign(connectError, {
    message: connectError.rawMessage,
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

/** Yields each message of a server stream in the unary path's JSON shape until the stream ends or aborts. */
async function* serverStream<I extends DescMessage, O extends DescMessage, T>(
  transport: Transport,
  method: DescMethodServerStreaming<I, O>,
  request: unknown,
  params: AdapterRequestOptions = {},
): AsyncGenerator<T> {
  const input = fromJson(method.input, withoutUndefined(request) as JsonValue);
  const signal = params.signal ?? undefined;
  // Closes the stream if the caller stops reading before it ends.
  const done = new AbortController();
  try {
    const response = await transport.stream(
      method,
      signal ? AbortSignal.any([signal, done.signal]) : done.signal,
      undefined,
      params.headers,
      createAsyncIterable([input]),
    );
    for await (const message of response.message) {
      const data = toJson(method.output, message, { alwaysEmitImplicit: true });
      nameUnknownEnums(method.output, data);
      yield data as T;
    }
  } catch (error) {
    if (signal?.aborted) throw signal.reason;
    throw toGatewayError(error);
  } finally {
    done.abort();
  }
}

// The tail arrives as one burst; a pause this long usually means the controller
// has moved on to following new entries. GetLogs has no end-of-tail marker, so
// the read confirms the pause against the controller's newest entry.
export const LOG_TAIL_IDLE_MS = 750;
// Connect servers send stream headers with the first entry, so a stream that
// has not opened by now usually has no matching entries, though a slow
// controller looks the same. The read reports that it cannot tell which.
export const LOG_TAIL_OPEN_MS = 2_000;
// Upper bound on a tail read. A controller still sending at this point may not
// have finished its tail, so the read reports itself as interrupted.
export const LOG_TAIL_DEADLINE_MS = 5_000;

const IDLE = Symbol("idle");

const raceTimeout = async <T>(promise: Promise<T>, ms: number): Promise<T | typeof IDLE> => {
  // The losing promise rejects once the read is aborted; nobody awaits it then.
  promise.catch(() => undefined);
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<typeof IDLE>((resolve) => {
        timer = setTimeout(() => resolve(IDLE), ms);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
};

type LogStreamEnd = "limit" | "closed" | "paused" | "deadline" | "unopened";

/**
 * Reads one GetLogs stream into `entries` until it pauses, ends, fills `tail`
 * or hits the deadline. Entries stay in `entries` if the stream fails.
 */
const readLogStream = async (
  transport: Transport,
  query: SystemServiceGetLogsParams,
  entries: V2GetLogsResponse[],
  signal: AbortSignal,
  headers: AdapterRequestOptions["headers"],
  { openMs, deadline }: { openMs: number; deadline: number },
): Promise<LogStreamEnd> => {
  const method = SystemService.method.getLogs;
  // Ends the stream on return; GetLogs would otherwise follow new entries forever.
  const done = new AbortController();
  const limit = query.tail ?? 0;
  try {
    const input = fromJson(method.input, withoutUndefined(query) as JsonValue);
    const response = await raceTimeout(
      transport.stream(
        method,
        AbortSignal.any([signal, done.signal]),
        undefined,
        headers,
        createAsyncIterable([input]),
      ),
      openMs,
    );
    if (response === IDLE) return "unopened";
    const messages = response.message[Symbol.asyncIterator]();
    while (limit <= 0 || entries.length < limit) {
      const wait = Math.min(LOG_TAIL_IDLE_MS, deadline - Date.now());
      const result = wait > 0 ? await raceTimeout(messages.next(), wait) : IDLE;
      if (result === IDLE) return wait < LOG_TAIL_IDLE_MS ? "deadline" : "paused";
      if (result.done) return "closed";
      const data = toJson(method.output, result.value, { alwaysEmitImplicit: true });
      nameUnknownEnums(method.output, data);
      entries.push(data as V2GetLogsResponse);
    }
    return "limit";
  } finally {
    done.abort();
  }
};

const MISSED_NEWEST = "the controller's newest matching entry was not received";

const sameLogEntry = (a: V2GetLogsResponse | undefined, b: V2GetLogsResponse | undefined) =>
  a?.timestamp === b?.timestamp && a?.level === b?.level && a?.component === b?.component && a?.message === b?.message;

const readLogTail = async (
  transport: Transport,
  query: SystemServiceGetLogsParams,
  params: AdapterRequestOptions = {},
): Promise<LogTailRead> => {
  const outer = params.signal ?? undefined;
  const controller = new AbortController();
  const abort = () => controller.abort(outer?.reason);
  outer?.addEventListener("abort", abort, { once: true });
  const deadline = Date.now() + LOG_TAIL_DEADLINE_MS;
  // Follow-up reads may queue behind the page's other requests, so they get
  // whatever time is left rather than the first read's open timeout.
  const read = (request: SystemServiceGetLogsParams, into: V2GetLogsResponse[], openMs: number) =>
    readLogStream(transport, request, into, controller.signal, params.headers, { openMs, deadline });
  const entries: V2GetLogsResponse[] = [];
  try {
    outer?.throwIfAborted();
    const end = await read(query, entries, LOG_TAIL_OPEN_MS);
    if (end === "unopened") return { entries, noResponse: true };
    if (end === "deadline") {
      return {
        entries,
        interruption: `the controller was still sending after ${LOG_TAIL_DEADLINE_MS / 1000} seconds`,
      };
    }
    if (end !== "paused") return { entries };
    // GetLogs has no end-of-tail marker, and a stall mid-tail looks like its
    // end while the newest entries are still to come. The tail is complete
    // only if it reaches the controller's newest matching entry.
    const latest: V2GetLogsResponse[] = [];
    await read({ ...query, tail: 1 }, latest, deadline - Date.now());
    const [newest] = latest;
    if (sameLogEntry(newest, entries[entries.length - 1])) return { entries };
    if (!newest) return { entries, interruption: "the controller did not confirm its newest entry" };
    // A stall, or entries logged since the first read: read the tail again.
    const again: V2GetLogsResponse[] = [];
    await read(query, again, deadline - Date.now());
    if (again.some((entry) => sameLogEntry(entry, newest))) return { entries: again };
    return { entries: again.length ? again : entries, interruption: MISSED_NEWEST };
  } catch (error) {
    if (outer?.aborted) throw outer.reason;
    // With no entries there is nothing partial to show: the logs are unavailable.
    if (!entries.length) throw toGatewayError(error);
    return { entries, interruption: toGatewayError(error).message };
  } finally {
    outer?.removeEventListener("abort", abort);
    controller.abort();
  }
};

const timeRange = <Q extends { "range.start"?: string; "range.end"?: string }>({
  "range.start": start,
  "range.end": end,
  ...query
}: Q) => ({ ...query, range: { start, end } });

const eventWindow = <Q extends { "window.start"?: string; "window.end"?: string }>({
  "window.start": start,
  "window.end": end,
  ...query
}: Q) => ({ ...query, window: { start, end } });

export const createMdkV2Client = (transport: Transport): MdkV2Client => {
  const system = SystemService.method;
  const mining = MiningService.method;
  const miningGroup = MiningGroupService.method;
  const hashboard = HashboardService.method;
  const pool = PoolService.method;
  const power = PowerService.method;
  const runIn = RunInService.method;
  const telemetry = TelemetryService.method;

  return {
    feedMappingClient: () => ({
      report: (params) => unary(transport, FeedMappingService.method.getFeedMappingReport, {}, params),
      active: (params) => unary(transport, FeedMappingService.method.getActiveFeedMappingRun, {}, params),
      get: (runId, params) => unary(transport, FeedMappingService.method.getFeedMappingRun, { runId }, params),
      plan: (config, params) => unary(transport, FeedMappingService.method.planFeedMappingRun, { config }, params),
      start: (config, params) => unary(transport, FeedMappingService.method.startFeedMappingRun, { config }, params),
      cancel: (runId, params) => unary(transport, FeedMappingService.method.cancelFeedMappingRun, { runId }, params),
      verify: (macAddresses, params) =>
        unary(transport, FeedMappingService.method.requestWiringVerification, { macAddresses }, params),
    }),
    miningGroupServiceGetMiningGroupPoolDiagnostics: (path, params) =>
      unary(transport, miningGroup.getMiningGroupPoolDiagnostics, path, params),
    miningGroupServiceUpdateMiningGroup: (path, data, params) =>
      unary(transport, miningGroup.updateMiningGroup, { ...path, ...data }, params),
    miningGroupServiceDeleteMiningGroup: (query, params) =>
      unary(transport, miningGroup.deleteMiningGroup, query, params),
    miningGroupServiceCreateMiningGroup: (data, params) =>
      unary(transport, miningGroup.createMiningGroup, data, params),
    miningGroupServiceActivateMiningGroups: (data, params) =>
      unary(transport, miningGroup.activateMiningGroups, data, params),
    miningGroupServiceReplaceMiningGroupPools: (path, data, params) =>
      unary(transport, miningGroup.replaceMiningGroupPools, { ...path, ...data }, params),
    miningGroupServiceUpdateMiningGroupIntent: (path, data, params) =>
      unary(transport, miningGroup.updateMiningGroupIntent, { ...path, ...data }, params),
    miningGroupServiceRequestMiningGroupTransfer: (data, params) =>
      unary(transport, miningGroup.requestMiningGroupTransfer, data, params),
    miningGroupServiceGetMiningGroupOperation: (path, params) =>
      unary(transport, miningGroup.getMiningGroupOperation, path, params),
    miningGroupServiceGetMiningGroupCapability: (params) =>
      unary(transport, miningGroup.getMiningGroupCapability, {}, params),
    miningGroupServiceListMiningGroups: (params) => unary(transport, miningGroup.listMiningGroups, {}, params),
    miningGroupServiceListMiningGroupMemberships: (query, params) =>
      unary(transport, miningGroup.listMiningGroupMemberships, query, params),
    miningGroupServiceListMiningGroupPools: (path, params) =>
      unary(transport, miningGroup.listMiningGroupPools, path, params),
    miningGroupServiceGetMiningGroup: (path, params) => unary(transport, miningGroup.getMiningGroup, path, params),
    systemServiceReadLogTail: (query, params) => readLogTail(transport, query, params),
    systemServiceListAuditEvents: (query, params) => unary(transport, system.listAuditEvents, timeRange(query), params),
    firmwareServiceBeginFirmwareArtifactUpload: (data, params) =>
      unary(transport, FirmwareService.method.beginFirmwareArtifactUpload, data, params),
    firmwareServiceWriteFirmwareArtifactUploadChunk: (path, data, params) =>
      unary(transport, FirmwareService.method.writeFirmwareArtifactUploadChunk, { ...data, ...path }, params),
    firmwareServiceCommitFirmwareArtifactUpload: (path, params) =>
      unary(transport, FirmwareService.method.commitFirmwareArtifactUpload, path, params),
    firmwareServiceAbortFirmwareArtifactUpload: (path, params) =>
      unary(transport, FirmwareService.method.abortFirmwareArtifactUpload, path, params),
    firmwareServiceStartFirmwareUpdate: (data, params) =>
      unary(transport, FirmwareService.method.startFirmwareUpdate, data, params),
    firmwareServiceGetFirmwareUpdate: (path, params) =>
      unary(transport, FirmwareService.method.getFirmwareUpdate, path, params),
    firmwareServiceListFirmwareUpdates: (query, params) =>
      unary(transport, FirmwareService.method.listFirmwareUpdates, query ?? {}, params),
    firmwareServiceCancelFirmwareUpdate: (path, params) =>
      unary(transport, FirmwareService.method.cancelFirmwareUpdate, path, params),
    systemServiceGetSystemInfo: (params) => unary(transport, system.getSystemInfo, {}, params),
    systemServiceGetSystemStatus: (params) => unary(transport, system.getSystemStatus, {}, params),
    systemServiceGetHaStatus: (params) => unary(transport, system.getHAStatus, {}, params),
    miningServiceGetMiningStatus: (params) => unary(transport, mining.getMiningStatus, {}, params),
    miningServiceGetTargetPower: (params) => unary(transport, mining.getTargetPower, {}, params),
    miningServiceSetTargetPower: (data, params) => unary(transport, mining.setTargetPower, data, params),
    miningServiceSetPowerTargetScoped: (data, params) => unary(transport, mining.setPowerTargetScoped, data, params),
    miningServiceClearPowerTargetScoped: (data, params) =>
      unary(transport, mining.clearPowerTargetScoped, data, params),
    miningServiceStartMiningScoped: (data, params) => unary(transport, mining.startMiningScoped, data, params),
    miningServiceStopMiningScoped: (data, params) => unary(transport, mining.stopMiningScoped, data, params),
    miningServiceSetIsolation: (data, params) => unary(transport, mining.setIsolation, data, params),
    miningServiceListModuleVoltageSettings: (params) => unary(transport, mining.listModuleVoltageSettings, {}, params),
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
    hashboardServiceGetHashboardEvents: (query, params) =>
      unary(transport, hashboard.getHashboardEvents, eventWindow(query), params),
    hashboardServiceListRecoveryEvidence: (query = {}, params) =>
      unary(transport, hashboard.listRecoveryEvidence, query, params),
    hashboardServiceGetRecoveryEvidence: ({ evidenceId }, params) =>
      unary(transport, hashboard.getRecoveryEvidence, { evidenceId }, params),
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
    powerServiceListPowerFeeds: (params) => unary(transport, power.listPowerFeeds, {}, params),
    powerServiceSetPowerFeedState: ({ powerFeedId }, data, params) =>
      unary(transport, power.setPowerFeedState, { ...data, powerFeedId }, params),
    powerServiceGetActivePowerRecoveryCycle: (params) =>
      unary(transport, power.getActivePowerRecoveryCycle, {}, params),
    powerServiceSetMainCircuitBreakerState: (data, params) =>
      unary(transport, power.setMainCircuitBreakerState, data, params),
    powerServiceMuteContainerAlarm: (data, params) => unary(transport, power.muteContainerAlarm, data, params),
    powerServiceResetContainerAlarm: (data, params) => unary(transport, power.resetContainerAlarm, data, params),
    powerServiceRunDryCoolerFanReverse: (data, params) => unary(transport, power.runDryCoolerFanReverse, data, params),
    runInServiceStartModuleRunIn: ({ moduleId, taskId }, config, params) =>
      unary(transport, runIn.startModuleRunIn, { moduleId, taskId, config }, params),
    telemetryServiceFollowAsicHealth: ({ identifier }, params) =>
      serverStream(transport, telemetry.streamASICHealth, { identifier }, params),
    telemetryServiceGetContainerHealth: (query = {}, params) =>
      unary(transport, telemetry.getContainerHealth, query, params),
    telemetryServiceQueryTimeSeries: (query, params) =>
      unary(transport, telemetry.queryTimeSeries, timeRange(query), params),
    telemetryServiceQueryShareQualityHistory: (query, params) =>
      unary(transport, telemetry.queryShareQualityHistory, timeRange(query), params),
  };
};
