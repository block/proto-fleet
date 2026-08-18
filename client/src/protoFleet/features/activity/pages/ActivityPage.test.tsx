import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import ActivityPage from "./ActivityPage";
import {
  type ActivityEntry,
  ActivityEntrySchema,
  type ActivityFilter,
} from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { buildAlertHistoryEntry, buildPagedAlertsResult } from "@/protoFleet/features/alerts/alertHistory.fixtures";
import type { UsePagedAlertsOptions } from "@/protoFleet/features/alerts/api/usePagedAlerts";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

const permissionsMock = vi.hoisted(() => ({
  current: { "activity:read": true, "alert:read": true } as Record<string, boolean>,
}));
const useActivityMock = vi.hoisted(() => vi.fn());
const usePagedAlertsMock = vi.hoisted(() => vi.fn());
const alertsEnabledMock = vi.hoisted(() => ({ current: true, resolved: true, failing: false }));
const filtersEventTypesMock = vi.hoisted(() => ({ current: [] as { eventType: string }[] }));
const filtersOnScopesChangeMock = vi.hoisted(() => ({
  current: undefined as ((scopes: string[]) => void) | undefined,
}));
const filtersOnTypesChangeMock = vi.hoisted(() => ({
  current: undefined as ((types: string[]) => void) | undefined,
}));
const exportCsvMock = vi.hoisted(() => vi.fn());
const activeSiteMock = vi.hoisted(() => ({ current: { kind: "all" } as ActiveSite }));

let listFilter: ActivityFilter | undefined;
let exportFilter: ActivityFilter | undefined;

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (permission: string) => permissionsMock.current[permission] ?? false,
}));

vi.mock("@/protoFleet/features/alerts/api/useAlertsEnabled", () => ({
  useAlertsEnabledState: () => ({
    enabled: alertsEnabledMock.current,
    resolved: alertsEnabledMock.resolved,
    failing: alertsEnabledMock.failing,
  }),
}));

vi.mock("@/protoFleet/features/alerts/api/usePagedAlerts", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/features/alerts/api/usePagedAlerts")>()),
  usePagedAlerts: usePagedAlertsMock,
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
  default: ({
    actions,
    eventTypes,
    onScopesChange,
    onTypesChange,
  }: {
    actions?: ReactNode;
    eventTypes: { eventType: string }[];
    onScopesChange: (scopes: string[]) => void;
    onTypesChange: (types: string[]) => void;
  }) => {
    filtersEventTypesMock.current = eventTypes;
    filtersOnScopesChangeMock.current = onScopesChange;
    filtersOnTypesChangeMock.current = onTypesChange;
    return <div data-testid="activity-filters">{actions}</div>;
  },
}));

