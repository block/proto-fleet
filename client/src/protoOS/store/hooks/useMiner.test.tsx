import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { toCanonicalTelemetry } from "../../api/adapters/mdkV1/normalize";
import {
  asComponentId,
  asContainerId,
  asDeviceId,
  asEntityKey,
  asHashboardId,
  asModuleId,
  asTankId,
  type CommandCapabilities,
  type HardwareState,
  type HashboardNode,
  type ModuleNode,
  ROOT_MODULE_ID,
  ROOT_MODULE_KEY,
  type TankNode,
  type TelemetryState,
} from "../contract";
import useMinerStore from "../useMinerStore";
import { createEmptyTelemetryState } from "../utils/createEmptyTelemetryState";
import { getHashboardId } from "../utils/entityIds";
import { getAsicId } from "../utils/getAsicId";
import { aggregateMetricTimeSeries } from "./kpiChartData";
import { useHashboardSerialsByBay, useHashboardsHardware, useMinerHardware } from "./useHardware";
import { useChartDataForMetric, useMiner, useMinerHashboardAsics } from "./useMiner";
import { useErrors } from "./useMinerStatus";
import { useFansTelemetry, useMinerTelemetry } from "./useTelemetry";
import { MetricUnit } from "@/protoOS/api/adapters/mdkV1";

