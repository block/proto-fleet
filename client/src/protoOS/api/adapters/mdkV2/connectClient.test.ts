import { describe, expect, it, vi } from "vitest";
import {
  type DescMethod,
  type DescMethodServerStreaming,
  type DescMethodUnary,
  type JsonValue,
  toJson,
} from "@bufbuild/protobuf";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";

import {
  createMdkV2Client,
  LOG_TAIL_DEADLINE_MS,
  LOG_TAIL_IDLE_MS,
  LOG_TAIL_OPEN_MS,
  type MdkV2Client,
} from "./connectClient";
import { V2HashboardIdentifyLEDMode, V2PerformanceMode } from "./generated/generatedApi";
import { BucketSize, LogLevel, TimeSeriesMetric } from "./generated/proto/mdk/v2/common_pb";
import { FirmwareService } from "./generated/proto/mdk/v2/firmware_pb";
import { HashboardErrorCause, HashboardService } from "./generated/proto/mdk/v2/hashboard_pb";
import { MiningService, PerformanceMode } from "./generated/proto/mdk/v2/mining_pb";
import { PoolService } from "./generated/proto/mdk/v2/pool_pb";
import { PowerService } from "./generated/proto/mdk/v2/power_pb";
import { RunInService } from "./generated/proto/mdk/v2/runin_pb";
import { SystemService } from "./generated/proto/mdk/v2/system_pb";
import { HistorySource, TelemetryService } from "./generated/proto/mdk/v2/telemetry_pb";
import { MdkV2Adapter } from "./MdkV2Adapter";
import { isAbortError } from "@/shared/utils/abort";

const containerMapWithBoardError = (cause: HashboardErrorCause) => ({
  containerMap: {
    tanks: [
      {
        id: 1,
        modules: [
          {
            id: 1,
            slots: [
              {
                hashboard: {
                  macAddress: "02:00:00:00:00:01",
                  location: { hashboardPosition: 1 },
                  health: { errors: [{ cause }] },
                },
              },
            ],
          },
        ],
      },
    ],
  },
});

const isUnary = (method: DescMethod): method is DescMethodUnary => method.methodKind === "unary";
const isServerStreaming = (method: DescMethod): method is DescMethodServerStreaming =>
  method.methodKind === "server_streaming";

const recordingClient = () => {
  const calls: { rpc: string; request: JsonValue }[] = [];
  const transport = createRouterTransport((router) => {
    for (const service of [
      FirmwareService,
      SystemService,
      MiningService,
      HashboardService,
      PoolService,
      PowerService,
      RunInService,
      TelemetryService,
    ]) {
      for (const method of service.methods.filter(isUnary)) {
        router.rpc(method, (request) => {
          calls.push({ rpc: `${service.typeName}/${method.name}`, request: toJson(method.input, request) });
          return {};
        });
      }
      for (const method of service.methods.filter(isServerStreaming)) {
        // eslint-disable-next-line require-yield -- records the request; the stream sends nothing
        router.rpc(method, async function* (request) {
          calls.push({ rpc: `${service.typeName}/${method.name}`, request: toJson(method.input, request) });
        });
      }
    }
  });
  return { calls, client: createMdkV2Client(transport) };
};

const range = { "range.start": "2026-09-16T00:00:00Z", "range.end": "2026-09-16T01:00:00Z" };
const scope = { modules: [{ tank: 1, module: 2 }] };

