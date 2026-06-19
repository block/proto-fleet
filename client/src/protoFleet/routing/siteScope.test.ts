import { describe, expect, it } from "vitest";

import {
  activeSiteFromScopablePath,
  activeSiteFromSegment,
  appEntryPath,
  isPathScopable,
  scopeCurrentOrDashboardPath,
  scopedPath,
  unscopedScopablePath,
} from "./siteScope";

describe("siteScope routing helpers", () => {
  it("parses supported path scope segments", () => {
    expect(activeSiteFromSegment("7")).toEqual({ kind: "site", id: "7" });
    expect(activeSiteFromSegment("unassigned")).toEqual({ kind: "unassigned" });
    expect(activeSiteFromSegment("settings")).toBeNull();
    expect(activeSiteFromSegment("0")).toBeNull();
  });

  it("strips path scope from scopable routes only", () => {
    expect(unscopedScopablePath("/fleet/miners")).toBe("/fleet/miners");
    expect(unscopedScopablePath("/7/fleet/racks")).toBe("/fleet/racks");
    expect(unscopedScopablePath("/7/dashboard")).toBe("/dashboard");
    expect(unscopedScopablePath("/7/groups/team-a")).toBe("/groups/team-a");
    expect(unscopedScopablePath("/unassigned/activity")).toBe("/activity");
    expect(unscopedScopablePath("/unassigned/fleet/buildings")).toBe("/fleet/buildings");
    expect(unscopedScopablePath("/settings/general")).toBe("/settings/general");
  });

  it("detects scopable paths", () => {
    expect(isPathScopable("/dashboard")).toBe(true);
    expect(isPathScopable("/fleet")).toBe(true);
    expect(isPathScopable("/7/fleet/miners")).toBe(true);
    expect(isPathScopable("/7/groups/team-a")).toBe(true);
    expect(isPathScopable("/energy")).toBe(true);
    expect(isPathScopable("/settings")).toBe(false);
  });

  it("derives the active site from scopable paths", () => {
    expect(activeSiteFromScopablePath("/dashboard")).toEqual({ kind: "all" });
    expect(activeSiteFromScopablePath("/fleet/miners")).toEqual({ kind: "all" });
    expect(activeSiteFromScopablePath("/7/fleet/miners")).toEqual({ kind: "site", id: "7" });
    expect(activeSiteFromScopablePath("/7/activity")).toEqual({ kind: "site", id: "7" });
    expect(activeSiteFromScopablePath("/unassigned/fleet/miners")).toEqual({ kind: "unassigned" });
    expect(activeSiteFromScopablePath("/settings/general")).toBeNull();
  });

  it("prefixes scopable paths while preserving search and hash", () => {
    expect(scopedPath("/fleet/miners?site=8#rows", { kind: "site", id: "7" })).toBe("/7/fleet/miners?site=8#rows");
    expect(scopedPath("/7/fleet/miners?site=8", { kind: "all" })).toBe("/fleet/miners?site=8");
    expect(scopedPath("/fleet/racks", { kind: "unassigned" })).toBe("/unassigned/fleet/racks");
    expect(scopedPath("/dashboard", { kind: "site", id: "7" })).toBe("/7/dashboard");
    expect(scopedPath("/groups/team-a", { kind: "site", id: "7" })).toBe("/7/groups/team-a");
  });

  it("does not prefix non-scopable paths", () => {
    expect(scopedPath("/settings/general?tab=team", { kind: "site", id: "7" })).toBe("/settings/general?tab=team");
  });

  it("maps app entry to the preferred Dashboard scope", () => {
    expect(appEntryPath({ kind: "all" })).toBe("/dashboard");
    expect(appEntryPath({ kind: "site", id: "7" })).toBe("/7/dashboard");
    expect(appEntryPath({ kind: "unassigned" })).toBe("/unassigned/dashboard");
  });

  it("uses the current scopable path for picker navigation and Dashboard landing elsewhere", () => {
    expect(scopeCurrentOrDashboardPath("/fleet/miners", "?model=s19", "#top", { kind: "site", id: "7" })).toBe(
      "/7/fleet/miners?model=s19#top",
    );
    expect(scopeCurrentOrDashboardPath("/activity", "?type=event", "#top", { kind: "site", id: "7" })).toBe(
      "/7/activity?type=event#top",
    );
    expect(scopeCurrentOrDashboardPath("/settings/general", "?tab=team", "#top", { kind: "site", id: "7" })).toBe(
      "/7/dashboard",
    );
  });
});
