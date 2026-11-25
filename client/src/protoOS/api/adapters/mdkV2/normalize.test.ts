import { describe, expect, test } from "vitest";

import {
  type V2GetContainerMapResponse,
  type V2Hashboard,
  V2HashboardErrorCause,
  V2MiningOperatingState,
  V2MiningState,
  V2ModuleNumberingOrder,
  V2Phase,
  V2PoolConnState,
  V2Severity,
} from "./generated/generatedApi";
import {
  toCanonicalContainer,
  toCanonicalContainerTimeSeries,
  toCanonicalErrors,
  toCanonicalHashboardStatus,
  toCanonicalOperationalStatus,
  toCanonicalPools,
  toCanonicalSystemInfo,
} from "./normalize";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

test("preserves container controller details and hashboard serial separately from MAC identity", () => {
  const info = toCanonicalSystemInfo({
    info: { id: "ct1", controlBoard: { boardModel: "CB", serialNumber: "CB-001", machineName: "Primary" } },
  });
  expect(info.controlBoard).toEqual({ model: "CB", serialNumber: "CB-001", machineName: "Primary" });
  const { hardware } = toCanonicalContainer(map, {}, {}, "ct1");
  expect([...hardware.hashboards.values()][0]).toMatchObject({
    identity: { kind: "mac", value: "26:B0:3F:F0:D7:92" },
    serialNumber: "HB-1",
  });
});

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
                    timestamp: new Date().toISOString(),
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

