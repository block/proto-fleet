import { describe, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";

import { createMdkV2Client } from "./connectClient";
import { BucketSize, TimeSeriesMetric } from "./generated/proto/mdk/v2/common_pb";
import { HashboardErrorCause, HashboardService } from "./generated/proto/mdk/v2/hashboard_pb";
import { MiningService, PerformanceMode } from "./generated/proto/mdk/v2/mining_pb";
import { PoolService } from "./generated/proto/mdk/v2/pool_pb";
import { PowerService } from "./generated/proto/mdk/v2/power_pb";
import { RunInService } from "./generated/proto/mdk/v2/runin_pb";
import { HistorySource, TelemetryService } from "./generated/proto/mdk/v2/telemetry_pb";
import { MdkV2Adapter } from "./MdkV2Adapter";
import { isAbortError } from "@/shared/utils/abort";

describe("MDK v2 Connect client", () => {
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
          service(HashboardService, {
            getContainerMap: () => ({
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
                              health: { errors: [{ cause: 4242 as HashboardErrorCause }] },
                            },
                          },
                        ],
                      },
                    ],
                  },
                ],
              },
            }),
          });
          service(TelemetryService, { getContainerHealth: () => ({}) });
        }),
      ),
    );

    await expect(adapter.getErrors()).resolves.toContainEqual(
      expect.objectContaining({ code: "HASHBOARD_ERROR_CAUSE_4242", message: "4242", severity: "critical" }),
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
