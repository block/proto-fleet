import { type ComponentProps, createElement } from "react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import FleetInfraPage from "./FleetInfraPage";
import type { FleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout";
import type { InfraDeviceDraft, InfraDeviceItem, InfraDevicePatch } from "@/protoFleet/features/infrastructure/types";
import { useHasPermission } from "@/protoFleet/store";

const listAllBuildingsMock = vi.hoisted(() => vi.fn());
const listRacksMock = vi.hoisted(() => vi.fn());
const useActiveSiteMock = vi.hoisted(() => vi.fn());
const useInfrastructureDevicesMock = vi.hoisted(() => vi.fn());
const infraDeviceListPropsSpy = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    listAllBuildings: listAllBuildingsMock,
  }),
}));

vi.mock("@/protoFleet/api/useInfrastructureDevices", () => ({
  default: useInfrastructureDevicesMock,
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listRacks: listRacksMock }),
}));

vi.mock("@/protoFleet/features/infrastructure/components/InfraDeviceList", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/features/infrastructure/components/InfraDeviceList")>();

  return {
    ...actual,
    default: (props: ComponentProps<typeof actual.default>) => {
      infraDeviceListPropsSpy(props);
      return createElement(actual.default, props);
    },
  };
});

// Keep the real siteFilterFromActive and only stub useActiveSite so the
// scope-to-filter translation stays under test.
vi.mock("@/protoFleet/components/PageHeader/SitePicker", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/components/PageHeader/SitePicker")>();

  return {
    ...actual,
    useActiveSite: useActiveSiteMock,
  };
});

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

const device: InfraDeviceItem = {
  id: "101",
  siteId: "8",
  siteName: "Austin",
  buildingName: "Building 1",
  rackName: "Rack A1",
  name: "Roof exhaust",
  deviceKind: "fan_group",
  fanCount: 12,
  enabled: true,
  driverType: "modbus_tcp",
  driverConfig: JSON.stringify({
    endpoint: "10.12.1.21",
    port: 502,
    unit_id: 17,
    register_address: 2001,
    write_mode: "coil",
  }),
};

const fleetContext = {
  sites: [{ site: { id: 7n, name: "Denver" } }, { site: { id: 8n, name: "Austin" } }],
  sitesError: null,
  sitesLoaded: true,
  siteCatalogAccessGranted: true,
  refetchSites: vi.fn(),
  notifyPairingCompleted: vi.fn(),
  minersChangedAt: 0,
  publishViewFilterContext: vi.fn(),
} as unknown as FleetOutletContext;

const buildHookResult = (overrides: Record<string, unknown> = {}) => ({
  devices: [],
  isLoading: false,
  loadError: null,
  updatingDeviceIds: new Set<string>(),
  listDevices: vi.fn(),
  createDevice: vi.fn(),
  updateDevice: vi.fn(),
  setDeviceEnabled: vi.fn(),
  deleteDevice: vi.fn(),
  ...overrides,
});

type InfraDeviceListCallbacks = {
  canManage?: boolean;
  siteOptions?: string[];
  rackOptions?: Array<{ siteName: string; buildingName: string; rackName: string }>;
  onCreateDevice?: (draft: InfraDeviceDraft) => Promise<void>;
  onUpdateDevice?: (patch: InfraDevicePatch) => Promise<void>;
  onRetry?: () => void;
};

const lastInfraDeviceListProps = () =>
  infraDeviceListPropsSpy.mock.calls[infraDeviceListPropsSpy.mock.calls.length - 1]?.[0] as InfraDeviceListCallbacks;

