import { describe, expect, test } from "vitest";

import {
  type V2Hashboard,
  V2MiningOperatingState,
  V2MiningState,
  V2Phase,
  V2PoolConnState,
} from "./generated/generatedApi";
import {
  toCanonicalContainer,
  toCanonicalContainerTimeSeries,
  toCanonicalHashboardStatus,
  toCanonicalOperationalStatus,
  toCanonicalPools,
  toCanonicalSystemInfo,
} from "./normalize";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const map = {
  containerMap: {
    tanks: [
      {
        id: 1,
        modules: [
          {
            id: 2,
            slots: [
              {
                hashboard: {
                  macAddress: "26:B0:3F:F0:D7:92",
                  serial: "HB-1",
                  hwRevision: "rev-a",
                  moduleId: "T1-M02",
                  miningState: V2MiningState.MINING_STATE_MINING,
                  location: { tank: 1, module: 2, hashboardPosition: 3, slot: 99 },
                  health: {
                    hashrate: 2.5,
                    power: 40,
                    temperatures: [60, 64],
                    averageAsicTemperatureC: 66,
                    hashrailOk: true,
                    refclksOk: true,
                  },
                  observedAsicCount: 2,
                  firmwareVersion: "2.0.0",
                },
                powerFeedId: "P1",
              },
            ],
          },
          { id: 3, slots: [{ occupied: false }] },
        ],
      },
    ],
  },
};

