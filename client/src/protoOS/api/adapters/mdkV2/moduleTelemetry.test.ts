import { describe, expect, test } from "vitest";

import type { V2GetContainerHealthResponse, V2Hashboard } from "./generated/generatedApi";
import { toCanonicalContainer, toCanonicalHashboardStatus } from "./normalize";
import { containerInventory } from "@/protoOS/features/settings/components/Hardware/containerInventory";
import { asEntityKey, ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";

const normalizeModules = (modules: V2Hashboard[][], health: V2GetContainerHealthResponse = {}, now?: number) =>
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
                  health: board.health ? { timestamp: new Date().toISOString(), ...board.health } : undefined,
                },
              })),
            })),
          },
        ],
      },
    },
    {},
    health,
    "ct1",
    now,
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
    const { telemetry } = normalizeModules([[{ health: { power, hashrate, psuElectrical: { inputPower: power } } }]]);
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
  test.each([0, undefined])(
    "reprojects retained module and tank input (%s W) before topology bootstrap",
    (inputPower) => {
      const now = Date.parse("2026-11-17T19:00:00Z");
      const snapshot = normalizeModules(
        [
          [
            {
              health: {
                timestamp: new Date(now - 150_000).toISOString(),
                hashrate: 25,
                psuElectrical: { inputPower: 500, inputVoltage: 410 },
                psuThermal: { inletTemperature: 30 },
              },
            },
            {
              health: {
                timestamp: new Date(now + 6_000).toISOString(),
                hashrate: 25,
                psuElectrical: { inputPower, inputVoltage: 490, sampleAgeMs: 10_000 },
                psuThermal: { inletTemperature: 60 },
              },
            },
          ],
          [{ health: { timestamp: new Date(now).toISOString(), hashrate: 100, psuElectrical: { inputPower: 1000 } } }],
        ],
        { plant: { facilityPowerW: 12_000 } },
        NaN,
      );
      const [module, sibling] = [...snapshot.hardware.topology!.modules.values()];
      const moduleKey = asEntityKey(module.id);
      const siblingKey = asEntityKey(sibling.id);
      const tankKey = asEntityKey(module.tankId!);
      const points = [{ timestampMs: now - 10_000, value: 2000 }];
      snapshot.telemetry.rollupsByEntity.get(tankKey)!.power!.timeSeries = { points, units: "W" };
      const store = useMinerStore.getState;
      store().resetDeviceData();
      try {
        const telemetry = () => store().telemetry;
        telemetry().mergeTelemetry(snapshot.telemetry);
        const row = (key: typeof moduleKey) => telemetry().rollupsByEntity.get(key);
        const check = (key: typeof moduleKey, power: number | null, efficiency: number | null) => {
          expect(row(key)).toMatchObject({
            power: { latest: { value: power } },
            efficiency: { latest: { value: efficiency } },
            powerCoverage: { kind: power === null ? "unknown" : "complete" },
          });
        };
        const unaffected = [row(siblingKey), row(ROOT_MODULE_KEY), telemetry().plant];
        const retainedTankPower = inputPower === 0 ? 1000 : null;

        expect(store().hardware.topology).toBeUndefined();
        check(moduleKey, inputPower ?? null, inputPower ?? null);
        check(tankKey, retainedTankPower, inputPower === 0 ? 1000 / 150 : null);
        telemetry().refreshPsuReadings(now);
        const psu = () => telemetry().psus.get(module.psuIds[0]);
        expect(psu()).toMatchObject({
          inputVoltage: { latest: { value: 410 } },
          inletTemperature: { latest: { value: 30 } },
          observedAt: { electrical: now - 150_000, temperatures: now - 150_000 },
        });
        check(moduleKey, 500, 10);
        check(tankKey, 1500, 10);

        telemetry().refreshPsuReadings(now + 1_000);
        expect(psu()).toMatchObject({
          inputVoltage: { latest: { value: 490 } },
          inletTemperature: { latest: { value: 60 } },
          observedAt: { electrical: now - 4_000, temperatures: now + 6_000 },
        });
        check(moduleKey, inputPower ?? null, inputPower ?? null);
        check(tankKey, retainedTankPower, inputPower === 0 ? 1000 / 150 : null);
        expect(row(tankKey)?.power?.timeSeries?.points).toEqual(points);
        expect(row(siblingKey)).toBe(unaffected[0]);
        expect(row(ROOT_MODULE_KEY)).toBe(unaffected[1]);
        expect(telemetry().plant).toBe(unaffected[2]);
        expect([...telemetry().plant.values()][0].facilityPower?.latest?.value).toBe(12_000);

        const prior = telemetry();
        telemetry().refreshPsuReadings(now + 2_000);
        expect(telemetry()).toBe(prior);
      } finally {
        store().resetDeviceData();
      }
    },
  );

  test("uses each module's freshest PSU input once for module and tank power and efficiency", () => {
    const { hardware, telemetry } = normalizeModules([
      [
        { health: { power: 1000, hashrate: 100, psuElectrical: { inputPower: 3500, sampleAgeMs: 10 } } },
        { health: { power: 1000, hashrate: 100, psuElectrical: { inputPower: 3600, sampleAgeMs: 1 } } },
        { health: { power: 1000, hashrate: 100, psuElectrical: { inputPower: 3600, sampleAgeMs: 1 } } },
      ],
      [{ health: { power: 2000, hashrate: 200, psuElectrical: { inputPower: 2400 } } }],
    ]);
    const modules = [...hardware.topology!.modules.values()];
    for (const [id, power, efficiency] of [
      [modules[0].id, 3600, 12],
      [modules[1].id, 2400, 12],
      [modules[0].tankId!, 6000, 12],
    ] as const) {
      expect(telemetry.rollupsByEntity.get(asEntityKey(id))).toMatchObject({
        power: { latest: { value: power, units: "W" } },
        efficiency: { latest: { value: efficiency, units: "J/TH" } },
        powerCoverage: { kind: "complete", includesNonHashboardLoads: true },
      });
    }
    expect([...telemetry.hashboards.values()][0].power?.latest?.value).toBe(1000);
  });

  test.each([undefined, NaN, Infinity, 0])(
    "preserves missing or zero PSU input (%s) without using board power",
    (inputPower) => {
      const { hardware, telemetry } = normalizeModules([
        [{ health: { power: 1000, hashrate: 100, psuElectrical: { inputPower: 1200 } } }],
        [{ health: { power: 1000, hashrate: 100, psuElectrical: { inputPower } } }],
      ]);
      const modules = [...hardware.topology!.modules.values()];
      expect(telemetry.rollupsByEntity.get(asEntityKey(modules[1].id))?.power?.latest?.value).toBe(
        inputPower === 0 ? 0 : null,
      );
      expect(telemetry.rollupsByEntity.get(asEntityKey(modules[1].id))?.efficiency?.latest?.value).toBe(
        inputPower === 0 ? 0 : null,
      );
      const tank = telemetry.rollupsByEntity.get(asEntityKey(modules[0].tankId!));
      expect(tank?.power?.latest?.value).toBe(inputPower === 0 ? 1200 : null);
      expect(tank?.efficiency?.latest?.value).toBe(inputPower === 0 ? 6 : null);
    },
  );

  test("does not omit modules with no PSU snapshot or no boards from tank totals", () => {
    for (const missingModule of [[{ health: { power: 1000, hashrate: 100 } }], []]) {
      const { hardware, telemetry } = normalizeModules([
        [{ health: { power: 1000, hashrate: 100, psuElectrical: { inputPower: 1200 } } }],
        missingModule,
      ]);
      const modules = [...hardware.topology!.modules.values()];
      for (const id of [modules[1].id, modules[0].tankId!]) {
        expect(telemetry.rollupsByEntity.get(asEntityKey(id))?.power?.latest?.value).toBeNull();
        expect(telemetry.rollupsByEntity.get(asEntityKey(id))?.efficiency?.latest?.value).toBeNull();
      }
    }
  });

  test("clears missing PSU readings after a partial or absent successful snapshot and preserves history", () => {
    const store = useMinerStore.getState();
    store.resetDeviceData();
    const initial = normalizeModules([
      [
        {
          health: {
            psuElectrical: { inputPower: 5000, inputVoltage: 415 },
            psuThermal: {
              inletTemperature: 80,
              srHeatsinkTemperature: 85,
              pfcHeatsinkTemperature: 82,
              thermalStateValid: true,
              overTemperatureWarning: true,
            },
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
    expect(psu.inletTemperature?.latest?.value).toBeNull();
    expect(psu.srHeatsinkTemperature?.latest?.value).toBeNull();
    expect(psu.pfcHeatsinkTemperature?.latest?.value).toBeNull();
    expect(psu.observedAt?.temperatures).toBeUndefined();
    expect(psu.observedAt?.thermalState).toBeUndefined();
    expect(psu.inputPower?.timeSeries?.points).toEqual([{ timestampMs: 1, value: 5000 }]);
    expect(psu.inputVoltage?.latest?.value).toBe(415);
    store.telemetry.mergeTelemetry(normalizeModules([[{}]]).telemetry, 3);
    psu = useMinerStore.getState().telemetry.psus.get(id)!;
    expect(psu.inputVoltage?.latest?.value).toBeNull();
    expect(psu.health).toBe("unknown");
    expect(psu.observedAt).toEqual({});
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

  test.each([false, true])(
    "keeps newer PSU and plant inventory during hardware refresh (topology exists: %s)",
    (ready) => {
      const store = useMinerStore.getState;
      store().resetDeviceData();
      try {
        if (ready) store().hardware.replaceHardware(normalizeModules([[{}], [{}]]).hardware);
        const acceptHardware = beginStoreWrite("hardware");
        const acceptTelemetry = beginStoreWrite("telemetry:current:hashboard,miner");
        const older = normalizeModules(
          [
            [{ psuIdentity: { serial: "old-psu" }, health: { psuElectrical: { inputPower: 1000 } } }],
            [{ psuIdentity: { serial: "unreported-psu" }, health: { psuElectrical: { inputPower: 2000 } } }],
          ],
          { powerDistributions: [{ componentId: "old-feed", activePowerW: 6000 }] },
        );
        const current = normalizeModules(
          [[{ psuIdentity: { serial: "new-psu" }, health: { psuElectrical: { inputPower: 3000 } } }], [{}]],
          { powerDistributions: [{ componentId: "new-feed", activePowerW: 7000 }] },
        );
        expect(acceptTelemetry()).toBe(true);
        store().telemetry.mergeTelemetry(current.telemetry, acceptTelemetry.requestId);
        expect([...store().hardware.psus.keys()]).toEqual(ready ? [...current.hardware.psus.keys()] : []);
        expect(acceptHardware()).toBe(true);
        store().telemetry.mergeTelemetry(older.telemetry, acceptHardware.requestId, older.hardware);

        const { hardware, telemetry } = store();
        const topology = hardware.topology;
        if (topology?.kind !== "container") throw new Error("Expected installed container topology");
        const [reported, unreported] = [...topology.modules.values()];
        const [id] = current.hardware.psus.keys();
        expect(reported.psuIds).toEqual([id]);
        expect(unreported.psuIds).toEqual([]);
        expect([...hardware.psus.keys()]).toEqual([id]);
        expect(hardware.psus.get(id)?.serialNumber).toBe("new-psu");
        expect(telemetry.psus.get(id)?.inputPower?.latest?.value).toBe(3000);
        expect([...telemetry.psus.keys()]).toEqual([id]);
        expect([...hardware.plant.keys()]).toEqual([...current.hardware.plant.keys()]);
        expect(topology.container.plantComponentIds).toEqual([...current.hardware.plant.keys()]);
        const feed = [...hardware.plant.values()].find((component) => component.name === "new-feed");
        expect(feed && telemetry.plant.get(feed.ref.id)?.totalPower?.latest?.value).toBe(7000);
        expect([...telemetry.plant.keys()]).toEqual([...current.telemetry.plant.keys()]);
      } finally {
        store().resetDeviceData();
      }
    },
  );

  test("publishes a pending PSU only after its reported module joins the topology", () => {
    const reportedModuleId = "0011223344556677";
    const snapshot = (moduleId: string | undefined, model: string, inputPower: number) =>
      normalizeModules([
        [{ moduleId, psuIdentity: { serial: "PSU-A", model }, health: { psuElectrical: { inputPower } } }],
      ]);
    const initial = snapshot(undefined, "original", 1000);
    const delayed = snapshot(reportedModuleId, "older inventory", 2000);
    const first = snapshot(reportedModuleId, "first poll", 3000);
    const latest = snapshot(reportedModuleId, "latest poll", 4000);
    const [originalPsuId] = initial.hardware.psus.keys();
    const [reportedPsuId] = latest.hardware.psus.keys();
    expect(reportedPsuId).not.toBe(originalPsuId);

    const store = useMinerStore.getState;
    const psuInventory = () => containerInventory(store().hardware).find(({ name }) => name === "PSUs")?.items;
    store().resetDeviceData();
    try {
      const acceptInitial = beginStoreWrite("hardware");
      expect(acceptInitial()).toBe(true);
      store().telemetry.mergeTelemetry(initial.telemetry, acceptInitial.requestId, initial.hardware);
      const acceptDelayed = beginStoreWrite("hardware");
      for (const [result, power] of [
        [first, 3000],
        [latest, 4000],
      ] as const) {
        const acceptPoll = beginStoreWrite("telemetry:current:hashboard,miner");
        expect(acceptPoll()).toBe(true);
        store().telemetry.mergeTelemetry(result.telemetry, acceptPoll.requestId);
        expect(psuInventory()).toMatchObject([{ id: originalPsuId, serialNumber: "PSU-A" }]);
        expect(store().telemetry.psus.get(reportedPsuId)?.inputPower?.latest?.value).toBe(power);
      }

      expect(acceptDelayed()).toBe(true);
      store().telemetry.mergeTelemetry(delayed.telemetry, acceptDelayed.requestId, delayed.hardware);
      expect(psuInventory()).toMatchObject([{ id: reportedPsuId, serialNumber: "PSU-A", model: "latest poll" }]);
      expect([...store().hardware.topology!.modules.values()][0].psuIds).toEqual([reportedPsuId]);
      expect(store().telemetry.psus.get(reportedPsuId)?.inputPower?.latest?.value).toBe(4000);
    } finally {
      store().resetDeviceData();
    }
  });

  test.each([false, true])("keeps a pending PSU through a missing reading (module removed: %s)", (removed) => {
    const moduleId = "0011223344556677";
    const ready = normalizeModules([
      [
        {
          moduleId,
          psuIdentity: { serial: "PSU-A" },
          health: { temperatures: [42], psuElectrical: { inputPower: 5000 } },
        },
      ],
    ]);
    const missing = normalizeModules(removed ? [] : [[{ moduleId }]]);
    const [id] = ready.hardware.psus.keys();
    const [reportedModule] = ready.hardware.topology!.modules.keys();
    const store = useMinerStore.getState;
    store().resetDeviceData();
    try {
      store().telemetry.mergeTelemetry(ready.telemetry, 2);
      const moduleReadings = () => store().telemetry.rollupsByEntity.get(asEntityKey(reportedModule));
      expect(moduleReadings()?.power?.latest?.value).toBe(5000);
      expect(moduleReadings()?.temperature?.latest?.value).toBe(42);
      store().telemetry.mergeTelemetry(missing.telemetry, 3);
      expect(store().hardware.psus.size).toBe(0);
      expect(moduleReadings()?.power?.latest?.value).toBeNull();
      expect(moduleReadings()?.temperature?.latest?.value).toBeNull();

      store().telemetry.mergeTelemetry(ready.telemetry, 1, ready.hardware);
      const inventory = containerInventory(store().hardware).find(({ name }) => name === "PSUs");
      expect(inventory?.items ?? []).toMatchObject(removed ? [] : [{ id, serialNumber: "PSU-A" }]);
      expect([...store().hardware.topology!.modules.values()][0].psuIds).toEqual(removed ? [] : [id]);
      expect(store().telemetry.psus.get(id)?.inputPower?.latest?.value).toBeNull();
      expect(moduleReadings()?.power?.latest?.value).toBeNull();
      expect(moduleReadings()?.temperature?.latest?.value).toBeNull();
    } finally {
      store().resetDeviceData();
    }
  });

  test("a delayed hardware refresh keeps the PSU only for the module still reported", () => {
    const initial = normalizeModules([[{ psuIdentity: { serial: "PSU-A" } }], [{ psuIdentity: { serial: "PSU-B" } }]]);
    const delayed = normalizeModules([[{}], [{}]]);
    const latest = normalizeModules([[{}]]);
    const store = useMinerStore.getState;
    const serials = () =>
      containerInventory(store().hardware)
        .find(({ name }) => name === "PSUs")
        ?.items?.map((psu) => psu.serialNumber);
    store().resetDeviceData();
    try {
      store().telemetry.mergeTelemetry(initial.telemetry, 1, initial.hardware);
      expect(serials()).toEqual(["PSU-A", "PSU-B"]);
      const [firstPsuId] = initial.hardware.psus.keys();

      store().telemetry.mergeTelemetry(latest.telemetry, 3);
      store().telemetry.mergeTelemetry(delayed.telemetry, 2, delayed.hardware);
      expect(serials()).toEqual(["PSU-A"]);
      const [first, second] = store().hardware.topology!.modules.values();
      expect(first.psuIds).toEqual([firstPsuId]);
      expect(second.psuIds).toEqual([]);
    } finally {
      store().resetDeviceData();
    }
  });

  test("a delayed map cannot restore a removed tank's readings", () => {
    const ready = normalizeModules([[{ health: { temperatures: [42], psuElectrical: { inputPower: 5000 } } }]]);
    const empty = toCanonicalContainer({ containerMap: { tanks: [] } }, {}, {}, "ct1");
    const [module] = ready.hardware.topology!.modules.values();
    const key = asEntityKey(module.tankId!);
    const store = useMinerStore.getState;
    const tank = () => store().telemetry.rollupsByEntity.get(key);
    store().resetDeviceData();
    try {
      store().telemetry.mergeTelemetry(ready.telemetry, 2);
      expect(tank()?.temperature?.latest?.value).toBe(42);
      expect(tank()?.power?.latest?.value).toBe(5000);
      store().telemetry.mergeTelemetry(empty.telemetry, 3);
      store().telemetry.mergeTelemetry(ready.telemetry, 1, ready.hardware);
      expect(tank()?.temperature?.latest?.value).toBeNull();
      expect(tank()?.power?.latest?.value).toBeNull();
      expect(tank()?.miningStatus).toBe("unknown");
    } finally {
      store().resetDeviceData();
    }
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

test("expires board-sensor summaries in every scope even if the next poll fails", () => {
  const now = Date.now();
  const snapshot = normalizeModules([
    [
      { health: { temperatures: [0], timestamp: new Date(now).toISOString() } },
      { health: { temperatures: [40, 50], timestamp: new Date(now - 60_000).toISOString() } },
    ],
  ]);
  const store = () => useMinerStore.getState().telemetry;
  store().clearAllData();
  try {
    store().mergeTelemetry(snapshot.telemetry);
    // A summary poll supplies no new board observations.
    const summary = toCanonicalContainer(undefined, { averageHashboardTemperatureC: 90 }, {}, "ct1").telemetry;
    store().mergeTelemetry(summary);
    for (const [elapsed, average, minimum, maximum] of [
      [0, 30, 0, 50],
      [60_000, 0, 0, 0],
      [120_000, null, null, null],
    ] as const) {
      store().refreshBoardTemperatures(now + elapsed);
      for (const row of store().rollupsByEntity.values()) {
        expect(row.temperature?.latest?.value).toBe(average);
        expect(row.minimumTemperature?.latest?.value).toBe(minimum);
        expect(row.maximumTemperature?.latest?.value).toBe(maximum);
      }
    }
  } finally {
    store().clearAllData();
  }
});

test.each([NaN, -20_000])("recovers temperatures after a clock interruption and a map read at offset %s", (offset) => {
  const at = Date.parse("2026-01-01T00:00:00Z");
  const full = (value: number, now: number) =>
    normalizeModules([[{ health: { temperatures: [value], timestamp: new Date(at).toISOString() } }]], {}, now);
  const store = () => useMinerStore.getState().telemetry;
  const temperature = () => store().rollupsByEntity.get(ROOT_MODULE_KEY)?.temperature?.latest?.value;
  store().clearAllData();
  try {
    store().mergeTelemetry(full(42, at).telemetry);
    store().refreshBoardTemperatures(NaN);
    expect(temperature()).toBeNull();
    store().refreshBoardTemperatures(at + 5_000);
    expect(temperature()).toBe(42);

    store().mergeTelemetry(full(0, at + offset).telemetry);
    expect(temperature()).toBeNull();
    store().refreshBoardTemperatures(at + 5_000);
    expect(temperature()).toBe(0);
    store().refreshBoardTemperatures(at + 120_000);
    expect(temperature()).toBeNull();
  } finally {
    store().clearAllData();
  }
});

test.each([
  { state: "an empty store", cached: false },
  { state: "a cached board summary", cached: true },
])("applies a full board summary after a newer miner-only poll with $state", ({ cached }) => {
  const full = (temperatures: number[]) => normalizeModules([[{ health: { temperatures } }]]);
  const summary = toCanonicalContainer(undefined, { averageHashboardTemperatureC: 90, power: 600 }, {}, "ct1");
  const history = createEmptyTelemetryState();
  const timeSeries = { points: [{ timestampMs: 1, value: 100 }], units: "°C" as const };
  history.rollupsByEntity.set(ROOT_MODULE_KEY, { temperature: { timeSeries } });
  const store = () => useMinerStore.getState().telemetry;
  store().clearAllData();
  try {
    if (cached) store().mergeTelemetry(full([20, 40]).telemetry, 1);
    store().mergeTelemetry(summary.telemetry, 4);
    store().mergeTelemetry(history, 5);
    const updated = full([30, 50]);
    store().mergeTelemetry(updated.telemetry, 3);
    store().mergeTelemetry(full([60, 80]).telemetry, 2);

    const topology = updated.hardware.topology;
    if (topology?.kind !== "container") throw new Error("Expected a container fixture");
    for (const key of [ROOT_MODULE_KEY, asEntityKey(topology.containerId)]) {
      expect(store().rollupsByEntity.get(key)).toMatchObject({
        minimumTemperature: { latest: { value: 30 } },
        temperature: { latest: { value: 40 } },
        maximumTemperature: { latest: { value: 50 } },
        power: { latest: { value: 600 } },
      });
    }
    expect(store().rollupsByEntity.get(ROOT_MODULE_KEY)?.temperature?.timeSeries).toEqual(timeSeries);
  } finally {
    store().clearAllData();
  }
});
