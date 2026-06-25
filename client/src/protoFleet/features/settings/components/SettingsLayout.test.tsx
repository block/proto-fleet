import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import SettingsLayout from "./SettingsLayout";

const permissionsMock = vi.hoisted(() => ({ current: [] as string[] }));

vi.mock("@/protoFleet/store", () => ({
  usePermissions: () => permissionsMock.current,
}));

vi.mock("@/protoFleet/components/SecondaryNavigation", () => ({
  default: () => <nav data-testid="secondary-nav" />,
}));

vi.mock("@/shared/utils/prefetchRoutes", () => ({
  prefetchRoutes: vi.fn(() => () => {}),
}));

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{location.pathname}</div>;
};

const renderSettingsRoute = (initialPath: string) =>
  render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route
          path="/settings/team"
          element={
            <SettingsLayout>
              <div data-testid="team-page">Team</div>
            </SettingsLayout>
          }
        />
        <Route
          path="/settings/network"
          element={
            <SettingsLayout>
              <div data-testid="network-page">Network</div>
            </SettingsLayout>
          }
        />
      </Routes>
      <LocationProbe />
    </MemoryRouter>,
  );

describe("SettingsLayout permission guard", () => {
  beforeEach(() => {
    permissionsMock.current = [];
  });

  test("redirects protected settings routes before rendering their children", async () => {
    renderSettingsRoute("/settings/team");

    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/settings/network"));
    expect(screen.queryByTestId("team-page")).not.toBeInTheDocument();
    expect(screen.getByTestId("network-page")).toBeInTheDocument();
  });

  test("renders protected settings routes when the org permission is present", () => {
    permissionsMock.current = ["user:read"];

    renderSettingsRoute("/settings/team");

    expect(screen.getByTestId("location-probe").textContent).toBe("/settings/team");
    expect(screen.getByTestId("team-page")).toBeInTheDocument();
  });

  test("renders Team when only role management permission is present", () => {
    permissionsMock.current = ["role:manage"];

    renderSettingsRoute("/settings/team");

    expect(screen.getByTestId("location-probe").textContent).toBe("/settings/team");
    expect(screen.getByTestId("team-page")).toBeInTheDocument();
  });
});
