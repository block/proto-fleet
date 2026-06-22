import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import SettingsLayout from "./SettingsLayout";

const orgPermissionsMock = vi.hoisted(() => ({ current: [] as string[] }));

vi.mock("@/protoFleet/store", () => ({
  useOrgPermissions: () => orgPermissionsMock.current,
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
          path="/settings/general"
          element={
            <SettingsLayout>
              <div data-testid="general-page">General</div>
            </SettingsLayout>
          }
        />
      </Routes>
      <LocationProbe />
    </MemoryRouter>,
  );

describe("SettingsLayout permission guard", () => {
  beforeEach(() => {
    orgPermissionsMock.current = [];
  });

  test("redirects protected settings routes before rendering their children", async () => {
    renderSettingsRoute("/settings/team");

    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/settings/general"));
    expect(screen.queryByTestId("team-page")).not.toBeInTheDocument();
    expect(screen.getByTestId("general-page")).toBeInTheDocument();
  });

  test("renders protected settings routes when the org permission is present", () => {
    orgPermissionsMock.current = ["user:read"];

    renderSettingsRoute("/settings/team");

    expect(screen.getByTestId("location-probe").textContent).toBe("/settings/team");
    expect(screen.getByTestId("team-page")).toBeInTheDocument();
  });
});
