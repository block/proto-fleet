import { MemoryRouter } from "react-router-dom";
import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import Dashboard from "./Dashboard";
import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { SiteScopeProvider } from "@/protoFleet/routing/siteScope";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// Capture the scope the data hooks are asked to fetch. The scope-validation
// hooks run at the top of Dashboard before the paired/unpaired branch, so an
// unpaired render (stubbed MinersPage) is enough to assert scoping.
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

vi.mock("@/protoFleet/api/useOnboardedStatus", () => ({
  useOnboardedStatus: () => ({ devicePaired: false, statusLoaded: true }),
}));

vi.mock("@/protoFleet/features/onboarding", () => ({
  MinersPage: () => <div data-testid="miners-page" />,
}));

vi.mock("@/protoFleet/features/alerts/api/useAlertsEnabled", () => ({
  useAlertsEnabled: () => false,
}));

vi.mock("@/shared/hooks/useStickyState", () => ({
  useStickyState: () => ({ refs: { vertical: { start: { current: null }, end: { current: null } } } }),
}));

// Dashboard reads duration/permission from the store barrel; useActiveSite
// reads the real useFleetStore module, so leave that untouched.
vi.mock("@/protoFleet/store", () => ({
  useDuration: () => "24h",
  useSetDuration: () => vi.fn(),
  useHasPermission: () => false,
}));

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
vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => sitesCtx.current,
}));

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
  useFleetStore.setState((state) => {
    state.ui.activeSite = DEFAULT_ACTIVE_SITE;
  });
});

describe("Dashboard route scoping", () => {
  it("keeps the route site scope when the org catalog was skipped for permissions", () => {
    // Site-scoped operator: reached /:site/dashboard via ResolveSiteBySlug, but
    // lacks org-scoped site:read, so the shared catalog is [] / not granted.
    sitesCtx.current = { ...sitesCtx.current, sites: [], siteCatalogAccessGranted: false };

    renderScopedDashboard();

    // The catalog is treated as unknown, so useActiveSite does NOT strip the
    // scope — counts stay scoped to the route site rather than going org-wide.
    expect(fleetCountsMock).toHaveBeenCalled();
    expect(lastSiteIds()).toEqual([7n]);
  });

  it("de-scopes to all-sites when an authoritative catalog does not contain the route site", () => {
    // Org reader with a loaded catalog that genuinely lacks site 7 (deleted).
    sitesCtx.current = {
      ...sitesCtx.current,
      sites: [create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 9n, name: "Dallas", slug: "dallas" }) })],
      sitesLoaded: true,
      siteCatalogAccessGranted: true,
    };

    renderScopedDashboard();

    expect(lastSiteIds()).toEqual([]);
  });
});
