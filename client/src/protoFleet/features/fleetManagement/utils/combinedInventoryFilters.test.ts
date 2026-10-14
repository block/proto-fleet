import { describe, expect, it } from "vitest";
import {
  combinedInventoryOptions,
  combineInventoryFilters,
  separateInventoryFilters,
} from "./combinedInventoryFilters";
import { encodeActiveFiltersToURL, parseUrlToActiveFilters } from "./filterUrlParams";

describe("combined inventory filters", () => {
  it("restores colliding identities and unavailable options and preserves URL round trips", () => {
    const url = new URLSearchParams(
      "building=7&building=null&container=7&rack=9&rack=null&tank=9&tank=999&site=2&zone=A",
    );
    const combined = combineInventoryFilters(parseUrlToActiveFilters(url));
    expect(combined.dropdownFilters.building).toEqual(["7", "null", "container:7"]);
    expect(combined.dropdownFilters.rack).toEqual(["9", "null", "tank:9", "tank:999"]);
    expect(combined.dropdownFilters.container).toBeUndefined();
    const restored = encodeActiveFiltersToURL(separateInventoryFilters(combined));
    for (const key of ["building", "container", "rack", "tank", "site", "zone"])
      expect(restored.getAll(key).sort()).toEqual(url.getAll(key).sort());
    delete combined.dropdownFilters.building;
    const cleared = encodeActiveFiltersToURL(separateInventoryFilters(combined));
    expect(cleared.has("building")).toBe(false);
    expect(cleared.has("container")).toBe(false);
    expect(cleared.getAll("tank")).toEqual(["9", "999"]);
  });
  it("distinguishes identically named and numbered conventional and CT1 options", () => {
    expect(combinedInventoryOptions([{ id: "7", label: "North" }], "container", [{ id: "7", label: "North" }])).toEqual(
      [
        { id: "7", label: "North" },
        { id: "container:7", label: "North (Container)" },
      ],
    );
  });
});