describe("CT1 hashboards without a location", () => {
  const unlocatedBoards: V2Hashboard[] = Array.from({ length: 5 }, (_, index) => ({
    macAddress: `02:00:00:00:00:0${index + 1}`,
    serial: `UNLOCATED-${index + 1}`,
    location: { tank: 0, module: 0, hashboardPosition: 0 },
    miningState: V2MiningState.MINING_STATE_MINING,
    health: {
      hashrate: index + 1,
      power: (index + 1) * 10,
      temperatures: [40 + index],
      errors: index === 0 ? [{ cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_BOARD_OVER_VOLTAGE }] : [],
      warnings: index === 1 ? [{ cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT }] : [],
    },
  }));
  const mixedMap = {
    containerMap: {
      tanks: [
        { id: 0, modules: [{ id: 0, slots: unlocatedBoards.map((hashboard) => ({ hashboard })) }] },
        ...map.containerMap.tanks,
      ],
    },
  };

  test("does not create a tank or module for zero location numbers", () => {
    const { hardware, telemetry } = toCanonicalContainer(mixedMap, {}, {}, "ct1-a");
    if (hardware.topology?.kind !== "container") throw new Error("expected container topology");

    expect([...hardware.topology.tanks.keys()]).toEqual(["container:ct1-a:tank:1"]);
    expect([...hardware.topology.modules.keys()]).toEqual([
      "container:ct1-a:module:T1-M02",
      "container:ct1-a:tank:1:module:3",
    ]);
    expect(hardware.topology.container.tankIds).toEqual(["container:ct1-a:tank:1"]);
    expect(telemetry.rollupsByEntity.has("container:ct1-a:tank:0" as never)).toBe(false);
    expect(telemetry.rollupsByEntity.has("container:ct1-a:tank:0:module:0" as never)).toBe(false);
  });

  test("retains every unlocated board with its identity and telemetry", () => {
    const { hardware, telemetry } = toCanonicalContainer(mixedMap, {}, {}, "ct1-a");
    if (hardware.topology?.kind !== "container") throw new Error("expected container topology");

    expect(hardware.hashboards.size).toBe(6);
    expect(hardware.topology.hashboards.size).toBe(6);
    expect(telemetry.hashboards.size).toBe(6);
    for (const [index, board] of unlocatedBoards.entries()) {
      const id = `mac:${board.macAddress}` as never;
      expect(hardware.hashboards.get(id)).toMatchObject({
        identity: { kind: "mac", value: board.macAddress },
        serialNumber: board.serial,
        ref: { id, kind: "hashboard", owner: { kind: "container", id: "container:ct1-a" } },
      });
      const node = hardware.topology.hashboards.get(id);
      expect(node).toMatchObject({ id, identity: { kind: "mac", value: board.macAddress } });
      expect(node?.moduleId).toBeUndefined();
      expect(node?.location?.slot).toBeUndefined();
      expect(telemetry.hashboards.get(id)).toMatchObject({
        hashrate: { latest: { value: index + 1, units: "TH/s" } },
        power: { latest: { value: (index + 1) * 10, units: "W" } },
        reportedIssueCount: index < 2 ? 1 : 0,
        health: index === 0 ? "fault" : index === 1 ? "degraded" : "ok",
      });
    }
  });

  test("includes unlocated boards in container totals and health", () => {
    const { telemetry } = toCanonicalContainer(mixedMap, {}, {}, "ct1-a");

    for (const id of ["container:ct1-a", ROOT_MODULE_KEY]) {
      expect(telemetry.rollupsByEntity.get(id as never)).toMatchObject({
        hashrate: { latest: { value: 17.5, units: "TH/s" } },
        boardCount: 6,
        onlineBoardCount: 6,
        health: "fault",
      });
    }
  });

  test("retains unlocated board errors under the container", () => {
    const errors = toCanonicalErrors(mixedMap, "ct1-a");

    expect(errors).toHaveLength(2);
    expect(errors).toEqual([
      expect.objectContaining({
        severity: "critical",
        owner: { kind: "container", id: "container:ct1-a" },
        component: {
          kind: "hashboard",
          id: "mac:02:00:00:00:00:01",
          owner: { kind: "container", id: "container:ct1-a" },
        },
      }),
      expect.objectContaining({
        severity: "warning",
        owner: { kind: "container", id: "container:ct1-a" },
        component: {
          kind: "hashboard",
          id: "mac:02:00:00:00:00:02",
          owner: { kind: "container", id: "container:ct1-a" },
        },
      }),
    ]);
  });

  test("preserves real modules and excludes unlocated boards from their totals", () => {
    const { hardware, telemetry } = toCanonicalContainer(mixedMap, {}, {}, "ct1-a");
    if (hardware.topology?.kind !== "container") throw new Error("expected container topology");

    expect(hardware.topology.modules.get("container:ct1-a:module:T1-M02" as never)).toMatchObject({
      tankId: "container:ct1-a:tank:1",
      hashboardIds: ["mac:26:B0:3F:F0:D7:92"],
      location: { tankLabel: "Tank 1", moduleSlot: 2 },
    });
    expect(hardware.topology.hashboards.get("mac:26:B0:3F:F0:D7:92" as never)).toMatchObject({
      moduleId: "container:ct1-a:module:T1-M02",
      location: { slot: 3 },
      powerFeedId: "P1",
    });
    for (const id of ["container:ct1-a:tank:1", "container:ct1-a:module:T1-M02"]) {
      expect(telemetry.rollupsByEntity.get(id as never)).toMatchObject({
        hashrate: { latest: { value: 2.5, units: "TH/s" } },
        boardCount: 1,
        onlineBoardCount: 1,
        health: "ok",
      });
    }
  });
});