const rpcTable: [
  method: keyof MdkV2Client,
  rpc: string,
  request: JsonValue,
  call: (client: MdkV2Client) => Promise<unknown>,
][] = [
  [
    "firmwareServiceBeginFirmwareArtifactUpload",
    "mdk.v2.FirmwareService/BeginFirmwareArtifactUpload",
    {},
    (c) => c.firmwareServiceBeginFirmwareArtifactUpload({}),
  ],
  [
    "firmwareServiceWriteFirmwareArtifactUploadChunk",
    "mdk.v2.FirmwareService/WriteFirmwareArtifactUploadChunk",
    { uploadId: "session", imageIndex: 1, offset: "1024", data: "AQID" },
    (c) =>
      c.firmwareServiceWriteFirmwareArtifactUploadChunk(
        { uploadId: "session" },
        { imageIndex: 1, offset: "1024", data: "AQID" },
      ),
  ],
  [
    "firmwareServiceCommitFirmwareArtifactUpload",
    "mdk.v2.FirmwareService/CommitFirmwareArtifactUpload",
    { uploadId: "session" },
    (c) => c.firmwareServiceCommitFirmwareArtifactUpload({ uploadId: "session" }),
  ],
  [
    "firmwareServiceAbortFirmwareArtifactUpload",
    "mdk.v2.FirmwareService/AbortFirmwareArtifactUpload",
    { uploadId: "session" },
    (c) => c.firmwareServiceAbortFirmwareArtifactUpload({ uploadId: "session" }),
  ],
  [
    "firmwareServiceStartFirmwareUpdate",
    "mdk.v2.FirmwareService/StartFirmwareUpdate",
    { taskId: "task", scope: { identifiers: ["mac"] }, options: { force: true, build: "prod" } },
    (c) =>
      c.firmwareServiceStartFirmwareUpdate({
        taskId: "task",
        scope: { identifiers: ["mac"] },
        options: { force: true, build: "prod" },
      }),
  ],
  [
    "firmwareServiceGetFirmwareUpdate",
    "mdk.v2.FirmwareService/GetFirmwareUpdate",
    { taskId: "task", serverId: "owner" },
    (c) => c.firmwareServiceGetFirmwareUpdate({ taskId: "task", serverId: "owner" }),
  ],
  [
    "firmwareServiceListFirmwareUpdates",
    "mdk.v2.FirmwareService/ListFirmwareUpdates",
    { localOnly: true },
    (c) => c.firmwareServiceListFirmwareUpdates({ localOnly: true }),
  ],
  [
    "firmwareServiceCancelFirmwareUpdate",
    "mdk.v2.FirmwareService/CancelFirmwareUpdate",
    { taskId: "task", serverId: "owner" },
    (c) => c.firmwareServiceCancelFirmwareUpdate({ taskId: "task", serverId: "owner" }),
  ],
  ["systemServiceGetSystemInfo", "mdk.v2.SystemService/GetSystemInfo", {}, (c) => c.systemServiceGetSystemInfo()],
  ["systemServiceGetSystemStatus", "mdk.v2.SystemService/GetSystemStatus", {}, (c) => c.systemServiceGetSystemStatus()],
  [
    "systemServiceReadLogTail",
    "mdk.v2.SystemService/GetLogs",
    { level: "LOG_LEVEL_WARNING", component: "pool", tail: 50 },
    (c) => c.systemServiceReadLogTail!({ level: "LOG_LEVEL_WARNING", component: "pool", tail: 50 }),
  ],
  [
    "systemServiceListAuditEvents",
    "mdk.v2.SystemService/ListAuditEvents",
    { range: { start: "2026-09-16T00:00:00Z", end: "2026-09-16T01:00:00Z" }, limit: 100, result: "error" },
    (c) => c.systemServiceListAuditEvents!({ ...range, limit: 100, result: "error" }),
  ],
  ["miningServiceGetMiningStatus", "mdk.v2.MiningService/GetMiningStatus", {}, (c) => c.miningServiceGetMiningStatus()],
  ["miningServiceGetTargetPower", "mdk.v2.MiningService/GetTargetPower", {}, (c) => c.miningServiceGetTargetPower()],
  [
    "miningServiceSetTargetPower",
    "mdk.v2.MiningService/SetTargetPower",
    { watts: 900, performanceMode: "PERFORMANCE_MODE_EFFICIENCY", balanceBays: false },
    (c) =>
      c.miningServiceSetTargetPower({
        watts: 900,
        performanceMode: V2PerformanceMode.PERFORMANCE_MODE_EFFICIENCY,
        balanceBays: false,
      }),
  ],
  [
    "miningServiceSetPowerTargetScoped",
    "mdk.v2.MiningService/SetPowerTargetScoped",
    { scope, watts: 850 },
    (c) => c.miningServiceSetPowerTargetScoped({ scope, watts: 850 }),
  ],
  [
    "miningServiceClearPowerTargetScoped",
    "mdk.v2.MiningService/ClearPowerTargetScoped",
    { scope: { identifiers: ["02:00:00:00:00:01"] } },
    (c) => c.miningServiceClearPowerTargetScoped({ scope: { identifiers: ["02:00:00:00:00:01"] } }),
  ],
  [
    "miningServiceStartMiningScoped",
    "mdk.v2.MiningService/StartMiningScoped",
    { scope },
    (c) => c.miningServiceStartMiningScoped({ scope }),
  ],
  [
    "miningServiceStopMiningScoped",
    "mdk.v2.MiningService/StopMiningScoped",
    { scope },
    (c) => c.miningServiceStopMiningScoped({ scope }),
  ],
  [
    "miningServiceSetIsolation",
    "mdk.v2.MiningService/SetIsolation",
    { scope, isolated: true },
    (c) => c.miningServiceSetIsolation({ scope, isolated: true }),
  ],
  [
    "miningServiceSetModuleFixedVoltage",
    "mdk.v2.MiningService/SetModuleFixedVoltage",
    { moduleId: "T1-M02", millivolts: 28000 },
    (c) => c.miningServiceSetModuleFixedVoltage({ moduleId: "T1-M02" }, { millivolts: 28000 }),
  ],
  [
    "miningServiceClearModuleFixedVoltage",
    "mdk.v2.MiningService/ClearModuleFixedVoltage",
    { moduleId: "T1-M02" },
    (c) => c.miningServiceClearModuleFixedVoltage({ moduleId: "T1-M02" }),
  ],
  ["miningServiceStartMining", "mdk.v2.MiningService/StartMining", {}, (c) => c.miningServiceStartMining()],
  ["miningServiceStopMining", "mdk.v2.MiningService/StopMining", {}, (c) => c.miningServiceStopMining()],
  [
    "hashboardServiceGetContainerMap",
    "mdk.v2.HashboardService/GetContainerMap",
    {},
    (c) => c.hashboardServiceGetContainerMap(),
  ],
  [
    "hashboardServiceSetIdentifyLed",
    "mdk.v2.HashboardService/SetIdentifyLED",
    { scope, mode: "HASHBOARD_IDENTIFY_LED_MODE_BLINK" },
    (c) =>
      c.hashboardServiceSetIdentifyLed({ scope, mode: V2HashboardIdentifyLEDMode.HASHBOARD_IDENTIFY_LED_MODE_BLINK }),
  ],
  [
    "hashboardServiceGetHashboardAsiCs",
    "mdk.v2.HashboardService/GetHashboardASICs",
    { identifier: "02:00:00:00:00:01" },
    (c) => c.hashboardServiceGetHashboardAsiCs({ identifier: "02:00:00:00:00:01" }),
  ],
  ["poolServiceListPools", "mdk.v2.PoolService/ListPools", {}, (c) => c.poolServiceListPools()],
  [
    "poolServiceGetPoolDiagnostics",
    "mdk.v2.PoolService/GetPoolDiagnostics",
    { includeLastRecordedShares: true },
    (c) => c.poolServiceGetPoolDiagnostics({ includeLastRecordedShares: true }),
  ],
  [
    "poolServiceCreatePool",
    "mdk.v2.PoolService/CreatePool",
    { pool: { name: "Backup", url: "stratum+tcp://pool.test:3333", priority: 2 }, password: "secret" },
    (c) =>
      c.poolServiceCreatePool({
        pool: { name: "Backup", url: "stratum+tcp://pool.test:3333", priority: 2 },
        password: "secret",
      }),
  ],
  [
    "poolServiceUpdatePool",
    "mdk.v2.PoolService/UpdatePool",
    { pool: { id: "7", priority: 3 }, updateMask: "priority" },
    (c) => c.poolServiceUpdatePool({ poolId: "7" }, { pool: { priority: 3 }, updateMask: "priority" }),
  ],
  ["poolServiceDeletePool", "mdk.v2.PoolService/DeletePool", { id: "7" }, (c) => c.poolServiceDeletePool({ id: "7" })],
  [
    "powerServiceGetContainerControls",
    "mdk.v2.PowerService/GetContainerControls",
    {},
    (c) => c.powerServiceGetContainerControls(),
  ],
  [
    "powerServiceSetContainerControl",
    "mdk.v2.PowerService/SetContainerControl",
    { name: "dry_cooler_auto", enabled: true },
    (c) => c.powerServiceSetContainerControl({ name: "dry_cooler_auto" }, { enabled: true }),
  ],
  [
    "powerServiceSetContainerSetpoint",
    "mdk.v2.PowerService/SetContainerSetpoint",
    { name: "dry_cooler_auto_temp_c", value: 31.5 },
    (c) => c.powerServiceSetContainerSetpoint({ name: "dry_cooler_auto_temp_c" }, { value: 31.5 }),
  ],
  [
    "runInServiceStartModuleRunIn",
    "mdk.v2.RunInService/StartModuleRunIn",
    { moduleId: "T1-M02", taskId: "0123456789abcdef", config: { stopMiningOnFinish: true } },
    (c) =>
      c.runInServiceStartModuleRunIn({ moduleId: "T1-M02", taskId: "0123456789abcdef" }, { stopMiningOnFinish: true }),
  ],
  [
    "telemetryServiceGetContainerHealth",
    "mdk.v2.TelemetryService/GetContainerHealth",
    { includeModuleDistribution: true },
    (c) => c.telemetryServiceGetContainerHealth({ includeModuleDistribution: true }),
  ],
  [
    "telemetryServiceQueryTimeSeries",
    "mdk.v2.TelemetryService/QueryTimeSeries",
    {
      metric: "TIME_SERIES_METRIC_HASHRATE",
      identifier: "02:00:00:00:00:01",
      bucketSize: "BUCKET_SIZE_5M",
      range: { start: "2026-09-16T00:00:00Z", end: "2026-09-16T01:00:00Z" },
    },
    (c) =>
      c.telemetryServiceQueryTimeSeries({
        metric: "TIME_SERIES_METRIC_HASHRATE",
        identifier: "02:00:00:00:00:01",
        bucketSize: "BUCKET_SIZE_5M",
        ...range,
      }),
  ],
  [
    "telemetryServiceQueryShareQualityHistory",
    "mdk.v2.TelemetryService/QueryShareQualityHistory",
    {
      poolId: 2,
      grouping: "SHARE_QUALITY_HISTORY_GROUPING_TOTAL",
      bucketSize: "BUCKET_SIZE_1H",
      range: { start: "2026-09-16T00:00:00Z", end: "2026-09-16T01:00:00Z" },
    },
    (c) =>
      c.telemetryServiceQueryShareQualityHistory({
        poolId: 2,
        grouping: "SHARE_QUALITY_HISTORY_GROUPING_TOTAL",
        bucketSize: "BUCKET_SIZE_1H",
        ...range,
      }),
  ],
];

