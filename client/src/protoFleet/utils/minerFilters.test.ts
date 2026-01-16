import { describe, expect, it } from "vitest";
import { createModelFilter, filterByModel } from "./minerFilters";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";

describe("minerFilters", () => {
  describe("createModelFilter", () => {
    it("should create a dropdown filter with all models as options", () => {
      const models = ["S19", "S21", "T21"];
      const filter = createModelFilter(models);

      expect(filter.type).toBe("dropdown");
      expect(filter.title).toBe("Model");
      expect(filter.value).toBe("model");
      expect(filter.options).toHaveLength(3);
      expect(filter.options.map((o) => o.id)).toEqual(["S19", "S21", "T21"]);
      expect(filter.options.map((o) => o.label)).toEqual(["S19", "S21", "T21"]);
    });

    it("should set all models as default selected options", () => {
      const models = ["S19", "S21"];
      const filter = createModelFilter(models);

      expect(filter.defaultOptionIds).toEqual(["S19", "S21"]);
    });

    it("should handle empty models array", () => {
      const filter = createModelFilter([]);

      expect(filter.options).toHaveLength(0);
      expect(filter.defaultOptionIds).toHaveLength(0);
    });

    it("should handle single model", () => {
      const filter = createModelFilter(["S19"]);

      expect(filter.options).toHaveLength(1);
      expect(filter.defaultOptionIds).toEqual(["S19"]);
    });
  });

  describe("filterByModel", () => {
    const createItem = (model: string) => ({ model, id: "test-id" });

    it("should return true when no model filter is applied (empty array)", () => {
      const filters: ActiveFilters = {
        buttonFilters: [],
        dropdownFilters: { model: [] },
      };

      expect(filterByModel(createItem("S19"), filters)).toBe(true);
      expect(filterByModel(createItem("S21"), filters)).toBe(true);
    });

    it("should return true when no model filter is applied (undefined)", () => {
      const filters: ActiveFilters = {
        buttonFilters: [],
        dropdownFilters: {},
      };

      expect(filterByModel(createItem("S19"), filters)).toBe(true);
    });

    it("should return true when item model matches filter", () => {
      const filters: ActiveFilters = {
        buttonFilters: [],
        dropdownFilters: { model: ["S19", "S21"] },
      };

      expect(filterByModel(createItem("S19"), filters)).toBe(true);
      expect(filterByModel(createItem("S21"), filters)).toBe(true);
    });

    it("should return false when item model does not match filter", () => {
      const filters: ActiveFilters = {
        buttonFilters: [],
        dropdownFilters: { model: ["S19"] },
      };

      expect(filterByModel(createItem("S21"), filters)).toBe(false);
      expect(filterByModel(createItem("T21"), filters)).toBe(false);
    });

    it("should work with single model filter", () => {
      const filters: ActiveFilters = {
        buttonFilters: [],
        dropdownFilters: { model: ["S19"] },
      };

      expect(filterByModel(createItem("S19"), filters)).toBe(true);
      expect(filterByModel(createItem("S21"), filters)).toBe(false);
    });
  });
});