describe("MDK v2 CT1 normalization", () => {
  test.each([
    { rates: [60, 40], expected: 100 },
    { rates: [0, 0], expected: 0 },
    { rates: [60, undefined], expected: null },
    { rates: [60, Number.NaN], expected: null },
    { rates: [60, Number.POSITIVE_INFINITY], expected: null },
    { rates: [], expected: null },
  ])("uses complete current board hashrate, not accepted shares: $rates", ({ rates, expected }) => {
    const wire = {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [
              {
                id: 1,
                slots: rates.map((hashrate, index) => ({
                  hashboard: { serial: `HB-${index}`, health: hashrate === undefined ? undefined : { hashrate } },
                })),
              },
            ],
          },
        ],
      },
    };
    useMinerStore.getState().resetDeviceData();
    // A later missing reading must clear the previous complete total.
    for (const snapshot of [map, wire]) {
      const { telemetry } = toCanonicalContainer(snapshot, { hashrate: 900 }, {}, "ct1");
      useMinerStore.getState().telemetry.mergeTelemetry(telemetry);
    }
    expect(useMinerStore.getState().telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.latest).toEqual({
      value: expected,
      units: "TH/s",
    });
    useMinerStore.getState().resetDeviceData();
  });

  test.each([
    [V2MiningState.MINING_STATE_IDLE, "stopped", "ok"],
    [V2MiningState.MINING_STATE_READY, "stopped", "ok"],
    [V2MiningState.MINING_STATE_DISCONNECTED, "unknown", "offline"],
    [V2MiningState.MINING_STATE_MINING, "mining", "fault"],
  ] as const)(
    "interprets disabled rails/clocks in the context of %s",
    (miningState, expectedStatus, expectedHealth) => {
      for (const health of [
        { hashrailOk: false, refclksOk: true },
        { hashrailOk: true, refclksOk: false },
        { hashrailOk: false, refclksOk: false },
      ]) {
        const result = toCanonicalContainer(
          {
            containerMap: {
              tanks: [
                { id: 4, modules: [{ id: 4, slots: [{ hashboard: { macAddress: "board-1", miningState, health } }] }] },
              ],
            },
          },
          {},
          {},
          "ct1-a",
        );
        expect([...result.telemetry.hashboards.values()][0]).toMatchObject({
          health: expectedHealth,
          miningStatus: expectedStatus,
        });
      }
    },
  );

  test.each<[V2MiningState, NonNullable<V2Hashboard["health"]>, "fault" | "degraded"]>([
    [V2MiningState.MINING_STATE_IDLE, { errors: [{}] }, "fault"],
    [V2MiningState.MINING_STATE_IDLE, { warnings: [{}] }, "degraded"],
    [V2MiningState.MINING_STATE_DISCONNECTED, { errors: [{}] }, "fault"],
    [V2MiningState.MINING_STATE_DISCONNECTED, { warnings: [{}] }, "degraded"],
  ])("preserves explicit issues on inactive boards (%s, %j)", (miningState, issues, expectedHealth) => {
    const result = toCanonicalContainer(
      {
        containerMap: {
          tanks: [
            {
              id: 4,
              modules: [
                {
                  id: 4,
                  slots: [
                    {
                      hashboard: {
                        macAddress: "board-1",
                        miningState,
                        health: { hashrailOk: false, refclksOk: false, ...issues },
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
    expect([...result.telemetry.hashboards.values()][0].health).toBe(expectedHealth);
  });

  test("counts each board's reported errors and warnings independently, excluding shutdown history", () => {
    const health: V2Hashboard["health"][] = [
      { errors: [{}], lastShutdownError: {} },
      { warnings: [{}, {}] },
      { errors: [{}, {}], warnings: [{}] },
      { lastShutdownError: {} },
      undefined,
    ];
    const result = toCanonicalContainer(
      {
        containerMap: {
          tanks: [
            {
              id: 1,
              modules: [
                {
                  id: 1,
                  slots: health.map((health, index) => ({
                    hashboard: { macAddress: `board-${index}`, health },
                  })),
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
    expect([...result.telemetry.hashboards.values()].map((board) => board.reportedIssueCount)).toEqual([
      1,
      2,
      3,
      0,
      null,
    ]);

    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().telemetry.mergeTelemetry(result.telemetry);
    const first = [...result.telemetry.hashboards.values()][0];
    result.telemetry.hashboards.set(first.hashboardId, { ...first, reportedIssueCount: null });
    useMinerStore.getState().telemetry.mergeTelemetry(result.telemetry);
    expect(useMinerStore.getState().telemetry.hashboards.get(first.hashboardId)?.reportedIssueCount).toBeNull();
  });

  test.each(["disconnected", "expired", "missing", "invalid", "future"])(
    "excludes %s temperatures from live rollups",
    (reason) => {
      const wire: V2GetContainerMapResponse = structuredClone(map);
      const slots = wire.containerMap!.tanks![0].modules![0].slots!;
      const board = slots[0].hashboard!;
      board.health!.temperatures = [78];
      const health = board.health as NonNullable<V2Hashboard["health"]>;
      health.timestamp = new Date(Date.now() - (reason === "expired" ? 120_000 : 0)).toISOString();
      if (reason === "missing") delete health.timestamp;
      if (reason === "invalid") health.timestamp = "invalid";
      if (reason === "future") health.timestamp = new Date(Date.now() + 60_000).toISOString();
      if (reason === "disconnected") board.miningState = V2MiningState.MINING_STATE_DISCONNECTED;
      const check = (expected: number | null) => {
        const { telemetry } = toCanonicalContainer(wire, { averageHashboardTemperatureC: 78 }, {}, "ct1");
        for (const rollup of telemetry.rollupsByEntity.values()) {
          if (rollup.boardCount === 0) continue;
          expect(rollup.temperature?.latest?.value).toBe(expected);
          expect(rollup.minimumTemperature?.latest?.value).toBe(expected);
          expect(rollup.maximumTemperature?.latest?.value).toBe(expected);
        }
      };
      check(null);
      slots.push({
        hashboard: {
          ...board,
          macAddress: "current",
          miningState: V2MiningState.MINING_STATE_IDLE,
          health: { ...board.health, timestamp: new Date().toISOString(), temperatures: [40] },
        } as typeof board,
        powerFeedId: "P1",
      });
      check(40);
    },
  );

  test("preserves tank layout and rolls up actual temperature extrema", () => {
    const result = toCanonicalContainer(
      {
        containerMap: {
          tanks: [
            {
              ...map.containerMap.tanks[0],
              moduleLayout: {
                rows: 2,
                columns: 8,
                numbering: V2ModuleNumberingOrder.MODULE_NUMBERING_ORDER_COLUMN_MAJOR,
              },
            },
          ],
        },
      },
      {},
      {},
      "ct1-a",
    );
    if (result.hardware.topology?.kind !== "container") throw new Error("expected container");
    expect([...result.hardware.topology.tanks.values()][0].moduleLayout).toEqual({
      rows: 2,
      columns: 8,
      numbering: "columnMajor",
    });
    expect(result.telemetry.rollupsByEntity.get("container:ct1-a:tank:1" as never)).toMatchObject({
      minimumTemperature: { latest: { value: 60, units: "°C" } },
      maximumTemperature: { latest: { value: 64, units: "°C" } },
    });
  });

  test.each([
    { readings: [60, 80, 100], expected: 80 },
    { readings: [0, 80, undefined], expected: 40 },
    { readings: [undefined, Number.NaN, Infinity], expected: null },
    { readings: [60, null, "80"], expected: 60 },
  ])("rolls up only available ASIC averages within each tank: $readings", ({ readings, expected }) => {
    const board = (id: string, temperature: unknown) => ({
      macAddress: id,
      health: {
        timestamp: new Date().toISOString(),
        temperatures: [20, 30],
        averageAsicTemperatureC: temperature as number | undefined,
      },
    });
    const { telemetry } = toCanonicalContainer(
      {
        containerMap: {
          tanks: [
            {
              id: 1,
              modules: [
                { id: 1, slots: readings.slice(0, 2).map((value, i) => ({ hashboard: board(`a-${i}`, value) })) },
                { id: 2, slots: [{ hashboard: board("b", readings[2]) }] },
              ],
            },
            { id: 2, modules: [{ id: 1, slots: [{ hashboard: board("c", 120) }] }] },
          ],
        },
      },
      {},
      {},
      "ct1-a",
    );
    expect(telemetry.rollupsByEntity.get("container:ct1-a:tank:1" as never)).toMatchObject({
      temperature: { latest: { value: 25, units: "°C" } },
      averageAsicTemperature: { latest: { value: expected, units: "°C" } },
    });
    expect(telemetry.rollupsByEntity.get("container:ct1-a:tank:2" as never)?.averageAsicTemperature).toEqual({
      latest: { value: 120, units: "°C" },
    });
  });

  test("missing board measurements clear old rollups rather than becoming zero or retaining stale values", () => {
    const populated = toCanonicalContainer(map, {}, {}, "ct1-a");
    const empty = toCanonicalContainer(
      {
        containerMap: {
          tanks: [{ id: 1, modules: [{ id: 2, slots: [{ hashboard: { macAddress: "26:B0:3F:F0:D7:92" } }] }] }],
        },
      },
      {},
      {},
      "ct1-a",
    );
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().telemetry.mergeTelemetry(populated.telemetry);
    useMinerStore.getState().telemetry.mergeTelemetry(empty.telemetry);
    expect(useMinerStore.getState().telemetry.rollupsByEntity.get("container:ct1-a:tank:1" as never)).toMatchObject({
      hashrate: { latest: { value: null } },
      power: { latest: { value: null } },
      minimumTemperature: { latest: { value: null } },
      maximumTemperature: { latest: { value: null } },
      averageAsicTemperature: { latest: { value: null } },
    });
  });

  test.each([
    [V2MiningState.MINING_STATE_IDLE, "stopped", "ok"],
    [V2MiningState.MINING_STATE_READY, "stopped", "ok"],
    [V2MiningState.MINING_STATE_DISCONNECTED, "unknown", "offline"],
    [V2MiningState.MINING_STATE_ERROR, "error", "fault"],
  ])("retains board health and mining state for %s", (state, expectedStatus, expectedHealth) => {
    const result = toCanonicalContainer(
      {
        containerMap: {
          tanks: [
            {
              id: 1,
              modules: [
                { id: 1, slots: [{ hashboard: { macAddress: "board-1", miningState: state as V2MiningState } }] },
              ],
            },
          ],
        },
      },
      {},
      {},
      "ct1-a",
    );
    expect([...result.telemetry.hashboards.values()][0]).toMatchObject({
      health: expectedHealth,
      miningStatus: expectedStatus,
    });
  });
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

  test("honors per-pool activity in weighted mode without enabling disabled pools", () => {
    const pools = toCanonicalPools(
      {
        pools: [
          { id: "1", enabled: true },
          { id: "2", enabled: true },
          { id: "3", enabled: false },
          { id: "4", enabled: true },
        ],
      },
      {
        weightedMode: true,
        activePoolId: "1",
        pools: [
          { poolId: "1", connState: V2PoolConnState.POOL_CONN_STATE_ACTIVE, isActive: true },
          { poolId: "2", connState: V2PoolConnState.POOL_CONN_STATE_IDLE, isActive: true },
          { poolId: "3", connState: V2PoolConnState.POOL_CONN_STATE_IDLE, isActive: true },
          { poolId: "4", connState: V2PoolConnState.POOL_CONN_STATE_IDLE, isActive: false },
        ],
      },
    );
    expect(pools.map(({ status }) => status)).toEqual(["Active", "Active", "Disabled", "Idle"]);
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
        plant: {
          totalPowerW: 45,
          facilityPowerW: 48,
          pue: 1.1,
          flowRateLpm: 12,
          coolantPumpSpeedHz: 50,
          pumpInletPressurePa: 194_430,
          heatRejectionActive: true,
          inletTemperatureC: 35,
          outletATemperatureC: 48,
          outletBTemperatureC: 42,
          tankALiquidLevelMm: 480,
          tankBLiquidLevelMm: 460,
        },
        controls: [{ name: "dry_cooler_auto", enabled: true, stateKnown: true }],
        setpoints: [{ name: "dry_cooler_auto_temp_c", value: 18, unit: "°C" }],
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
      hashrate: { latest: { value: 2.5, units: "TH/s" } },
      temperature: { latest: { value: 62, units: "°C" } },
      averageAsicTemperature: { latest: { value: 66, units: "°C" } },
      boardCount: 1,
      onlineBoardCount: 1,
    });
    expect(result.telemetry.plant.get("container:ct1-a:plant" as never)).toMatchObject({
      totalPower: { latest: { value: 45, units: "W" } },
      pue: { latest: { value: 1.1, units: "ratio" } },
      flowRate: { latest: { value: 12, units: "L/min" } },
      coolantPumpSpeed: { latest: { value: 50, units: "Hz" } },
      pumpInletPressure: { latest: { value: 194_430, units: "Pa" } },
      heatRejectionActive: true,
      controls: [{ name: "dry_cooler_auto", enabled: true, stateKnown: true }],
      setpoints: [{ name: "dry_cooler_auto_temp_c", value: 18, unit: "°C" }],
      inletTemperature: { latest: { value: 35, units: "°C" } },
      outletATemperature: { latest: { value: 48, units: "°C" } },
      outletBTemperature: { latest: { value: 42, units: "°C" } },
      tankALiquidLevel: { latest: { value: 480, units: "mm" } },
      tankBLiquidLevel: { latest: { value: 460, units: "mm" } },
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

  test.each([undefined, null, "", "12", false, Number.NaN, Infinity, 0])(
    "normalizes optional plant sensors without inventing readings: %s",
    (value) => {
      // The generated client's static types do not validate device JSON at runtime.
      const sensor = value as number | undefined;
      const { telemetry } = toCanonicalContainer(
        map,
        {},
        {
          plant: {
            flowRateLpm: sensor,
            inletTemperatureC: sensor,
            outletTemperatureC: sensor,
            totalPowerW: sensor,
            outletATemperatureC: sensor,
            outletBTemperatureC: sensor,
            tankALiquidLevelMm: sensor,
            tankBLiquidLevelMm: sensor,
            ambientTemperatureC: sensor,
            dryCoolerInletTemperatureC: sensor,
            coolantPumpSpeedHz: sensor,
            pumpInletPressurePa: sensor,
            pumpOutletPressurePa: sensor,
            pumpPressureDiffPa: sensor,
            heatRejectionInletPressurePa: sensor,
            heatRejectionOutletPressurePa: sensor,
            heatRejectionPressureDiffPa: sensor,
          },
        },
        "ct1-a",
      );
      const plant = telemetry.plant.get("container:ct1-a:plant" as never)!;
      for (const metric of [
        plant.flowRate,
        plant.inletTemperature,
        plant.outletTemperature,
        plant.totalPower,
        plant.outletATemperature,
        plant.outletBTemperature,
        plant.tankALiquidLevel,
        plant.tankBLiquidLevel,
        plant.ambientTemperature,
        plant.dryCoolerInletTemperature,
        plant.coolantPumpSpeed,
        plant.pumpInletPressure,
        plant.pumpOutletPressure,
        plant.pumpPressureDelta,
        plant.heatRejectionInletPressure,
        plant.heatRejectionOutletPressure,
        plant.heatRejectionPressureDelta,
      ]) {
        expect(metric?.latest?.value).toBe(value === 0 ? 0 : null);
      }
    },
  );

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

describe("plant component faults", () => {
  test("reports a fault only for a non-zero code or error bitmap", () => {
    const { hardware, telemetry } = toCanonicalContainer(
      { containerMap: { tanks: [] } },
      {},
      {
        powerDistributions: [{ componentId: "pdu-1" }, { componentId: "pdu-2" }, { componentId: "pdu-3" }],
        coolingFans: [{ componentId: "heat-rejection-fan-1" }, { componentId: "heat-rejection-fan-2" }],
        faults: [
          { componentId: "pdu-1", code: 0 },
          { componentId: "pdu-2", code: 7 },
          { componentId: "pdu-3", code: 0, errorBitmap: 4 },
          { componentId: "heat-rejection-fan-1", code: 0, errorBitmap: 0 },
          { componentId: "heat-rejection-fan-2", code: 3 },
        ],
      },
      "ct1-a",
    );
    const health = (name: string) =>
      [...hardware.plant.entries()]
        .filter(([, component]) => component.name === name)
        .map(([id]) => telemetry.plant.get(id)?.health)[0];

    expect(["pdu-1", "pdu-2", "pdu-3"].map(health)).toEqual(["ok", "fault", "fault"]);
    expect([...telemetry.fans.values()].map((fan) => fan.health)).toEqual(["ok", "fault"]);
  });
});

describe("cooling fan power telemetry", () => {
  const fans = (health: Parameters<typeof toCanonicalContainer>[2]) => [
    ...toCanonicalContainer({ containerMap: { tanks: [] } }, {}, health, "ct1-a").telemetry.fans.values(),
  ];

  test("maps measured fan power with the snapshot observation time", () => {
    const [fan] = fans({
      timestamp: "2026-11-14T12:00:00Z",
      coolingFans: [{ componentId: "dry-cooler-1", powerW: 412.5, speedPercent: 75 }],
    });

    expect(fan).toMatchObject({
      power: { latest: { value: 412.5, units: "W" } },
      observedAtMs: Date.parse("2026-11-14T12:00:00Z"),
    });
  });

  test("keeps a measured zero and clears a missing or invalid reading", () => {
    const readings = fans({
      coolingFans: [
        { componentId: "dry-cooler-1", powerW: 0 },
        { componentId: "dry-cooler-2" },
        { componentId: "dry-cooler-3", powerW: Number.NaN },
      ],
    });

    expect(readings.map((fan) => fan.power?.latest?.value)).toEqual([0, null, null]);
    expect(readings.every((fan) => fan.observedAtMs === undefined)).toBe(true);
  });

  test("clears an earlier power reading once a later snapshot omits it", () => {
    const store = useMinerStore.getState();
    store.resetDeviceData();
    const snapshot = (powerW?: number) =>
      toCanonicalContainer(
        { containerMap: { tanks: [] } },
        {},
        { coolingFans: [{ componentId: "dry-cooler-1", powerW }] },
        "ct1-a",
      ).telemetry;

    store.telemetry.mergeTelemetry(snapshot(400));
    store.telemetry.mergeTelemetry(snapshot());

    const [fan] = useMinerStore.getState().telemetry.fans.values();
    expect(fan?.power?.latest?.value).toBeNull();
  });
});

describe("plant alarm telemetry", () => {
  const plantTelemetry = (health: Parameters<typeof toCanonicalContainer>[2]) =>
    [...toCanonicalContainer({ containerMap: { tanks: [] } }, {}, health, "ct1-a").telemetry.plant.values()][0];

  test("carries the alarm list, flags, mute narrowing and the controller observation time", () => {
    const plant = plantTelemetry({
      timestamp: "2026-11-14T12:00:00Z",
      alarmMuteSupported: false,
      alarms: [
        { id: "a1", name: " Coolant low pressure ", severity: V2Severity.SEVERITY_WARNING, componentId: "pump-1" },
        { id: "a2", severity: V2Severity.SEVERITY_CRITICAL },
        { name: "No identity", severity: V2Severity.SEVERITY_UNSPECIFIED },
      ],
      plant: { alarmActive: true, criticalAlarmActive: true, buzzerActive: false },
    });
    expect(plant).toMatchObject({
      alarmActive: true,
      criticalAlarmActive: true,
      buzzerActive: false,
      alarmMuteSupported: false,
      observedAtMs: Date.parse("2026-11-14T12:00:00Z"),
      alarms: [
        { id: "a1", name: "Coolant low pressure", severity: "warning", componentId: "pump-1" },
        { id: "a2", name: "a2", severity: "critical", componentId: undefined },
        { id: undefined, name: "No identity", severity: "unknown", componentId: undefined },
      ],
    });
  });

  test("keeps unreported flags and a missing observation time unknown", () => {
    for (const timestamp of [undefined, "not a time"]) {
      const plant = plantTelemetry({ timestamp, plant: {} });
      expect(plant.observedAtMs).toBeUndefined();
      expect(plant.alarmActive).toBeUndefined();
      expect(plant.buzzerActive).toBeUndefined();
      expect(plant.alarmMuteSupported).toBeUndefined();
      expect(plant.alarms).toEqual([]);
    }
  });
});

test("preserves disconnection separately from faults and excludes PSU reports from board severity", () => {
  const { telemetry } = toCanonicalContainer(
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
                      serial: "offline",
                      miningState: V2MiningState.MINING_STATE_DISCONNECTED,
                      health: { errors: [{ cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_UNKNOWN_ERROR }] },
                    },
                  },
                  {
                    hashboard: {
                      serial: "psu",
                      miningState: V2MiningState.MINING_STATE_MINING,
                      health: { errors: [{ cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY }] },
                    },
                  },
                  {
                    hashboard: {
                      serial: "warning",
                      miningState: V2MiningState.MINING_STATE_MINING,
                      health: { warnings: [{ cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_UNKNOWN_ERROR }] },
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
    "ct1",
  );
  expect([...telemetry.hashboards.values()]).toEqual([
    expect.objectContaining({ disconnected: true, faultSeverity: "critical" }),
    expect.objectContaining({ disconnected: false, faultSeverity: null }),
    expect.objectContaining({ disconnected: false, faultSeverity: "warning" }),
  ]);
});

test("keeps an explicit unspecified operating state unknown rather than a warning", () => {
  const { telemetry } = toCanonicalContainer(
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
                    hashboard: { serial: "unknown", miningState: V2MiningState.MINING_STATE_UNSPECIFIED },
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
    "ct1",
  );
  expect([...telemetry.hashboards.values()][0]).toMatchObject({
    health: "unknown",
    miningStatus: "unknown",
    faultSeverity: null,
    disconnected: false,
  });
});
