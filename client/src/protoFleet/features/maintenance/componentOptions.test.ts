import { describe, expect, it } from "vitest";
import { getComponentOptions } from "./componentOptions";

describe("ticket component choices", () => {
  it("defaults every category to Unknown and offers Other without irrelevant named components", () => {
    expect(getComponentOptions("module").map((option) => option.value)).toEqual([
      "Unknown",
      "Fan",
      "Hashboard",
      "PSU",
      "Control Board",
      "Other",
    ]);
    expect(getComponentOptions("infrastructure").map((option) => option.value)).toContain("Network");
    expect(getComponentOptions("other").map((option) => option.value)).toEqual(["Unknown", "Other"]);
  });
  it("keeps historical arbitrary component values readable", () => {
    expect(getComponentOptions("miner", "Custom legacy part")).toContainEqual({
      value: "Custom legacy part",
      label: "Custom legacy part",
    });
  });
});
