import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ActivityPage from "./ActivityPage";
import type { ActivityFilter } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

const canReadActivityMock = vi.hoisted(() => ({ current: true }));
const useActivityMock = vi.hoisted(() => vi.fn());
const exportCsvMock = vi.hoisted(() => vi.fn());
const activeSiteMock = vi.hoisted(() => ({ current: { kind: "all" } as ActiveSite }));

let listFilter: ActivityFilter | undefined;
let exportFilter: ActivityFilter | undefined;

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: () => canReadActivityMock.current,
}));

vi.mock("@/protoFleet/api/useActivity", () => ({
  useActivity: useActivityMock,
}));

vi.mock("@/protoFleet/api/useActivityFilterOptions", () => ({
  useActivityFilterOptions: () => ({ eventTypes: [], scopeTypes: [], users: [], isLoading: false, error: null }),
}));

vi.mock("@/protoFleet/api/useExportActivity", () => ({
  useExportActivity: () => ({ exportCsv: exportCsvMock, isExportingCsv: false }),
}));

// Keep the real siteFilterFromActive and only stub useActiveSite so each case
// can pin a route scope. The page no longer fetches sites itself; the global
// SitePicker owns ListSites and knownSiteIds validation.
vi.mock("@/protoFleet/components/PageHeader/SitePicker", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/components/PageHeader/SitePicker")>();
  return { ...actual, useActiveSite: () => ({ activeSite: activeSiteMock.current, setActiveSite: vi.fn() }) };
});

// The presentational children pull in their own dependency trees; stub them so
// these tests isolate permission gating and filter wiring.
vi.mock("@/protoFleet/features/activity/components/ActivityFilters", () => ({
  default: ({ actions }: { actions?: ReactNode }) => <div data-testid="activity-filters">{actions}</div>,
}));

vi.mock("@/protoFleet/features/activity/components/ActivityTable", () => ({
  default: () => <div data-testid="activity-table" />,
}));

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{location.pathname}</div>;
};

const renderActivityRoute = () =>
  render(
    <MemoryRouter initialEntries={["/activity"]}>
      <Routes>
        <Route path="/" element={<div data-testid="home-page">Home</div>} />
        <Route path="/activity" element={<ActivityPage />} />
      </Routes>
      <LocationProbe />
    </MemoryRouter>,
  );

describe("ActivityPage", () => {
  beforeEach(() => {
    canReadActivityMock.current = true;
    activeSiteMock.current = { kind: "all" };
    listFilter = undefined;
    exportFilter = undefined;
    vi.clearAllMocks();
    useActivityMock.mockImplementation(({ filter }: { filter?: ActivityFilter }) => {
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
    });
    exportCsvMock.mockImplementation((filter?: ActivityFilter) => {
      exportFilter = filter;
    });
  });

  describe("permission guard", () => {
    it("redirects without calling activity data hooks when org activity:read is missing", async () => {
      canReadActivityMock.current = false;

      renderActivityRoute();

      await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/"));
      expect(screen.getByTestId("home-page")).toBeInTheDocument();
      expect(useActivityMock).not.toHaveBeenCalled();
    });

    it("renders activity content when org activity:read is present", () => {
      renderActivityRoute();

      expect(screen.getByTestId("location-probe").textContent).toBe("/activity");
      expect(screen.getByTestId("activity-table")).toBeInTheDocument();
      expect(useActivityMock).toHaveBeenCalledOnce();
    });
  });

  describe("site scope", () => {
    it("sends an empty site filter for the all-sites route", () => {
      activeSiteMock.current = { kind: "all" };

      render(<ActivityPage />);

      expect(listFilter?.siteIds).toEqual([]);
      expect(listFilter?.includeUnassigned).toBe(false);
    });

    it("sends the active site id for a site-scoped route", () => {
      activeSiteMock.current = { kind: "site", id: "42", slug: "north" };

      render(<ActivityPage />);

      expect(listFilter?.siteIds).toEqual([42n]);
      expect(listFilter?.includeUnassigned).toBe(false);
    });

    it("sends include_unassigned for the unassigned route", () => {
      activeSiteMock.current = { kind: "unassigned" };

      render(<ActivityPage />);

      expect(listFilter?.siteIds).toEqual([]);
      expect(listFilter?.includeUnassigned).toBe(true);
    });

    it("applies the same scope to the CSV export as the feed", () => {
      activeSiteMock.current = { kind: "site", id: "7", slug: "north" };

      render(<ActivityPage />);
      screen.getByText("Export CSV").click();

      expect(exportFilter?.siteIds).toEqual([7n]);
      expect(exportFilter?.includeUnassigned).toBe(false);
    });
  });
});
