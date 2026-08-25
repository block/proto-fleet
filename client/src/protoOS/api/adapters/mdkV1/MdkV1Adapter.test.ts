import { describe, expect, test, vi } from "vitest";
import { MdkV1Adapter } from "./MdkV1Adapter";
import { MetricUnit } from "@/protoOS/api/generatedApi";
import { poolInfoToPoolConfig, poolInfoToTestConnection } from "@/protoOS/api/poolAdapters";
import { getAsicId } from "@/protoOS/store";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";

describe("MdkV1Adapter.getTimeSeries", () => {
  test("adds hashboard identity context to ASIC-only history requests", async () => {
    const getTimeSeries = vi.fn().mockResolvedValue({
      data: {
        meta: { start_time: "2026-08-18T00:00:00Z", interval: "PT1M" },
        data: {
          hashboards: [{ index: 0, serial_number: "HB-1", hashrate: { unit: MetricUnit.THS, values: [50] } }],
          asics: [{ hashboard_index: 0, index: 2, temperature: { unit: MetricUnit.ValueC, values: [70] } }],
        },
      },
    });
    const adapter = new MdkV1Adapter({ getTimeSeries } as never);

    const telemetry = await adapter.getTimeSeries({
      startTime: "2026-08-18T00:00:00Z",
      interval: "PT1M",
      levels: [{ type: "asic", fields: ["temperature"] }],
    });

    expect(getTimeSeries).toHaveBeenCalledWith(
      expect.objectContaining({
        levels: [
          { type: "asic", fields: ["temperature"] },
          { type: "hashboard", fields: ["hashrate"] },
        ],
      }),
      {},
    );
    expect(telemetry.asics.get(getAsicId("HB-1", 2))?.temperature?.timeSeries?.points[0]?.value).toBe(70);
  });

  test("uses the requested interval when response metadata omits it", async () => {
    const getTimeSeries = vi.fn().mockResolvedValue({
      data: {
        data: { miner: { hashrate: { unit: MetricUnit.THS, values: [90, 100] } } },
      },
    });
    const adapter = new MdkV1Adapter({ getTimeSeries } as never);

    const telemetry = await adapter.getTimeSeries({
      startTime: "2026-08-18T00:00:00Z",
      interval: "PT5M",
      levels: [{ type: "miner", fields: ["hashrate"] }],
    });

    expect(telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries?.points).toEqual([
      { timestampMs: Date.parse("2026-08-18T00:00:00Z"), value: 90 },
      { timestampMs: Date.parse("2026-08-18T00:05:00Z"), value: 100 },
    ]);
  });

  test("rejects history when no valid interval is available", async () => {
    const getTimeSeries = vi.fn().mockResolvedValue({
      data: {
        meta: { interval: "invalid" },
        data: { miner: { hashrate: { unit: MetricUnit.THS, values: [90, 100] } } },
      },
    });
    const adapter = new MdkV1Adapter({ getTimeSeries } as never);

    await expect(
      adapter.getTimeSeries({
        startTime: "2026-08-18T00:00:00Z",
        levels: [{ type: "miner", fields: ["hashrate"] }],
      }),
    ).rejects.toThrow("valid positive interval");
  });
});

describe("MdkV1Adapter.getCurrentTelemetry", () => {
  test("joins telemetry levels into a single comma-separated value", async () => {
    const getCurrentTelemetry = vi.fn().mockResolvedValue({ data: {} });
    const adapter = new MdkV1Adapter({ getCurrentTelemetry } as never);

    await adapter.getCurrentTelemetry(["miner", "hashboard"]);

    // Must stay comma-joined (a single value): the firmware rejects repeated
    // `level=` params with 400 "duplicate field `level`" (OpenAPI explode:false,
    // contradicting the generated array type). Verified against a real rig.
    expect(getCurrentTelemetry).toHaveBeenCalledWith({ level: "miner,hashboard" }, {});
  });

  test("omits the level param entirely when no levels are requested", async () => {
    const getCurrentTelemetry = vi.fn().mockResolvedValue({ data: {} });
    const adapter = new MdkV1Adapter({ getCurrentTelemetry } as never);

    await adapter.getCurrentTelemetry([]);

    expect(getCurrentTelemetry).toHaveBeenCalledWith({ level: undefined }, {});
  });
});

describe("MdkV1Adapter.getHashboards", () => {
  test("unwraps the hashboards-info envelope into normalized descriptors", async () => {
    const getAllHashboards = vi.fn().mockResolvedValue({
      data: { "hashboards-info": [{ hb_sn: "HB-1", slot: 4, board: "B3a" }] },
    });
    const adapter = new MdkV1Adapter({ getAllHashboards } as never);

    expect(await adapter.getHashboards()).toEqual([{ serial: "HB-1", slot: 4, board: "B3a", bay: 2, asicIds: [] }]);
  });
});