vi.mock("@/protoFleet/features/activity/components/ActivityTable", () => ({
  default: ({ activities }: { activities: ActivityEntry[] }) => (
    <div data-testid="activity-table">{activities.map((entry) => entry.eventId).join(",")}</div>
  ),
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

const pagedAlertsEnabled = (): boolean =>
  usePagedAlertsMock.mock.calls.some((call) => (call[2] as UsePagedAlertsOptions | undefined)?.enabled === true);

const lastUseActivityParams = (): { enabled?: boolean } | undefined =>
  useActivityMock.mock.calls[useActivityMock.mock.calls.length - 1]?.[0] as { enabled?: boolean } | undefined;

describe("ActivityPage", () => {
  beforeEach(() => {
    permissionsMock.current = { "activity:read": true, "alert:read": true };
    alertsEnabledMock.current = true;
    alertsEnabledMock.resolved = true;
    alertsEnabledMock.failing = false;
    filtersEventTypesMock.current = [];
    filtersOnScopesChangeMock.current = undefined;
    filtersOnTypesChangeMock.current = undefined;
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
    usePagedAlertsMock.mockReturnValue(buildPagedAlertsResult());
    exportCsvMock.mockImplementation((filter?: ActivityFilter) => {
      exportFilter = filter;
    });
  });

  describe("permission guard", () => {
    it("redirects without calling activity data hooks when both reads are missing", async () => {
      permissionsMock.current = {};

      renderActivityRoute();

      await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/"));
      expect(screen.getByTestId("home-page")).toBeInTheDocument();
      expect(useActivityMock).not.toHaveBeenCalled();
    });

    it("renders activity content when org activity:read is present", () => {
      renderActivityRoute();

      expect(screen.getByTestId("location-probe").textContent).toBe("/activity");
      expect(screen.getByTestId("activity-table")).toBeInTheDocument();
      expect(useActivityMock).toHaveBeenCalled();
      expect(lastUseActivityParams()?.enabled).toBe(true);
    });

    it("renders an alerts-only view for alert:read without activity:read", () => {
      permissionsMock.current = { "alert:read": true };

      renderActivityRoute();

      expect(screen.getByTestId("location-probe").textContent).toBe("/activity");
      expect(lastUseActivityParams()?.enabled).toBe(false);
      expect(pagedAlertsEnabled()).toBe(true);
      expect(screen.queryByText("Export activity CSV")).not.toBeInTheDocument();
    });

    it("explains the empty page to alert-only viewers when the alerts feature is off", () => {
      permissionsMock.current = { "alert:read": true };
      alertsEnabledMock.current = false;

      renderActivityRoute();

      expect(screen.getByText(/alerts aren't enabled on this server/i)).toBeInTheDocument();
      expect(screen.queryByTestId("activity-table")).not.toBeInTheDocument();
      expect(pagedAlertsEnabled()).toBe(false);
    });

    it("shows loading, not the disabled claim, while the alerts probe is unanswered", () => {
      permissionsMock.current = { "alert:read": true };
      alertsEnabledMock.current = false;
      alertsEnabledMock.resolved = false;

      renderActivityRoute();

      expect(screen.queryByText(/alerts aren't enabled on this server/i)).not.toBeInTheDocument();
      expect(screen.queryByTestId("activity-table")).not.toBeInTheDocument();
    });

    it("warns dual-permission viewers that the feed may be missing alerts while the probe fails", () => {
      alertsEnabledMock.current = false;
      alertsEnabledMock.resolved = false;
      alertsEnabledMock.failing = true;

      renderActivityRoute();

      expect(screen.getByTestId("activity-table")).toBeInTheDocument();
      expect(screen.getByText(/may be missing alert events/i)).toBeInTheDocument();
    });

    it("tells alert-only viewers when the probe keeps failing instead of spinning forever", () => {
      permissionsMock.current = { "alert:read": true };
      alertsEnabledMock.current = false;
      alertsEnabledMock.resolved = false;
      alertsEnabledMock.failing = true;

      renderActivityRoute();

      expect(screen.getByText(/can't be loaded right now/i)).toBeInTheDocument();
      expect(screen.queryByText(/alerts aren't enabled on this server/i)).not.toBeInTheDocument();
      expect(screen.queryByTestId("activity-table")).not.toBeInTheDocument();
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
      screen.getByText("Export activity CSV").click();

      expect(exportFilter?.siteIds).toEqual([7n]);
      expect(exportFilter?.includeUnassigned).toBe(false);
    });
  });

  describe("merged alert history", () => {
    const alertItem = buildAlertHistoryEntry({ id: "9", received_at: "2026-08-01T00:00:10Z" });

    it("interleaves alert history into the feed by time", () => {
      useActivityMock.mockReturnValue({
        activities: [
          create(ActivityEntrySchema, {
            eventId: "act-new",
            createdAt: timestampFromDate(new Date("2026-08-01T00:00:20Z")),
          }),
          create(ActivityEntrySchema, {
            eventId: "act-old",
            createdAt: timestampFromDate(new Date("2026-08-01T00:00:00Z")),
          }),
        ],
        totalCount: 2,
        isLoading: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refresh: vi.fn(),
      });
      usePagedAlertsMock.mockReturnValue(buildPagedAlertsResult({ items: [alertItem] }));

      render(<ActivityPage />);

      expect(pagedAlertsEnabled()).toBe(true);
      expect(screen.getByTestId("activity-table").textContent).toBe("act-new,alert-9,act-old");
      expect(filtersEventTypesMock.current.some((option) => option.eventType === "alert")).toBe(true);
    });

    it("withholds early-arriving activities until the alert feed's first page lands", () => {
      useActivityMock.mockReturnValue({
        activities: [
          create(ActivityEntrySchema, {
            eventId: "act-new",
            createdAt: timestampFromDate(new Date("2026-08-01T00:00:30Z")),
          }),
          create(ActivityEntrySchema, {
            eventId: "act-old",
            createdAt: timestampFromDate(new Date("2026-08-01T00:00:00Z")),
          }),
        ],
        totalCount: 2,
        isLoading: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refresh: vi.fn(),
      });
      usePagedAlertsMock.mockReturnValue(buildPagedAlertsResult({ loading: true }));

      const { rerender } = render(<ActivityPage />);

      // Rendering the resolved activities now would show act-old only to hide it once the alert
      // cursor arrives, so the initial spinner covers both feeds' first responses.
      expect(screen.queryByTestId("activity-table")).not.toBeInTheDocument();

      usePagedAlertsMock.mockReturnValue(
        buildPagedAlertsResult({
          items: [buildAlertHistoryEntry({ id: "9", received_at: "2026-08-01T00:00:20Z" })],
          hasMore: true,
        }),
      );
      rerender(<ActivityPage />);

      expect(screen.getByTestId("activity-table").textContent).toBe("act-new,alert-9");
    });

    it("keeps loaded activities visible while a late-enabled alert feed loads its first page", () => {
      alertsEnabledMock.current = false;
      alertsEnabledMock.resolved = false;
      useActivityMock.mockReturnValue({
        activities: [],
        totalCount: 0,
        isLoading: true,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refresh: vi.fn(),
      });

      const { rerender } = render(<ActivityPage />);

      useActivityMock.mockReturnValue({
        activities: [
          create(ActivityEntrySchema, {
            eventId: "act-new",
            createdAt: timestampFromDate(new Date("2026-08-01T00:00:30Z")),
          }),
        ],
        totalCount: 1,
        isLoading: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refresh: vi.fn(),
      });
      rerender(<ActivityPage />);
      expect(screen.getByTestId("activity-table").textContent).toBe("act-new");

      // The probe answers late: the first alert page's ordering barrier must not blank the rows
      // already on screen the way it withholds them on a cold load.
      alertsEnabledMock.current = true;
      alertsEnabledMock.resolved = true;
      usePagedAlertsMock.mockReturnValue(buildPagedAlertsResult({ loading: true }));
      rerender(<ActivityPage />);
      expect(screen.getByTestId("activity-table").textContent).toBe("act-new");

      usePagedAlertsMock.mockReturnValue(
        buildPagedAlertsResult({ items: [buildAlertHistoryEntry({ id: "9", received_at: "2026-08-01T00:00:20Z" })] }),
      );
      rerender(<ActivityPage />);
      expect(screen.getByTestId("activity-table").textContent).toBe("act-new,alert-9");
    });

    it("stops applying a selected Alerts pseudo-type once the org-scoped read is denied", () => {
      const { rerender } = render(<ActivityPage />);
      act(() => filtersOnTypesChangeMock.current?.(["alert"]));
      expect(listFilter?.eventTypes).toEqual(["alert"]);

      usePagedAlertsMock.mockReturnValue(buildPagedAlertsResult({ error: "permission denied", denied: true }));
      rerender(<ActivityPage />);

      // The Alerts option is gone, so its stale selection must not keep filtering activities to none.
      expect(listFilter?.eventTypes).toEqual([]);
    });

    it("ignores activity-only filters for an alert-only viewer instead of hiding the alert feed", () => {
      permissionsMock.current = { "alert:read": true };
      usePagedAlertsMock.mockReturnValue(buildPagedAlertsResult({ items: [alertItem] }));

      render(<ActivityPage />);
      act(() => filtersOnTypesChangeMock.current?.(["login"]));

      expect(screen.getByTestId("activity-table").textContent).toBe("alert-9");
    });

    it("keeps the alert feed when activity-only filters outlive a server-side activity denial", () => {
      // The cached client permission still enables the hook; the server denies and the hook reports it.
      useActivityMock.mockImplementation(({ filter }: { filter?: ActivityFilter }) => {
        listFilter = filter;
        return {
          activities: [],
          totalCount: 0,
          isLoading: false,
          error: "permission denied",
          denied: true,
          hasMore: false,
          loadMore: vi.fn(),
          refresh: vi.fn(),
        };
      });
      usePagedAlertsMock.mockReturnValue(buildPagedAlertsResult({ items: [alertItem] }));

      render(<ActivityPage />);
      act(() => filtersOnTypesChangeMock.current?.(["login"]));

      expect(screen.getByTestId("activity-table").textContent).toBe("alert-9");
    });

    it("retires non-device scope selections that outlive a server-side activity denial", () => {
      useActivityMock.mockImplementation(({ filter }: { filter?: ActivityFilter }) => {
        listFilter = filter;
        return {
          activities: [],
          totalCount: 0,
          isLoading: false,
          error: "permission denied",
          denied: true,
          hasMore: false,
          loadMore: vi.fn(),
          refresh: vi.fn(),
        };
      });
      usePagedAlertsMock.mockReturnValue(
        buildPagedAlertsResult({
          items: [buildAlertHistoryEntry({ id: "9" }), buildAlertHistoryEntry({ id: "8", device_name: "" })],
        }),
      );

      render(<ActivityPage />);
      act(() => filtersOnScopesChangeMock.current?.(["rack"]));

      // A rack scope can never match an alert; applied, it would blank the only remaining feed.
      expect(screen.getByTestId("activity-table").textContent).toBe("alert-9,alert-8");

      act(() => filtersOnScopesChangeMock.current?.(["rack", "device"]));

      // The device portion of the selection still filters: alert-8 carries no device scope.
      expect(screen.getByTestId("activity-table").textContent).toBe("alert-9");
    });

    it("does not hold activities back behind scope-filtered alert rows", () => {
      useActivityMock.mockReturnValue({
        activities: [
          create(ActivityEntrySchema, {
            eventId: "act-new",
            createdAt: timestampFromDate(new Date("2026-08-01T00:00:30Z")),
          }),
          create(ActivityEntrySchema, {
            eventId: "act-mid",
            createdAt: timestampFromDate(new Date("2026-08-01T00:00:12Z")),
          }),
        ],
        totalCount: 2,
        isLoading: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refresh: vi.fn(),
      });
      usePagedAlertsMock.mockReturnValue(
        buildPagedAlertsResult({
          items: [
            buildAlertHistoryEntry({ id: "9", received_at: "2026-08-01T00:00:20Z" }),
            buildAlertHistoryEntry({ id: "8", received_at: "2026-08-01T00:00:05Z", device_name: "" }),
          ],
          hasMore: true,
        }),
      );

      render(<ActivityPage />);
      act(() => filtersOnScopesChangeMock.current?.(["device"]));

      // act-mid (00:12) sits between the device alert (00:20) and the loaded device-less alert (00:05);
      // the merge frontier is the full loaded alert feed, so hiding alert-8 must not also hide act-mid.
      expect(screen.getByTestId("activity-table").textContent).toBe("act-new,alert-9,act-mid");
    });

    it("keeps the alert feed off without alert:read", () => {
      permissionsMock.current = { "activity:read": true };

      render(<ActivityPage />);

      expect(pagedAlertsEnabled()).toBe(false);
    });

    it("keeps the alert feed off when the alerts feature is disabled", () => {
      alertsEnabledMock.current = false;

      render(<ActivityPage />);

      expect(pagedAlertsEnabled()).toBe(false);
    });

    it("keeps the alert feed off on a site-scoped route since ListAlerts is org-wide", () => {
      activeSiteMock.current = { kind: "site", id: "42", slug: "north" };

      render(<ActivityPage />);

      expect(pagedAlertsEnabled()).toBe(false);
    });

    it("degrades a denied org-scoped read to a visible partial-data note, not a silent omission", () => {
      // The hook clears its own rows and cursor on denial, so denied arrives with an empty feed.
      usePagedAlertsMock.mockReturnValue(buildPagedAlertsResult({ error: "permission denied", denied: true }));

      render(<ActivityPage />);

      expect(screen.getByTestId("activity-table").textContent).toBe("");
      expect(screen.queryByText("permission denied")).not.toBeInTheDocument();
      expect(screen.getByText(/alert history is unavailable/i)).toBeInTheDocument();
      expect(filtersEventTypesMock.current.some((option) => option.eventType === "alert")).toBe(false);
    });

    it("surfaces the denial error for an alert-only viewer with no fallback feed", () => {
      permissionsMock.current = { "alert:read": true };
      usePagedAlertsMock.mockReturnValue(buildPagedAlertsResult({ error: "permission denied", denied: true }));

      render(<ActivityPage />);

      expect(screen.getByText("permission denied")).toBeInTheDocument();
    });
  });
});
