import { describe, expect, it } from "vitest";
import { formatScope } from "./formatScope";

describe("formatScope", () => {
  it("distinguishes containers from miners", () => {
    expect(formatScope("container", undefined, 1)).toBe("1 container");
    expect(formatScope("container", "Site A", 2)).toBe("Site A (2 containers)");
    expect(formatScope(undefined, undefined, 1)).toBe("1 miner");
    expect(formatScope("miner", "Site A", 2)).toBe("Site A (2 miners)");
  });

  it("preserves labels and empty scopes", () => {
    expect(formatScope("container", "Container 1")).toBe("Container 1");
    expect(formatScope("container")).toBe("—");
  });
});
