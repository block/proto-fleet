import { describe, expect, test, vi } from "vitest";
import { Api, MetricUnit } from "./generated/generatedApi";
import { MdkV1Adapter } from "./MdkV1Adapter";
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

    expect(await adapter.login("pw")).toEqual({ accessToken: "a", refreshToken: "r" });
    expect(login).toHaveBeenCalledWith({ password: "pw" }, {});
  });

  test("refreshAccessToken returns the renewed access token", async () => {
    const refreshToken = vi.fn().mockResolvedValue({ data: { access_token: "a2" } });
    const adapter = new MdkV1Adapter({ refreshToken } as never);

    expect(await adapter.refreshAccessToken("r")).toEqual({ accessToken: "a2" });
    expect(refreshToken).toHaveBeenCalledWith({ refresh_token: "r" }, {});
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

    await adapter.startMining();

    expect(startMining).toHaveBeenCalledWith({});
  });

  test("stopMining forwards the auth params to the client", async () => {
    const stopMining = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ stopMining } as never);

    await adapter.stopMining();

    expect(stopMining).toHaveBeenCalledWith({});
  });

  test("rebootSystem forwards the auth params to the client", async () => {
    const rebootSystem = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ rebootSystem } as never);

    await adapter.rebootSystem();

    expect(rebootSystem).toHaveBeenCalledWith({});
  });

  test("changePassword forwards the v1 request body and auth params", async () => {
    const changePassword = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ changePassword } as never);

    const request = { current_password: "current123", new_password: "new456789" };
    await adapter.changePassword(request);

    expect(changePassword).toHaveBeenCalledWith(request, {});
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

    await adapter.setPassword("pw");

    expect(setPassword).toHaveBeenCalledWith({ password: "pw" }, {});
  });
});

describe("MdkV1Adapter firmware update commands", () => {
  test("startFirmwareUpdate initiates the update and hides the raw response", async () => {
    const postUpdateSystem = vi.fn().mockResolvedValue({ status: 202, ok: true });
    const adapter = new MdkV1Adapter({ postUpdateSystem } as never);

    await expect(adapter.startFirmwareUpdate()).resolves.toBeUndefined();
    expect(postUpdateSystem).toHaveBeenCalledWith({});
  });

  test("checkForFirmwareUpdate initiates the check and hides the raw response", async () => {
    const updateCheck = vi.fn().mockResolvedValue({ status: 200, ok: true });
    const adapter = new MdkV1Adapter({ updateCheck } as never);

    await expect(adapter.checkForFirmwareUpdate()).resolves.toBeUndefined();
    expect(updateCheck).toHaveBeenCalledWith({});
  });

  test.each([
    ["startFirmwareUpdate", "postUpdateSystem"],
    ["checkForFirmwareUpdate", "updateCheck"],
  ] as const)("%s treats a successful empty-body JSON parse error as success", async (adapterMethod, clientMethod) => {
    const clientCall = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      error: new SyntaxError("Unexpected end of JSON input"),
    });
    const adapter = new MdkV1Adapter({ [clientMethod]: clientCall } as never);

    await expect(adapter[adapterMethod]()).resolves.toBeUndefined();
  });

  test.each([
    ["startFirmwareUpdate", "postUpdateSystem"],
    ["checkForFirmwareUpdate", "updateCheck"],
  ] as const)("%s treats a thrown empty-body JSON parse error as success", async (adapterMethod, clientMethod) => {
    const clientCall = vi.fn().mockRejectedValue({
      error: new SyntaxError("Unexpected end of JSON input"),
    });
    const adapter = new MdkV1Adapter({ [clientMethod]: clientCall } as never);

    await expect(adapter[adapterMethod]()).resolves.toBeUndefined();
  });

  test.each([
    ["startFirmwareUpdate", "postUpdateSystem"],
    ["checkForFirmwareUpdate", "updateCheck"],
  ] as const)("%s accepts the top-level parse error thrown by older clients", async (adapterMethod, clientMethod) => {
    const clientCall = vi.fn().mockRejectedValue(new SyntaxError("Unexpected end of JSON input"));
    const adapter = new MdkV1Adapter({ [clientMethod]: clientCall } as never);

    await expect(adapter[adapterMethod]()).resolves.toBeUndefined();
  });

  test.each([
    ["startFirmwareUpdate", "postUpdateSystem"],
    ["checkForFirmwareUpdate", "updateCheck"],
  ] as const)(
    "%s propagates a non-2xx empty-body failure instead of swallowing it",
    async (adapterMethod, clientMethod) => {
      const failure = { ok: false, status: 503, error: new SyntaxError("Unexpected end of JSON input") };
      const clientCall = vi.fn().mockRejectedValue(failure);
      const adapter = new MdkV1Adapter({ [clientMethod]: clientCall } as never);

      await expect(adapter[adapterMethod]()).rejects.toBe(failure);
    },
  );

  test.each([
    ["startFirmwareUpdate", "postUpdateSystem"],
    ["checkForFirmwareUpdate", "updateCheck"],
  ] as const)("%s preserves unrelated failures", async (adapterMethod, clientMethod) => {
    const failure = new Error("network unavailable");
    const clientCall = vi.fn().mockRejectedValue(failure);
    const adapter = new MdkV1Adapter({ [clientMethod]: clientCall } as never);

    await expect(adapter[adapterMethod]()).rejects.toBe(failure);
  });
});

