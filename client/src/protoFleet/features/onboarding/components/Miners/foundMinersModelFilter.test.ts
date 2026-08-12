import { describe, expect, it } from "vitest";
import { createFoundMinersModelFilter, filterFoundMinerByModel } from "./foundMinersModelFilter";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";

const activeFilters = (models?: string[]): ActiveFilters => ({
  buttonFilters: [],
  dropdownFilters: models === undefined ? {} : { model: models },
  numericFilters: {},
  textareaListFilters: {},
});

describe("createFoundMinersModelFilter", () => {
  it("groups raw container variants into one logical option", () => {
    const filter = createFoundMinersModelFilter(["CU1", "cu-200", "Cu2", "S21 Pro"]);

    expect(filter.options).toEqual([
      { id: "CU", label: "Proto Container" },
      { id: "S21 Pro", label: "S21 Pro" },
    ]);
    expect(filter.defaultOptionIds).toEqual(["CU", "S21 Pro"]);
  });
});

describe("filterFoundMinerByModel", () => {
  it("matches every raw container variant through the logical container option", () => {
    const filters = activeFilters(["CU"]);

    expect(filterFoundMinerByModel({ model: "CU1" }, filters)).toBe(true);
    expect(filterFoundMinerByModel({ model: "cu-200" }, filters)).toBe(true);
    expect(filterFoundMinerByModel({ model: "Cu2" }, filters)).toBe(true);
    expect(filterFoundMinerByModel({ model: "S21 Pro" }, filters)).toBe(false);
  });

  it("keeps non-container options exact", () => {
    const filters = activeFilters(["S21 Pro"]);

    expect(filterFoundMinerByModel({ model: "S21 Pro" }, filters)).toBe(true);
    expect(filterFoundMinerByModel({ model: "S19" }, filters)).toBe(false);
    expect(filterFoundMinerByModel({ model: "CU1" }, filters)).toBe(false);
  });

  it("shows every model when no option is selected", () => {
    expect(filterFoundMinerByModel({ model: "CU1" }, activeFilters())).toBe(true);
    expect(filterFoundMinerByModel({ model: "S21 Pro" }, activeFilters([]))).toBe(true);
  });
});
