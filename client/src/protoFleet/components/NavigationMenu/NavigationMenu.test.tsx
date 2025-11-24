import { MemoryRouter } from "react-router-dom";
import { render, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import NavigationMenu from "./NavigationMenu";
import { NavItem } from "@/protoFleet/config/navItems";

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

    const currentItem = getByText("Foo");
    await waitFor(() => {
      expect(currentItem).toHaveClass("bg-core-primary-5");
    });
  });
});