describe("MdkV1Adapter request decoration", () => {
  test("adds a bearer header to every decorated request when a token is available", async () => {
    const getSystemInfo = vi.fn().mockResolvedValue({ data: {} });
    const adapter = new MdkV1Adapter({ getSystemInfo } as never, () => "tok-123");

    await adapter.getSystemInfo();

    expect(getSystemInfo).toHaveBeenCalledWith({ headers: { Authorization: "Bearer tok-123" } });
  });

  test("merges the bearer header with caller-supplied params, caller headers winning", async () => {
    const putSystemTag = vi.fn().mockResolvedValue({});
    const adapter = new MdkV1Adapter({ putSystemTag } as never, () => "tok-123");

    await adapter.setSystemTag("rig-a", { signal: null, headers: { Authorization: "Bearer override" } });

    expect(putSystemTag).toHaveBeenCalledWith("rig-a", {
      signal: null,
      headers: { Authorization: "Bearer override" },
    });
  });

  test("attaches no auth header when the credential accessor is absent (fleet / pre-login)", async () => {
    const getSystemStatus = vi.fn().mockResolvedValue({ data: {} });
    const adapter = new MdkV1Adapter({ getSystemStatus } as never);

    await adapter.getSystemStatus();

    expect(getSystemStatus).toHaveBeenCalledWith({});
  });

  test("attaches no auth header when the accessor yields an empty token", async () => {
    const getSystemStatus = vi.fn().mockResolvedValue({ data: {} });
    const adapter = new MdkV1Adapter({ getSystemStatus } as never, () => "");

    await adapter.getSystemStatus();

    expect(getSystemStatus).toHaveBeenCalledWith({});
  });

  test("does not decorate login or refresh — they establish the credential", async () => {
    const login = vi.fn().mockResolvedValue({ data: { access_token: "a", refresh_token: "r" } });
    const refreshToken = vi.fn().mockResolvedValue({ data: { access_token: "a2" } });
    const adapter = new MdkV1Adapter({ login, refreshToken } as never, () => "stale-token");

    await adapter.login("pw");
    await adapter.refreshAccessToken("r");

    expect(login).toHaveBeenCalledWith({ password: "pw" }, {});
    expect(refreshToken).toHaveBeenCalledWith({ refresh_token: "r" }, {});
  });
});

