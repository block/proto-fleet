import { afterEach, describe, expect, test, vi } from "vitest";

import {
  type V2GetContainerMapResponse,
  V2HashboardErrorCause,
  V2MiningOperatingState,
  V2PerformanceMode,
  V2PLLAlgorithm,
  V2PoolConnState,
  V2Severity,
} from "./generated/generatedApi";
import { MdkV2Adapter, type MdkV2Client } from "./MdkV2Adapter";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";

const client = (overrides: Partial<MdkV2Client> = {}) =>
  ({
    miningServiceSetPowerTargetScoped: vi.fn(),
    miningServiceClearPowerTargetScoped: vi.fn(),
    miningServiceStartMiningScoped: vi.fn(),
    miningServiceStopMiningScoped: vi.fn(),
    miningServiceSetModuleFixedVoltage: vi.fn(),
    miningServiceClearModuleFixedVoltage: vi.fn(),
    powerServiceSetContainerControl: vi.fn(),
    powerServiceSetContainerSetpoint: vi.fn(),
    runInServiceStartModuleRunIn: vi.fn(),
    telemetryServiceQueryTimeSeries: vi.fn(),
    telemetryServiceGetContainerHealth: vi.fn().mockResolvedValue({ data: {} }),
    ...overrides,
  }) as unknown as MdkV2Client;

