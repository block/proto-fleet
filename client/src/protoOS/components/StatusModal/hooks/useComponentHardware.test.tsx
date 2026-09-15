import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useComponentHardware } from "./useComponentHardware";
import { useComponentTelemetry } from "./useComponentTelemetry";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import { useMinerStore } from "@/protoOS/store";
import { asModuleId } from "@/protoOS/store/contract";
import { componentName, resolveComponentAddress } from "@/protoOS/store/utils/componentSelection";

const seedContainer = () => {
  const snapshot = toCanonicalContainer(
    {
      containerMap: {
        tanks: [1, 2].map((tank) => ({
          id: tank,
          modules: [1, 2].map((module) => ({
            id: module,
            slots: [
              {
                hashboard: {
                  serial: `board-${tank}-${module}`,
                  location: { hashboardPosition: 3 },
                  hwRevision: `model-${tank}-${module}`,
                  health: { hashrate: tank * 100 + module },
                },
              },
            ],
          })),
        })),
      },
    },
    {},
    {},
    "ct1",
  );
  useMinerStore.getState().hardware.replaceHardware(snapshot.hardware);
  useMinerStore.getState().telemetry.mergeTelemetry(snapshot.telemetry);
  return snapshot;
};

describe("canonical status component selection", () => {
  beforeEach(() => useMinerStore.getState().resetDeviceData());

  it("resolves duplicate slots within the selected module and reads exact hardware and telemetry", () => {
    const snapshot = seedContainer();
    for (const module of snapshot.hardware.topology!.modules.values()) {
      const ref = resolveComponentAddress({ source: "HASHBOARD", slot: 3, moduleId: module.id }, snapshot.hardware)!;
      expect(ref.id).toBe(module.hashboardIds[0]);
      const { result, unmount } = renderHook(() => ({
        hardware: useComponentHardware(ref),
        telemetry: useComponentTelemetry(ref),
      }));
      expect(result.current.hardware).toBe(snapshot.hardware.hashboards.get(module.hashboardIds[0]));
      expect(result.current.telemetry).toEqual(snapshot.telemetry.hashboards.get(module.hashboardIds[0]));
      expect(componentName(ref, snapshot.hardware)).toContain(
        `${module.location!.tankLabel} · Module ${module.location!.moduleSlot}`,
      );
      unmount();
    }
  });

  it("does not resolve an unscoped or missing CT1 slot to another module", () => {
    const { hardware } = seedContainer();
    expect(resolveComponentAddress({ source: "HASHBOARD", slot: 3 }, hardware)).toBeUndefined();
    expect(
      resolveComponentAddress({ source: "HASHBOARD", slot: 3, moduleId: asModuleId("missing") }, hardware),
    ).toBeUndefined();
  });

  it("keeps the selected ID when hardware disappears", () => {
    const { hardware } = seedContainer();
    const ref = [...hardware.hashboards.values()][0].ref;
    const { result } = renderHook(() => useComponentHardware(ref));
    act(() => useMinerStore.getState().hardware.reset());
    expect(result.current).toBeUndefined();
    expect(resolveComponentAddress(ref, useMinerStore.getState().hardware)).toEqual(ref);
  });

  it("resolves existing rig selections to canonical hardware", () => {
    useMinerStore.getState().hardware.setControlBoard({ serial: "CU1-CB-0001", boardId: "1" });
    const ref = resolveComponentAddress({ source: "RIG" }, useMinerStore.getState().hardware);
    const { result } = renderHook(() => useComponentHardware(ref));
    expect(result.current).toMatchObject({ serialNumber: "CU1-CB-0001", boardId: "1" });
  });
});
