import { describe, expect, it } from "vitest";
import { createFoundMinersModelFilter, filterFoundMinerByModel } from "./foundMinersModelFilter";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";

const activeFilters = (models?: string[]): ActiveFilters => ({
  buttonFilters: [],
  dropdownFilters: models === undefined ? {} : { model: models },
  numericFilters: {},
  textareaListFilters: {},
});

describe("createFoundMinersModelFilter", () => {
  it("groups raw container variants into one logical option", () => {
    const filter = createFoundMinersModelFilter([
      { model: "CT1", deviceClass: DeviceClass.CONTAINER },
      { model: "Another model", deviceClass: DeviceClass.CONTAINER },
      { model: "CU1", deviceClass: DeviceClass.RIG },
      { model: "container", deviceClass: DeviceClass.RIG },
    ]);

    expect(filter.options).toEqual([
      { id: "container", label: "Proto Container" },
      { id: "model:CU1", label: "CU1" },
      { id: "model:container", label: "container" },
    ]);
    expect(filter.defaultOptionIds).toEqual(["container", "model:CU1", "model:container"]);
  });
});

describe("filterFoundMinerByModel", () => {
  it("matches every raw container variant through the logical container option", () => {
    const filters = activeFilters(["container"]);

    expect(filterFoundMinerByModel({ model: "CU1", deviceClass: DeviceClass.CONTAINER }, filters)).toBe(true);
    expect(filterFoundMinerByModel({ model: "CT1", deviceClass: DeviceClass.CONTAINER }, filters)).toBe(true);
    expect(filterFoundMinerByModel({ model: "Any model", deviceClass: DeviceClass.CONTAINER }, filters)).toBe(true);
    expect(filterFoundMinerByModel({ model: "S21 Pro", deviceClass: DeviceClass.RIG }, filters)).toBe(false);
    expect(filterFoundMinerByModel({ model: "CU1", deviceClass: DeviceClass.RIG }, filters)).toBe(false);
  });

  it("keeps non-container options exact", () => {
    const filters = activeFilters(["model:S21 Pro"]);

    expect(filterFoundMinerByModel({ model: "S21 Pro", deviceClass: DeviceClass.RIG }, filters)).toBe(true);
    expect(filterFoundMinerByModel({ model: "S19", deviceClass: DeviceClass.RIG }, filters)).toBe(false);
    expect(filterFoundMinerByModel({ model: "CU1", deviceClass: DeviceClass.CONTAINER }, filters)).toBe(false);
  });

  it("shows every model when no option is selected", () => {
    expect(filterFoundMinerByModel({ model: "CU1", deviceClass: DeviceClass.CONTAINER }, activeFilters())).toBe(true);
    expect(filterFoundMinerByModel({ model: "S21 Pro", deviceClass: DeviceClass.RIG }, activeFilters([]))).toBe(true);
  });
});
