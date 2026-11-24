// @vitest-environment node
import { describe, expect, it } from "vitest";
import { containerAsicPlacement } from "./containerAsicLayout";

describe("containerAsicPlacement", () => {
  it("wraps a long stack row to the board's columns", () => {
    const entries = [0, 51].map((x) => ({ stackId: 1, stackIndex: x, x, y: 1 }));
    const place = containerAsicPlacement(entries, 52);
    expect(place(entries[0])).toEqual({ row: 2, column: 0 });
    expect(place(entries[1])).toEqual({ row: 3, column: 25 });
  });

  it("gives no cell to positions outside the grid", () => {
    const valid = { stackId: 0, stackIndex: 0, x: 0, y: 0 };
    const place = containerAsicPlacement([valid, { x: 26, y: 2_147_483_647 }], 26);
    expect(place(valid)).toEqual({ row: 0, column: 0 });
    expect(place({ x: 26, y: 2_147_483_647 })).toEqual({});
    expect(place({ x: -1, y: 0 })).toEqual({});
    expect(place({ x: 0, y: -3 })).toEqual({});
    expect(place(undefined)).toEqual({});
  });
});