// Guard against the decorate() omission trap: auth is applied per-method via
// this.decorate(params) rather than a single choke point, so a future method
// that forgets it would silently send unauthenticated. This table asserts every
// firmware-talking method carries the bearer; login/refreshAccessToken are the
// only intentional exceptions (covered above). decorate() is always the last
// argument to the client call, so we assert on the last arg regardless of
// signature shape. Add a row here whenever a new adapter method is added.
describe("MdkV1Adapter decorates every authenticated method", () => {
  const TOKEN = "table-token";
  const AUTH = { Authorization: `Bearer ${TOKEN}` };
  // A valid pool so poolInfoToPoolConfig / poolInfoToTestConnection don't throw
  // before the client call (they run on the arg prior to decorate()).
  const POOL = { name: "p", url: "stratum+tcp://h:1", username: "u", password: "pw", priority: 0 };

  const cases: Array<[string, (adapter: MdkV1Adapter) => Promise<unknown>]> = [
    ["getSystemInfo", (a) => a.getSystemInfo()],
    ["getNetwork", (a) => a.getNetworkInfo()],
    ["getSystemStatus", (a) => a.getSystemStatus()],
    ["getSystemTag", (a) => a.getSystemTag()],
    ["putSystemTag", (a) => a.setSystemTag("rig")],
    ["getMiningStatus", (a) => a.getMiningStatus()],
    ["getErrors", (a) => a.getErrors()],
    ["listPools", (a) => a.getPools()],
    ["getMiningTarget", (a) => a.getMiningTarget()],
    ["editMiningTarget", (a) => a.updateMiningTarget({ valueWatts: 1 } as never)],
    ["getHardware", (a) => a.getHardware({ deviceId: "d", profile: "unknown" })],
    ["getAllHashboards", (a) => a.getHashboards()],
    ["getHashboardStatus", (a) => a.getHashboardStatus(["s1"])],
    ["getSystemLogs", (a) => a.getSystemLogs({ lines: 1 })],
    ["getCurrentTelemetry", (a) => a.getCurrentTelemetry()],
    ["getTimeSeries", (a) => a.getTimeSeries({ startTime: "2020-01-01T00:00:00Z", levels: [] } as never)],
    ["getCooling", (a) => a.getCoolingStatus()],
    ["setCoolingMode", (a) => a.setCoolingMode("auto" as never)],
    ["startMining", (a) => a.startMining()],
    ["stopMining", (a) => a.stopMining()],
    ["rebootSystem", (a) => a.rebootSystem()],
    ["postUpdateSystem", (a) => a.startFirmwareUpdate()],
    ["putUpdateSystem", (a) => a.uploadFirmware(new File(["swu"], "r2.swu"))],
    ["updateCheck", (a) => a.checkForFirmwareUpdate()],
    ["locateSystem", (a) => a.locateSystem({ enable: true })],
    ["createPools", (a) => a.createPools([])],
    ["editPool", (a) => a.editPool(1, POOL as never)],
    ["testPoolConnection", (a) => a.testPoolConnection(POOL as never)],
    ["setPassword", (a) => a.setPassword("pw")],
    ["changePassword", (a) => a.changePassword({} as never)],
  ];

  test.each(cases)("%s carries the bearer header", async (clientMethod, invoke) => {
    const clientCall = vi.fn().mockResolvedValue({ data: {} });
    const adapter = new MdkV1Adapter({ [clientMethod]: clientCall } as never, () => TOKEN);

    // Response-shape processing after the client call is irrelevant here; we
    // only care that the client was invoked with a decorated argument.
    await invoke(adapter).catch(() => {});

    expect(clientCall).toHaveBeenCalled();
    const lastCall = clientCall.mock.calls[clientCall.mock.calls.length - 1];
    const lastArg = lastCall[lastCall.length - 1];
    expect(lastArg).toMatchObject({ headers: AUTH });
  });
});

describe("MdkV1Adapter.uploadFirmware", () => {
  test("sends one authenticated multipart PUT with the selected file and accepts an empty response", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
    const adapter = new MdkV1Adapter(new Api({ baseUrl: "http://rig", customFetch: fetch }).api, () => "token");
    const file = new File(["swu bytes"], "r2.swu", { type: "application/octet-stream" });
    const controller = new AbortController();

    await adapter.uploadFirmware(file, { signal: controller.signal });

    expect(fetch).toHaveBeenCalledOnce();
    const [url, params] = fetch.mock.calls[0];
    expect(url).toBe("http://rig/api/v1/system/update");
    expect(params).toMatchObject({
      method: "PUT",
      headers: { Authorization: "Bearer token" },
      signal: controller.signal,
    });
    expect(new Headers(params.headers).has("Content-Type")).toBe(false);
    expect(params.body).toBeInstanceOf(FormData);
    expect([...params.body.keys()]).toEqual(["file"]);
    expect(params.body.get("file")).toBe(file);
  });

  test.each([400, 401, 409, 413, 500])("preserves HTTP %s failures", async (status) => {
    const fetch = vi.fn().mockResolvedValue(new Response(null, { status }));
    const adapter = new MdkV1Adapter(new Api({ customFetch: fetch }).api);
    await expect(adapter.uploadFirmware(new File(["swu"], "r2.swu"))).rejects.toMatchObject({ status });
  });
});
