import { MemoryRouter } from "react-router-dom";
import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import NavigationMenu from "./NavigationMenu";
import { NavItem } from "@/protoFleet/config/navItems";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

const { mockUseWindowDimensions, permissionsMock, activeSiteMock } = vi.hoisted(() => ({
  mockUseWindowDimensions: vi.fn(),
  permissionsMock: { current: [] as string[] },
  activeSiteMock: { current: { kind: "all" } as ActiveSite },
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: mockUseWindowDimensions,
}));

vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  usePermissions: () => permissionsMock.current,
}));

vi.mock("@/protoFleet/components/PageHeader/SitePicker", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/components/PageHeader/SitePicker")>()),
  useActiveSite: () => ({ activeSite: activeSiteMock.current }),
}));

describe("Navigation Menu", () => {
  const items: NavItem[] = [
    {
      path: "/foo",
      label: "Foo",
    },
    {
      path: "/bar",
      label: "Bar",
    },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });
    permissionsMock.current = [];
    activeSiteMock.current = { kind: "all" };
  });

  it("should render the correct number nav items", () => {
    const { getByTestId } = render(
      <MemoryRouter>
        <NavigationMenu items={items} />
      </MemoryRouter>,
    );

    const navMenu = getByTestId("navigation-menu");
    const navItems = navMenu.querySelectorAll("li");
    expect(navItems.length).toBe(2);
  });

  it("should show the correct active nav item", async () => {
    const { getByText } = render(
      <MemoryRouter initialEntries={["/foo"]}>
        <NavigationMenu items={items} />
      </MemoryRouter>,
    );

    const currentItem = getByText("Foo").closest("a");
    await waitFor(() => {
      expect(currentItem).toHaveClass("bg-core-primary-5");
    });
  });

  describe("site scoping of scopable links", () => {
    const scopableItems: NavItem[] = [{ path: "/fleet", label: "Fleet", scopable: true }];

    beforeEach(() => {
      activeSiteMock.current = { kind: "site", id: "1", slug: "alpha" };
    });

    it("scopes the link to the active site when the role can read sites", () => {
      permissionsMock.current = ["site:read"];
      const { getByText } = render(
        <MemoryRouter>
          <NavigationMenu items={scopableItems} />
        </MemoryRouter>,
      );
      expect(getByText("Fleet").closest("a")).toHaveAttribute("href", "/alpha/fleet");
    });

    it("keeps the link unscoped for a role without site:read", () => {
      // Resolving the /alpha slug is site:read-gated; a site-less role reaching
      // Fleet via miner:read would be bounced, so the link must stay unscoped.
      permissionsMock.current = ["miner:read", "fleet:read"];
      const { getByText } = render(
        <MemoryRouter>
          <NavigationMenu items={scopableItems} />
        </MemoryRouter>,
      );
      expect(getByText("Fleet").closest("a")).toHaveAttribute("href", "/fleet");
    });
  });
});
