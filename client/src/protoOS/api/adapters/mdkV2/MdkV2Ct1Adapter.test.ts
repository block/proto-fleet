import { describe, expect, test, vi } from "vitest";

import { V2MiningOperatingState, V2PerformanceMode, V2PLLAlgorithm, V2PoolConnState } from "./generated/generatedApi";
import { type MdkV2Client, MdkV2Ct1Adapter } from "./MdkV2Ct1Adapter";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";

const client = (overrides: Partial<MdkV2Client> = {}) =>
  ({
    miningServiceSetPowerTargetScoped: vi.fn(),
    miningServiceClearPowerTargetScoped: vi.fn(),
    miningServiceStartMiningScoped: vi.fn(),
    miningServiceStopMiningScoped: vi.fn(),
    miningServiceSetModuleFixedVoltage: vi.fn(),
    miningServiceClearModuleFixedVoltage: vi.fn(),
    runInServiceStartModuleRunIn: vi.fn(),
    telemetryServiceQueryTimeSeries: vi.fn(),
    ...overrides,
  }) as unknown as MdkV2Client;

describe("MdkV2Ct1Adapter", () => {
  test("attributes requests while allowing an explicit caller actor to win", () => {
    const adapter = new MdkV2Ct1Adapter(client());
    expect(adapter.decorate()).toEqual({ headers: { "X-Frigate-Actor": "proto-os" } });
    expect(adapter.decorate({ headers: { "X-Frigate-Actor": "fleet-proxy", Trace: "abc" } })).toEqual({
      headers: { "X-Frigate-Actor": "fleet-proxy", Trace: "abc" },
    });
  });

  test("uses the canonical context device id without double-prefixing when no probe id was reported", async () => {
    const adapter = new MdkV2Ct1Adapter(
      client({
        hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({ data: { containerMap: { tanks: [] } } }),
        miningServiceGetMiningStatus: vi.fn().mockResolvedValue({ data: {} }),
        telemetryServiceGetContainerHealth: vi.fn().mockResolvedValue({ data: { timestamp: "2026-09-02T12:00:00Z" } }),
      }),
    );

    const { hardware } = await adapter.getHardware({ deviceId: "device:ct1-context", profile: "ct1" });

    if (hardware.topology?.kind !== "container") throw new Error("expected container topology");
    expect(hardware.topology.deviceId).toBe("device:ct1-context");
    expect(hardware.topology.containerId).toBe("container:ct1-context");
  });

  test("forwards scoped power and module-voltage writes with actor attribution", async () => {
    const setScopedPower = vi.fn().mockResolvedValue(undefined);
    const setModuleFixedVoltage = vi.fn().mockResolvedValue(undefined);
    const adapter = new MdkV2Ct1Adapter(
      client({
        miningServiceSetPowerTargetScoped: setScopedPower,
        miningServiceSetModuleFixedVoltage: setModuleFixedVoltage,
      }),
    );

    await adapter.setScopedPowerTarget({ modules: [{ tank: 1, module: 2 }] }, 4_000, V2PLLAlgorithm.PLL_ALGORITHM_NONE);
    await adapter.setModuleFixedVoltage("container:ct1-a:module:T1-M02", 14_000);

    expect(setScopedPower).toHaveBeenCalledWith(
      {
        scope: { modules: [{ tank: 1, module: 2 }] },
        watts: 4_000,
        pllAlgorithm: V2PLLAlgorithm.PLL_ALGORITHM_NONE,
      },
      { headers: { "X-Frigate-Actor": "proto-os" } },
    );
    expect(setModuleFixedVoltage).toHaveBeenCalledWith(
      { moduleId: "T1-M02" },
      { millivolts: 14_000 },
      { headers: { "X-Frigate-Actor": "proto-os" } },
    );
  });

  test("uses the generated miner-wide target-power endpoints", async () => {
    const setTargetPower = vi.fn().mockResolvedValue({ data: {} });
    const getTargetPower = vi.fn().mockResolvedValue({
      data: {
        watts: 11_500,
        defaultWatts: 10_000,
        minimumWatts: 8_000,
        maximumWatts: 13_000,
        performanceMode: V2PerformanceMode.PERFORMANCE_MODE_MAXIMUM_HASHRATE,
        balanceBays: true,
        hashOnDisconnect: false,
      },
    });
    const adapter = new MdkV2Ct1Adapter(
      client({ miningServiceSetTargetPower: setTargetPower, miningServiceGetTargetPower: getTargetPower }),
    );

    const target = await adapter.updateMiningTarget({
      valueWatts: 11_500,
      performanceMode: "maximumHashrate",
      balanceBays: true,
      hashOnDisconnect: false,
    });

    expect(setTargetPower).toHaveBeenCalledWith(
      {
        watts: 11_500,
        performanceMode: V2PerformanceMode.PERFORMANCE_MODE_MAXIMUM_HASHRATE,
        balanceBays: true,
        hashOnDisconnect: false,
      },
      { headers: { "X-Frigate-Actor": "proto-os" } },
    );
    expect(target).toMatchObject({
      valueWatts: 11_500,
      defaultWatts: 10_000,
      boundsWatts: { min: 8_000, max: 13_000 },
      performanceMode: "maximumHashrate",
    });
  });

  test("combines configured pools with their runtime diagnostics", async () => {
    const listPools = vi.fn().mockResolvedValue({
      data: { pools: [{ id: "1", url: "stratum+tcp://primary", priority: 0, enabled: true }] },
    });
    const getPoolDiagnostics = vi.fn().mockResolvedValue({
      data: { pools: [{ poolId: "1", connState: V2PoolConnState.POOL_CONN_STATE_ACTIVE }] },
    });
    const adapter = new MdkV2Ct1Adapter(
      client({ poolServiceListPools: listPools, poolServiceGetPoolDiagnostics: getPoolDiagnostics }),
    );

    await expect(adapter.getPools()).resolves.toEqual([
      expect.objectContaining({ id: "pool:1", url: "stratum+tcp://primary", status: "Active" }),
    ]);
    expect(listPools).toHaveBeenCalledWith({ headers: { "X-Frigate-Actor": "proto-os" } });
    expect(getPoolDiagnostics).toHaveBeenCalledWith({ headers: { "X-Frigate-Actor": "proto-os" } });
  });

  test("does not fetch the container map for miner-only telemetry", async () => {
    const getContainerMap = vi.fn();
    const getMiningStatus = vi.fn().mockResolvedValue({
      data: {
        hashrate: 806.2,
        power: 11_500,
        activeBoards: 288,
        totalBoards: 288,
        mining: true,
        operatingState: V2MiningOperatingState.MINING_OPERATING_STATE_MINING,
      },
    });
    const getContainerHealth = vi.fn().mockResolvedValue({
      data: { timestamp: "2026-09-02T12:00:00Z", plant: { facilityPowerW: 12_000 } },
    });
    const adapter = new MdkV2Ct1Adapter(
      client({
        hashboardServiceGetContainerMap: getContainerMap,
        miningServiceGetMiningStatus: getMiningStatus,
        telemetryServiceGetContainerHealth: getContainerHealth,
      }),
      { deviceId: "ct1-a" },
    );

    const telemetry = await adapter.getCurrentTelemetry(["miner"]);

    expect(getContainerMap).not.toHaveBeenCalled();
    expect(telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)).toMatchObject({
      hashrate: { latest: { value: 806.2, units: "TH/s" } },
      power: { latest: { value: 11_500, units: "W" } },
      miningStatus: "mining",
      boardCount: 288,
      onlineBoardCount: 288,
    });
    expect(telemetry.plant.get("container:ct1-a:plant" as never)).toMatchObject({
      facilityPower: { latest: { value: 12_000, units: "W" } },
    });
  });

  test("starts a module run-in with the generated client and actor attribution", async () => {
    const startModuleRunIn = vi.fn().mockResolvedValue({
      data: { taskId: "task-1", moduleId: "T1-M02", status: "HASHBOARD_RUN_IN_STATUS_QUEUED" },
    });
    const adapter = new MdkV2Ct1Adapter(client({ runInServiceStartModuleRunIn: startModuleRunIn }));

    const result = await adapter.startModuleRunIn("container:ct1-a:module:T1-M02", {
      taskId: "task-1",
      duration: "PT30M",
      sampleInterval: "PT10S",
      boardPowerTargetWatts: 5_000,
      stopMiningOnFinish: true,
    });

    expect(startModuleRunIn).toHaveBeenCalledWith(
      { moduleId: "T1-M02", taskId: "task-1" },
      {
        duration: "PT30M",
        sampleInterval: "PT10S",
        boardPowerTargetWatts: 5_000,
        stopMiningOnFinish: true,
      },
      { headers: { "X-Frigate-Actor": "proto-os" } },
    );
    expect(result).toMatchObject({ taskId: "task-1", moduleId: "T1-M02" });
  });

  test("queries and normalizes aggregate historical telemetry", async () => {
    const queryTimeSeries = vi.fn().mockImplementation(({ metric }) => {
      const value = metric === "TIME_SERIES_METRIC_HASHRATE" ? 100 : metric === "TIME_SERIES_METRIC_POWER" ? 1_500 : 65;
      return Promise.resolve({
        data: {
          metric,
          buckets: [{ time: "2026-09-02T11:00:00Z", avg: value, min: value - 1, max: value + 1, count: "10" }],
        },
      });
    });
    const adapter = new MdkV2Ct1Adapter(client({ telemetryServiceQueryTimeSeries: queryTimeSeries }), {
      deviceId: "ct1-a",
    });

    const telemetry = await adapter.getTimeSeries({
      startTime: "2026-09-02T11:00:00Z",
      duration: "PT1H",
      interval: "PT5M",
      aggregation: "mean",
      levels: [{ type: "miner", fields: ["hashrate", "power", "efficiency", "temperature"] }],
    });

    expect(queryTimeSeries).toHaveBeenCalledTimes(3);
    expect(queryTimeSeries).toHaveBeenCalledWith(
      expect.objectContaining({
        "range.start": "2026-09-02T11:00:00Z",
        "range.end": "2026-09-02T12:00:00.000Z",
        bucketSize: "BUCKET_SIZE_5M",
      }),
      { headers: { "X-Frigate-Actor": "proto-os" } },
    );
    expect(telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)).toMatchObject({
      hashrate: { timeSeries: { points: [{ value: 100 }], bucketSizeMs: 300_000 } },
      power: { timeSeries: { points: [{ value: 1_500 }] } },
      efficiency: { timeSeries: { points: [{ value: 15, timestampMs: Date.parse("2026-09-02T11:00:00Z") }] } },
      temperature: { timeSeries: { points: [{ value: 65 }] } },
    });
  });

  test("uses the closest supported bucket for the shared 48-hour interval", async () => {
    const queryTimeSeries = vi.fn().mockResolvedValue({
      data: { buckets: [{ time: "2026-09-02T11:00:00Z", avg: 100, count: "10" }] },
    });
    const adapter = new MdkV2Ct1Adapter(client({ telemetryServiceQueryTimeSeries: queryTimeSeries }));

    const telemetry = await adapter.getTimeSeries({
      startTime: "2026-08-31T12:00:00Z",
      duration: "PT48H",
      interval: "PT30M",
      levels: [{ type: "miner", fields: ["hashrate"] }],
    });

    expect(queryTimeSeries).toHaveBeenCalledWith(
      expect.objectContaining({ bucketSize: "BUCKET_SIZE_15M" }),
      expect.anything(),
    );
    expect(telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries?.bucketSizeMs).toBe(900_000);
  });

  test("queries selected hashboard histories by v2 identifier", async () => {
    const getContainerMap = vi.fn().mockResolvedValue({
      data: {
        containerMap: {
          tanks: [
            {
              id: 1,
              modules: [
                {
                  id: 1,
                  slots: [
                    { hashboard: { macAddress: "26:B0:3F:F0:D7:91" } },
                    { hashboard: { macAddress: "26:B0:3F:F0:D7:92" } },
                  ],
                },
              ],
            },
          ],
        },
      },
    });
    const queryTimeSeries = vi.fn().mockImplementation(({ metric, identifier }) =>
      Promise.resolve({
        data: {
          metric,
          buckets: [
            {
              time: "2026-09-02T11:00:00Z",
              avg: metric === "TIME_SERIES_METRIC_HASHRATE" ? 2 : 30,
              count: "10",
            },
          ],
          identifier,
        },
      }),
    );
    const adapter = new MdkV2Ct1Adapter(
      client({ hashboardServiceGetContainerMap: getContainerMap, telemetryServiceQueryTimeSeries: queryTimeSeries }),
      { deviceId: "ct1-a" },
    );

    const telemetry = await adapter.getTimeSeries({
      startTime: "2026-09-02T11:00:00Z",
      duration: "PT1H",
      interval: "PT5M",
      levels: [{ type: "hashboard", indexes: [1], fields: ["hashrate", "power", "efficiency"] }],
    });

    expect(queryTimeSeries).toHaveBeenCalledTimes(2);
    expect(queryTimeSeries).toHaveBeenCalledWith(expect.objectContaining({ identifier: "26:B0:3F:F0:D7:92" }), {
      headers: { "X-Frigate-Actor": "proto-os" },
    });
    expect(telemetry.hashboards.has("mac:26:B0:3F:F0:D7:91" as never)).toBe(false);
    expect(telemetry.hashboards.get("mac:26:B0:3F:F0:D7:92" as never)).toMatchObject({
      hashrate: { timeSeries: { points: [{ value: 2 }] } },
      efficiency: { timeSeries: { points: [{ value: 15 }] } },
    });
  });

  test("returns CT1 plant fans from the cooling-status adapter method", async () => {
    const getContainerHealth = vi.fn().mockResolvedValue({
      data: { coolingFans: [{ componentId: "dry-cooler-1", powerW: 400, speedPercent: 75 }] },
    });
    const adapter = new MdkV2Ct1Adapter(client({ telemetryServiceGetContainerHealth: getContainerHealth }), {
      deviceId: "ct1-a",
    });

    await expect(adapter.getCoolingStatus()).resolves.toMatchObject({
      mode: "unknown",
      fans: [
        {
          componentId: "container:ct1-a:fan:dry-cooler-1",
          power: { latest: { value: 400, units: "W" } },
          speedPercent: { latest: { value: 75, units: "%" } },
        },
      ],
    });
    expect(getContainerHealth).toHaveBeenCalledWith({ headers: { "X-Frigate-Actor": "proto-os" } });
  });

  test("rejects unsupported granular histories instead of returning a container aggregate", async () => {
    const adapter = new MdkV2Ct1Adapter(client());

    await expect(
      adapter.getTimeSeries({
        startTime: "2026-09-02T11:00:00Z",
        interval: "PT5M",
        levels: [{ type: "asic", fields: ["hashrate"] }],
      }),
    ).rejects.toThrow("asic time-series telemetry is not supported");
  });

  test("requires explicit hashboard indexes to avoid querying every CT1 board", async () => {
    const queryTimeSeries = vi.fn();
    const adapter = new MdkV2Ct1Adapter(client({ telemetryServiceQueryTimeSeries: queryTimeSeries }));

    await expect(
      adapter.getTimeSeries({
        startTime: "2026-09-02T11:00:00Z",
        interval: "PT5M",
        levels: [{ type: "hashboard", fields: ["hashrate"] }],
      }),
    ).rejects.toThrow("require explicit zero-based indexes");
    expect(queryTimeSeries).not.toHaveBeenCalled();
  });
});
