import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import SiteDetailPage from "./SiteDetailPage";
import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const listBuildingsBySiteMock = vi.hoisted(() => vi.fn());
const listSitesMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    listBuildingsBySite: listBuildingsBySiteMock,
  }),
}));

vi.mock("@/protoFleet/api/sites", async () => {
  const actual = await vi.importActual<typeof import("@/protoFleet/api/sites")>("@/protoFleet/api/sites");
  return {
    ...actual,
    useSites: () => ({
      listSites: listSitesMock,
    }),
  };
});

// SiteDetailPage reads the site catalog from the shell-level SitesProvider.
// Drive it directly so the page renders against a known catalog without the
// provider's fetch/poll machinery.
const sitesCtx = vi.hoisted(() => ({
  current: {
    sites: undefined as SiteWithCounts[] | undefined,
    sitesError: null as string | null,
    sitesLoaded: false,
    sitesSettled: false,
    sitesPermissionDenied: false,
    siteCatalogAccessGranted: false,
    refetchSites: vi.fn(),
  },
}));
vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => sitesCtx.current,
}));

const useTelemetryMetricsMock = vi.hoisted(() => vi.fn((_options: unknown) => ({ data: { metrics: [] } })));

vi.mock("@/protoFleet/api/useTelemetryMetrics", () => ({
  useTelemetryMetrics: (options: unknown) => useTelemetryMetricsMock(options),
}));

const useSiteStatsMock = vi.hoisted(() =>
  vi.fn((_options: unknown) => ({ stats: undefined, error: null, refetch: vi.fn() })),
);

vi.mock("@/protoFleet/api/useSiteStats", () => ({
  useSiteStats: (options: unknown) => useSiteStatsMock(options),
}));

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection", () => ({
  DeviceSetPerformanceSection: () => <div data-testid="device-set-performance-section">Performance charts</div>,
}));

vi.mock("@/protoFleet/features/sites/components/SiteModals", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/protoFleet/features/sites/hooks/useSiteModals", () => ({
  useSiteModals: () => ({
    openManageEdit: vi.fn(),
  }),
}));

vi.mock("@/protoFleet/features/buildings/components/BuildingModals", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/protoFleet/features/buildings/hooks/useBuildingModals", () => ({
  useBuildingModals: () => ({
    openDetailsCreate: vi.fn(),
    openDetailsEdit: vi.fn(),
  }),
}));

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{`${location.pathname}${location.search}`}</div>;
};

const makeSite = (id: bigint, name: string, slug = name.toLowerCase()) =>
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, {
      id,
      name,
      slug,
      country: "US",
    }),
    deviceCount: 0n,
    buildingCount: 0n,
    rackCount: 0n,
  });

const renderPage = (initialEntry = "/sites/7") =>
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/sites/:id" element={<SiteDetailPage />} />
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );

describe("SiteDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
      // Reset per-test so the performance section's fleet:read gate starts
      // from a known (denied) baseline; tests opt in explicitly.
      state.auth.permissions = [];
    });
    sitesCtx.current = {
      sites: [makeSite(7n, "Dallas"), makeSite(8n, "Austin")],
      sitesError: null,
      sitesLoaded: true,
      sitesSettled: true,
      sitesPermissionDenied: false,
      siteCatalogAccessGranted: true,
      refetchSites: vi.fn(),
    };
    listBuildingsBySiteMock.mockImplementation(({ onSuccess }: { onSuccess: (buildings: []) => void }) =>
      onSuccess([]),
    );
  });

  it("preserves the selected site when a site detail mismatch redirects back to Fleet", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });

    renderPage();

    await waitFor(() => expect(screen.getByTestId("location-probe")).toHaveTextContent("/austin/fleet"));
  });

  it("renders the metrics row scoped to the resolved site", async () => {
    renderPage("/sites/7");

    expect(await screen.findByTestId("site-detail-metrics-row")).toBeInTheDocument();
    await waitFor(() =>
      expect(useSiteStatsMock).toHaveBeenCalledWith(expect.objectContaining({ siteId: 7n, enabled: true })),
    );
  });

  it("renders the performance section scoped to the resolved site for fleet:read operators", async () => {
    useFleetStore.setState((state) => {
      state.auth.permissions = ["fleet:read"];
    });

    renderPage("/sites/7");

    expect(await screen.findByTestId("site-detail-performance")).toBeInTheDocument();
    expect(screen.getByTestId("device-set-performance-section")).toBeInTheDocument();

    await waitFor(() =>
      expect(useTelemetryMetricsMock).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [7n], enabled: true })),
    );
  });

  it("hides the performance section and disables telemetry without fleet:read", async () => {
    // Site-scoped operators (site:read only) can load the metrics row but
    // GetCombinedMetrics requires org-default fleet:read, so the section is
    // gated and the telemetry fetch must stay disabled rather than poll a
    // request the server will deny.
    renderPage("/sites/7");

    // Metrics row still renders for the site-scoped operator.
    expect(await screen.findByTestId("site-detail-metrics-row")).toBeInTheDocument();
    expect(screen.queryByTestId("site-detail-performance")).not.toBeInTheDocument();
    expect(useTelemetryMetricsMock).toHaveBeenCalledWith(expect.objectContaining({ enabled: false }));
  });

  it("updates active site before navigating to a breadcrumb sibling site", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "dallas" };
    });

    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByTestId("site-detail-breadcrumb-switcher"));
    await user.click(screen.getByTestId("site-detail-breadcrumb-menu-item-Austin"));

    await waitFor(() => expect(screen.getByTestId("site-detail-breadcrumb-switcher")).toHaveTextContent("Austin"));
    expect(screen.queryByTestId("location-probe")).not.toBeInTheDocument();
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "8", slug: "austin" });
  });
});
