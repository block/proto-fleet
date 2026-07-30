import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import RackOverviewPage from "./RackOverviewPage";
import { DeviceSetSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const mockUseParams = vi.fn();
const mockNavigate = vi.fn();
const mockUseBuildings = vi.fn();
const mockUseDeviceSets = vi.fn();
const mockUseDeviceSetStateCounts = vi.fn();
const mockUseSites = vi.fn();
const mockUseTelemetryMetrics = vi.fn();
const mockUseComponentErrors = vi.fn();
const listRacksMock = vi.hoisted(() => vi.fn());

const rackName = "Rack BA-Z01-R01";
const rackZone = "Building A";

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return {
    ...actual,
    useParams: () => mockUseParams(),
  };
});

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => mockUseDeviceSets(),
}));

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => mockUseBuildings(),
}));

vi.mock("@/protoFleet/api/sites", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/sites")>()),
  useSites: () => mockUseSites(),
}));

// RackOverviewPage reads the site catalog (for the breadcrumb site label) from
// the shell-level SitesProvider. Drive it directly here.
const sitesCtx = vi.hoisted(() => ({ current: { sites: [] as unknown[] } }));
vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => sitesCtx.current,
}));

vi.mock("@/protoFleet/api/useDeviceSetStateCounts", () => ({
  useDeviceSetStateCounts: () => mockUseDeviceSetStateCounts(),
}));

vi.mock("@/protoFleet/api/useTelemetryMetrics", () => ({
  useTelemetryMetrics: (options: unknown) => mockUseTelemetryMetrics(options),
}));

vi.mock("@/protoFleet/api/useComponentErrors", () => ({
  useComponentErrors: () => mockUseComponentErrors(),
}));

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: () => mockNavigate,
}));

vi.mock("@/protoFleet/store", () => ({
  useDuration: () => "12h",
  useSetDuration: () => vi.fn(),
}));

vi.mock("@/shared/hooks/useStickyState", () => ({
  useStickyState: () => ({
    refs: {
      vertical: {
        start: { current: null },
        end: { current: null },
      },
    },
  }),
}));

vi.mock("@/shared/assets/icons", () => ({
  ChevronDown: ({ className }: { className?: string }) => <svg aria-hidden="true" className={className} />,
}));

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection", () => ({
  DeviceSetPerformanceSection: ({ className, gapClassName }: { className?: string; gapClassName?: string }) => (
    <div className={className} data-gap-class={gapClassName} data-testid="rack-page-performance-grid">
      Performance section
    </div>
  ),
}));

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetActionsMenu", () => ({
  __esModule: true,
  default: ({ viewLabel }: { viewLabel?: string }) => (
    <div data-testid="rack-page-device-set-actions-menu">{viewLabel}</div>
  ),
}));

vi.mock("@/protoFleet/features/kpis/components/FleetErrors", () => ({
  __esModule: true,
  default: ({ className, gapClassName }: { className?: string; gapClassName?: string }) => (
    <div className={className} data-gap-class={gapClassName} data-testid="rack-page-fleet-errors">
      Fleet errors
    </div>
  ),
}));

