import { beforeEach, describe, expect, test } from "vitest";

import { asDeviceId, asHashboardId, asResourceKey, ROOT_MODULE_KEY } from "./contract";
import useMinerStore from "./useMinerStore";
import { getFanId, getPsuId } from "./utils/entityIds";
import { getAsicId } from "./utils/getAsicId";

const systemInfo = (model: string, deviceType: "rig" | "container" = "rig") => ({
  identity: {
    deviceId: asDeviceId(`device:${model || "unknown"}`),
    deviceType,
    adapterId: "mdk-v1" as const,
    model,
    productName: deviceType === "container" ? "Proto CU1" : "Proto Rig",
    macAddresses: [],
  },
  moduleModel: deviceType === "container" ? ("ct1" as const) : ("r2" as const),
});

describe("flow unit preference", () => {
  beforeEach(() => {
    localStorage.clear();
    useMinerStore.getState().ui.setFlowUnit("GPM");
  });

  test("defaults to gallons per minute", () => {
    expect(useMinerStore.getInitialState().ui.flowUnit).toBe("GPM");
  });

  test.each(["GPM", "L/min"] as const)("persists and restores %s across reloads and device switches", async (unit) => {
    useMinerStore.getState().ui.setFlowUnit(unit);
    const saved = localStorage.getItem("proto-ui-preferences")!;
    expect(JSON.parse(saved).state.ui.flowUnit).toBe(unit);
    useMinerStore.getState().ui.setFlowUnit(unit === "GPM" ? "L/min" : "GPM");
    localStorage.setItem("proto-ui-preferences", saved);
    await useMinerStore.persist.rehydrate();
    expect(useMinerStore.getState().ui.flowUnit).toBe(unit);
    useMinerStore.getState().resetDeviceData();
    expect(useMinerStore.getState().ui.flowUnit).toBe(unit);
  });

  test.each([undefined, "invalid"])("uses GPM when a saved preference is %s", async (flowUnit) => {
    useMinerStore.getState().ui.setFlowUnit("L/min");
    localStorage.setItem("proto-ui-preferences", JSON.stringify({ state: { ui: { flowUnit } }, version: 0 }));
    await useMinerStore.persist.rehydrate();
    expect(useMinerStore.getState().ui.flowUnit).toBe("GPM");
  });
});

describe("useMinerStore.resetDeviceData", () => {
  beforeEach(() => {
    useMinerStore.getState().resetDeviceData();
  });

  test("clears every device slice", () => {
    const s = useMinerStore.getState();
    s.pools.setPoolsInfo([{ url: "stratum+tcp://a" }] as never);
    s.hardware.setHashboards([{ serial: "HB-A-1" }] as never);
    s.telemetry.updateFanTelemetry(0, { rpm: 4200 } as never);
    s.telemetry.updateCoolingMode("automatic");
    s.systemInfo.setSystemInfo(systemInfo("Rig"));
    s.networkInfo.setNetworkInfo(new Map([["primary", { id: "primary", macAddress: "AA:BB", isPrimary: true }]]));

    // Sanity: the flattened info slices actually hold the seeded data first.
    expect(useMinerStore.getState().systemInfo.data?.identity.model).toBe("Rig");
    expect(useMinerStore.getState().networkInfo.interfaces.get("primary")?.macAddress).toBe("AA:BB");

    useMinerStore.getState().resetDeviceData();

    const after = useMinerStore.getState();
    expect(after.pools.byOwner.has(ROOT_MODULE_KEY)).toBe(false);
    expect(after.hardware.hashboards.size).toBe(0);
    expect(after.telemetry.fans.size).toBe(0);
    expect(after.telemetry.coolingModeByOwner.size).toBe(0);
    // setSystemInfo(undefined)/setNetworkInfo(undefined) are no-ops, so these
    // assert the real reset clears the flattened fields.
    expect(after.systemInfo.data).toBeUndefined();
    expect(after.networkInfo.interfaces.size).toBe(0);
  });

  test("clears the derived device type so a rig doesn't inherit a module's layout", () => {
    useMinerStore.getState().systemInfo.setSystemInfo(systemInfo("CU1", "container"));
    expect(useMinerStore.getState().systemInfo.data?.moduleModel).toBe("ct1");

    useMinerStore.getState().resetDeviceData();

    expect(useMinerStore.getState().systemInfo.data).toBeUndefined();
  });

  test("preserves UI preferences and onboarding/identity flags", () => {
    const s = useMinerStore.getState();
    s.minerStatus.setOnboarded(true);
    s.minerStatus.setDefaultPasswordActive(false);
    const theme = s.ui.theme;

    useMinerStore.getState().resetDeviceData();

    const after = useMinerStore.getState();
    expect(after.minerStatus.lifecycle.onboarded).toBe(true);
    expect(after.minerStatus.lifecycle.defaultPasswordActive).toBe(false);
    expect(after.ui.theme).toBe(theme);
  });

  test("rejects stale writes after a device reset and out-of-order responses", () => {
    const resource = asResourceKey("telemetry:current");
    const beforeReset = useMinerStore.getState().beginStoreWrite();

    useMinerStore.getState().resetDeviceData();
    expect(useMinerStore.getState().acceptStoreWrite(resource, beforeReset)).toBe(false);

    const older = useMinerStore.getState().beginStoreWrite();
    const newer = useMinerStore.getState().beginStoreWrite();
    expect(useMinerStore.getState().acceptStoreWrite(resource, newer)).toBe(true);
    expect(useMinerStore.getState().acceptStoreWrite(resource, older)).toBe(false);
  });
});