describe("MDK v2 CT1 normalization", () => {
  test.each([
    [V2MiningOperatingState.MINING_OPERATING_STATE_UNINITIALIZED, "uninitialized"],
    [V2MiningOperatingState.MINING_OPERATING_STATE_NO_POOLS, "noPools"],
    [V2MiningOperatingState.MINING_OPERATING_STATE_ERROR, "error"],
  ] as const)("normalizes %s as %s", (operatingState, expected) => {
    expect(toCanonicalOperationalStatus({ operatingState, mining: false }, "container:ct1-a").miningStatus).toBe(
      expected,
    );
  });

  test("counts only explicit board errors as hardware errors", () => {
    expect(
      toCanonicalOperationalStatus(
        {
          operatingState: V2MiningOperatingState.MINING_OPERATING_STATE_STOPPED,
          mining: false,
          activeBoards: 0,
          totalBoards: 288,
          boardsByState: {
            [V2MiningState.MINING_STATE_IDLE]: 286,
            [V2MiningState.MINING_STATE_ERROR]: 2,
          },
        },
        "container:ct1-a",
      ),
    ).toMatchObject({ miningStatus: "stopped", hardwareErrorCount: 2 });
  });

  test.each<{ description: string; board: V2Hashboard }>([
    {
      description: "an explicit board error state",
      board: { macAddress: "26:B0:3F:F0:D7:92", miningState: V2MiningState.MINING_STATE_ERROR },
    },
    {
      description: "an active board health error",
      board: {
        macAddress: "26:B0:3F:F0:D7:92",
        miningState: V2MiningState.MINING_STATE_MINING,
        health: { errors: [{}] },
      },
    },
  ])("normalizes $description as a fault", ({ board }) => {
    const result = toCanonicalContainer(
      {
        containerMap: {
          tanks: [
            {
              id: 1,
              modules: [{ id: 1, slots: [{ hashboard: board }] }],
            },
          ],
        },
      },
      {},
      {},
      "ct1-a",
    );

    expect(result.telemetry.rollupsByEntity.get("container:ct1-a:tank:1" as never)?.health).toBe("fault");
  });

  test("keeps a faulted board online while it remains in the mining state", () => {
    const result = toCanonicalContainer(
      {
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
                        macAddress: "26:B0:3F:F0:D7:92",
                        miningState: V2MiningState.MINING_STATE_MINING,
                        health: { errors: [{}] },
                      },
                    },
                  ],
                },
              ],
            },
          ],
        },
      },
      {},
      {},
      "ct1-a",
    );

    expect(result.telemetry.rollupsByEntity.get("container:ct1-a:tank:1" as never)).toMatchObject({
      miningStatus: "mining",
      health: "fault",
      onlineBoardCount: 1,
    });
  });

  test("normalizes every configured pool with its runtime diagnostics", () => {
    expect(
      toCanonicalPools(
        {
          pools: [
            { id: "2", name: "Backup", url: "stratum+tcp://backup", priority: 2, enabled: true },
            { id: "1", name: "Primary", url: "stratum+tcp://primary", priority: 1, enabled: true },
            { id: "3", name: "Disabled", url: "stratum+tcp://disabled", priority: 3, enabled: false },
          ],
        },
        {
          pools: [
            { poolId: "1", connState: V2PoolConnState.POOL_CONN_STATE_ACTIVE, currentDifficulty: 2_048 },
            { poolId: "2", connState: V2PoolConnState.POOL_CONN_STATE_DEAD },
          ],
        },
      ),
    ).toEqual([
      expect.objectContaining({ id: "pool:1", name: "Primary", priority: 1, status: "Active", difficulty: 2_048 }),
      expect.objectContaining({ id: "pool:2", name: "Backup", priority: 2, status: "Dead" }),
      expect.objectContaining({ id: "pool:3", name: "Disabled", priority: 3, status: "Disabled" }),
    ]);
  });

  test("normalizes the container hierarchy, board telemetry, and plant telemetry", () => {
    const result = toCanonicalContainer(
      map,
      {
        hashrate: 2.4,
        power: 39,
        efficiency: 16.25,
        activeBoards: 1,
        totalBoards: 1,
        mining: true,
        operatingState: V2MiningOperatingState.MINING_OPERATING_STATE_MINING,
        averageAsicTemperatureC: 65.5,
      },
      {
        timestamp: "2026-09-02T12:00:00Z",
        plant: { totalPowerW: 45, facilityPowerW: 48, pue: 1.1, flowRateLpm: 12 },
        phases: [{ phase: V2Phase.PHASE_A, powerW: 15, currentA: 1.25, voltageV: 12 }],
        coolingFans: [{ componentId: "dry-cooler-1", powerW: 400, speedPercent: 75 }],
        powerDistributions: [{ componentId: "pdu-1", activeFeedCount: 2, activePowerW: 42 }],
        powerFeeds: [{ powerFeedId: "P1", active: true, powerW: 21, voltageV: 415, currentA: 50 }],
      },
      "ct1-a",
    );

    expect(result.hardware.topology).toMatchObject({
      kind: "container",
      deviceId: "device:ct1-a",
      containerId: "container:ct1-a",
    });
    if (result.hardware.topology?.kind !== "container") throw new Error("expected container topology");
    expect([...result.hardware.topology.tanks.values()][0]).toMatchObject({ label: "Tank 1", slot: 1 });
    expect([...result.hardware.topology.modules.values()][0]).toMatchObject({
      id: "container:ct1-a:module:T1-M02",
      tankId: "container:ct1-a:tank:1",
      location: { tankLabel: "Tank 1", moduleSlot: 2 },
    });
    expect(result.hardware.topology.modules.get("container:ct1-a:tank:1:module:3" as never)).toMatchObject({
      hashboardIds: [],
      location: { tankLabel: "Tank 1", moduleSlot: 3 },
    });
    expect([...result.hardware.topology.tanks.values()][0]?.moduleIds).toHaveLength(2);
    expect([...result.hardware.topology.hashboards.values()][0]).toMatchObject({
      location: { slot: 3 },
      powerFeedId: "P1",
    });
    expect([...result.hardware.hashboards.values()][0]).toMatchObject({
      identity: { kind: "mac", value: "26:B0:3F:F0:D7:92" },
      miningAsicCount: 2,
    });
    expect(result.telemetry.rollupsByEntity.get("container:ct1-a" as never)).toMatchObject({
      hashrate: { latest: { value: 2.4, units: "TH/s" } },
      temperature: { latest: { value: 65.5, units: "°C" } },
      boardCount: 1,
      onlineBoardCount: 1,
    });
    expect(result.telemetry.plant.get("container:ct1-a:plant" as never)).toMatchObject({
      totalPower: { latest: { value: 45, units: "W" } },
      pue: { latest: { value: 1.1, units: "ratio" } },
      flowRate: { latest: { value: 12, units: "L/min" } },
    });
    expect(result.hardware.topology.container.fanIds).toEqual(["container:ct1-a:fan:dry-cooler-1"]);
    expect(result.telemetry.fans.get("container:ct1-a:fan:dry-cooler-1" as never)).toMatchObject({
      power: { latest: { value: 400, units: "W" } },
      speedPercent: { latest: { value: 75, units: "%" } },
    });
    expect(result.telemetry.plant.get("container:ct1-a:distribution:pdu-1" as never)).toMatchObject({
      activeFeedCount: 2,
      totalPower: { latest: { value: 42, units: "W" } },
    });
    expect(result.telemetry.plant.get("container:ct1-a:power-feed:P1" as never)).toMatchObject({
      powered: true,
      totalPower: { latest: { value: 21, units: "W" } },
    });
  });

  test("converts deci-Celsius, tenths of GH/s, millivolts, and basis points", () => {
    const status = toCanonicalHashboardStatus("26:B0:3F:F0:D7:92", {
      asics: [
        {
          index: 7,
          temperatureDc: 657,
          hashrateGhs10: 10759,
          voltageMv: 391,
          frequencyMhz: 650,
          passRateBp: 9987,
        },
      ],
      layout: { entries: [{ index: 7, x: 4, y: 2 }] },
    });

    expect(status.asics[0]).toMatchObject({ index: 7, row: 2, column: 4 });
    expect(status.asicTelemetry[0]).toEqual({
      asicId: "mac:26:B0:3F:F0:D7:92:asic:7",
      hashrate: { value: 1075.9, units: "GH/s" },
      temperature: { value: 65.7, units: "°C" },
      voltage: { value: 0.391, units: "V" },
      frequency: { value: 650, units: "MHz" },
      passRate: { value: 99.87, units: "%" },
    });
  });

  test("converts v2 historical telemetry into canonical units", () => {
    const telemetry = toCanonicalContainerTimeSeries(
      {
        hashrate: {
          unit: "GH/s",
          buckets: [
            { time: "2026-09-02T12:00:00Z", avg: 1_000, min: 900, max: 1_100, count: "1" },
            { time: "2026-09-02T12:05:00Z", avg: 3_000, min: 2_900, max: 3_100, count: "3" },
          ],
        },
      },
      "ct1-a",
      300_000,
    );

    expect(telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate).toMatchObject({
      timeSeries: {
        units: "TH/s",
        points: [
          { value: 1, min: 0.9, max: 1.1, sampleCount: 1 },
          { value: 3, min: 2.9, max: 3.1, sampleCount: 3 },
        ],
        aggregates: { avg: { value: 2.5, units: "TH/s" } },
      },
    });
  });

  test("weights derived efficiency by the source bucket sample counts", () => {
    const telemetry = toCanonicalContainerTimeSeries(
      {
        hashrate: {
          unit: "TH/s",
          buckets: [
            { time: "2026-09-02T12:00:00Z", avg: 1, count: "1" },
            { time: "2026-09-02T12:05:00Z", avg: 3, count: "3" },
          ],
        },
        power: {
          unit: "W",
          buckets: [
            { time: "2026-09-02T12:00:00Z", avg: 10, count: "1" },
            { time: "2026-09-02T12:05:00Z", avg: 60, count: "3" },
          ],
        },
      },
      "ct1-a",
      300_000,
    );

    expect(telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.efficiency).toMatchObject({
      timeSeries: {
        points: [
          { value: 10, sampleCount: 1 },
          { value: 20, sampleCount: 3 },
        ],
        aggregates: { avg: { value: 17.5, units: "J/TH" } },
      },
    });
  });

  test("keeps an unresolved CT1 board position unknown", () => {
    const result = toCanonicalContainer(
      {
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
                        macAddress: "26:B0:3F:F0:D7:92",
                        moduleId: "T1-M01",
                        location: { tank: 1, module: 1 },
                      },
                    },
                  ],
                },
              ],
            },
          ],
        },
      },
      {},
      {},
      "ct1-a",
    );

    if (result.hardware.topology?.kind !== "container") throw new Error("expected container topology");
    expect([...result.hardware.topology.hashboards.values()][0]?.location?.slot).toBeUndefined();
  });

  test("normalizes identifier-scoped history under its hashboard", () => {
    const telemetry = toCanonicalContainerTimeSeries({}, "ct1-a", 300_000, "mean", [
      {
        hashboardId: "mac:26:B0:3F:F0:D7:92" as never,
        responses: {
          power: {
            unit: "kW",
            buckets: [{ time: "2026-09-02T12:00:00Z", avg: 4.5, count: "10" }],
          },
        },
      },
    ]);

    expect(telemetry.hashboards.get("mac:26:B0:3F:F0:D7:92" as never)?.power).toMatchObject({
      timeSeries: { units: "W", points: [{ value: 4_500 }] },
    });
  });

  test("identifies CT1 as a v2 container without introducing another device-type source", () => {
    expect(
      toCanonicalSystemInfo({ info: { id: "ct1-a", version: "2.0.0", product: "CT1", ipAddresses: [] } }),
    ).toMatchObject({
      identity: { deviceId: "device:ct1-a", deviceType: "container", adapterId: "mdk-v2", productName: "CT1" },
      moduleModel: "ct1",
    });
  });

  test("preserves MAC identity and module ownership when ASIC detail enriches shared hardware", () => {
    const normalized = toCanonicalContainer(map, {}, {}, "ct1-a");
    const store = useMinerStore.getState();
    store.resetDeviceData();
    store.hardware.replaceHardware(normalized.hardware);

    const asicId = "mac:26:B0:3F:F0:D7:92:asic:7";
    store.hardware.addHashboard({ serial: "26:B0:3F:F0:D7:92", asicIds: [asicId] });
    store.hardware.batchAddAsics([{ id: asicId, hashboardSerial: "26:B0:3F:F0:D7:92", index: 7, row: 2, column: 4 }]);

    const hardware = useMinerStore.getState().hardware;
    expect(hardware.hashboards.has("serial:26:B0:3F:F0:D7:92" as never)).toBe(false);
    expect(hardware.topology?.hashboards.get("mac:26:B0:3F:F0:D7:92" as never)).toMatchObject({
      moduleId: "container:ct1-a:module:T1-M02",
      identity: { kind: "mac", value: "26:B0:3F:F0:D7:92" },
      asicIds: [asicId],
    });
    expect(hardware.asics.get(asicId as never)?.hashboardId).toBe("mac:26:B0:3F:F0:D7:92");
    expect(hardware.getAsicsByHashboard("26:B0:3F:F0:D7:92")).toEqual([
      expect.objectContaining({ id: asicId, hashboardSerial: "26:B0:3F:F0:D7:92" }),
    ]);
    expect(hardware.getAsicRowsByHashboard("26:B0:3F:F0:D7:92")).toEqual([2]);
  });
});
