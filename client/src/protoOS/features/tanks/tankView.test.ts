import { describe, expect, test } from "vitest";

import { tanksFixture } from "./Tanks.fixtures";
import { boardStatus, moduleStatus, tankGrid, tankMiningScope, tankModules } from "./tankView";

describe("tank presentation", () => {
  test("distinguishes isolated boards from offline boards and mixed modules", () => {
    const { telemetry } = tanksFixture();
    const board = [...telemetry.hashboards.values()][0];
    const isolated = { ...board, isolated: true };
    expect(boardStatus(isolated)).toBe("isolated");
    expect(moduleStatus([isolated, isolated, isolated])).toBe("isolated");
    expect(moduleStatus([board, isolated, board])).toBe("attention");
  });
  test("preserves module gaps and positions the first row at the bottom", () => {
    const { topology } = tanksFixture();
    const tank = [...topology.tanks.values()][0];
    const modules = tankModules(tank, topology).filter((module) =>
      [1, 3, 9, 16].includes(module.location!.moduleSlot!),
    );
    expect(tankGrid(tank, modules)).toEqual({
      columns: 8,
      rows: 2,
      positions: [
        { row: 2, column: 1 },
        { row: 2, column: 3 },
        { row: 1, column: 1 },
        { row: 1, column: 8 },
      ],
    });
    expect(
      tankGrid({ ...tank, moduleLayout: { columns: 8, rows: 2, numbering: "columnMajor" } }, modules).positions,
    ).toEqual([
      { row: 2, column: 1 },
      { row: 2, column: 2 },
      { row: 2, column: 5 },
      { row: 1, column: 8 },
    ]);
  });

  test("refuses partial, empty and unresolved mining scopes", () => {
    const { topology } = tanksFixture();
    const tank = [...topology.tanks.values()][0];
    const modules = tankModules(tank, topology);
    expect(tankMiningScope(tank, [])).toBeUndefined();
    expect(tankMiningScope(tank, modules.slice(1))).toBeUndefined();
    expect(tankMiningScope({ ...tank, slot: undefined }, modules)).toBeUndefined();
    expect(
      tankMiningScope(
        tank,
        modules.map((module) => ({ ...module, location: undefined })),
      ),
    ).toBeUndefined();
  });

  test("distinguishes sleeping, offline, unknown and mixed module states", () => {
    const { telemetry } = tanksFixture();
    const board = [...telemetry.hashboards.values()][0];
    expect(boardStatus({ ...board, miningStatus: "stopped" })).toBe("sleeping");
    expect(boardStatus({ ...board, health: "offline", miningStatus: "unknown" })).toBe("offline");
    expect(boardStatus(undefined)).toBe("unknown");
    expect(moduleStatus([])).toBe("unknown");
    expect(moduleStatus([board, undefined])).toBe("unknown");
    expect(moduleStatus([board, { ...board, miningStatus: "stopped" }])).toBe("attention");
    expect(moduleStatus([{ ...board, miningStatus: "stopped" }])).toBe("sleeping");
    expect(moduleStatus([board, { ...board, health: "fault" }])).toBe("attention");
  });
});
