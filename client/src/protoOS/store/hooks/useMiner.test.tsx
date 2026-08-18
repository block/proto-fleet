import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { ROOT_MODULE_KEY } from "../contract";
import useMinerStore from "../useMinerStore";
import { useHashboardsHardware, useMinerHardware } from "./useHardware";
import { useMiner } from "./useMiner";
import { useFansTelemetry, useMinerTelemetry } from "./useTelemetry";

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
});