describe("MdkV1Adapter.getHashboardStatus", () => {
  test("fetches each serial and unwraps the hashboard-stats envelope", async () => {
    const getHashboardStatus = vi.fn().mockResolvedValue({
      data: { "hashboard-stats": { asics: [{ index: 0, row: 0, column: 0, voltage_mv: 300 }] } },
    });
    const adapter = new MdkV1Adapter({ getHashboardStatus } as never);

    const [status] = await adapter.getHashboardStatus(["HB-1"]);

    expect(getHashboardStatus).toHaveBeenCalledWith({ hbSn: "HB-1" }, {});
    expect(status).toEqual({
      serial: "HB-1",
      asicIds: [getAsicId("HB-1", 0)],
      asics: [{ id: getAsicId("HB-1", 0), hashboardSerial: "HB-1", index: 0, row: 0, column: 0 }],
      asicTelemetry: [{ asicId: getAsicId("HB-1", 0), voltage: { value: 0.3, units: "V" } }],
    });
  });
});

describe("MdkV1Adapter auth + logs", () => {
  test("login returns a canonical token pair", async () => {
    const login = vi.fn().mockResolvedValue({ data: { access_token: "a", refresh_token: "r" } });
    const adapter = new MdkV1Adapter({ login } as never);

    expect(await adapter.login("pw", { secure: false })).toEqual({ accessToken: "a", refreshToken: "r" });
    expect(login).toHaveBeenCalledWith({ password: "pw" }, { secure: false });
  });

  test("refreshAccessToken returns the renewed access token", async () => {
    const refreshToken = vi.fn().mockResolvedValue({ data: { access_token: "a2" } });
    const adapter = new MdkV1Adapter({ refreshToken } as never);

    expect(await adapter.refreshAccessToken("r", { secure: false })).toEqual({ accessToken: "a2" });
    expect(refreshToken).toHaveBeenCalledWith({ refresh_token: "r" }, { secure: false });
  });

  test("getSystemLogs unwraps the logs envelope", async () => {
    const getSystemLogs = vi.fn().mockResolvedValue({ data: { logs: { content: ["line"], lines: 1 } } });
    const adapter = new MdkV1Adapter({ getSystemLogs } as never);

    expect(await adapter.getSystemLogs({ lines: 1, source: "miner_sw" })).toEqual({ content: ["line"], lines: 1 });
    expect(getSystemLogs).toHaveBeenCalledWith({ lines: 1, source: "miner_sw" }, {});
  });

  test("getSystemLogs returns undefined when the logs envelope is missing", async () => {
    const getSystemLogs = vi.fn().mockResolvedValue({ data: {} });
    const adapter = new MdkV1Adapter({ getSystemLogs } as never);

    expect(await adapter.getSystemLogs({ lines: 1 })).toBeUndefined();
  });
});

describe("MdkV1Adapter commands", () => {
  test("forwards fire-and-forget commands with the auth params", async () => {
    const startMining = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ startMining } as never);

    await adapter.startMining({ secure: true });

    expect(startMining).toHaveBeenCalledWith({ secure: true });
  });

  test("stopMining forwards the auth params to the client", async () => {
    const stopMining = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ stopMining } as never);

    await adapter.stopMining({ secure: true });

    expect(stopMining).toHaveBeenCalledWith({ secure: true });
  });

  test("rebootSystem forwards the auth params to the client", async () => {
    const rebootSystem = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ rebootSystem } as never);

    await adapter.rebootSystem({ secure: true });

    expect(rebootSystem).toHaveBeenCalledWith({ secure: true });
  });

  test("changePassword forwards the v1 request body and auth params", async () => {
    const changePassword = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ changePassword } as never);

    const request = { current_password: "current123", new_password: "new456789" };
    await adapter.changePassword(request, { secure: false });

    expect(changePassword).toHaveBeenCalledWith(request, { secure: false });
  });

  test("locateSystem maps canonical options to the v1 query, omitting undefined keys", async () => {
    const locateSystem = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ locateSystem } as never);

    await adapter.locateSystem({ ledOnTime: 60 });
    expect(locateSystem).toHaveBeenCalledWith({ led_on_time: 60 }, {});

    await adapter.locateSystem({ enable: false });
    expect(locateSystem).toHaveBeenCalledWith({ enable: false }, {});
  });

  const samplePool = {
    name: "Primary",
    url: "stratum+tcp://pool",
    username: "worker",
    password: "secret",
    priority: 0,
  };

  test("createPools maps each pool through poolInfoToPoolConfig", async () => {
    const createPools = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ createPools } as never);

    await adapter.createPools([samplePool]);

    expect(createPools).toHaveBeenCalledWith([poolInfoToPoolConfig(samplePool)], {});
  });

  test("editPool nests the pool id into the v1 path param and maps the body", async () => {
    const editPool = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ editPool } as never);

    await adapter.editPool(3, samplePool);

    expect(editPool).toHaveBeenCalledWith({ id: 3 }, poolInfoToPoolConfig(samplePool), {});
  });

  test("testPoolConnection maps the pool through poolInfoToTestConnection", async () => {
    const testPoolConnection = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ testPoolConnection } as never);

    await adapter.testPoolConnection(samplePool);

    expect(testPoolConnection).toHaveBeenCalledWith(poolInfoToTestConnection(samplePool), {});
  });

  test("setPassword wraps the password in the v1 request body", async () => {
    const setPassword = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ setPassword } as never);

    await adapter.setPassword("pw", { secure: false });

    expect(setPassword).toHaveBeenCalledWith({ password: "pw" }, { secure: false });
  });
});
