import type { ComponentProps } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import FleetInfraPage from "./FleetInfraPage";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { useHasPermission } from "@/protoFleet/store";

const listAllBuildingsMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    listAllBuildings: listAllBuildingsMock,
  }),
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

const device: InfraDeviceItem = {
  id: "aus-b1-roof-exhaust",
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

const renderPage = (props?: ComponentProps<typeof FleetInfraPage>) =>
  render(
    <MemoryRouter initialEntries={["/fleet/infrastructure"]}>
      <Routes>
        <Route path="/fleet/infrastructure" element={<FleetInfraPage devices={[device]} {...props} />} />
        <Route path="/fleet" element={<div data-testid="fleet-redirect" />} />
      </Routes>
    </MemoryRouter>,
  );

describe("FleetInfraPage", () => {
  beforeEach(() => {
    vi.mocked(useHasPermission).mockReset();
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
});
