import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import NavigationMenu from "./NavigationMenu";
import { NavItem, primaryNavItems } from "@/protoFleet/config/navItems";
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

  it("uses the standard mobile nav row height for Settings and its submenu links", async () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: true,
      isTablet: false,
    });

    render(
      <MemoryRouter>
        <NavigationMenu items={primaryNavItems} isVisible />
      </MemoryRouter>,
    );

    const settingsToggle = screen.getByRole("button", { name: "Settings menu toggle" });
    expect(settingsToggle).toHaveClass("h-10", "px-2.5", "py-2");

    fireEvent.click(settingsToggle);

    const securityLink = await screen.findByRole("link", { name: "Security" });
    expect(securityLink).toHaveClass("h-10", "flex", "items-center");
  });

  it("uses the nav list as the mobile drawer scroll boundary", () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: true,
      isTablet: false,
    });

    render(
      <MemoryRouter>
        <NavigationMenu items={primaryNavItems} isVisible />
      </MemoryRouter>,
    );

    expect(screen.getByRole("navigation", { name: "Main" })).toHaveClass("h-dvh", "max-h-dvh", "overflow-hidden");
    expect(screen.getByTestId("navigation-menu")).toHaveClass(
      "min-h-0",
      "flex-1",
      "overflow-y-auto",
      "overscroll-contain",
    );
    expect(screen.getByTestId("logout-button").parentElement).toHaveClass("border-t", "border-border-5", "pt-3");
  });
});
