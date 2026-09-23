import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import NavigationMenu from "./NavigationMenu";
import { NavItem, primaryNavItems } from "@/protoFleet/config/navItems";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

const { mockUseWindowDimensions, permissionsMock, activeSiteMock, accountMock } = vi.hoisted(() => ({
  mockUseWindowDimensions: vi.fn(),
  permissionsMock: { current: [] as string[] },
  activeSiteMock: { current: { kind: "all" } as ActiveSite },
  accountMock: { username: "achen", role: "SUPER_ADMIN" },
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: mockUseWindowDimensions,
}));

vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  usePermissions: () => permissionsMock.current,
  useUsername: () => accountMock.username,
  useRole: () => accountMock.role,
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
    accountMock.username = "achen";
    accountMock.role = "SUPER_ADMIN";
  });

  it.each([
    ["SUPER_ADMIN", "achen · Owner"],
    ["Night Shift", "achen · Night Shift"],
    ["__proto__", "achen · __proto__"],
    ["", "achen"],
  ])("exposes the account identity with role %s when its visible text is hidden", (role, identity) => {
    accountMock.role = role;
    render(
      <MemoryRouter>
        <NavigationMenu items={items} />
      </MemoryRouter>,
    );

    // Simulate the collapsed rail's display:none text block without Tailwind in jsdom.
    screen.getByText("achen").parentElement!.style.display = "none";

    expect(screen.getByRole("group", { name: identity })).toHaveAttribute("title", identity);
    expect(screen.getByRole("button", { name: "Log out" })).toBeVisible();
  });

  it.each([
    { isTablet: true, isLaptop: false },
    { isTablet: false, isLaptop: true },
  ])("shows a closed rail at compact non-phone widths: %o", (dimensions) => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: false, ...dimensions });
    render(
      <MemoryRouter>
        <NavigationMenu items={primaryNavItems} isVisible />
      </MemoryRouter>,
    );
    expect(screen.getByRole("navigation", { name: "Main" })).toBeVisible();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Settings menu toggle" })).not.toBeInTheDocument();
  });

  it("keeps phone navigation hidden until opened", () => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: true });
    render(
      <MemoryRouter>
        <NavigationMenu items={items} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  });

  it("opens and dismisses the phone drawer with keyboard focus restored", async () => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: true });
    const user = userEvent.setup();
    function Harness() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <button onClick={() => setOpen(true)}>Open menu</button>
          <NavigationMenu items={primaryNavItems} isVisible={open} closeMenu={() => setOpen(false)} />
        </>
      );
    }
    render(
      <MemoryRouter>
        <Harness />
      </MemoryRouter>,
    );
    const trigger = screen.getByRole("button", { name: "Open menu" });
    await user.click(trigger);
    const dialog = screen.getByRole("dialog", { name: "Navigation menu" });
    expect(screen.getByRole("button", { name: "Settings menu toggle" })).toBeVisible();
    const logoLink = dialog.querySelector('a[aria-label="Home"]');
    expect(logoLink).toHaveFocus();
    await user.tab({ shift: true });
    expect(screen.getByRole("button", { name: "Log out" })).toHaveFocus();
    await user.tab();
    expect(logoLink).toHaveFocus();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(trigger).toHaveFocus();
    expect(screen.queryByRole("navigation", { name: "Main" })).not.toBeInTheDocument();
  });

  it("switches between phone, tablet, landscape and desktop navigation without remounting the shell", () => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: true });
    const content = (
      <MemoryRouter>
        <NavigationMenu items={primaryNavItems} />
      </MemoryRouter>
    );
    const { rerender } = render(content);
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
    for (const dimensions of [{ isTablet: true }, { isLaptop: true }, { isDesktop: true }]) {
      mockUseWindowDimensions.mockReturnValue({ isPhone: false, ...dimensions });
      rerender(
        <MemoryRouter>
          <NavigationMenu items={primaryNavItems} />
        </MemoryRouter>,
      );
      expect(screen.getByRole("navigation", { name: "Main" })).toBeVisible();
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    }
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

    expect(screen.getByRole("navigation", { name: "Main" })).toHaveClass(
      "h-dvh",
      "min-h-0",
      "max-h-dvh",
      "overflow-hidden",
    );
    expect(screen.getByRole("navigation", { name: "Main" })).not.toHaveClass("min-h-screen");
    expect(screen.getByTestId("navigation-menu")).toHaveClass(
      "min-h-0",
      "flex-1",
      "overflow-y-auto",
      "overscroll-contain",
    );
    expect(screen.getByTestId("logout-button").parentElement).toHaveClass("border-t", "border-border-5", "pt-3");
  });
});