describe("partial normalized store updates", () => {
  beforeEach(() => {
    useMinerStore.getState().resetDeviceData();
  });

  test("preserves fan calibration fields when a cooling poll only updates live values", () => {
    const telemetry = useMinerStore.getState().telemetry;
    telemetry.updateFanTelemetry(1, {
      minimumRpm: { latest: { value: 1_200, units: "RPM" } },
      maximumRpm: { latest: { value: 6_000, units: "RPM" } },
    });

    telemetry.updateFanTelemetry(1, {
      rpm: { latest: { value: 4_800, units: "RPM" } },
      speedPercent: { latest: { value: 75, units: "%" } },
    });

    const fan = useMinerStore.getState().telemetry.fans.get(getFanId(1));
    expect(fan?.minimumRpm?.latest?.value).toBe(1_200);
    expect(fan?.maximumRpm?.latest?.value).toBe(6_000);
    expect(fan?.rpm?.latest?.value).toBe(4_800);
  });

  test("clears live fan power when a cooling poll no longer reports the fan", () => {
    const telemetry = useMinerStore.getState().telemetry;
    const id = getFanId(1);
    telemetry.replaceFanLiveTelemetry([{ componentId: id, power: { latest: { value: 400, units: "W" } } }]);
    expect(useMinerStore.getState().telemetry.fans.get(id)?.power?.latest?.value).toBe(400);

    telemetry.replaceFanLiveTelemetry([]);

    expect(useMinerStore.getState().telemetry.fans.get(id)?.power).toBeUndefined();
  });

  test("persists timeSeries through the PSU/fan writers, not just latest", () => {
    const telemetry = useMinerStore.getState().telemetry;
    const psuPoints = [{ timestampMs: 1_000, value: 3_000 }];
    const fanPoints = [{ timestampMs: 1_000, value: 4_800 }];

    telemetry.updatePsuTelemetry(2, {
      inputPower: {
        latest: { value: 3_000, units: "W" },
        timeSeries: { points: psuPoints, bucketSizeMs: 1_000, units: "W" },
      },
    });
    telemetry.updateFanTelemetry(3, {
      rpm: {
        latest: { value: 4_800, units: "RPM" },
        timeSeries: { points: fanPoints, bucketSizeMs: 1_000, units: "RPM" },
      },
    });

    const psu = useMinerStore.getState().telemetry.psus.get(getPsuId(2));
    const fan = useMinerStore.getState().telemetry.fans.get(getFanId(3));
    // Regression: the old mapMeasurement writer copied only `latest`, dropping this.
    expect(psu?.inputPower?.timeSeries?.points).toEqual(psuPoints);
    expect(psu?.inputPower?.latest?.value).toBe(3_000);
    expect(fan?.rpm?.timeSeries?.points).toEqual(fanPoints);
    expect(fan?.rpm?.latest?.value).toBe(4_800);
  });

  test("preserves independently loaded system tags across system-info refreshes", () => {
    const store = useMinerStore.getState();
    store.systemInfo.setSystemTag("rack-7");

    store.systemInfo.setSystemInfo(systemInfo("Rig"));
    expect(useMinerStore.getState().systemInfo.systemTag).toBe("rack-7");

    useMinerStore.getState().systemInfo.setSystemInfo(systemInfo("Rig refreshed"));
    expect(useMinerStore.getState().systemInfo.systemTag).toBe("rack-7");
  });

  test("preserves ASIC enrichment when refreshing the same hardware inventory", () => {
    const hardware = useMinerStore.getState().hardware;
    hardware.setHashboards([{ serial: "HB-1" }]);
    const asicId = getAsicId("HB-1", 0);
    hardware.batchAddAsics([{ id: asicId, hashboardSerial: "HB-1", index: 0, row: 2, column: 3 }]);
    hardware.linkAsicToHashboard(asicId, "HB-1");

    const current = useMinerStore.getState().hardware;
    hardware.replaceHardware({
      resource: { status: "ready", updatedAtMs: Date.now() },
      topology: current.topology
        ? {
            ...current.topology,
            hashboards: new Map([...current.topology.hashboards].map(([id, node]) => [id, { ...node, asicIds: [] }])),
          }
        : undefined,
      controlBoards: new Map(current.controlBoards),
      hashboards: new Map(current.hashboards),
      asics: new Map(),
      psus: new Map(current.psus),
      fans: new Map(current.fans),
      plant: new Map(current.plant),
    });

    const refreshed = useMinerStore.getState().hardware;
    expect(refreshed.asics.get(asicId)).toMatchObject({ index: 0, row: 2, column: 3 });
    expect(refreshed.topology?.hashboards.get(asHashboardId("serial:HB-1"))?.asicIds).toContain(asicId);
  });

  test("ignores undefined metric parts instead of clearing last-known telemetry", () => {
    const hashboardId = asHashboardId("serial:HB-1");
    const telemetry = useMinerStore.getState().telemetry;
    telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity: new Map(),
      hashboards: new Map([[hashboardId, { hashboardId, temperature: { latest: { value: 65, units: "°C" } } }]]),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity: new Map(),
      hashboards: new Map([[hashboardId, { hashboardId, temperature: { latest: undefined } }]]),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    expect(useMinerStore.getState().telemetry.hashboards.get(hashboardId)?.temperature?.latest).toEqual({
      value: 65,
      units: "°C",
    });
  });
});
