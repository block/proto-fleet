import { describe, expect, test } from "vitest";

import type { V2Hashboard } from "./generated/generatedApi";
import { toCanonicalContainer, toCanonicalHashboardStatus } from "./normalize";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const normalizeModules = (modules: V2Hashboard[][]) =>
  toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: modules.map((boards, index) => ({
              id: index + 1,
              slots: boards.map((board, slot) => ({
                hashboard: {
                  macAddress: `T1-M${index + 1}-HB${slot + 1}`,
                  ...board,
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

describe("CT1 efficiency", () => {
  test.each([
    { power: 1500, hashrate: 100, expected: 15 },
    { power: 0, hashrate: 100, expected: 0 },
    { power: 1500, hashrate: 0, expected: null },
    { power: undefined, hashrate: 100, expected: null },
    { power: 1500, hashrate: undefined, expected: null },
    { power: -1, hashrate: 100, expected: null },
    { power: 1500, hashrate: -1, expected: null },
    { power: Infinity, hashrate: 100, expected: null },
    { power: 1500, hashrate: NaN, expected: null },
  ])("calculates J/TH from $power W and $hashrate TH/s", ({ power, hashrate, expected }) => {
    const { telemetry } = normalizeModules([[{ health: { power, hashrate } }]]);
    const board = [...telemetry.hashboards.values()][0];
    expect(board.efficiency?.latest).toEqual({ value: expected, units: "J/TH" });
    const moduleAndTank = [...telemetry.rollupsByEntity.entries()].filter(([id]) => id.includes(":tank:"));
    expect(moduleAndTank).toHaveLength(2);
    for (const [, rollup] of moduleAndTank) {
      expect(rollup.efficiency?.latest).toEqual({ value: expected, units: "J/TH" });
    }
    const container = toCanonicalContainer(undefined, { power, hashrate }, {}, "ct1");
    expect(container.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.efficiency?.latest).toEqual({
      value: expected,
      units: "J/TH",
    });
  });

  test("prefers reported container efficiency when available", () => {
    const result = toCanonicalContainer(undefined, { power: 1500, hashrate: 100, efficiency: 16 }, {}, "ct1");
    expect(result.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.efficiency?.latest?.value).toBe(16);
  });

  test("clears calculated efficiency when a later snapshot cannot calculate it", () => {
    useMinerStore.getState().resetDeviceData();
    for (const hashrate of [100, 0]) {
      const { telemetry } = normalizeModules([[{ health: { power: 1500, hashrate } }]]);
      useMinerStore.getState().telemetry.mergeTelemetry(telemetry);
    }
    const board = [...useMinerStore.getState().telemetry.hashboards.values()][0];
    expect(board.efficiency?.latest?.value).toBeNull();
    useMinerStore.getState().resetDeviceData();
  });
});

describe("module PSU telemetry", () => {
  test("clears missing PSU readings after a partial or absent successful snapshot and preserves history", () => {
    const store = useMinerStore.getState();
    store.resetDeviceData();
    const initial = normalizeModules([
      [
        {
          health: {
            psuElectrical: { inputPower: 5000, inputVoltage: 415 },
            psuThermal: { inletTemperature: 80, thermalStateValid: true, overTemperatureWarning: true },
          },
        },
      ],
    ]);
    store.hardware.replaceHardware(initial.hardware);
    const id = [...initial.telemetry.psus.keys()][0];
    initial.telemetry.psus.get(id)!.inputPower!.timeSeries = { points: [{ timestampMs: 1, value: 5000 }], units: "W" };
    store.telemetry.mergeTelemetry(initial.telemetry, 1);
    const partial = normalizeModules([[{ health: { psuElectrical: { inputVoltage: 415 } } }]]);
    store.telemetry.mergeTelemetry(partial.telemetry, 2);
    let psu = useMinerStore.getState().telemetry.psus.get(id)!;
    expect(psu.inputPower?.latest?.value).toBeNull();
    expect(psu.hotspotTemperature?.latest?.value).toBeNull();
    expect(psu.inputPower?.timeSeries?.points).toEqual([{ timestampMs: 1, value: 5000 }]);
    expect(psu.inputVoltage?.latest?.value).toBe(415);
    store.telemetry.mergeTelemetry(normalizeModules([[{}]]).telemetry, 3);
    psu = useMinerStore.getState().telemetry.psus.get(id)!;
    expect(psu.inputVoltage?.latest?.value).toBeNull();
    expect(psu.health).toBe("unknown");
    store.telemetry.mergeTelemetry(initial.telemetry, 1);
    expect(useMinerStore.getState().telemetry.psus.get(id)?.inputVoltage?.latest?.value).toBeNull();
  });

  test("discovers a PSU during polling without replacing existing ASIC hardware", () => {
    const store = useMinerStore.getState();
    store.resetDeviceData();
    const initial = normalizeModules([[{}], [{}]]);
    store.hardware.replaceHardware(initial.hardware);
    store.telemetry.mergeTelemetry(initial.telemetry, 1);
    const hardwareBefore = useMinerStore.getState().hardware;
    const ready = normalizeModules([
      [{ psuIdentity: { serial: "PSU-A" }, health: { psuElectrical: { inputPower: 5000 } } }],
      [{}],
    ]);
    store.telemetry.mergeTelemetry(ready.telemetry, 2);
    const state = useMinerStore.getState();
    const modules = [...state.hardware.topology!.modules.values()];
    const id = [...ready.telemetry.psus.keys()][0];
    expect(modules[0].psuIds).toEqual([id]);
    expect(modules[1].psuIds).toEqual([]);
    expect(state.hardware.psus.get(id)?.serialNumber).toBe("PSU-A");
    expect(state.telemetry.psus.get(id)?.inputPower?.latest?.value).toBe(5000);
    expect(state.hardware.asics).toBe(hardwareBefore.asics);
    expect(state.hardware.hashboards).toBe(hardwareBefore.hashboards);
  });

  test("ignores an older discovery after a newer absent snapshot and leaves PSU data alone on miner-only polls", () => {
    const store = useMinerStore.getState();
    store.resetDeviceData();
    const empty = normalizeModules([[{}]]);
    store.hardware.replaceHardware(empty.hardware);
    store.telemetry.mergeTelemetry(empty.telemetry, 2);
    const ready = normalizeModules([[{ health: { psuElectrical: { inputPower: 5000 } } }]]);
    store.telemetry.mergeTelemetry(ready.telemetry, 1);
    expect(useMinerStore.getState().hardware.psus.size).toBe(0);
    expect(useMinerStore.getState().telemetry.psus.size).toBe(0);
    store.telemetry.mergeTelemetry(ready.telemetry, 3);
    const before = useMinerStore.getState();
    store.telemetry.mergeTelemetry(toCanonicalContainer(undefined, {}, {}, "ct1").telemetry, 4);
    expect(useMinerStore.getState().hardware.psus).toBe(before.hardware.psus);
    expect(useMinerStore.getState().telemetry.psus).toBe(before.telemetry.psus);
  });

  test("shares one PSU per module and keeps warnings on their owning module", () => {
    const result = normalizeModules([
      [
        {
          psuIdentity: { serial: "PSU-A" },
          health: {
            psuThermal: {
              thermalStateValid: true,
              overTemperatureWarning: true,
              inletTemperature: 40,
              srHeatsinkTemperature: 60,
              pfcHeatsinkTemperature: 50,
            },
          },
        },
        { psuIdentity: { serial: "PSU-A" } },
      ],
      [
        {
          psuIdentity: { serial: "PSU-B" },
          health: {
            psuThermal: {
              thermalStateValid: true,
              overTemperatureWarning: false,
              overTemperatureFault: false,
            },
          },
        },
      ],
    ]);
    const modules = [...result.hardware.topology!.modules.values()];
    expect(modules.map((module) => module.psuIds.length)).toEqual([1, 1]);
    expect(result.hardware.psus.size).toBe(2);
    const [first, second] = modules.map((module) => result.telemetry.psus.get(module.psuIds[0])!);
    expect(first.health).toBe("degraded");
    expect(second.health).toBe("ok");
    expect(first.averageTemperature?.latest?.value).toBe(50);
    expect(first.hotspotTemperature?.latest?.value).toBe(60);
    expect(result.hardware.psus.get(modules[0].psuIds[0])?.ref.owner.id).toBe(modules[0].id);
  });

  test("selects fresh electrical and thermal snapshots independently and preserves missing and zero values", () => {
    const result = normalizeModules([
      [
        {
          health: {
            psuElectrical: { inputPower: 100, sampleAgeMs: 100 },
            psuThermal: {
              thermalStateValid: true,
              overTemperatureFault: true,
              thermalStateAgeMs: 100,
            },
          },
        },
        { health: { psuElectrical: { inputPower: 0, sampleAgeMs: 1 } } },
        {
          health: {
            psuThermal: {
              thermalStateValid: true,
              overTemperatureFault: false,
              overTemperatureWarning: false,
              thermalStateAgeMs: 1,
            },
          },
        },
      ],
    ]);
    const psu = [...result.telemetry.psus.values()][0];
    expect(psu.inputPower?.latest?.value).toBe(0);
    expect(psu.inputVoltage?.latest?.value).toBeNull();
    expect(psu.averageTemperature?.latest?.value).toBeNull();
    expect(psu.health).toBe("ok");
  });

  test("does not present invalid thermal state as a fault or invent PSUs without data", () => {
    const result = normalizeModules([
      [{ health: { psuThermal: { thermalStateValid: false, overTemperatureFault: true } } }],
      [{}],
    ]);
    expect([...result.telemetry.psus.values()][0].health).toBe("unknown");
    expect([...result.hardware.topology!.modules.values()][1].psuIds).toEqual([]);
  });
});

describe("CT1 ASIC display positions", () => {
  test("keeps a partial stack at its own display rows", () => {
    const result = toCanonicalHashboardStatus("HB-1", {
      layout: { stackCount: 4, asicsPerStack: 78, entries: [{ index: 78, stackId: 1, stackIndex: 0, x: 0, y: 1 }] },
      asics: [{ index: 78 }],
    });
    expect(result.asics[0]).toMatchObject({ index: 78, row: 3, column: 0 });
  });

  test("wraps each API stack into three rows without changing chip identity or telemetry", () => {
    const entries = Array.from({ length: 312 }, (_, index) => ({
      index,
      stackId: Math.floor(index / 78),
      stackIndex: index % 78,
      x: index % 78,
      y: Math.floor(index / 78),
    }));
    const result = toCanonicalHashboardStatus("HB-1", {
      layout: { stackCount: 4, asicsPerStack: 78, totalAsics: 312, entries: [...entries].reverse() },
      asics: entries.map(({ index }) => ({ index, temperatureDc: 500 + index })),
    });
    expect(new Set(result.asics.map((asic) => `${asic.row}:${asic.column}`)).size).toBe(312);
    result.asics.forEach((asic, index) => {
      expect(asic).toMatchObject({ index, row: Math.floor(index / 26), column: index % 26 });
      expect(asic.id).toBe(`serial:HB-1:asic:${index}`);
      expect(result.asicTelemetry[index].temperature?.value).toBe((500 + index) / 10);
    });
  });

  test("preserves physical 26-column coordinates and leaves missing layout positions unknown", () => {
    const result = toCanonicalHashboardStatus("HB-1", {
      layout: { stackCount: 4, asicsPerStack: 78, entries: [{ index: 0, stackId: 0, stackIndex: 0, x: 25, y: 2 }] },
      asics: [{ index: 0 }, { index: 1 }],
    });
    expect(result.asics[0]).toMatchObject({ row: 2, column: 25 });
    expect(result.asics[1].row).toBeUndefined();
    expect(result.asics[1].column).toBeUndefined();
  });
});

test("retains explicit isolation separately from offline health", () => {
  const { telemetry } = normalizeModules([[{ isolated: true }]]);
  expect([...telemetry.hashboards.values()][0]).toMatchObject({ isolated: true, health: "offline" });
});
