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
  DeviceSetPerformanceSection: ({ className, gapClassName }: { className?: string; gapClassName?: string }) => (
    <div className={className} data-gap-class={gapClassName} data-testid="device-set-performance-section">
      Performance charts
    </div>
  ),
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

const installLocalStorageMock = () => {
  const storage = new Map<string, string>();
  const localStorageMock: Storage = {
    get length() {
      return storage.size;
    },
    clear: () => storage.clear(),
    getItem: (key) => storage.get(key) ?? null,
    key: (index) => Array.from(storage.keys())[index] ?? null,
    removeItem: (key) => {
      storage.delete(key);
    },
    setItem: (key, value) => {
      storage.set(key, value);
    },
  };

  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: localStorageMock,
  });
};

if (typeof globalThis.localStorage === "undefined") {
  installLocalStorageMock();
}

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
    localStorage.clear();
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

  it("syncs a mismatched persisted scope to the site being viewed instead of bouncing away", async () => {
    // Deep-link to /sites/7 (Dallas) while the header scope points at another
    // site (Austin). The headerless route must adopt the viewed site rather
    // than redirect to /fleet (#764).
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });

    renderPage();

    expect(await screen.findByTestId("site-detail-page")).toBeInTheDocument();
    expect(screen.queryByTestId("location-probe")).not.toBeInTheDocument();
    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });

  it("adopts the viewed site when the persisted scope is 'unassigned'", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "unassigned" };
    });

    renderPage();

    expect(await screen.findByTestId("site-detail-page")).toBeInTheDocument();
    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });

  it("leaves an all-sites scope untouched when viewing a site", async () => {
    // Viewing one entity shouldn't collapse an intentional org-wide view.
    renderPage();

    expect(await screen.findByTestId("site-detail-page")).toBeInTheDocument();
    expect(useFleetStore.getState().ui.activeSite).toEqual(DEFAULT_ACTIVE_SITE);
  });

  it("renders the metrics row scoped to the resolved site", async () => {
    renderPage("/sites/7");

    expect(await screen.findByTestId("site-detail-metrics-row")).toBeInTheDocument();
    await waitFor(() =>
      expect(useSiteStatsMock).toHaveBeenCalledWith(expect.objectContaining({ siteId: 7n, enabled: true })),
    );
  });

  it("keeps the edit action in the site detail title row on mobile", async () => {
    useFleetStore.setState((state) => {
      state.auth.permissions = ["site:manage"];
    });

    renderPage("/sites/7");

    expect(await screen.findByTestId("site-detail-title")).toHaveClass("truncate");
    expect(screen.getByTestId("site-detail-edit").parentElement).toHaveClass("ml-3", "shrink-0");
  });

  it("uses the detail view spacing rhythm for sections and section content", async () => {
    useFleetStore.setState((state) => {
      state.auth.permissions = ["fleet:read"];
    });

    renderPage("/sites/7");

    expect(await screen.findByTestId("site-detail-page")).toHaveClass(
      "gap-10",
      "px-4",
      "py-6",
      "laptop:px-8",
      "laptop:py-10",
    );
    expect(screen.getByTestId("site-detail-heading")).toHaveClass("gap-3", "px-2");
    expect(screen.getByTestId("site-detail-metrics-section")).toHaveClass("gap-3", "px-2");
    expect(screen.getByTestId("site-metric-hashrate-value")).toHaveClass("text-emphasis-400");
    expect(screen.getByTestId("site-metric-power-value")).toHaveClass("text-emphasis-400");
    expect(screen.getByTestId("site-detail-buildings-section")).toHaveClass("gap-3");
    expect(screen.getByTestId("site-detail-performance")).toHaveClass("gap-3");
    expect(screen.getByTestId("device-set-performance-section")).toHaveClass("p-2");
    expect(screen.getByTestId("device-set-performance-section")).toHaveAttribute("data-gap-class", "gap-1");
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
