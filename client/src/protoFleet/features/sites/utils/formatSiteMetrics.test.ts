import { describe, expect, it } from "vitest";

import { formatLocation } from "./formatSiteMetrics";

describe("formatLocation", () => {
  it("joins city and state with a comma", () => {
    expect(formatLocation("Austin", "TX")).toBe("Austin, TX");
  });

  it("falls back to whichever field is set", () => {
    expect(formatLocation("Austin", "")).toBe("Austin");
    expect(formatLocation("", "TX")).toBe("TX");
  });

  it("returns null when both are empty or whitespace-only", () => {
    expect(formatLocation("", "")).toBeNull();
    expect(formatLocation("  ", " ")).toBeNull();
  });
});