describe("useMiner", () => {
  beforeEach(() => {
    useMinerStore.getState().resetDeviceData();
  });

  it("returns a stable snapshot after normalized hardware and telemetry hydrate", () => {
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
    useMinerStore.getState().telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity: new Map([[ROOT_MODULE_KEY, { hashrate: { latest: { value: 100, units: "TH/s" } } }]]),
      hashboards: new Map(),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    const { result } = renderHook(() => useMiner());

    expect(result.current).toMatchObject({ hashboardSerials: ["HB-1"] });
    expect(result.current?.hashrate?.latest).toEqual({ value: 100, units: "TH/s" });
  });

  it("keeps compatibility hardware and telemetry selector snapshots stable", () => {
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
    useMinerStore.getState().telemetry.updateFanTelemetry(1, {
      rpm: { latest: { value: 4_800, units: "RPM" } },
    });

    const { result } = renderHook(() => ({
      miner: useMinerHardware(),
      hashboards: useHashboardsHardware(),
      telemetry: useMinerTelemetry(),
      fans: useFansTelemetry(),
    }));

    expect(result.current.miner?.hashboardSerials).toEqual(["HB-1"]);
    expect(result.current.hashboards).toHaveLength(1);
    expect(result.current.fans[0]?.rpm?.latest?.value).toBe(4_800);
  });

  it("keeps compatibility error selector snapshots stable", () => {
    useMinerStore.getState().minerStatus.setErrors([
      {
        id: "error-1",
        code: "FanSpeedTooLow",
        message: "Fan 1 is below its minimum speed",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        component: {
          kind: "fan",
          id: asComponentId("fan:1"),
          owner: { kind: "module", id: ROOT_MODULE_ID },
        },
        severity: "critical",
      },
    ]);

    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([
      {
        errorCode: "FanSpeedTooLow",
        message: "Fan 1 is below its minimum speed",
        slot: 1,
        source: "FAN",
      },
    ]);
  });

  it("groups slot-less errors by their preserved source", () => {
    useMinerStore.getState().minerStatus.setErrors([
      {
        id: "error-slotless-fan",
        code: "FanUnknown",
        message: "Fan subsystem unavailable",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        source: { kind: "fan" },
        severity: "critical",
      },
      {
        id: "error-hashboard-2",
        code: "BoardFault",
        message: "Hashboard fault",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        source: { kind: "hashboard", slot: 2 },
        severity: "critical",
      },
    ]);

    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([
      expect.objectContaining({ source: "FAN", slot: undefined }),
      expect.objectContaining({ source: "HASHBOARD", slot: 2 }),
    ]);
  });

  it("maps a PSU-sourced error to the PSU source with its slot", () => {
    useMinerStore.getState().minerStatus.setErrors([
      {
        id: "error-psu-3",
        code: "PsuFault",
        message: "PSU 3 fault",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        source: { kind: "psu", slot: 3 },
        severity: "critical",
      },
    ]);

    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([expect.objectContaining({ source: "PSU", slot: 3 })]);
  });

  it("falls back to the rig source with no slot when an error has neither source nor component", () => {
    useMinerStore.getState().minerStatus.setErrors([
      {
        id: "error-generic",
        code: "SystemFault",
        message: "Something went wrong",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        severity: "critical",
      },
    ]);

    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([expect.objectContaining({ source: "RIG", slot: undefined })]);
  });

  it("returns no errors when the owner map has no entry", () => {
    // resetDeviceData() in beforeEach leaves errorsByOwner empty for the root module.
    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([]);
  });

  it("joins normalized ASIC temperatures to hashboard-status positions", () => {
    const asicId = getAsicId("HB-1", 0);
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1, asicIds: [asicId] }]);
    useMinerStore
      .getState()
      .hardware.batchAddAsics([{ id: asicId, hashboardSerial: "HB-1", index: 0, row: 0, column: 0 }]);
    useMinerStore.getState().telemetry.mergeTelemetry(
      toCanonicalTelemetry({
        timestamp: "2026-08-18T00:00:00Z",
        hashboards: [
          {
            index: 0,
            serial_number: "HB-1",
            efficiency: { value: 30, unit: MetricUnit.JTH },
            hashrate: { value: 0.5, unit: MetricUnit.THS },
            power: { value: 15, unit: MetricUnit.W },
            temperature: { average: 65, inlet: 40, outlet: 70, unit: MetricUnit.ValueC },
            asics: {
              hashrate: { values: [0.5], unit: MetricUnit.THS },
              temperature: { values: [72], unit: MetricUnit.ValueC },
            },
          },
        ],
      }),
    );

    const { result } = renderHook(() => useMinerHashboardAsics("HB-1"));

    expect(result.current).toEqual([
      expect.objectContaining({
        id: "serial:HB-1:asic:0",
        index: 0,
        row: 0,
        column: 0,
        temperature: { latest: { value: 72, units: "°C" } },
      }),
    ]);
  });

  it("uses point timestamps even when bucket metadata is absent", () => {
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
    useMinerStore.getState().telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity: new Map([
        [
          ROOT_MODULE_KEY,
          {
            hashrate: {
              timeSeries: {
                points: [
                  { timestampMs: 1_000, value: 90 },
                  { timestampMs: 2_000, value: 100 },
                ],
              },
            },
          },
        ],
      ]),
      hashboards: new Map(),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    const { result } = renderHook(() => useChartDataForMetric("hashrate"));

    expect(result.current.chartData).toEqual([
      { datetime: 1_000, miner: 90 },
      { datetime: 2_000, miner: 100 },
    ]);
  });

  it.each(["aligned", "misaligned"])("builds container KPI chart series with %s tank histories", (alignment) => {
    const commands: CommandCapabilities = {
      startStop: "unknown",
      powerTarget: "unknown",
      locate: "unknown",
      reboot: "unknown",
      cooling: "unknown",
      logs: "unknown",
      firmwareUpdate: "unknown",
      poolEditing: "unknown",
      passwordManagement: "unknown",
    };
    const containerId = asContainerId("container:ct1-01");
    const tanks = Array.from({ length: 6 }, (_, index) => {
      const slot = index + 1;
      const id = asTankId(`tank:${slot}`);
      return [
        id,
        {
          id,
          containerId,
          capabilities: commands,
          moduleIds: [],
          label: `Tank ${slot}`,
          slot,
        } satisfies TankNode,
      ] as const;
    });
    const hardware: HardwareState = {
      resource: { status: "ready", updatedAtMs: 1_000 },
      topology: {
        kind: "container",
        deviceId: asDeviceId("device:ct1-01"),
        containerId,
        container: {
          id: containerId,
          capabilities: commands,
          tankIds: tanks.map(([id]) => id),
          controlBoardIds: [],
          fanIds: [],
          plantComponentIds: [],
        },
        tanks: new Map(tanks),
        modules: new Map(),
        hashboards: new Map(),
      },
      controlBoards: new Map(),
      hashboards: new Map(),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
    };
    const points = [
      { timestampMs: 1_000, value: 800 },
      { timestampMs: 2_000, value: 810 },
    ];

    const rollupsByEntity: TelemetryState["rollupsByEntity"] = new Map([
      [
        asEntityKey(String(containerId)),
        {
          hashrate: {
            latest: { value: 806.2, units: "TH/s" },
            timeSeries: {
              points,
              bucketSizeMs: 1_000,
              units: "TH/s",
              aggregates: {
                min: { value: 782.4, units: "TH/s" },
                avg: { value: 800.4, units: "TH/s" },
                max: { value: 809.8, units: "TH/s" },
              },
            },
          },
        },
      ],
    ]);
    tanks.forEach(([tankId], index) => {
      const offset = index === 2 ? 18 : index;
      rollupsByEntity.set(asEntityKey(String(tankId)), {
        hashrate: {
          timeSeries: {
            points: points.map((point) => ({ ...point, value: point.value - offset })),
            bucketSizeMs: 1_000,
            aggregates: { avg: { value: 800 - offset, units: "TH/s" } },
          },
        },
      });
    });

    if (alignment === "misaligned") {
      rollupsByEntity.set(asEntityKey(String(tanks[0][0])), {
        hashrate: {
          timeSeries: {
            points: [
              { timestampMs: 3_000, value: 30 },
              { timestampMs: 500, value: 0 },
              { timestampMs: 2_000, value: 20 },
            ],
          },
        },
      });
    }

    useMinerStore.getState().hardware.replaceHardware(hardware);
    useMinerStore.getState().telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity,
      hashboards: new Map(),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    const { result } = renderHook(() => useChartDataForMetric("hashrate"));

    expect(result.current.segmentsLabel).toBe("tanks");
    expect(result.current.series.map(({ label, name }) => ({ label, name }))).toEqual(
      Array.from({ length: 6 }, (_, index) => ({ label: String(index + 1), name: `Tank ${index + 1}` })),
    );
    expect(result.current.chartLines).toEqual(["miner", ...tanks.map(([id]) => String(id))]);
    if (alignment === "misaligned") {
      expect(
        result.current.chartData.map(({ datetime, miner, "tank:1": tankOne }) => ({ datetime, miner, tankOne })),
      ).toEqual([
        { datetime: 500, miner: null, tankOne: 0 },
        { datetime: 1_000, miner: 800, tankOne: null },
        { datetime: 2_000, miner: 810, tankOne: 20 },
        { datetime: 3_000, miner: null, tankOne: 30 },
      ]);
      expect(result.current.xAxisDomain?.[0]).toBe(500);
    } else {
      expect(result.current.chartData[0]).toMatchObject({
        miner: 800,
        "tank:1": 800,
        "tank:3": 782,
        "tank:6": 795,
      });
    }

    const { result: miner } = renderHook(() => useMiner());
    expect(miner.current?.hashboardSerials).toEqual([]);
    expect(miner.current?.hashrate?.latest).toEqual({ value: 806.2, units: "TH/s" });
    expect(miner.current?.hashrate?.timeSeries?.aggregates?.avg).toEqual({ value: 800.4, units: "TH/s" });
  });

  it("derives container tank chart history from hashboard history when tank history is absent", () => {
    const commands: CommandCapabilities = {
      startStop: "unknown",
      powerTarget: "unknown",
      locate: "unknown",
      reboot: "unknown",
      cooling: "unknown",
      logs: "unknown",
      firmwareUpdate: "unknown",
      poolEditing: "unknown",
      passwordManagement: "unknown",
    };
    const containerId = asContainerId("container:ct1-01");
    const tankOneId = asTankId("tank:1");
    const tankTwoId = asTankId("tank:2");
    const moduleOneId = asModuleId("module:1");
    const moduleTwoId = asModuleId("module:2");
    const hashboardOneId = asHashboardId("serial:HB-1");
    const hashboardTwoId = asHashboardId("serial:HB-2");
    const modules = new Map([
      [
        moduleOneId,
        {
          id: moduleOneId,
          profile: "ct1",
          capabilities: { commands },
          tankId: tankOneId,
          hashboardIds: [hashboardOneId],
          controlBoardIds: [],
          psuIds: [],
          fanIds: [],
        } satisfies ModuleNode,
      ],
      [
        moduleTwoId,
        {
          id: moduleTwoId,
          profile: "ct1",
          capabilities: { commands },
          tankId: tankTwoId,
          hashboardIds: [hashboardTwoId],
          controlBoardIds: [],
          psuIds: [],
          fanIds: [],
        } satisfies ModuleNode,
      ],
    ]);
    const hashboards = new Map([
      [
        hashboardOneId,
        {
          id: hashboardOneId,
          identity: { kind: "serial", value: "HB-1" },
          moduleId: moduleOneId,
          asicIds: [],
        } satisfies HashboardNode,
      ],
      [
        hashboardTwoId,
        {
          id: hashboardTwoId,
          identity: { kind: "serial", value: "HB-2" },
          moduleId: moduleTwoId,
          asicIds: [],
        } satisfies HashboardNode,
      ],
    ]);

    useMinerStore.getState().hardware.replaceHardware({
      resource: { status: "ready", updatedAtMs: 1_000 },
      topology: {
        kind: "container",
        deviceId: asDeviceId("device:ct1-01"),
        containerId,
        container: {
          id: containerId,
          capabilities: commands,
          tankIds: [tankOneId, tankTwoId],
          controlBoardIds: [],
          fanIds: [],
          plantComponentIds: [],
        },
        tanks: new Map([
          [
            tankOneId,
            {
              id: tankOneId,
              containerId,
              capabilities: commands,
              moduleIds: [moduleOneId],
              label: "Tank 1",
              slot: 1,
            },
          ],
          [
            tankTwoId,
            {
              id: tankTwoId,
              containerId,
              capabilities: commands,
              moduleIds: [moduleTwoId],
              label: "Tank 2",
              slot: 2,
            },
          ],
        ]),
        modules,
        hashboards,
      },
      controlBoards: new Map(),
      hashboards: new Map(
        [...hashboards.values()].map((board) => [
          board.id,
          {
            ref: { id: board.id, kind: "hashboard", owner: { kind: "module", id: board.moduleId } },
            identity: board.identity,
          },
        ]),
      ),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
    });
    useMinerStore.getState().telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity: new Map(),
      hashboards: new Map([
        [
          hashboardOneId,
          {
            hashboardId: hashboardOneId,
            hashrate: {
              timeSeries: {
                points: [
                  { timestampMs: 1_000, value: 10 },
                  { timestampMs: 2_000, value: 12 },
                ],
                bucketSizeMs: 1_000,
                units: "TH/s",
              },
            },
          },
        ],
        [
          hashboardTwoId,
          {
            hashboardId: hashboardTwoId,
            hashrate: {
              timeSeries: {
                points: [
                  { timestampMs: 1_000, value: 20 },
                  { timestampMs: 2_000, value: 30 },
                ],
                bucketSizeMs: 1_000,
                units: "TH/s",
              },
            },
          },
        ],
      ]),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    const { result } = renderHook(() => useChartDataForMetric("hashrate"));

    expect(result.current.series.map(({ name, averageValue }) => ({ name, averageValue }))).toEqual([
      { name: "Tank 1", averageValue: 11 },
      { name: "Tank 2", averageValue: 25 },
    ]);
    expect(result.current.chartData).toEqual([
      { datetime: 1_000, miner: 30, "tank:1": 10, "tank:2": 20 },
      { datetime: 2_000, miner: 42, "tank:1": 12, "tank:2": 30 },
    ]);

    const moduleSelection = { level: "module" as const, moduleId: moduleOneId };
    const { result: moduleChart } = renderHook(() => useChartDataForMetric("hashrate", moduleSelection));
    expect(moduleChart.current.segmentsLabel).toBe("hashboards");
    expect(moduleChart.current.series).toEqual([
      expect.objectContaining({ key: "HB-1", label: "1", name: "Hashboard 1", averageValue: 11 }),
    ]);
    expect(moduleChart.current.chartData).toEqual([
      { datetime: 1_000, miner: 10, "HB-1": 10 },
      { datetime: 2_000, miner: 12, "HB-1": 12 },
    ]);

    const { result: moduleMiner } = renderHook(() => useMiner(moduleSelection));
    expect(moduleMiner.current?.hashboardSerials).toEqual(["HB-1"]);
    expect(moduleMiner.current?.hashrate?.timeSeries?.aggregates?.avg).toEqual({ value: 11, units: "TH/s" });
  });

  it("builds a multi-series chart from bucketed points, filling gaps and null values with null", () => {
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
    useMinerStore.getState().telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity: new Map([
        [
          ROOT_MODULE_KEY,
          {
            hashrate: {
              timeSeries: {
                bucketSizeMs: 1_000,
                points: [
                  { timestampMs: 10_000, value: 90 },
                  { timestampMs: 11_000, value: 100 },
                  { timestampMs: 12_000, value: null },
                ],
              },
            },
          },
        ],
      ]),
      hashboards: new Map([
        [
          getHashboardId("HB-1"),
          {
            hashboardId: getHashboardId("HB-1"),
            hashrate: {
              timeSeries: {
                bucketSizeMs: 1_000,
                points: [
                  { timestampMs: 10_000, value: 45 },
                  { timestampMs: 11_000, value: null },
                ],
              },
            },
          },
        ],
      ]),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    const { result } = renderHook(() => useChartDataForMetric("hashrate"));

    expect(result.current.chartLines).toEqual(["miner", "HB-1"]);
    expect(result.current.chartData).toEqual([
      { datetime: 10_000, miner: 90, "HB-1": 45 },
      { datetime: 11_000, miner: 100, "HB-1": null },
      { datetime: 12_000, miner: null, "HB-1": null },
    ]);
    expect(result.current.xAxisDomain?.[0]).toBe(10_000);
  });

  it.each([
    {
      name: "a board starting late with an interior gap",
      minerPoints: [
        { timestampMs: 1_000, value: 100 },
        { timestampMs: 2_000, value: 110 },
        { timestampMs: 3_000, value: 120 },
        { timestampMs: 4_000, value: 130 },
      ],
      boardPoints: [
        { timestampMs: 2_000, value: 50 },
        { timestampMs: 4_000, value: 60 },
      ],
      expected: [
        { datetime: 1_000, miner: 100, "HB-1": null },
        { datetime: 2_000, miner: 110, "HB-1": 50 },
        { datetime: 3_000, miner: 120, "HB-1": null },
        { datetime: 4_000, miner: 130, "HB-1": 60 },
      ],
    },
    {
      name: "unordered readings and board-only timestamps, including zero and null",
      minerPoints: [
        { timestampMs: 3_000, value: 120 },
        { timestampMs: 2_000, value: 0 },
      ],
      boardPoints: [
        { timestampMs: 4_000, value: 60 },
        { timestampMs: 2_000, value: null },
        { timestampMs: 1_000, value: 0 },
      ],
      expected: [
        { datetime: 1_000, miner: null, "HB-1": 0 },
        { datetime: 2_000, miner: 0, "HB-1": null },
        { datetime: 3_000, miner: 120, "HB-1": null },
        { datetime: 4_000, miner: null, "HB-1": 60 },
      ],
    },
    {
      name: "board history without miner history",
      minerPoints: [],
      boardPoints: [{ timestampMs: 2_000, value: 50 }],
      expected: [{ datetime: 2_000, miner: null, "HB-1": 50 }],
    },
    {
      name: "miner history with an empty board series",
      minerPoints: [{ timestampMs: 1_000, value: 100 }],
      boardPoints: [],
      expected: [{ datetime: 1_000, miner: 100, "HB-1": null }],
    },
    { name: "empty histories", minerPoints: [], boardPoints: [], expected: [] },
  ])("aligns chart data by timestamp for $name", ({ minerPoints, boardPoints, expected }) => {
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
    const telemetry = createEmptyTelemetryState();
    telemetry.rollupsByEntity.set(ROOT_MODULE_KEY, {
      hashrate: { timeSeries: { points: minerPoints, bucketSizeMs: 1_000 } },
    });
    const hashboardId = getHashboardId("HB-1");
    telemetry.hashboards.set(hashboardId, {
      hashboardId,
      hashrate: { timeSeries: { points: boardPoints, bucketSizeMs: 1_000 } },
    });
    useMinerStore.getState().telemetry.mergeTelemetry(telemetry);

    const { result } = renderHook(() => useChartDataForMetric("hashrate"));

    expect(result.current.chartData).toEqual(expected);
    expect(result.current.xAxisDomain?.[0]).toBe(expected[0]?.datetime);
    expect(
      useMinerStore.getState().telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries?.points,
    ).toEqual(minerPoints);
    expect(useMinerStore.getState().telemetry.hashboards.get(hashboardId)?.hashrate?.timeSeries?.points).toEqual(
      boardPoints,
    );
  });

  it("derives aggregate efficiency history from total power and hashrate", () => {
    const efficiency = aggregateMetricTimeSeries(
      "efficiency",
      [
        { timeSeries: { points: [{ timestampMs: 1_000, value: 100 }], units: "J/TH" } },
        { timeSeries: { points: [{ timestampMs: 1_000, value: 10 }], units: "J/TH" } },
      ],
      [
        {
          power: { timeSeries: { points: [{ timestampMs: 1_000, value: 100 }], units: "W" } },
          hashrate: { timeSeries: { points: [{ timestampMs: 1_000, value: 1 }], units: "TH/s" } },
        },
        {
          power: { timeSeries: { points: [{ timestampMs: 1_000, value: 100 }], units: "W" } },
          hashrate: { timeSeries: { points: [{ timestampMs: 1_000, value: 10 }], units: "TH/s" } },
        },
      ],
    );

    expect(efficiency?.points).toEqual([{ timestampMs: 1_000, value: 200 / 11, sampleCount: 2 }]);
    expect(efficiency?.aggregates?.avg).toEqual({ value: 200 / 11, units: "J/TH" });
  });

  it("derives fallback bays from one-based hashboard slots", () => {
    useMinerStore.getState().hardware.setHashboards([
      { serial: "HB-3", slot: 3 },
      { serial: "HB-4", slot: 4, bay: 2 },
    ]);

    const { result } = renderHook(() => useHashboardSerialsByBay());

    expect(result.current).toEqual({
      1: [null, null, "HB-3"],
      2: ["HB-4", null, null],
    });
  });
});