describe("MdkV2Adapter", () => {
  test("pool settings preserve string IDs and use masked edits without resetting credentials or weights", async () => {
    const update = vi.fn();
    const create = vi.fn();
    const remove = vi.fn();
    const adapter = new MdkV2Adapter(
      client({ poolServiceUpdatePool: update, poolServiceCreatePool: create, poolServiceDeletePool: remove }),
    );
    const pool = { name: "Renamed", url: "stratum+tcp://pool:3333", username: "worker", password: "", priority: 0 };
    await adapter.savePoolSettings(pool, "9007199254740993");
    expect(update).toHaveBeenCalledWith(
      { poolId: "9007199254740993" },
      {
        pool: { name: pool.name, url: pool.url, username: pool.username },
        password: undefined,
        updateMask: "name,url,username",
      },
      { headers: { "X-Frigate-Actor": "proto-os" } },
    );
    await adapter.savePoolSettings({ ...pool, priority: 3, password: "secret" });
    expect(create).toHaveBeenCalledWith(
      {
        pool: { name: pool.name, url: pool.url, username: pool.username, priority: 3, enabled: true },
        password: "secret",
      },
      expect.anything(),
    );
    await adapter.deletePoolSettings("42");
    expect(remove).toHaveBeenCalledWith({ id: "42" }, expect.anything());
  });

  test("reordering never writes duplicate priorities or other pool fields", async () => {
    const pools = [
      { id: "42", priority: 0 },
      { id: "91", priority: 1 },
      { id: "103", priority: 2 },
    ];
    const update = vi.fn(async ({ poolId }, { pool, updateMask }) => {
      expect(updateMask).toBe("priority");
      expect(Object.keys(pool)).toEqual(["priority"]);
      expect(pools.filter((p) => p.id !== poolId).map((p) => p.priority)).not.toContain(pool.priority);
      pools.find((p) => p.id === poolId)!.priority = pool.priority;
    });
    const adapter = new MdkV2Adapter(
      client({
        poolServiceListPools: vi.fn().mockResolvedValue({ data: { pools } }),
        poolServiceUpdatePool: update as never,
      }),
    );
    await adapter.reorderPoolSettings(["103", "42", "91"]);
    expect(pools.map((pool) => pool.priority)).toEqual([1, 2, 0]);
    update.mockClear();
    await expect(adapter.reorderPoolSettings(["42", "missing"])).rejects.toThrow("configuration changed");
    expect(update).not.toHaveBeenCalled();
  });

  test("a failed reorder stops without replaying or replacing pools", async () => {
    const update = vi.fn().mockResolvedValueOnce({}).mockRejectedValueOnce(new Error("reload failed"));
    const adapter = new MdkV2Adapter(
      client({
        poolServiceListPools: vi.fn().mockResolvedValue({
          data: {
            pools: [
              { id: "42", priority: 0 },
              { id: "91", priority: 1 },
            ],
          },
        }),
        poolServiceUpdatePool: update,
      }),
    );
    await expect(adapter.reorderPoolSettings(["91", "42"])).rejects.toThrow("reload failed");
    expect(update).toHaveBeenCalledTimes(2);
  });

  test("container controls retain unknown states and only reported bounds", async () => {
    const setpoint = vi.fn();
    const control = vi.fn();
    const adapter = new MdkV2Adapter(
      client({
        powerServiceGetContainerControls: vi.fn().mockResolvedValue({
          data: {
            supported: true,
            controls: [
              { name: "coolant_pump", enabled: false, stateKnown: false },
              { name: "dry_cooler", stateKnown: true },
            ],
            setpoints: [{ name: "temperature", value: 0, min: 0, max: 0 }],
          },
        }),
        powerServiceSetContainerControl: control,
        powerServiceSetContainerSetpoint: setpoint,
      }),
    );
    const settings = await adapter.getContainerSettings();
    expect(settings.controls.map((item) => item.enabled)).toEqual([undefined, false]);
    expect(settings.setpoints[0]).toMatchObject({ value: 0, min: undefined, max: undefined });
    await adapter.setContainerControl("coolant_pump", false);
    expect(control).toHaveBeenCalledWith({ name: "coolant_pump" }, { enabled: false }, expect.anything());
    await expect(adapter.setContainerSetpoint("temperature", Infinity)).rejects.toThrow("finite");
    await expect(adapter.setContainerSetpoint("temperature", 1e99)).rejects.toThrow("finite");
    expect(setpoint).not.toHaveBeenCalled();
    await adapter.setContainerSetpoint("temperature", 39.5);
    expect(setpoint).toHaveBeenCalledWith({ name: "temperature" }, { value: 39.5 }, expect.anything());
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  test("returns board errors and warnings with canonical ownership for repeated board slots", async () => {
    const map: V2GetContainerMapResponse = {
      containerMap: {
        tanks: [1, 2].map((tank) => ({
          id: tank,
          modules: [
            {
              id: 1,
              slots: [
                {
                  hashboard: {
                    macAddress: `board-${tank}`,
                    moduleId: `T${tank}-M01`,
                    location: { tank, module: 1, hashboardPosition: 3 },
                    health:
                      tank === 1
                        ? {
                            errors: [
                              {
                                cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_ASIC_OVER_TEMPERATURE,
                                asic: { id: 7, temperature: 98 },
                              },
                            ],
                          }
                        : {
                            warnings: [
                              {
                                cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_STACK_DELTA_THERMAL_THROTTLE,
                                stack: 0,
                              },
                            ],
                          },
                  },
                },
              ],
            },
          ],
        })),
      },
    };
    const getContainerMap = vi.fn().mockResolvedValue({ data: map });
    const adapter = new MdkV2Adapter(
      client({
        hashboardServiceGetContainerMap: getContainerMap,
      }),
      { deviceId: "ct1-a" },
    );

    const errors = await adapter.getErrors();

    expect(errors).toHaveLength(2);
    expect(errors).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          owner: { kind: "module", id: "container:ct1-a:module:T1-M01" },
          component: {
            kind: "hashboard",
            id: "mac:board-1",
            owner: { kind: "module", id: "container:ct1-a:module:T1-M01" },
          },
          severity: "critical",
          message: expect.stringMatching(/ASIC.*7.*98/i),
        }),
        expect.objectContaining({
          owner: { kind: "module", id: "container:ct1-a:module:T2-M01" },
          component: {
            kind: "hashboard",
            id: "mac:board-2",
            owner: { kind: "module", id: "container:ct1-a:module:T2-M01" },
          },
          severity: "warning",
          message: expect.stringMatching(/stack/i),
        }),
      ]),
    );

    for (const tank of map.containerMap!.tanks!) {
      tank.modules![0].slots![0].hashboard!.health = { errors: [], warnings: [] };
    }
    expect(await adapter.getErrors()).toEqual([]);
  });

  test("shows active plant alarms with their reported severity and component, then clears them", async () => {
    const getContainerHealth = vi
      .fn()
      .mockResolvedValueOnce({
        data: {
          alarms: [
            {
              id: "pump_overload",
              name: "Coolant pump overload",
              componentId: "coolant_pump",
              severity: V2Severity.SEVERITY_CRITICAL,
            },
            {
              id: "fan_warning",
              name: "Cooling fan slow",
              componentId: "fan-2",
              severity: V2Severity.SEVERITY_WARNING,
            },
          ],
        },
      })
      .mockResolvedValueOnce({ data: { alarms: [] } });
    const adapter = new MdkV2Adapter(
      client({
        hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({ data: { containerMap: { tanks: [] } } }),
        telemetryServiceGetContainerHealth: getContainerHealth,
      }),
      { deviceId: "ct1-a" },
    );

    expect(await adapter.getErrors()).toEqual([
      {
        id: "container:ct1-a:alarm:pump_overload",
        code: "pump_overload",
        message: "Critical: Coolant pump overload · Component coolant_pump",
        owner: { kind: "container", id: "container:ct1-a" },
        severity: "critical",
      },
      {
        id: "container:ct1-a:alarm:fan_warning",
        code: "fan_warning",
        message: "Warning: Cooling fan slow · Component fan-2",
        owner: { kind: "container", id: "container:ct1-a" },
        severity: "warning",
      },
    ]);
    expect(await adapter.getErrors()).toEqual([]);
    expect(getContainerHealth).toHaveBeenCalledTimes(2);
  });

  test("keeps board errors when an older CT1 lacks plant-health support", async () => {
    const adapter = new MdkV2Adapter(
      client({
        hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({ data: { containerMap: { tanks: [] } } }),
        telemetryServiceGetContainerHealth: vi.fn().mockRejectedValue({ status: 404 }),
      }),
    );
    await expect(adapter.getErrors()).resolves.toEqual([]);
  });

  test.each([
    ["warning", { overTemperatureWarning: true }],
    ["critical", { overTemperatureFault: true }],
  ] as const)(
    "scopes PSU %s details to the owning module and clears them on refresh",
    async (severity, thermalFlags) => {
      const map = (active: boolean): V2GetContainerMapResponse => ({
        containerMap: {
          tanks: [
            {
              id: 1,
              modules: [1, 2].map((module) => ({
                id: module,
                slots: [1, 2].map((position) => ({
                  hashboard: {
                    macAddress: `board-${module}-${position}`,
                    moduleId: `T1-M0${module}`,
                    health: {
                      psuThermal: {
                        thermalStateValid: true,
                        thermalStateAgeMs: position * 10,
                        overTemperatureWarning: false,
                        overTemperatureFault: false,
                        ...(module === 1 && active ? thermalFlags : {}),
                      },
                    },
                  },
                })),
              })),
            },
          ],
        },
      });
      const getContainerMap = vi
        .fn()
        .mockResolvedValueOnce({ data: map(true) })
        .mockResolvedValueOnce({ data: map(false) });
      const adapter = new MdkV2Adapter(client({ hashboardServiceGetContainerMap: getContainerMap }), {
        deviceId: "ct1-a",
      });

      expect(await adapter.getErrors()).toEqual([
        expect.objectContaining({
          severity,
          component: {
            kind: "psu",
            id: "container:ct1-a:module:T1-M01:psu:1",
            owner: { kind: "module", id: "container:ct1-a:module:T1-M01" },
          },
          message: expect.stringMatching(/temperature/i),
        }),
      ]);
      expect(await adapter.getErrors()).toEqual([]);
    },
  );

  test("does not turn latched shutdown history or invalid PSU thermal state into active faults", async () => {
    const adapter = new MdkV2Adapter(
      client({
        hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({
          data: {
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
                            macAddress: "board-1",
                            moduleId: "T1-M01",
                            health: {
                              lastShutdownError: {
                                cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE,
                              },
                              psuThermal: {
                                thermalStateValid: false,
                                overTemperatureFault: true,
                                overTemperatureWarning: true,
                              },
                            },
                          },
                        },
                        { hashboard: { macAddress: "board-2", moduleId: "T1-M01" } },
                      ],
                    },
                  ],
                },
              ],
            },
          },
        }),
      }),
      { deviceId: "ct1-a" },
    );

    expect(await adapter.getErrors()).toEqual([]);
  });

  test("uses the freshest shared PSU thermal state when an older board report still has a warning", async () => {
    const adapter = new MdkV2Adapter(
      client({
        hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({
          data: {
            containerMap: {
              tanks: [
                {
                  id: 1,
                  modules: [
                    {
                      id: 1,
                      slots: [100, 10].map((age) => ({
                        hashboard: {
                          macAddress: `board-${age}`,
                          moduleId: "T1-M01",
                          health: {
                            psuThermal: {
                              thermalStateAgeMs: age,
                              thermalStateValid: true,
                              overTemperatureWarning: age === 100,
                              overTemperatureFault: false,
                            },
                          },
                        },
                      })),
                    },
                  ],
                },
              ],
            },
          },
        }),
      }),
      { deviceId: "ct1-a" },
    );

    expect(await adapter.getErrors()).toEqual([]);
  });

  test("attributes requests while allowing an explicit caller actor to win", () => {
    const adapter = new MdkV2Adapter(client());
    expect(adapter.decorate()).toEqual({ headers: { "X-Frigate-Actor": "proto-os" } });
    expect(adapter.decorate({ headers: { "X-Frigate-Actor": "fleet-proxy", Trace: "abc" } })).toEqual({
      headers: { "X-Frigate-Actor": "fleet-proxy", Trace: "abc" },
    });
  });

  test("uses the canonical context device id without double-prefixing when no probe id was reported", async () => {
    const adapter = new MdkV2Adapter(
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

  test("uses the hardware context identity for subsequent component errors when the probe has no device ID", async () => {
    const adapter = new MdkV2Adapter(
      client({
        hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({
          data: {
            containerMap: {
              tanks: [
                { id: 1, modules: [{ id: 1, slots: [{ hashboard: { serial: "HB-1", health: { errors: [{}] } } }] }] },
              ],
            },
          },
        }),
        miningServiceGetMiningStatus: vi.fn().mockResolvedValue({ data: {} }),
        telemetryServiceGetContainerHealth: vi.fn().mockResolvedValue({ data: {} }),
      }),
    );
    const { hardware } = await adapter.getHardware({ deviceId: "device:context-ct1", profile: "ct1" });
    const errors = await adapter.getErrors();
    expect(errors[0].component).toEqual([...hardware.hashboards.values()][0].ref);
  });

  test("forwards scoped power and module-voltage writes with actor attribution", async () => {
    const setScopedPower = vi.fn().mockResolvedValue(undefined);
    const setModuleFixedVoltage = vi.fn().mockResolvedValue(undefined);
    const adapter = new MdkV2Adapter(
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

  test("forwards container controls and setpoints with actor attribution", async () => {
    const setControl = vi.fn().mockResolvedValue(undefined);
    const setSetpoint = vi.fn().mockResolvedValue(undefined);
    const adapter = new MdkV2Adapter(
      client({ powerServiceSetContainerControl: setControl, powerServiceSetContainerSetpoint: setSetpoint }),
    );

    await adapter.setContainerControl("dry_cooler_auto", true);
    await adapter.setContainerSetpoint("dry_cooler_auto_temp_c", 18.3);

    expect(setControl).toHaveBeenCalledWith(
      { name: "dry_cooler_auto" },
      { enabled: true },
      { headers: { "X-Frigate-Actor": "proto-os" } },
    );
    expect(setSetpoint).toHaveBeenCalledWith(
      { name: "dry_cooler_auto_temp_c" },
      { value: 18.3 },
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
    const adapter = new MdkV2Adapter(
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
    const adapter = new MdkV2Adapter(
      client({ poolServiceListPools: listPools, poolServiceGetPoolDiagnostics: getPoolDiagnostics }),
    );

    await expect(adapter.getPools()).resolves.toEqual([
      expect.objectContaining({ id: "pool:1", url: "stratum+tcp://primary", status: "Active" }),
    ]);
    expect(listPools).toHaveBeenCalledWith({ headers: { "X-Frigate-Actor": "proto-os" } });
    expect(getPoolDiagnostics).toHaveBeenCalledWith({}, { headers: { "X-Frigate-Actor": "proto-os" } });
  });

  test("requests retained pool results only through the opt-in method and keeps exact pool IDs", async () => {
    const getPoolDiagnostics = vi.fn().mockResolvedValue({
      data: {
        lastRecordedShares: [
          {
            poolId: "9007199254740993",
            resultAt: "2026-09-29T02:00:00Z",
            accepted: false,
            assignedDifficulty: "9007199254740993",
            rejectReason: "stale_job",
          },
          { poolId: "2", resultAt: "2026-09-29T02:01:00Z", accepted: false, rejectReason: "submit_error" },
        ],
      },
    });
    const adapter = new MdkV2Adapter(client({ poolServiceGetPoolDiagnostics: getPoolDiagnostics }));

    await expect(adapter.getPoolRecordedShares()).resolves.toEqual({
      "pool:9007199254740993": {
        resultAtMs: Date.parse("2026-09-29T02:00:00Z"),
        assignedDifficulty: "9007199254740993",
        outcome: "rejected",
      },
      "pool:2": {
        resultAtMs: Date.parse("2026-09-29T02:01:00Z"),
        assignedDifficulty: undefined,
        outcome: "submitError",
      },
    });
    expect(getPoolDiagnostics).toHaveBeenCalledWith(
      { includeLastRecordedShares: true },
      { headers: { "X-Frigate-Actor": "proto-os" } },
    );
    getPoolDiagnostics.mockResolvedValueOnce({ data: {} });
    await expect(adapter.getPoolRecordedShares()).resolves.toEqual({});
    getPoolDiagnostics.mockRejectedValueOnce({ status: 400 });
    await expect(adapter.getPoolRecordedShares()).resolves.toEqual({});
    getPoolDiagnostics.mockRejectedValueOnce({ status: 503 });
    await expect(adapter.getPoolRecordedShares()).rejects.toMatchObject({ status: 503 });
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
    const adapter = new MdkV2Adapter(
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
    const adapter = new MdkV2Adapter(client({ runInServiceStartModuleRunIn: startModuleRunIn }));

    const result = await adapter.startModuleRunIn("container:ct1-a:module:T1-M02", {
      taskId: "task-1",
      duration: "1800s",
      sampleInterval: "10s",
      boardPowerTargetWatts: 5_000,
      stopMiningOnFinish: true,
    });

    expect(startModuleRunIn).toHaveBeenCalledWith(
      { moduleId: "T1-M02", taskId: "task-1" },
      {
        sampleInterval: "10s",
        tuningSweep: {
          dwellDuration: "1800s",
          powerTarget: { startPowerTargetWatts: 5_000, endPowerTargetWatts: 5_000, stepPowerTargetWatts: 1 },
        },
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
    const adapter = new MdkV2Adapter(client({ telemetryServiceQueryTimeSeries: queryTimeSeries }), {
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
    const adapter = new MdkV2Adapter(client({ telemetryServiceQueryTimeSeries: queryTimeSeries }));

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
    const adapter = new MdkV2Adapter(
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

  test("caches selected hashboard histories between polling intervals", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-02T12:00:00Z"));
    const getContainerMap = vi.fn().mockResolvedValue({
      data: {
        containerMap: {
          tanks: [{ id: 1, modules: [{ id: 1, slots: [{ hashboard: { macAddress: "26:B0:3F:F0:D7:92" } }] }] }],
        },
      },
    });
    const queryTimeSeries = vi.fn().mockResolvedValue({
      data: {
        buckets: [{ time: "2026-09-02T11:00:00Z", avg: 2, count: "10" }],
      },
    });
    const adapter = new MdkV2Adapter(
      client({ hashboardServiceGetContainerMap: getContainerMap, telemetryServiceQueryTimeSeries: queryTimeSeries }),
      { deviceId: "ct1-a" },
    );
    const request = (startTime: string) => ({
      startTime,
      duration: "PT1H",
      interval: "PT5M",
      levels: [{ type: "hashboard" as const, indexes: [0], fields: ["hashrate" as const, "power" as const] }],
    });

    await adapter.getTimeSeries(request("2026-09-02T11:00:00Z"));
    await adapter.getTimeSeries(request("2026-09-02T11:00:30Z"));
    await adapter.getTimeSeries(request("2026-09-02T11:04:59Z"));

    expect(getContainerMap).toHaveBeenCalledTimes(3);
    expect(queryTimeSeries).toHaveBeenCalledTimes(2);

    await adapter.getTimeSeries(request("2026-09-02T11:05:01Z"));

    expect(getContainerMap).toHaveBeenCalledTimes(4);
    expect(queryTimeSeries).toHaveBeenCalledTimes(4);

    vi.setSystemTime(new Date("2026-09-02T12:05:01Z"));

    await adapter.getTimeSeries(request("2026-09-02T11:05:30Z"));

    expect(getContainerMap).toHaveBeenCalledTimes(5);
    expect(queryTimeSeries).toHaveBeenCalledTimes(6);
  });

  test.each([
    ["hashrate", 289],
    ["power", 289],
    ["temperature", 289],
    ["efficiency", 578],
  ] as const)("limits a full container's %s history to %i cold requests", async (field, expectedRequests) => {
    const boardCount = 288;
    const queryTimeSeries = vi.fn().mockResolvedValue({ data: { buckets: [] } });
    const adapter = new MdkV2Adapter(
      client({
        telemetryServiceQueryTimeSeries: queryTimeSeries,
        hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({
          data: {
            containerMap: {
              tanks: [
                {
                  id: 1,
                  modules: [
                    {
                      id: 1,
                      slots: Array.from({ length: boardCount }, (_, index) => ({
                        hashboard: { serial: `board-${index}` },
                      })),
                    },
                  ],
                },
              ],
            },
          },
        }),
      }),
      { deviceId: "ct1-a" },
    );

    const query = {
      startTime: "2026-09-02T11:00:00Z",
      duration: "PT1H",
      interval: "PT5M",
    };
    await Promise.all([
      adapter.getTimeSeries({ ...query, levels: [{ type: "miner", fields: [field] }] }),
      adapter.getTimeSeries({
        ...query,
        levels: [{ type: "hashboard", fields: [field], indexes: Array.from({ length: boardCount }, (_, i) => i) }],
      }),
    ]);

    expect(queryTimeSeries).toHaveBeenCalledTimes(expectedRequests);
    const queriedMetrics = new Set(queryTimeSeries.mock.calls.map(([query]) => query.metric));
    expect(queriedMetrics.size).toBe(field === "efficiency" ? 2 : 1);
  });

  test("returns CT1 plant fans from the cooling-status adapter method", async () => {
    const getContainerHealth = vi.fn().mockResolvedValue({
      data: { coolingFans: [{ componentId: "dry-cooler-1", powerW: 400, speedPercent: 75 }] },
    });
    const adapter = new MdkV2Adapter(client({ telemetryServiceGetContainerHealth: getContainerHealth }), {
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
    expect(getContainerHealth).toHaveBeenCalledWith({}, { headers: { "X-Frigate-Actor": "proto-os" } });
  });

  test("rejects unsupported granular histories instead of returning a container aggregate", async () => {
    const adapter = new MdkV2Adapter(client());

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
    const adapter = new MdkV2Adapter(client({ telemetryServiceQueryTimeSeries: queryTimeSeries }));

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
