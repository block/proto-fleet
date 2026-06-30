import { beforeEach, describe, expect, test } from "vitest";

import useMinerStore from "./useMinerStore";

describe("useMinerStore.resetDeviceData", () => {
  beforeEach(() => {
    useMinerStore.getState().resetDeviceData();
  });

  test("clears every device slice", () => {
    const s = useMinerStore.getState();
    s.pools.setPoolsInfo([{ url: "stratum+tcp://a" }] as never);
    s.hardware.setHashboards([{ serial: "HB-A-1" }] as never);
    s.telemetry.updateFanTelemetry(0, { rpm: 4200 } as never);
    s.telemetry.updateCoolingMode("Auto" as never);
    s.systemInfo.setSystemInfo({ product_name: "Rig" } as never);
    s.networkInfo.setNetworkInfo({ mac: "AA:BB" } as never);

    useMinerStore.getState().resetDeviceData();

    const after = useMinerStore.getState();
    expect(after.pools.poolsInfo).toBeUndefined();
    expect(after.hardware.hashboards.size).toBe(0);
    expect(after.telemetry.fans.size).toBe(0);
    expect(after.telemetry.coolingMode).toBeNull();
  });

  test("preserves UI preferences and onboarding/identity flags", () => {
    const s = useMinerStore.getState();
    s.minerStatus.setOnboarded(true);
    s.minerStatus.setDefaultPasswordActive(false);
    const theme = s.ui.theme;

    useMinerStore.getState().resetDeviceData();

    const after = useMinerStore.getState();
    expect(after.minerStatus.onboarded).toBe(true);
    expect(after.minerStatus.defaultPasswordActive).toBe(false);
    expect(after.ui.theme).toBe(theme);
  });
});
