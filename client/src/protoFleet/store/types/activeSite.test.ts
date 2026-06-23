import { describe, expect, it } from "vitest";

import { DEFAULT_ACTIVE_SITE, isActiveSite, sanitizeActiveSite } from "./activeSite";

describe("active site runtime guard", () => {
  it("accepts supported active-site variants", () => {
    expect(isActiveSite({ kind: "all" })).toBe(true);
    expect(isActiveSite({ kind: "unassigned" })).toBe(true);
    expect(isActiveSite({ kind: "site", id: "7" })).toBe(true);
  });

  it("rejects malformed site ids", () => {
    expect(isActiveSite({ kind: "site", id: "" })).toBe(false);
    expect(isActiveSite({ kind: "site", id: "0" })).toBe(false);
    expect(isActiveSite({ kind: "site", id: "abc" })).toBe(false);
  });

  it("sanitizes invalid values to all-sites", () => {
    expect(sanitizeActiveSite({ kind: "site", id: "abc" })).toEqual(DEFAULT_ACTIVE_SITE);
  });
});
