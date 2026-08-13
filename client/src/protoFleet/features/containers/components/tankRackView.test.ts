import { describe, expect, it } from "vitest";
import type { TankModuleState } from "./TankModuleGrid";
import { toTankRackView } from "./tankRackView";

describe("toTankRackView", () => {
  it("maps a fully healthy powered-on tank to all-healthy slots", () => {
    const modules: TankModuleState[] = Array.from({ length: 16 }, () => "healthy");
    const view = toTankRackView({ cols: 8, rows: 2, modules, on: true });

    expect(view.rows).toBe(2);
    expect(view.cols).toBe(8);
    expect(Object.keys(view.slotStates)).toHaveLength(16);
    expect(view.hashingCount).toBe(16);
    expect(view.needsAttentionCount).toBe(0);
    expect(view.offlineCount).toBe(0);
    expect(Object.values(view.slotStates).every((s) => s === "healthy")).toBe(true);
  });

  it("maps attention modules to needsAttention slots and counts them", () => {
    const modules: TankModuleState[] = ["attention", "healthy", "attention", "healthy"];
    const view = toTankRackView({ cols: 2, rows: 2, modules, on: true });

    expect(view.hashingCount).toBe(2);
    expect(view.needsAttentionCount).toBe(2);
    expect(view.offlineCount).toBe(0);
    // Row-major: index 0 -> 0-0, index 2 -> 1-0.
    expect(view.slotStates["0-0"]).toBe("needsAttention");
    expect(view.slotStates["1-0"]).toBe("needsAttention");
    expect(view.slotStates["0-1"]).toBe("healthy");
  });

  it("reads every module offline when the tank PDU is off, regardless of module health", () => {
    const modules: TankModuleState[] = ["healthy", "attention", "healthy", "attention"];
    const view = toTankRackView({ cols: 2, rows: 2, modules, on: false });

    expect(view.offlineCount).toBe(4);
    expect(view.hashingCount).toBe(0);
    expect(view.needsAttentionCount).toBe(0);
    expect(Object.values(view.slotStates).every((s) => s === "offline")).toBe(true);
  });

  it("defaults missing module entries to healthy so the grid always fills", () => {
    const modules: TankModuleState[] = ["attention"]; // only 1 of 4 provided
    const view = toTankRackView({ cols: 2, rows: 2, modules, on: true });

    expect(Object.keys(view.slotStates)).toHaveLength(4);
    expect(view.needsAttentionCount).toBe(1);
    expect(view.hashingCount).toBe(3);
  });

  it("keeps module ordering row-major so displayed slot N is bar N on the tank card", () => {
    // 3 cols x 2 rows: index 4 is displayed as slot 5 with the default
    // top-left rack numbering.
    const modules: TankModuleState[] = ["healthy", "healthy", "healthy", "healthy", "attention", "healthy"];
    const view = toTankRackView({ cols: 3, rows: 2, modules, on: true });

    expect(view.slotStates["1-1"]).toBe("needsAttention");
  });

  it("places modules at coordinates matching a bottom-left displayed slot number", () => {
    // With bottom-left numbering, displayed slots 1..3 live on grid row 1.
    const modules: TankModuleState[] = ["attention", "healthy", "healthy", "healthy", "healthy", "healthy"];
    const view = toTankRackView({
      cols: 3,
      rows: 2,
      modules,
      on: true,
      numberingOrigin: "bottom-left",
    });

    expect(view.slotStates["1-0"]).toBe("needsAttention");
    expect(view.slotStates["0-0"]).toBe("healthy");
  });
});
