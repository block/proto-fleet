import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import Dashboard from "./Dashboard";
import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { SiteScopeProvider } from "@/protoFleet/routing/siteScope";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// Capture the scope the data hooks are asked to fetch.
const fleetCountsMock = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/api/useFleetCounts", () => ({
  __esModule: true,
  default: (opts: { siteIds?: string[] }) => {
    fleetCountsMock(opts);
    return { totalMiners: 0, stateCounts: undefined, hasLoaded: false };
  },
}));

vi.mock("@/protoFleet/api/useTelemetryMetrics", () => ({
  useTelemetryMetrics: () => ({ data: { metrics: [] } }),
}));

// Paired dashboard so the heading SitePicker renders (the picker is what
// previously de-scoped the route via its own useActiveSite).
vi.mock("@/protoFleet/api/useOnboardedStatus", () => ({
  useOnboardedStatus: () => ({ devicePaired: true, statusLoaded: true }),
}));

// site:read gates whether the org catalog is fetched (and the picker mounts);
// keep everything else denied so ActiveAlertsCard stays out.
const permMock = vi.hoisted(() => ({ current: (_key: string): boolean => true }));
vi.mock("@/protoFleet/store", () => ({
  useDuration: () => "24h",
  useSetDuration: () => vi.fn(),
  useHasPermission: (key: string) => permMock.current(key),
}));

vi.mock("@/protoFleet/features/alerts/api/useAlertsEnabled", () => ({ useAlertsEnabled: () => false }));
vi.mock("@/shared/hooks/useStickyState", () => ({
  useStickyState: () => ({ refs: { vertical: { start: { current: null }, end: { current: null } } } }),
}));

// Stub the heavy dashboard sections — this test only cares about scope
// resolution + whether the (real) SitePicker mounts.
vi.mock("@/protoFleet/features/onboarding/components/CompleteSetup", () => ({ CompleteSetup: () => null }));
vi.mock("@/protoFleet/features/dashboard/components/FleetHealthSection", () => ({
  __esModule: true,
  default: () => null,
}));
vi.mock("@/protoFleet/features/dashboard/components/FleetHealthMetrics", () => ({
  __esModule: true,
  default: () => null,
}));
vi.mock("@/protoFleet/features/dashboard/components/SitesSection", () => ({ __esModule: true, default: () => null }));
vi.mock("@/protoFleet/features/dashboard/components/SectionHeading", () => ({ __esModule: true, default: () => null }));
vi.mock("@/protoFleet/features/dashboard/components/HashratePanel", () => ({ HashratePanel: () => null }));
vi.mock("@/protoFleet/features/dashboard/components/UptimePanel", () => ({ UptimePanel: () => null }));
vi.mock("@/protoFleet/features/dashboard/components/TemperaturePanel", () => ({ TemperaturePanel: () => null }));
vi.mock("@/protoFleet/features/dashboard/components/PowerPanel", () => ({ PowerPanel: () => null }));
vi.mock("@/protoFleet/features/dashboard/components/EfficiencyPanel", () => ({ EfficiencyPanel: () => null }));
vi.mock("@/shared/components/DurationSelector", () => ({ __esModule: true, default: () => null, fleetDurations: [] }));

const sitesCtx = vi.hoisted(() => ({
  current: {
    sites: [] as SiteWithCounts[] | undefined,
    sitesError: null as string | null,
    sitesLoaded: false,
    sitesSettled: true,
    sitesPermissionDenied: false,
    siteCatalogAccessGranted: false,
    refetchSites: vi.fn(),
  },
}));
vi.mock("@/protoFleet/api/SitesContext", () => ({ useSitesContext: () => sitesCtx.current }));

const site = (id: number, name: string) =>
  create(SiteWithCountsSchema, { site: create(SiteSchema, { id: BigInt(id), name, slug: name.toLowerCase() }) });

const lastSiteIds = () => {
  const calls = fleetCountsMock.mock.calls;
  return calls[calls.length - 1][0].siteIds;
};

const renderScopedDashboard = () =>
  render(
    <MemoryRouter initialEntries={["/austin/dashboard"]}>
      <SiteScopeProvider value={{ kind: "site", id: "7", slug: "austin" }}>
        <Dashboard />
      </SiteScopeProvider>
    </MemoryRouter>,
  );

beforeEach(() => {
  fleetCountsMock.mockClear();
  permMock.current = () => true;
  useFleetStore.setState((state) => {
    state.ui.activeSite = DEFAULT_ACTIVE_SITE;
  });
});

describe("Dashboard route scoping", () => {
  it("preserves the route scope and hides the picker when the catalog was skipped for permissions", () => {
    // Site-scoped operator: reached /austin/dashboard via ResolveSiteBySlug but
    // lacks org-scoped site:read, so the shared catalog is [] / not granted.
    permMock.current = (key) => key !== "site:read";
    sitesCtx.current = { ...sitesCtx.current, sites: [], siteCatalogAccessGranted: false };

    renderScopedDashboard();

    // Picker is not mounted, so its internal useActiveSite can't strip the
    // scope; counts stay scoped to the route site.
    expect(screen.queryByTestId("site-picker-trigger")).not.toBeInTheDocument();
    expect(lastSiteIds()).toEqual([7n]);
  });

  it("mounts the picker and stays scoped when the loaded catalog contains the route site", () => {
    sitesCtx.current = {
      ...sitesCtx.current,
      sites: [site(7, "Austin")],
      sitesLoaded: true,
      siteCatalogAccessGranted: true,
    };

    renderScopedDashboard();

    expect(screen.getByTestId("site-picker-trigger")).toBeInTheDocument();
    expect(lastSiteIds()).toEqual([7n]);
  });

  it("de-scopes to all-sites when an authoritative catalog does not contain the route site", () => {
    sitesCtx.current = {
      ...sitesCtx.current,
      sites: [site(9, "Dallas")],
      sitesLoaded: true,
      siteCatalogAccessGranted: true,
    };

    renderScopedDashboard();

    expect(lastSiteIds()).toEqual([]);
  });
});
