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

    // Sanity: the flattened info slices actually hold the seeded data first.
    expect((useMinerStore.getState().systemInfo as unknown as Record<string, unknown>).product_name).toBe("Rig");
    expect((useMinerStore.getState().networkInfo as unknown as Record<string, unknown>).mac).toBe("AA:BB");

    useMinerStore.getState().resetDeviceData();

    const after = useMinerStore.getState();
    expect(after.pools.poolsInfo).toBeUndefined();
    expect(after.hardware.hashboards.size).toBe(0);
    expect(after.telemetry.fans.size).toBe(0);
    expect(after.telemetry.coolingMode).toBeNull();
    // setSystemInfo(undefined)/setNetworkInfo(undefined) are no-ops, so these
    // assert the real reset clears the flattened fields.
    expect((after.systemInfo as unknown as Record<string, unknown>).product_name).toBeUndefined();
    expect((after.networkInfo as unknown as Record<string, unknown>).mac).toBeUndefined();
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
