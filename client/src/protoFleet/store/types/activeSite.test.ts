import { describe, expect, it } from "vitest";

import { DEFAULT_ACTIVE_SITE, isActiveSite, sanitizeActiveSite } from "./activeSite";

describe("active site runtime guard", () => {
  it("accepts supported active-site variants", () => {
    expect(isActiveSite({ kind: "all" })).toBe(true);
    expect(isActiveSite({ kind: "unassigned" })).toBe(true);
    expect(isActiveSite({ kind: "site", id: "7", slug: "north-dc" })).toBe(true);
  });

  it("rejects malformed site ids and slugs", () => {
    expect(isActiveSite({ kind: "site", id: "", slug: "north-dc" })).toBe(false);
    expect(isActiveSite({ kind: "site", id: "0", slug: "north-dc" })).toBe(false);
    expect(isActiveSite({ kind: "site", id: "abc", slug: "north-dc" })).toBe(false);
    expect(isActiveSite({ kind: "site", id: "7" })).toBe(false);
    expect(isActiveSite({ kind: "site", id: "7", slug: "North_DC" })).toBe(false);
  });

  it("sanitizes invalid values to all-sites", () => {
    expect(sanitizeActiveSite({ kind: "site", id: "abc", slug: "north-dc" })).toEqual(DEFAULT_ACTIVE_SITE);
  });
});
