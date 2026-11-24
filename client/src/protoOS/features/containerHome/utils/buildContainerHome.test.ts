import { beforeEach, describe, expect, it } from "vitest";
import {
  buildContainerLive,
  efficiencyHistory,
  historyPoints,
  sumHistory,
  temperatureValue,
  toHomeSeries,
} from "./buildContainerHome";
import { toCanonicalContainer, toCanonicalContainerTelemetry } from "@/protoOS/api/adapters/mdkV2/normalize";
import { asEntityKey, type MetricTimeSeries } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";

const series = (points: [number, number | null][]): MetricTimeSeries => ({
  units: "W",
  bucketSizeMs: 60_000,
  points: points.map(([timestampMs, value]) => ({ timestampMs, value })),
});

const moduleSnapshot = (readings: { power?: number; temperatures?: number[] }[]) =>
  toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: readings.map(({ power, temperatures }, index) => ({
              id: index + 1,
              slots: [1, 2, 3].map((slot) => ({
                hashboard: {
                  serial: `module-${index + 1}-board-${slot}`,
                  health: { temperatures, psuElectrical: { inputPower: power } },
                },
              })),
            })),
          },
        ],
      },
    },
    {},
    {},
    "ct1",
  );

const moduleReadings = (readings: ReturnType<typeof buildContainerLive>["powerReadings"]) =>
  ["high", "low", "average"].map((stat) => readings.find(({ id }) => id === `module-${stat}`)?.value);

const hashboardReadings = (readings: ReturnType<typeof buildContainerLive>["temperatureReadings"]) =>
  ["high", "low", "average"].map((stat) => readings.find(({ id }) => id === `hashboard-${stat}`)?.value);

