import { describe, expect, it } from "vitest";
import { CONTAINER_MODULE_LAYOUT, RIG_LAYOUT } from "./layouts";
import type { AsicData } from "@/protoOS/store";

// The label helpers themselves are covered by containerHashboardLayout.test.ts
// and utility.test.ts; these cases pin which helper each descriptor delegates
// to, and how each handles an ASIC missing the data that helper needs.
const asic = (overrides: Partial<AsicData> = {}): AsicData => ({ ...overrides }) as AsicData;

describe("RIG_LAYOUT", () => {
  it("labels grid cells by ASIC index", () => {
    expect(RIG_LAYOUT.labelCell(asic({ index: 5, row: 0, column: 0 }), 8)).toBe("B1");
  });

  it("labels the detail popover by grid position, not index", () => {
    expect(RIG_LAYOUT.labelDetail(asic({ row: 2, column: 3, index: 5 }))).toBe("C4");
  });

  it("returns an empty cell label when the ASIC has no index", () => {
    expect(RIG_LAYOUT.labelCell(asic({ row: 1, column: 2 }), 8)).toBe("");
  });
});

describe("CONTAINER_MODULE_LAYOUT", () => {
  it("labels the cell and its popover by grid position, ignoring the index", () => {
    const target = asic({ row: 1, column: 0, index: 40 });

    expect(CONTAINER_MODULE_LAYOUT.labelCell(target, 312)).toBe("F52");
    expect(CONTAINER_MODULE_LAYOUT.labelDetail(target)).toBe("F52");
  });

  it("returns an empty label when the ASIC has no grid position", () => {
    expect(CONTAINER_MODULE_LAYOUT.labelCell(asic({ index: 3 }), 312)).toBe("");
    expect(CONTAINER_MODULE_LAYOUT.labelDetail(asic({ index: 3 }))).toBe("");
  });

  it("adds a second rail because airflow runs bottom to top", () => {
    expect(RIG_LAYOUT.rails.bottom).toBeUndefined();
    expect(CONTAINER_MODULE_LAYOUT.rails.top.left.reading).toBe("outlet");
    expect(CONTAINER_MODULE_LAYOUT.rails.bottom?.left.reading).toBe("inlet");
  });
});
