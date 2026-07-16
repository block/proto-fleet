import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useBuildingRackScope } from "./useBuildingRackScope";
import { type ActiveSite } from "@/protoFleet/store/types/activeSite";

// useBuildingRackScope reads the header SitePicker selection via useActiveSite
// and translates it into the rack list-filter shape. Mock only useActiveSite;
// keep the real siteFilterFromActive so we exercise the actual translation.
let mockActiveSite: ActiveSite = { kind: "all" };
vi.mock("@/protoFleet/components/PageHeader/SitePicker", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/components/PageHeader/SitePicker")>()),
  useActiveSite: () => ({ activeSite: mockActiveSite, setActiveSite: vi.fn() }),
}));

describe("useBuildingRackScope", () => {
  beforeEach(() => {
    mockActiveSite = { kind: "all" };
  });

  it("returns the empty filter for 'all sites' (no-op fetch, no regression)", () => {
    mockActiveSite = { kind: "all" };
    const { result } = renderHook(() => useBuildingRackScope());
    expect(result.current).toEqual({ siteIds: [], includeUnassigned: false });
  });

  it("scopes to the site AND surfaces site-unassigned racks for a specific site", () => {
    mockActiveSite = { kind: "site", id: "42", slug: "site-42" };
    const { result } = renderHook(() => useBuildingRackScope());
    expect(result.current.siteIds).toEqual([42n]);
    // The path into a site is assigning a currently site-unassigned rack, so
    // those must be visible even though the header scope is a single site.
    expect(result.current.includeUnassigned).toBe(true);
  });

  it("surfaces only site-unassigned racks for the 'unassigned' scope", () => {
    mockActiveSite = { kind: "unassigned" };
    const { result } = renderHook(() => useBuildingRackScope());
    expect(result.current).toEqual({ siteIds: [], includeUnassigned: true });
  });
});