describe("Home live module summaries", () => {
  it("summarizes module power and current hashboard sensor extrema and average", () => {
    const { hardware, telemetry } = moduleSnapshot([
      { power: 1000, temperatures: [10, 30] },
      { power: 3000, temperatures: [50] },
      { power: 8000, temperatures: [80, 100] },
    ]);
    const live = buildContainerLive(hardware, telemetry, "C");
    expect(moduleReadings(live.powerReadings)).toEqual(["8.0 kW", "1.0 kW", "4.0 kW"]);
    expect(hashboardReadings(live.temperatureReadings)).toEqual(["100.0 °C", "10.0 °C", "54.0 °C"]);
  });

  it("includes zero readings and converts temperature summaries to Fahrenheit", () => {
    const { hardware, telemetry } = moduleSnapshot([
      { power: 0, temperatures: [0] },
      { power: 2000, temperatures: [20] },
    ]);
    const live = buildContainerLive(hardware, telemetry, "F");
    expect(moduleReadings(live.powerReadings)).toEqual(["2.0 kW", "0.0 kW", "1.0 kW"]);
    expect(hashboardReadings(live.temperatureReadings)).toEqual(["68.0 °F", "32.0 °F", "50.0 °F"]);
  });

  it.each(["empty module", "removed module"])("clears obsolete rollups after a full map reports: %s", (transition) => {
    useMinerStore.getState().resetDeviceData();
    const snapshot = (populated: boolean) =>
      toCanonicalContainer(
        {
          containerMap: {
            tanks: [
              {
                id: 1,
                modules:
                  !populated && transition === "removed module"
                    ? []
                    : [
                        {
                          id: 1,
                          slots: populated
                            ? [
                                {
                                  hashboard: {
                                    serial: "board-1",
                                    moduleId: "T1-M01",
                                    health: { temperatures: [20], psuElectrical: { inputPower: 4000 } },
                                  },
                                },
                              ]
                            : [],
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
    const initial = snapshot(true);
    const moduleId = asEntityKey([...initial.hardware.topology!.modules.keys()][0]);
    const history = series([[0, 3500]]);
    initial.telemetry.rollupsByEntity.get(moduleId)!.power!.timeSeries = history;
    useMinerStore.getState().hardware.replaceHardware(initial.hardware);
    const merge = useMinerStore.getState().telemetry.mergeTelemetry;
    const live = () => buildContainerLive(initial.hardware, useMinerStore.getState().telemetry, "C");
    const expectPopulated = () => {
      expect(moduleReadings(live().powerReadings)).toEqual(["4.0 kW", "4.0 kW", "4.0 kW"]);
      expect(hashboardReadings(live().temperatureReadings)).toEqual(["20.0 °C", "20.0 °C", "20.0 °C"]);
    };
    const expectUnavailable = () => {
      expect(moduleReadings(live().powerReadings)).toEqual(["—", "—", "—"]);
      expect(hashboardReadings(live().temperatureReadings)).toEqual(["—", "—", "—"]);
      expect(useMinerStore.getState().telemetry.rollupsByEntity.get(moduleId)?.power?.timeSeries).toEqual(history);
    };
    merge(initial.telemetry, 1);
    expectPopulated();
    // Neither miner-only polling, history-only updates, nor another container's
    // full map claims this container's module inventory.
    merge(toCanonicalContainerTelemetry({}, {}, "ct1"), 2);
    merge(createEmptyTelemetryState(), 3);
    merge(toCanonicalContainer({ containerMap: { tanks: [] } }, {}, {}, "ct2").telemetry, 4);
    expectPopulated();

    const empty = snapshot(false).telemetry;
    expect(empty.rollupsByEntity.has(moduleId)).toBe(false);
    merge(empty, 6);
    expectUnavailable();
    merge(initial.telemetry, 5);
    expectUnavailable();
    merge(initial.telemetry, 7);
    expectPopulated();
    merge(empty, 6);
    expectPopulated();
  });

  it("clears incomplete summaries on refresh without hiding the other metric", () => {
    useMinerStore.getState().resetDeviceData();
    const initial = moduleSnapshot([
      { power: 1000, temperatures: [20] },
      { power: 3000, temperatures: [40] },
    ]);
    useMinerStore.getState().hardware.replaceHardware(initial.hardware);
    const merge = useMinerStore.getState().telemetry.mergeTelemetry;
    const live = () => buildContainerLive(initial.hardware, useMinerStore.getState().telemetry, "C");
    merge(initial.telemetry, 1);
    expect(moduleReadings(live().powerReadings)).toEqual(["3.0 kW", "1.0 kW", "2.0 kW"]);
    merge(moduleSnapshot([{ power: 1000, temperatures: [20] }, { temperatures: [40] }]).telemetry, 2);
    expect(moduleReadings(live().powerReadings)).toEqual(["—", "—", "—"]);
    expect(hashboardReadings(live().temperatureReadings)).toEqual(["40.0 °C", "20.0 °C", "30.0 °C"]);
    merge(moduleSnapshot([{ power: 1000, temperatures: [20] }, { power: 3000 }]).telemetry, 3);
    expect(hashboardReadings(live().temperatureReadings)).toEqual(["20.0 °C", "20.0 °C", "20.0 °C"]);
    expect(moduleReadings(live().powerReadings)).toEqual(["3.0 kW", "1.0 kW", "2.0 kW"]);
  });
});

describe("container Home live refresh", () => {
  beforeEach(() => useMinerStore.getState().resetDeviceData());

  it("preserves PDU history and other containers when a complete plant snapshot omits readings", () => {
    const initial = toCanonicalContainerTelemetry(
      {},
      { powerDistributions: [{ componentId: "pdu-a", activePowerW: 2000 }] },
      "ct1",
    );
    const pdu = [...initial.plant.values()].find((component) => component.totalPower?.latest?.value === 2000)!;
    const history = series([[0, 1500]]);
    pdu.totalPower!.timeSeries = history;
    const other = toCanonicalContainerTelemetry(
      {},
      { powerDistributions: [{ componentId: "pdu-b", activePowerW: 3000 }] },
      "ct2",
    );
    const otherId = [...other.plant.values()].find(
      (component) => component.totalPower?.latest?.value === 3000,
    )!.componentId;
    const merge = useMinerStore.getState().telemetry.mergeTelemetry;
    merge(initial, 1);
    merge(other, 2);
    // A partial/history-only update does not claim a complete plant inventory.
    merge(createEmptyTelemetryState(), 3);
    expect(useMinerStore.getState().telemetry.plant.get(pdu.componentId)?.totalPower?.latest?.value).toBe(2000);
    merge(toCanonicalContainerTelemetry({}, {}, "ct1"), 4);
    const telemetry = useMinerStore.getState().telemetry;
    expect(telemetry.plant.get(pdu.componentId)?.totalPower?.latest?.value).toBeNull();
    expect(telemetry.plant.get(pdu.componentId)?.totalPower?.timeSeries).toEqual(history);
    expect(telemetry.plant.get(otherId)?.totalPower?.latest?.value).toBe(3000);
  });

  it.each([
    { label: "omitted distributions", snapshot: {} },
    { label: "empty distributions", snapshot: { powerDistributions: [] } },
    { label: "removed distribution", snapshot: { powerDistributions: [{ componentId: "pdu-b", activePowerW: 500 }] } },
    { label: "omitted power", snapshot: { powerDistributions: [{ componentId: "pdu-a" }] } },
  ])("clears PDU power after $label and rejects stale restoration", ({ snapshot }) => {
    const initial = toCanonicalContainer(
      { containerMap: { tanks: [] } },
      {},
      { powerDistributions: [{ componentId: "pdu-a", activePowerW: 2000 }] },
      "ct1",
    );
    useMinerStore.getState().hardware.replaceHardware(initial.hardware);
    const merge = useMinerStore.getState().telemetry.mergeTelemetry;
    const reading = (name: string) => {
      const { hardware, telemetry } = useMinerStore.getState();
      const component = [...hardware.plant.values()].find((item) => item.kind === "distribution" && item.name === name);
      return component ? telemetry.plant.get(component.ref.id)?.totalPower?.latest?.value : undefined;
    };
    merge(initial.telemetry, 1);
    expect(reading("pdu-a")).toBe(2000);
    merge(toCanonicalContainerTelemetry({}, snapshot, "ct1"), 3);
    expect(reading("pdu-a")).toBe(null);
    if (snapshot.powerDistributions?.[0]?.componentId === "pdu-b") expect(reading("pdu-b")).toBe(500);
    merge(initial.telemetry, 2);
    expect(reading("pdu-a")).toBe(null);
    merge(
      toCanonicalContainerTelemetry({}, { powerDistributions: [{ componentId: "pdu-a", activePowerW: 0 }] }, "ct1"),
      4,
    );
    expect(reading("pdu-a")).toBe(0);
    merge(initial.telemetry, 5);
    expect(reading("pdu-a")).toBe(2000);
    merge(toCanonicalContainerTelemetry({}, {}, "ct1"), 2);
    expect(reading("pdu-a")).toBe(2000);
  });

  it.each([
    { label: "omitted facility power", snapshot: { plant: {} } },
    { label: "omitted plant", snapshot: {} },
    { label: "invalid facility power", snapshot: { plant: { facilityPowerW: Number.NaN } } },
  ])("clears stale power and efficiency after $label, then accepts zero and restored readings", ({ snapshot }) => {
    const initial = toCanonicalContainer(
      undefined,
      { power: 1500, hashrate: 100 },
      { plant: { facilityPowerW: 2000 } },
      "ct1",
    );
    const live = () => buildContainerLive(initial.hardware, useMinerStore.getState().telemetry, "C");
    const merge = useMinerStore.getState().telemetry.mergeTelemetry;
    merge(initial.telemetry, 1);
    expect(live().power).toBe(2000);
    expect(live().efficiency).toBe(20);

    merge(toCanonicalContainerTelemetry({ power: 1500, hashrate: 50 }, snapshot, "ct1"), 2);
    expect(live().hashrate).toBe(50);
    expect(live().power).toBeNull();
    expect(live().efficiency).toBeNull();
    expect(live().powerReadings.find(({ id }) => id === "container-power")?.value).toBe("—");
    expect(live().powerReadings.find(({ id }) => id === "container-efficiency")?.value).toBe("—");

    merge(toCanonicalContainerTelemetry({ hashrate: 50 }, { plant: { facilityPowerW: 0 } }, "ct1"), 3);
    expect(live().power).toBe(0);
    expect(live().efficiency).toBe(0);

    merge(toCanonicalContainerTelemetry({ hashrate: 50 }, { plant: { facilityPowerW: 2500 } }, "ct1"), 4);
    expect(live().power).toBe(2500);
    expect(live().efficiency).toBe(50);
  });
});

describe("container Home mapping", () => {
  it("uses facility power for container efficiency and rejects unknown module power coverage", () => {
    const state = toCanonicalContainer(
      undefined,
      { power: 1500, hashrate: 100 },
      {
        plant: { facilityPowerW: 2000, totalPowerW: 1500 },
        powerDistributions: [{ componentId: "PDU west", activePowerW: 0 }],
      },
      "ct1",
    );
    const live = buildContainerLive(state.hardware, state.telemetry, "C");
    expect(live.power).toBe(2000);
    expect(live.efficiency).toBe(20);
    expect(live.powerReadings.some((reading) => reading.label === "PDU west")).toBe(false);
    expect(live.powerReadings.find((reading) => reading.label === "Module efficiency")?.value).toBe("—");
    expect(live.powerLocations).toHaveLength(0);
  });

  it("excludes cooling overhead from module efficiency and divides combined power by combined hashrate", () => {
    // Two boards: 1000 W / 80 TH/s and 500 W / 20 TH/s. Their combined
    // efficiency is 15 J/TH, not the arithmetic mean of 12.5 and 25 J/TH.
    const state = toCanonicalContainer(
      undefined,
      { power: 1500, hashrate: 100 },
      { plant: { facilityPowerW: 2000 }, coolingFans: [{ componentId: "fan", powerW: 100 }] },
      "ct1",
    );
    const topology = state.hardware.topology;
    if (topology?.kind !== "container") throw new Error("Expected container fixture");
    const rollup = state.telemetry.rollupsByEntity.get(asEntityKey(topology.containerId))!;
    rollup.powerCoverage = { kind: "hashboardOnly", includesNonHashboardLoads: false, methods: ["firmwareAggregate"] };
    const live = buildContainerLive(state.hardware, state.telemetry, "C");
    expect(live.efficiency).toBe(20);
    expect(live.moduleEfficiency).toBe(15);
    expect(live.powerReadings.find((reading) => reading.id === "module-efficiency")?.value).toBe("15.0 J/TH");
  });

  it("does not substitute mining or plant power when overhead-inclusive power is absent", () => {
    const state = toCanonicalContainer(
      undefined,
      { power: 1500, hashrate: 100 },
      { plant: { totalPowerW: 1500, heatRejectionPowerW: 500 } },
      "ct1",
    );
    const live = buildContainerLive(state.hardware, state.telemetry, "C");
    expect(live.power).toBeNull();
    expect(live.efficiency).toBeNull();
    expect(live.moduleEfficiency).toBeNull();
  });

  it("keeps absent readings and zero-hashrate efficiency unavailable", () => {
    const state = toCanonicalContainer({ containerMap: { tanks: [] } }, { power: 1500, hashrate: 0 }, {}, "ct1");
    const live = buildContainerLive(state.hardware, state.telemetry, "F");
    expect(live.efficiency).toBeNull();
    expect(live.moduleEfficiency).toBeNull();
    expect(live.temperatureReadings.every((reading) => reading.value === "—")).toBe(true);
    expect(temperatureValue(0, "F")).toBe(32);
    expect(temperatureValue(-40, "F")).toBe(-40);
  });

  it("inserts gaps, filters the selected period and does not bridge outages", () => {
    expect(
      historyPoints(
        series([
          [0, 10],
          [60_000, 11],
          [240_000, 0],
          [300_000, 12],
        ]),
        [60_000, 300_000],
      ),
    ).toEqual([
      { datetime: 60_000, value: 11 },
      { datetime: 120_000, value: null },
      { datetime: 240_000, value: 0 },
    ]);
  });

  it("marks a missing final bucket unavailable without discarding earlier history or aggregates", () => {
    const input = series([[0, 10]]);
    input.aggregates = { avg: { value: 10, units: "W" } };
    const result = toHomeSeries("power", "Power", "orange", input, [0, 180_000], "W");
    expect(result.points).toEqual([
      { datetime: 0, value: 10 },
      { datetime: 120_000, value: null },
    ]);
    expect(result.latest).toBe("—");
    expect(result.average).toBe("10.0 W");
  });

  it("uses the last completed bucket and excludes a point at the requested end", () => {
    const result = toHomeSeries(
      "power",
      "Power",
      "orange",
      series([
        [120_000, 0],
        [180_000, 99],
      ]),
      [0, 180_000],
      "W",
    );
    expect(result.points).toEqual([{ datetime: 120_000, value: 0 }]);
    expect(result.latest).toBe("0.0 W");
  });

  it("joins efficiency by timestamp and rejects missing or zero denominators", () => {
    const result = efficiencyHistory(
      series([
        [0, 1000],
        [60_000, 4000],
        [120_000, 8000],
        [180_000, 4000],
      ]),
      series([
        [0, 100],
        [60_000, 0],
        [180_000, 200],
      ]),
    );
    expect(result?.points.map((point) => point.value)).toEqual([10, null, null, 20]);
    expect(result?.aggregates?.min?.value).toBe(10);
    expect(result?.aggregates?.max?.value).toBe(20);
  });

  it("requires all fan readings at each bucket and summarizes total bucket averages", () => {
    const a = series([
      [0, 100],
      [60_000, 400],
      [120_000, null],
    ]);
    const b = series([
      [0, 300],
      [60_000, 0],
      [120_000, 200],
    ]);
    expect(sumHistory([a, undefined])).toBeUndefined();
    const total = sumHistory([a, b]);
    expect(total?.points.map((point) => point.value)).toEqual([400, 400, null]);
    expect(total?.aggregates?.max?.value).toBe(400);
    expect(total?.aggregates?.avg?.value).toBe(400);
  });
});