describe("MDK v2 Connect client", () => {
  it.each(rpcTable)("%s calls %s", async (_method, rpc, request, call) => {
    const { calls, client } = recordingClient();
    await call(client);
    expect(calls).toEqual([{ rpc, request }]);
  });

  it("has an RPC table row for every client method", () => {
    expect(Object.keys(recordingClient().client).sort()).toEqual(rpcTable.map(([method]) => method).sort());
  });

  it("sends module run-in settings as the request config", async () => {
    let received: unknown;
    const client = createMdkV2Client(
      createRouterTransport(({ service }) =>
        service(RunInService, {
          startModuleRunIn: (request) => {
            received = request;
            return { taskId: request.taskId, moduleId: request.moduleId };
          },
        }),
      ),
    );

    const { data } = await client.runInServiceStartModuleRunIn(
      { moduleId: "T1-M01", taskId: "0123456789abcdef" },
      {
        stopMiningOnFinish: true,
        sampleInterval: "30s",
        tuningSweep: {
          dwellDuration: "3600s",
          powerTarget: { startPowerTargetWatts: 900, endPowerTargetWatts: 900, stepPowerTargetWatts: 1 },
        },
      },
    );

    expect(received).toMatchObject({
      moduleId: "T1-M01",
      taskId: "0123456789abcdef",
      config: {
        stopMiningOnFinish: true,
        sampleInterval: { seconds: 30n },
        tuningSweep: {
          dwellDuration: { seconds: 3600n },
          powerTarget: { startPowerTargetWatts: 900, endPowerTargetWatts: 900, stepPowerTargetWatts: 1 },
        },
      },
    });
    expect(data).toMatchObject({ taskId: "0123456789abcdef", moduleId: "T1-M01" });
  });

  it("merges REST path parameters into the request message", async () => {
    const received: Record<string, unknown> = {};
    const client = createMdkV2Client(
      createRouterTransport(({ service }) => {
        service(PoolService, {
          updatePool: (request) => {
            received.updatePool = request;
            return {};
          },
        });
        service(PowerService, {
          setContainerSetpoint: (request) => {
            received.setContainerSetpoint = request;
            return {};
          },
        });
        service(MiningService, {
          setModuleFixedVoltage: (request) => {
            received.setModuleFixedVoltage = request;
            return {};
          },
        });
      }),
    );

    await client.poolServiceUpdatePool(
      { poolId: "9007199254740993" },
      { pool: { priority: 2 }, password: undefined, updateMask: "priority" },
    );
    await client.powerServiceSetContainerSetpoint({ name: "dry_cooler_auto_temp_c" }, { value: 31.5 });
    await client.miningServiceSetModuleFixedVoltage({ moduleId: "T1-M01" }, { millivolts: 28000 });

    expect(received.updatePool).toMatchObject({
      pool: { id: 9007199254740993n, priority: 2 },
      password: "",
      updateMask: { paths: ["priority"] },
    });
    expect(received.setContainerSetpoint).toMatchObject({ name: "dry_cooler_auto_temp_c", value: 31.5 });
    expect(received.setModuleFixedVoltage).toMatchObject({ moduleId: "T1-M01", millivolts: 28000 });
  });

  it("maps REST time-range query fields and returns REST-shaped JSON", async () => {
    let received: unknown;
    let actor: string | null = null;
    const client = createMdkV2Client(
      createRouterTransport(({ service }) =>
        service(TelemetryService, {
          queryTimeSeries: (request, context) => {
            received = request;
            actor = context.requestHeader.get("X-Frigate-Actor");
            return { metric: request.metric, unit: "TH/s", buckets: [{ count: 9007199254740993n, avg: 0 }] };
          },
        }),
      ),
    );

    const { data } = await client.telemetryServiceQueryTimeSeries(
      {
        metric: "TIME_SERIES_METRIC_HASHRATE",
        bucketSize: "BUCKET_SIZE_5M",
        "range.start": "2026-09-16T00:00:00Z",
        "range.end": "2026-09-16T01:00:00Z",
      },
      { headers: { "X-Frigate-Actor": "proto-os" } },
    );

    expect(received).toMatchObject({
      metric: TimeSeriesMetric.HASHRATE,
      bucketSize: BucketSize.BUCKET_SIZE_5M,
      range: { start: { seconds: 1789516800n }, end: { seconds: 1789520400n } },
    });
    expect(actor).toBe("proto-os");
    expect(data).toMatchObject({
      metric: "TIME_SERIES_METRIC_HASHRATE",
      unit: "TH/s",
      buckets: [{ count: "9007199254740993", avg: 0 }],
    });
  });

  it("rejects request fields the RPC does not define", async () => {
    const client = createMdkV2Client(
      createRouterTransport(({ service }) =>
        service(MiningService, { setTargetPower: () => ({ performanceMode: PerformanceMode.EFFICIENCY }) }),
      ),
    );
    const body = { watts: 900, wattsPerBoard: 1 };

    await expect(client.miningServiceSetTargetPower(body)).rejects.toThrow(/wattsPerBoard/);
  });

  it("names enum values the bundled schema does not define", async () => {
    const client = createMdkV2Client(
      createRouterTransport(({ service }) => {
        service(MiningService, { getTargetPower: () => ({ watts: 900, performanceMode: 4242 as PerformanceMode }) });
        service(TelemetryService, {
          queryTimeSeries: () => ({ unit: "TH/s", buckets: [{ count: 1n, source: 77 as HistorySource }] }),
        });
      }),
    );

    const { data: target } = await client.miningServiceGetTargetPower();
    const { data: series } = await client.telemetryServiceQueryTimeSeries({
      metric: "TIME_SERIES_METRIC_HASHRATE",
      bucketSize: "BUCKET_SIZE_5M",
      "range.start": "2026-09-16T00:00:00Z",
      "range.end": "2026-09-16T01:00:00Z",
    });

    expect(target.performanceMode).toBe("PERFORMANCE_MODE_4242");
    expect(series.buckets?.[0]?.source).toBe("HISTORY_SOURCE_77");
  });

  it("keeps listing hashboard errors whose cause the bundled schema does not define", async () => {
    const adapter = new MdkV2Adapter(
      createMdkV2Client(
        createRouterTransport(({ service }) => {
          service(HashboardService, { getContainerMap: () => containerMapWithBoardError(4242 as HashboardErrorCause) });
          service(TelemetryService, { getContainerHealth: () => ({}) });
        }),
      ),
    );

    await expect(adapter.getErrors()).resolves.toContainEqual(
      expect.objectContaining({ code: "HASHBOARD_ERROR_CAUSE_4242", message: "4242", severity: "critical" }),
    );
  });

  it("keeps listing hashboard errors when the CT1 does not implement plant health", async () => {
    const adapter = new MdkV2Adapter(
      createMdkV2Client(
        createRouterTransport(({ service }) =>
          service(HashboardService, {
            getContainerMap: () => containerMapWithBoardError(HashboardErrorCause.BOARD_OVER_TEMPERATURE),
          }),
        ),
      ),
    );

    await expect(adapter.getErrors()).resolves.toContainEqual(
      expect.objectContaining({ code: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE", severity: "critical" }),
    );
  });

  it.each([
    [Code.InvalidArgument, 400],
    [Code.FailedPrecondition, 400],
    [Code.Unauthenticated, 401],
    [Code.PermissionDenied, 403],
    [Code.NotFound, 404],
    [Code.AlreadyExists, 409],
    [Code.Unimplemented, 501],
    [Code.Unavailable, 503],
  ])("rejects RPC code %s with HTTP %s and the gateway error body", async (code, status) => {
    const client = createMdkV2Client(
      createRouterTransport(({ service }) =>
        service(MiningService, {
          getMiningStatus: () => {
            throw new ConnectError("Rejected by the controller", code);
          },
        }),
      ),
    );

    await expect(client.miningServiceGetMiningStatus()).rejects.toMatchObject({
      status,
      message: "Rejected by the controller",
      error: { code, message: "Rejected by the controller" },
    });
  });

  it("rejects with the abort reason when the caller cancels", async () => {
    const controller = new AbortController();
    const client = createMdkV2Client(
      createRouterTransport(({ service }) =>
        service(MiningService, {
          getMiningStatus: (_request, context) =>
            new Promise((_, reject) => context.signal.addEventListener("abort", () => reject(context.signal.reason))),
        }),
      ),
    );

    const pending = client.miningServiceGetMiningStatus({ signal: controller.signal });
    const reason = new DOMException("The operation was aborted.", "AbortError");
    controller.abort(reason);

    const error = await pending.catch((rejection: unknown) => rejection);
    expect(error).toBe(reason);
    expect(isAbortError(error)).toBe(true);
  });
});

describe("MDK v2 Connect log tail read", () => {
  const entry = (message: string, level = LogLevel.INFO) => ({ level, component: "pool", message });
  const untilAborted = (signal: AbortSignal) =>
    new Promise<void>((resolve) => signal.addEventListener("abort", () => resolve(), { once: true }));
  const logClient = (getLogs: (signal: AbortSignal, tail: number) => AsyncIterable<ReturnType<typeof entry>>) =>
    createMdkV2Client(
      createRouterTransport(({ service }) =>
        service(SystemService, { getLogs: (request, context) => getLogs(context.signal, request.tail) }),
      ),
    );

  it("stops at the requested tail and ends the following stream", async () => {
    let serverSignal: AbortSignal | undefined;
    const client = logClient(async function* (signal) {
      serverSignal = signal;
      yield entry("one", LogLevel.WARNING);
      yield entry("two");
      yield entry("three");
      await untilAborted(signal);
    });

    const read = await client.systemServiceReadLogTail!({ tail: 2 });

    expect(read).toEqual({
      entries: [
        { level: "LOG_LEVEL_WARNING", component: "pool", message: "one" },
        { level: "LOG_LEVEL_INFO", component: "pool", message: "two" },
      ],
    });
    await expect.poll(() => serverSignal?.aborted).toBe(true);
  });

  it("returns the retained tail once the pause is confirmed by the controller's newest entry", async () => {
    const tails: number[] = [];
    const client = logClient(async function* (signal, tail) {
      tails.push(tail);
      if (tail !== 1) yield entry("one");
      yield entry("two");
      await untilAborted(signal);
    });

    const read = await client.systemServiceReadLogTail!({ tail: 10 });

    expect(read.entries.map((wire) => wire.message)).toEqual(["one", "two"]);
    expect(read.interruption).toBeUndefined();
    expect(tails).toEqual([10, 1]);
  });

  it("reads the tail again after a stall mid-tail and returns it once it reaches the newest entry", async () => {
    vi.useFakeTimers();
    try {
      const tails: number[] = [];
      const client = logClient(async function* (signal, tail) {
        tails.push(tail);
        if (tail !== 1) yield entry("older");
        // The first stream stalls longer than the idle gap while the tail is still arriving.
        if (tails.length === 1) await untilAborted(signal);
        yield entry("newer error", LogLevel.ERROR);
        await untilAborted(signal);
      });

      const pending = client.systemServiceReadLogTail!({ tail: 10 });
      await vi.advanceTimersByTimeAsync(2 * LOG_TAIL_IDLE_MS);
      const read = await pending;

      expect(read.entries.map((wire) => wire.message)).toEqual(["older", "newer error"]);
      expect(read.interruption).toBeUndefined();
      expect(tails).toEqual([10, 1, 10]);
    } finally {
      vi.useRealTimers();
    }
  });

  it("marks a read that stalls before the newest entry every time as interrupted rather than complete", async () => {
    vi.useFakeTimers();
    try {
      const client = logClient(async function* (signal, tail) {
        yield tail === 1 ? entry("newer error", LogLevel.ERROR) : entry("older");
        await untilAborted(signal);
      });

      const pending = client.systemServiceReadLogTail!({ tail: 10 });
      await vi.advanceTimersByTimeAsync(2 * LOG_TAIL_IDLE_MS);
      const read = await pending;

      expect(read.entries.map((wire) => wire.message)).toEqual(["older"]);
      expect(read.interruption).toBe("the controller's newest matching entry was not received");
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps the entries received before the stream fails and reports the interruption", async () => {
    const client = logClient(async function* () {
      yield entry("one");
      yield entry("two");
      throw new ConnectError("log stream reset by controller", Code.Unavailable);
    });

    const read = await client.systemServiceReadLogTail!({ tail: 10 });

    expect(read.entries.map((wire) => wire.message)).toEqual(["one", "two"]);
    expect(read.interruption).toBe("log stream reset by controller");
  });

  it("rejects as unavailable when the stream fails before any entry", async () => {
    // eslint-disable-next-line require-yield -- the controller fails before sending anything
    const client = logClient(async function* () {
      throw new ConnectError("process log stream is not configured", Code.Unavailable);
    });

    await expect(client.systemServiceReadLogTail!({ tail: 10 })).rejects.toMatchObject({
      status: 503,
      message: "process log stream is not configured",
    });
  });

  it("reports a stream that never opens as no response rather than an empty tail", async () => {
    vi.useFakeTimers();
    try {
      // eslint-disable-next-line require-yield -- the controller never sends anything
      const client = logClient(async function* (signal) {
        await untilAborted(signal);
      });

      const pending = client.systemServiceReadLogTail!({ tail: 10 });
      await vi.advanceTimersByTimeAsync(LOG_TAIL_OPEN_MS);

      await expect(pending).resolves.toEqual({ entries: [], noResponse: true });
    } finally {
      vi.useRealTimers();
    }
  });

  it("marks a read cut off by the deadline as interrupted", async () => {
    vi.useFakeTimers();
    try {
      const client = logClient(async function* (signal) {
        while (!signal.aborted) {
          yield entry("busy");
          await new Promise((resolve) => setTimeout(resolve, LOG_TAIL_IDLE_MS / 2));
        }
      });

      const pending = client.systemServiceReadLogTail!({ tail: 1000 });
      await vi.advanceTimersByTimeAsync(LOG_TAIL_DEADLINE_MS + LOG_TAIL_IDLE_MS);
      const read = await pending;

      expect(read.entries.length).toBeGreaterThan(1);
      expect(read.interruption).toBe("the controller was still sending after 5 seconds");
    } finally {
      vi.useRealTimers();
    }
  });

  it("rejects with the abort reason when the caller cancels mid-read", async () => {
    const controller = new AbortController();
    const reason = new DOMException("The operation was aborted.", "AbortError");
    const client = logClient(async function* (signal) {
      yield entry("one");
      controller.abort(reason);
      await untilAborted(signal);
    });

    await expect(client.systemServiceReadLogTail!({ tail: 10 }, { signal: controller.signal })).rejects.toBe(reason);
  });
});
