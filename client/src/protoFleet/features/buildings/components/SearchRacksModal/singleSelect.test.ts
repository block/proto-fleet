import { describe, expect, it } from "vitest";

import { reduceToSingleSelection } from "./singleSelect";

describe("reduceToSingleSelection", () => {
  it("returns empty when nothing is selected", () => {
    expect(reduceToSingleSelection([], [])).toEqual([]);
  });

  it("passes single-id selections through", () => {
    expect(reduceToSingleSelection([], ["abc"])).toEqual(["abc"]);
  });

  it("retains only the newly-toggled id when multiple come in", () => {
    // Operator had no selection; clicks two rapidly — keep the new id.
    expect(reduceToSingleSelection(["1"], ["1", "2"])).toEqual(["2"]);
  });

  it("falls back to the last id if no id is new (defensive)", () => {
    expect(reduceToSingleSelection(["1", "2"], ["1", "2"])).toEqual(["2"]);
  });

  it("ignores non-array input by treating it as empty", () => {
    expect(reduceToSingleSelection([], null)).toEqual([]);
    expect(reduceToSingleSelection([], undefined)).toEqual([]);
  });

  it("filters out non-string entries defensively", () => {
    // Type signature allows wider input; only retain strings.
    // Both "1" and "3" are new (currentSelected is empty); the
    // single-select reduce picks the first new id from the filtered
    // array.
    expect(reduceToSingleSelection([], ["1", 2 as unknown as string, "3"])).toEqual(["1"]);
  });
});
