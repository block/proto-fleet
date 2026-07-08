import type { ComponentProps } from "react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import FleetInfraPage from "./FleetInfraPage";
import type { FleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { useHasPermission } from "@/protoFleet/store";

const listAllBuildingsMock = vi.hoisted(() => vi.fn());
const useActiveSiteMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    listAllBuildings: listAllBuildingsMock,
  }),
}));

vi.mock("@/protoFleet/components/PageHeader/SitePicker", () => ({
  useActiveSite: useActiveSiteMock,
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

const device: InfraDeviceItem = {
  id: "aus-b1-roof-exhaust",
  unitId: 17,
  name: "Roof exhaust",
  buildingName: "Building 1",
  siteName: "Austin",
  connectionType: "modbus_tcp",
  endpoint: "10.12.1.21",
  port: 502,
  status: "offline",
  enabled: "auto",
  lastSeen: "Never",
  endpointKind: "fan_group",
  fanCount: 12,
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
  });

  test("uses site permissions for default read and management access", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");

    renderPage();

    expect(screen.getByRole("button", { name: "Add device" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Actions for Roof exhaust" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" })).toBeEnabled();
    expect(useHasPermission).toHaveBeenCalledWith("site:read");
    expect(useHasPermission).toHaveBeenCalledWith("site:manage");
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

  test("preselects the active site when opening the add device modal", async () => {
    const user = userEvent.setup();
    vi.mocked(useHasPermission).mockImplementation((key) => key === "site:read" || key === "site:manage");
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