const renderPage = (props?: ComponentProps<typeof FleetInfraPage>, outletContext?: FleetOutletContext) =>
  render(
    <MemoryRouter initialEntries={["/fleet/infrastructure"]}>
      <Routes>
        <Route path="/fleet" element={<Outlet context={outletContext} />}>
          <Route path="infrastructure" element={<FleetInfraPage devices={[device]} {...props} />} />
          <Route index element={<div data-testid="fleet-redirect" />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

describe("FleetInfraPage", () => {
  beforeEach(() => {
    vi.mocked(useHasPermission).mockReset();
    useActiveSiteMock.mockReturnValue({
      activeSite: { kind: "all" },
      setActiveSite: vi.fn(),
    });
    listAllBuildingsMock.mockReset();
    listRacksMock.mockReset();
    useInfrastructureDevicesMock.mockReset();
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult());
    infraDeviceListPropsSpy.mockReset();
  });

  test("uses site manage plus rack read for default management access", () => {
    vi.mocked(useHasPermission).mockImplementation(
      (key) => key === "site:read" || key === "site:manage" || key === "rack:read",
    );

    renderPage();

    expect(screen.getByRole("button", { name: "Add device" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Actions for Roof exhaust" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" })).toBeEnabled();
    expect(useHasPermission).toHaveBeenCalledWith("site:read");
    expect(useHasPermission).toHaveBeenCalledWith("site:manage");
    expect(useHasPermission).toHaveBeenCalledWith("rack:read");
  });

  test("disables management controls when rack read is denied", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");

    renderPage();

    expect(screen.queryByRole("button", { name: "Add device" })).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" })).toBeDisabled();
    expect(lastInfraDeviceListProps().canManage).toBe(false);
    expect(listRacksMock).not.toHaveBeenCalled();
  });

  test("disables management controls when site manage is denied", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read");

    renderPage();

    expect(screen.queryByRole("button", { name: "Add device" })).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" })).toBeDisabled();
  });

  test("redirects when site read is denied", () => {
    vi.mocked(useHasPermission).mockReturnValue(false);

    renderPage();

    expect(screen.getByTestId("fleet-redirect")).toBeInTheDocument();
    expect(screen.queryByText("Roof exhaust")).not.toBeInTheDocument();
  });

  test("renders API-backed devices when no override is provided", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ devices: [device] }));

    renderPage({ devices: undefined }, fleetContext);

    expect(useInfrastructureDevicesMock).toHaveBeenCalledWith(true, []);
    expect(screen.getByText("Roof exhaust")).toBeInTheDocument();
  });

  test("disables the API hook when a devices override is provided", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");

    renderPage();

    expect(useInfrastructureDevicesMock).toHaveBeenCalledWith(false, []);
    expect(screen.getByText("Roof exhaust")).toBeInTheDocument();
  });

  test("scopes the device list to the active site", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    useActiveSiteMock.mockReturnValue({
      activeSite: { kind: "site", id: "8", slug: "austin" },
      setActiveSite: vi.fn(),
    });
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ devices: [device] }));

    renderPage({ devices: undefined }, fleetContext);

    expect(useInfrastructureDevicesMock).toHaveBeenCalledWith(true, [8n]);
    expect(screen.getByText("Roof exhaust")).toBeInTheDocument();
  });

  test("restricts the form site options to the active scope", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    useActiveSiteMock.mockReturnValue({
      activeSite: { kind: "site", id: "8", slug: "austin" },
      setActiveSite: vi.fn(),
    });

    renderPage({ devices: undefined }, fleetContext);

    expect(lastInfraDeviceListProps().siteOptions).toEqual(["Austin"]);
  });

  test("offers the full site catalog when unscoped", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");

    renderPage({ devices: undefined }, fleetContext);

    expect(lastInfraDeviceListProps().siteOptions).toEqual(["Austin", "Denver"]);
  });

  test("loads rack options from the rack catalog", async () => {
    vi.mocked(useHasPermission).mockImplementation(
      (key) => key === "site:read" || key === "site:manage" || key === "rack:read",
    );
    listRacksMock.mockImplementation(async ({ onSuccess }) => {
      onSuccess?.([
        {
          label: "Rack A1",
          placement: {
            site: { id: 8n, label: "Austin" },
            building: { id: 80n, label: "Building 1" },
          },
        },
      ]);
    });

    renderPage({ devices: undefined }, fleetContext);

    await waitFor(() =>
      expect(lastInfraDeviceListProps().rackOptions).toEqual([
        { siteName: "Austin", buildingName: "Building 1", rackName: "Rack A1" },
      ]),
    );
  });

  test("rejects a create targeting a site outside the active scope", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    useActiveSiteMock.mockReturnValue({
      activeSite: { kind: "site", id: "8", slug: "austin" },
      setActiveSite: vi.fn(),
    });
    const createDevice = vi.fn();
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ createDevice }));

    renderPage({ devices: undefined }, fleetContext);

    await expect(
      lastInfraDeviceListProps().onCreateDevice!({
        siteName: "Denver",
        buildingName: "Building 1",
        rackName: "Rack A1",
        name: "Roof exhaust",
        deviceKind: "fan_group",
        fanCount: 12,
        driverType: "modbus_tcp",
        driverConfig: device.driverConfig,
      }),
    ).rejects.toThrow("Select a site within the current site scope.");

    expect(createDevice).not.toHaveBeenCalled();
  });

  test("hides device creation and offers no sites in the unassigned scope", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    useActiveSiteMock.mockReturnValue({
      activeSite: { kind: "unassigned" },
      setActiveSite: vi.fn(),
    });

    renderPage({ devices: undefined }, fleetContext);

    expect(screen.queryByRole("button", { name: "Add device" })).not.toBeInTheDocument();
    expect(lastInfraDeviceListProps().siteOptions).toEqual([]);
  });

  test("rejects a create in the unassigned scope", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    useActiveSiteMock.mockReturnValue({
      activeSite: { kind: "unassigned" },
      setActiveSite: vi.fn(),
    });
    const createDevice = vi.fn();
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ createDevice }));

    renderPage({ devices: undefined }, fleetContext);

    await expect(
      lastInfraDeviceListProps().onCreateDevice!({
        siteName: "Austin",
        buildingName: "Building 1",
        rackName: "Rack A1",
        name: "Roof exhaust",
        deviceKind: "fan_group",
        fanCount: 12,
        driverType: "modbus_tcp",
        driverConfig: device.driverConfig,
      }),
    ).rejects.toThrow("Select a site within the current site scope.");

    expect(createDevice).not.toHaveBeenCalled();
  });

  test("renders an empty list for the unassigned scope without fetching", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    useActiveSiteMock.mockReturnValue({
      activeSite: { kind: "unassigned" },
      setActiveSite: vi.fn(),
    });
    // The hook keeps its last result while disabled; the page must not
    // leak those devices into the unassigned scope.
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ devices: [device] }));

    renderPage({ devices: undefined }, fleetContext);

    expect(useInfrastructureDevicesMock).toHaveBeenCalledWith(false, []);
    expect(screen.queryByText("Roof exhaust")).not.toBeInTheDocument();
  });

  test("resolves the create draft site name before calling the API", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    const createDevice = vi.fn().mockResolvedValue(undefined);
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ createDevice }));

    renderPage({ devices: undefined }, fleetContext);

    await lastInfraDeviceListProps().onCreateDevice!({
      siteName: "Austin",
      buildingName: "Building 1",
      rackName: "Rack A1",
      name: "Roof exhaust",
      deviceKind: "fan_group",
      fanCount: 12,
      driverType: "modbus_tcp",
      driverConfig: device.driverConfig,
    });

    expect(createDevice).toHaveBeenCalledWith({
      siteId: "8",
      buildingName: "Building 1",
      rackName: "Rack A1",
      name: "Roof exhaust",
      deviceKind: "fan_group",
      fanCount: 12,
      driverType: "modbus_tcp",
      driverConfig: device.driverConfig,
    });
  });

  test("rejects unknown create draft sites without calling the API", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    const createDevice = vi.fn();
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ createDevice }));

    renderPage({ devices: undefined }, fleetContext);

    await expect(
      lastInfraDeviceListProps().onCreateDevice!({
        siteName: "Unknown",
        buildingName: "Building 1",
        rackName: "Rack A1",
        name: "Roof exhaust",
        deviceKind: "fan_group",
        fanCount: 12,
        driverType: "modbus_tcp",
        driverConfig: device.driverConfig,
      }),
    ).rejects.toThrow("Select a site from the catalog.");

    expect(createDevice).not.toHaveBeenCalled();
  });

  test("resolves the update site name before calling the API", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    const updateDevice = vi.fn().mockResolvedValue(undefined);
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ updateDevice }));

    renderPage({ devices: undefined }, fleetContext);

    await lastInfraDeviceListProps().onUpdateDevice!({
      id: "101",
      siteName: "Denver",
      buildingName: "Building 1",
    } as InfraDevicePatch);

    expect(updateDevice).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "101",
        siteId: "7",
        buildingName: "Building 1",
      }),
    );
  });

  test("a patch without a site change skips catalog resolution entirely", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    const updateDevice = vi.fn().mockResolvedValue(undefined);
    // An empty catalog would make any name-based resolution throw; a
    // name-only save must not depend on it.
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ updateDevice }));

    renderPage({ devices: undefined }, { ...fleetContext, sites: [] });

    await lastInfraDeviceListProps().onUpdateDevice!({
      id: "101",
      name: "Roof exhaust renamed",
    } as InfraDevicePatch);

    expect(updateDevice).toHaveBeenCalledWith(expect.objectContaining({ id: "101", name: "Roof exhaust renamed" }));
    expect(updateDevice.mock.calls[0][0].siteId).toBeUndefined();
  });

  test("retries the device list request", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
    const listDevices = vi.fn().mockResolvedValue(undefined);
    useInfrastructureDevicesMock.mockReturnValue(buildHookResult({ listDevices }));

    renderPage({ devices: undefined }, fleetContext);

    lastInfraDeviceListProps().onRetry!();

    expect(listDevices).toHaveBeenCalledOnce();
  });

  test("preselects the active site when opening the add device modal", async () => {
    const user = userEvent.setup();
    vi.mocked(useHasPermission).mockImplementation(
      (key) => key === "site:read" || key === "site:manage" || key === "rack:read",
    );
    useActiveSiteMock.mockReturnValue({
      activeSite: { kind: "site", id: "7", slug: "denver" },
      setActiveSite: vi.fn(),
    });

    renderPage(undefined, fleetContext);

    await user.click(screen.getByRole("button", { name: "Add device" }));

    expect(
      screen.getAllByRole("button", { name: "Site" }).some((button) => button.textContent?.includes("Denver")),
    ).toBe(true);
  });
});
