import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { getFirstAllowedSecondaryNavPath } from "@/protoFleet/config/navItems";
import SettingsLayout from "./SettingsLayout";

const permissionsMock = vi.hoisted(() => ({ current: [] as string[] }));
const activeSiteMock = vi.hoisted(() => ({ current: { kind: "all" } as { kind: string; id?: string } }));

vi.mock("@/protoFleet/store", () => ({
  usePermissions: () => permissionsMock.current,
}));

vi.mock("@/protoFleet/components/PageHeader/SitePicker", () => ({
  useActiveSite: () => ({ activeSite: activeSiteMock.current, setActiveSite: vi.fn() }),
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
        <Route
          path="/settings/firmware"
          element={
            <SettingsLayout>
              <div data-testid="firmware-page">Firmware</div>
            </SettingsLayout>
          }
        />
        <Route
          path="/settings/preferences"
          element={
            <SettingsLayout>
              <div data-testid="preferences-page">Preferences</div>
            </SettingsLayout>
          }
        />
        <Route
          path="/settings/schedules"
          element={
            <SettingsLayout>
              <div data-testid="schedules-page">Schedules</div>
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
    activeSiteMock.current = { kind: "all" };
  });

  test("redirects protected settings routes before rendering their children", async () => {
    renderSettingsRoute("/settings/team");

    await waitFor(() =>
      expect(screen.getByTestId("location-probe").textContent).toBe(getFirstAllowedSecondaryNavPath([])),
    );
    expect(screen.queryByTestId("team-page")).not.toBeInTheDocument();
    expect(screen.getByTestId("firmware-page")).toBeInTheDocument();
  });

  test("redirects Network when fleet read permission is missing", async () => {
    permissionsMock.current = ["role:manage"];

    renderSettingsRoute("/settings/network");

    await waitFor(() =>
      expect(screen.getByTestId("location-probe").textContent).toBe(getFirstAllowedSecondaryNavPath(["role:manage"])),
    );
    expect(screen.queryByTestId("network-page")).not.toBeInTheDocument();
  });

  test("renders Network when fleet read permission is present", () => {
    permissionsMock.current = ["fleet:read"];

    renderSettingsRoute("/settings/network");

    expect(screen.getByTestId("location-probe").textContent).toBe("/settings/network");
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

describe("SettingsLayout org-wide notice", () => {
  beforeEach(() => {
    permissionsMock.current = [];
    activeSiteMock.current = { kind: "site", id: "7" };
  });

  test("shows the org-wide notice on org-wide pages when a site is selected", () => {
    renderSettingsRoute("/settings/preferences");

    expect(screen.getByTestId("org-wide-notice")).toBeInTheDocument();
    expect(screen.getByTestId("preferences-page")).toBeInTheDocument();
  });

  test("hides the org-wide notice when all sites is selected", () => {
    activeSiteMock.current = { kind: "all" };

    renderSettingsRoute("/settings/preferences");

    expect(screen.queryByTestId("org-wide-notice")).not.toBeInTheDocument();
    expect(screen.getByTestId("preferences-page")).toBeInTheDocument();
  });

  test("hides the org-wide notice on site-aware pages", () => {
    permissionsMock.current = ["schedule:manage"];

    renderSettingsRoute("/settings/schedules");

    expect(screen.queryByTestId("org-wide-notice")).not.toBeInTheDocument();
    expect(screen.getByTestId("schedules-page")).toBeInTheDocument();
  });
});
