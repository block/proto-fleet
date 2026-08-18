import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { asComponentId, ROOT_MODULE_ID, ROOT_MODULE_KEY } from "../contract";
import useMinerStore from "../useMinerStore";
import { useHashboardsHardware, useMinerHardware } from "./useHardware";
import { useMiner } from "./useMiner";
import { useErrors } from "./useMinerStatus";
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
});
