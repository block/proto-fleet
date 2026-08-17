import { beforeEach, describe, expect, test } from "vitest";

import { asDeviceId, asResourceKey, ROOT_MODULE_KEY } from "./contract";
import useMinerStore from "./useMinerStore";

const systemInfo = (model: string, deviceType: "rig" | "container" = "rig") => ({
  identity: {
    deviceId: asDeviceId(`device:${model || "unknown"}`),
    deviceType,
    mdkVersion: "v1" as const,
    model,
    productName: deviceType === "container" ? "Proto CU1" : "Proto Rig",
    macAddresses: [],
  },
  moduleModel: deviceType === "container" ? ("ct1" as const) : ("r2" as const),
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
