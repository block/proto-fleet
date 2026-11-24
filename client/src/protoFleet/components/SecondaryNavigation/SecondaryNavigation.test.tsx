import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SecondaryNavigation from "./SecondaryNavigation";
import { SecondaryNavItem, secondaryNavItems } from "@/protoFleet/config/navItems";
import { HostedAccountsContext } from "@/protoFleet/features/hostedAccounts/HostedAccountsContext";

const { dimensions } = vi.hoisted(() => ({ dimensions: { isPhone: false, isTablet: false } }));
vi.mock("@/shared/hooks/useWindowDimensions", () => ({ useWindowDimensions: () => dimensions }));

describe("Secondary Navigation", () => {
  beforeEach(() => {
    dimensions.isPhone = false;
    dimensions.isTablet = false;
  });
  const items: SecondaryNavItem[] = [
    {
      path: "/bar/foo",
      label: "Bar Foo",
      parent: "/bar",
    },
    {
      path: "/bar/bar",
      label: "Bar Bar",
      parent: "/bar",
    },
    {
      path: "/bar/baz",
      label: "Bar Baz",
      parent: "/bar",
    },
  ];

  it.each([true, false])("shows Settings sections beside the rail but not on phones (phone=%s)", (isPhone) => {
    dimensions.isPhone = isPhone;
    dimensions.isTablet = !isPhone;
    const { queryByRole } = render(
      <MemoryRouter initialEntries={["/bar/foo"]}>
        <SecondaryNavigation items={items} />
      </MemoryRouter>,
    );
    if (isPhone) {
      expect(queryByRole("navigation", { name: "Settings" })).not.toBeInTheDocument();
    } else {
      expect(queryByRole("link", { name: "Bar Foo" })).toBeVisible();
    }
  });

  it("should render the correct number nav items", () => {
    const { getByTestId } = render(
      <MemoryRouter initialEntries={["/bar/foo"]}>
        <SecondaryNavigation items={items} />
      </MemoryRouter>,
    );

    const navMenu = getByTestId("secondary-nav");
    const navItems = navMenu.querySelectorAll("a");
    expect(navItems.length).toBe(3);
  });

  it("should show the correct active nav item", async () => {
    const { getByText } = render(
      <MemoryRouter initialEntries={["/bar/foo"]}>
        <SecondaryNavigation items={items} />
      </MemoryRouter>,
    );

    const currentItem = getByText("Bar Foo");
    await waitFor(() => {
      expect(currentItem).toHaveClass("bg-core-primary-5");
    });
  });

  it.each([true, false])("gates the Reports navigation on reporting access (%s)", (reportingAccess) => {
    render(
      <MemoryRouter initialEntries={["/accounts/reports"]}>
        <HostedAccountsContext.Provider
          value={{ accounts: [], hostAccess: true, reportingAccess, loading: false, error: null, refresh: vi.fn() }}
        >
          <SecondaryNavigation items={secondaryNavItems} parentPath="/settings" />
        </HostedAccountsContext.Provider>
      </MemoryRouter>,
    );
    if (reportingAccess) {
      expect(screen.getByRole("link", { name: "Reports" })).toHaveAttribute("aria-current", "page");
    } else {
      expect(screen.queryByRole("link", { name: "Reports" })).not.toBeInTheDocument();
    }
  });

  it.each(["/accounts/reports", "/accounts/customer/manage/assets"])(
    "selects only the matching section at %s",
    (pathname) => {
      render(
        <MemoryRouter initialEntries={[pathname]}>
          <SecondaryNavigation
            items={[
              { path: "/accounts", label: "Clients", parent: "/settings" },
              { path: "/accounts/reports", label: "Reports", parent: "/settings" },
            ]}
            parentPath="/settings"
          />
        </MemoryRouter>,
      );
      const selected = pathname === "/accounts/reports" ? "Reports" : "Clients";
      expect(screen.getByRole("link", { name: selected })).toHaveAttribute("aria-current", "page");
      expect(screen.getByRole("link", { name: selected === "Reports" ? "Clients" : "Reports" })).not.toHaveAttribute(
        "aria-current",
      );
    },
  );

  it("renders section labels when provided", () => {
    const { getByText } = render(
      <MemoryRouter initialEntries={["/bar/foo"]}>
        <SecondaryNavigation
          items={[
            { ...items[0], section: "Fleet" },
            { ...items[1], section: "Fleet" },
            { ...items[2], section: "Admin" },
          ]}
        />
      </MemoryRouter>,
    );

    expect(getByText("Fleet")).toBeInTheDocument();
    expect(getByText("Admin")).toBeInTheDocument();
  });
});