vi.mock("@/protoFleet/features/fleetManagement/components/RackHealthModule", () => ({
  RackHealthModule: () => <div>Rack health</div>,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/ManageRackModal", () => ({
  ManageRackModal: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/ManageRackModal/SearchMinersModal", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/shared/components/DurationSelector", () => ({
  __esModule: true,
  default: () => <div>Duration selector</div>,
  fleetDurations: [],
}));

vi.mock("@/shared/components/ProgressCircular", () => ({
  __esModule: true,
  default: () => <div>Loading</div>,
}));

const rack = create(DeviceSetSchema, {
  id: 7n,
  label: rackName,
  typeDetails: {
    case: "rackInfo",
    value: {
      rows: 6,
      columns: 5,
      zone: rackZone,
    },
  },
});

function mockResolvedRackPageData(
  deviceSet = rack,
  options: {
    allBuildings?: unknown[];
    sites?: unknown[];
    allRacks?: unknown[];
    memberDeviceIds?: string[];
  } = {},
): void {
  mockUseParams.mockReturnValue({ rackId: "7" });
  listRacksMock.mockImplementation(({ onSuccess }: { onSuccess: (racks: unknown[]) => void }) =>
    onSuccess(options.allRacks ?? [deviceSet]),
  );
  mockUseBuildings.mockReturnValue({
    listAllBuildings: ({ onSuccess }: { onSuccess: (buildings: unknown[]) => void }) =>
      onSuccess(options.allBuildings ?? []),
  });
  mockUseDeviceSets.mockReturnValue({
    getDeviceSet: ({ onSuccess }: { onSuccess: (resolvedDeviceSet: typeof rack) => void }) => onSuccess(deviceSet),
    listGroupMembers: ({ onSuccess }: { onSuccess: (deviceIds: string[]) => void }) =>
      onSuccess(options.memberDeviceIds ?? []),
    assignDevicesToRack: vi.fn(),
    listRacks: listRacksMock,
    setRackSlotPosition: vi.fn(),
    deleteGroup: vi.fn(),
  });
  mockUseSites.mockReturnValue({
    listSites: ({ onSuccess }: { onSuccess: (sites: unknown[]) => void }) => onSuccess(options.sites ?? []),
  });
  sitesCtx.current = { sites: options.sites ?? [] };
  mockUseDeviceSetStateCounts.mockReturnValue({
    stateCounts: {
      hashingCount: 0,
      brokenCount: 0,
      offlineCount: 0,
      sleepingCount: 0,
    },
    stats: {
      slotStatuses: [],
    },
    hasLoaded: true,
    refetch: vi.fn(),
  });
  mockUseTelemetryMetrics.mockReturnValue({
    data: {
      metrics: [],
    },
  });
  mockUseComponentErrors.mockReturnValue({
    controlBoardErrors: [],
    fanErrors: [],
    hashboardErrors: [],
    psuErrors: [],
  });
}

function renderRackOverviewPage() {
  return render(
    <MemoryRouter>
      <RackOverviewPage />
    </MemoryRouter>,
  );
}

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

describe("RackOverviewPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
    mockResolvedRackPageData();
  });

  it("renders the rack zone as a subtitle under the rack name", async () => {
    renderRackOverviewPage();

    await waitFor(() => expect(screen.getAllByText(rackName).length).toBeGreaterThan(0));

    const zone = screen.getByText(rackZone);
    expect(zone).toBeVisible();
    expect(zone.className).toContain("text-text-primary");
    expect(screen.getByTestId("rack-page-breadcrumb")).toBeVisible();
    expect(screen.queryByTestId("header-icon-button")).not.toBeInTheDocument();
  });

  it("does not render a subtitle when the rack zone is empty", async () => {
    const rackWithoutZone = create(DeviceSetSchema, {
      id: 7n,
      label: rackName,
      typeDetails: {
        case: "rackInfo",
        value: {
          rows: 6,
          columns: 5,
          zone: "",
        },
      },
    });

    mockResolvedRackPageData(rackWithoutZone);

    renderRackOverviewPage();

    await waitFor(() => expect(screen.getAllByText(rackName).length).toBeGreaterThan(0));

    expect(screen.queryByText(rackZone)).not.toBeInTheDocument();
  });

  it("renders a switcher on the current rack breadcrumb item when other racks exist", async () => {
    const rackInBuilding = create(DeviceSetSchema, {
      id: 7n,
      label: rackName,
      typeDetails: {
        case: "rackInfo",
        value: {
          rows: 6,
          columns: 5,
          zone: rackZone,
          buildingId: 11n,
        },
      },
    });
    const siblingRackName = "Rack BA-Z01-R02";
    const siblingRack = create(DeviceSetSchema, {
      id: 8n,
      label: siblingRackName,
      typeDetails: {
        case: "rackInfo",
        value: {
          rows: 6,
          columns: 5,
          zone: rackZone,
        },
      },
    });

    mockResolvedRackPageData(rackInBuilding, {
      allBuildings: [
        {
          building: {
            id: 11n,
            siteId: 22n,
            name: "Building A",
          },
        },
      ],
      sites: [
        {
          site: {
            id: 22n,
            name: "Denver",
          },
        },
      ],
      allRacks: [rackInBuilding, siblingRack],
    });

    const user = userEvent.setup();
    renderRackOverviewPage();

    const switcher = await screen.findByTestId("rack-page-breadcrumb-switcher");
    expect(switcher).toHaveTextContent(rackName);
    expect(listRacksMock).toHaveBeenCalledWith(expect.objectContaining({ buildingIds: [11n] }));

    await user.click(switcher);

    expect(screen.getByTestId(`rack-page-breadcrumb-menu-item-${siblingRackName}`)).toBeVisible();
  });

  it("preserves active fleet scope on the unparented rack list breadcrumb", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "unassigned" };
    });

    renderRackOverviewPage();

    expect(await screen.findByTestId("rack-page-breadcrumb-link-0")).toHaveAttribute("href", "/unassigned/fleet/racks");
  });

  it("syncs a mismatched header scope to the rack's own site on this headerless route (#764)", async () => {
    // Deep-link to a rack whose site differs from the persisted header scope.
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "99", slug: "elsewhere" };
    });
    const rackInSite = create(DeviceSetSchema, {
      id: 7n,
      label: rackName,
      typeDetails: { case: "rackInfo", value: { rows: 6, columns: 5, zone: rackZone, siteId: 22n } },
    });
    mockResolvedRackPageData(rackInSite, {
      sites: [{ site: { id: 22n, name: "Denver", slug: "denver" } }],
    });

    renderRackOverviewPage();

    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "22", slug: "denver" }),
    );
  });

  it("syncs from rack placement site when the building catalog is unavailable (#764)", async () => {
    // Rack is placed under a building (no rackInfo.siteId), and the auxiliary
    // listAllBuildings request returns nothing — but the rack's own placement
    // still carries its site, so the sync must fire off that.
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "99", slug: "elsewhere" };
    });
    const rackUnderBuilding = create(DeviceSetSchema, {
      id: 7n,
      label: rackName,
      typeDetails: { case: "rackInfo", value: { rows: 6, columns: 5, zone: rackZone, buildingId: 11n } },
      placement: { site: { id: 22n } },
    });
    mockResolvedRackPageData(rackUnderBuilding, {
      allBuildings: [],
      sites: [{ site: { id: 22n, name: "Denver", slug: "denver" } }],
    });

    renderRackOverviewPage();

    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "22", slug: "denver" }),
    );
  });

  it("moves a specific-site header scope to unassigned for an unassigned rack (#764)", async () => {
    // Unassigned rack (no placement site, no building) opened while a specific
    // site is persisted — the header must drop to the unassigned bucket so the
    // miner picker isn't filtered to the wrong site.
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "99", slug: "elsewhere" };
    });
    // Default rack (id 7) has no siteId, buildingId, or placement.

    renderRackOverviewPage();

    await waitFor(() => expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "unassigned" }));
  });

  it("leaves an all-sites header scope untouched when viewing a rack (#764)", async () => {
    const rackInSite = create(DeviceSetSchema, {
      id: 7n,
      label: rackName,
      typeDetails: { case: "rackInfo", value: { rows: 6, columns: 5, zone: rackZone, siteId: 22n } },
    });
    mockResolvedRackPageData(rackInSite, {
      sites: [{ site: { id: 22n, name: "Denver", slug: "denver" } }],
    });

    renderRackOverviewPage();

    await waitFor(() => expect(screen.getAllByText(rackName).length).toBeGreaterThan(0));
    expect(useFleetStore.getState().ui.activeSite).toEqual(DEFAULT_ACTIVE_SITE);
  });

  it("keeps rack detail header actions compact on mobile", async () => {
    renderRackOverviewPage();

    expect(await screen.findByTestId("rack-page-title")).toHaveClass("truncate");
    expect(screen.getByTestId("rack-page-header-actions")).toHaveClass("shrink-0");
    expect(screen.getByTestId("rack-page-header-actions-desktop")).toHaveClass("hidden", "tablet:flex");
    expect(screen.getByTestId("rack-page-header-actions-mobile")).toHaveClass("tablet:hidden");
    expect(screen.getByTestId("rack-page-edit-mobile")).toHaveTextContent("Edit rack");
    expect(screen.getByTestId("rack-page-device-set-actions-menu")).toHaveTextContent("View miners");
  });

  it("uses the detail view spacing rhythm for sections and section content", async () => {
    renderRackOverviewPage();

    expect(await screen.findByTestId("rack-health-section")).toHaveClass("px-4", "pt-10", "laptop:px-8");
    expect(screen.getByTestId("rack-health-section").firstElementChild).toHaveClass("gap-1", "overflow-visible", "p-2");
    expect(screen.getByTestId("rack-page-fleet-errors")).not.toHaveClass("-m-2");
    expect(screen.getByTestId("rack-page-fleet-errors")).toHaveAttribute("data-gap-class", "gap-1");
    expect(screen.getByTestId("rack-performance-section").querySelector(".sticky")).toHaveClass("pt-10", "pb-1");
    expect(screen.getByTestId("rack-page-performance-grid")).toHaveClass("p-2");
    expect(screen.getByTestId("rack-page-performance-grid")).not.toHaveClass("-m-2");
    expect(screen.getByTestId("rack-page-performance-grid")).toHaveAttribute("data-gap-class", "gap-1");
  });

  it("does not request uptime telemetry for rack performance charts", async () => {
    mockResolvedRackPageData(rack, { memberDeviceIds: ["miner-a"] });

    renderRackOverviewPage();

    await waitFor(() =>
      expect(mockUseTelemetryMetrics).toHaveBeenCalledWith(
        expect.objectContaining({
          enabled: true,
          measurementTypes: expect.not.arrayContaining([MeasurementType.UPTIME]),
        }),
      ),
    );
  });
});
