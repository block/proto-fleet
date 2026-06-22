import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ActivityPage from "./ActivityPage";
import type { ActivityFilter } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

// Capture the ActivityFilter the page builds and hands to the data layer so we
// can assert the active site scope is threaded into siteIds / includeUnassigned.
let listFilter: ActivityFilter | undefined;
let exportFilter: ActivityFilter | undefined;
const exportCsv = vi.fn((f?: ActivityFilter) => {
  exportFilter = f;
});

vi.mock("@/protoFleet/api/useActivity", () => ({
  useActivity: vi.fn(({ filter }: { filter?: ActivityFilter }) => {
    listFilter = filter;
    return {
      activities: [],
      totalCount: 1,
      isLoading: false,
      error: null,
      hasMore: false,
      loadMore: vi.fn(),
      refresh: vi.fn(),
    };
  }),
}));

vi.mock("@/protoFleet/api/useActivityFilterOptions", () => ({
  useActivityFilterOptions: () => ({ eventTypes: [], scopeTypes: [], users: [], isLoading: false, error: null }),
}));

vi.mock("@/protoFleet/api/useExportActivity", () => ({
  useExportActivity: () => ({ exportCsv, isExportingCsv: false }),
}));

// Keep the real siteFilterFromActive (the translation under test) and only
// stub useActiveSite so each case can pin a route scope. The page no longer
// fetches sites itself — the global SitePicker owns ListSites — so there is
// nothing else to mock here.
let mockActiveSite: ActiveSite = { kind: "all" };
vi.mock("@/protoFleet/components/PageHeader/SitePicker", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/components/PageHeader/SitePicker")>();
  return { ...actual, useActiveSite: () => ({ activeSite: mockActiveSite, setActiveSite: vi.fn() }) };
});

// The presentational children pull in their own dependency trees; stub them so
// the test isolates the filter-wiring logic.
vi.mock("@/protoFleet/features/activity/components/ActivityFilters", () => ({ default: () => null }));
vi.mock("@/protoFleet/features/activity/components/ActivityTable", () => ({ default: () => null }));

describe("ActivityPage site scope", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listFilter = undefined;
    exportFilter = undefined;
  });

  it("sends an empty site filter for the all-sites route", () => {
    mockActiveSite = { kind: "all" };
    render(<ActivityPage />);
    expect(listFilter?.siteIds).toEqual([]);
    expect(listFilter?.includeUnassigned).toBe(false);
  });

  it("sends the active site id for a site-scoped route", () => {
    mockActiveSite = { kind: "site", id: "42" };
    render(<ActivityPage />);
    expect(listFilter?.siteIds).toEqual([42n]);
    expect(listFilter?.includeUnassigned).toBe(false);
  });

  it("sends include_unassigned for the unassigned route", () => {
    mockActiveSite = { kind: "unassigned" };
    render(<ActivityPage />);
    expect(listFilter?.siteIds).toEqual([]);
    expect(listFilter?.includeUnassigned).toBe(true);
  });

  it("applies the same scope to the CSV export as the feed", () => {
    mockActiveSite = { kind: "site", id: "7" };
    const { getByText } = render(<ActivityPage />);
    getByText("Export CSV").click();
    expect(exportFilter?.siteIds).toEqual([7n]);
    expect(exportFilter?.includeUnassigned).toBe(false);
  });
});
