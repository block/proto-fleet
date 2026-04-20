import { MemoryRouter } from "react-router-dom";
import { render, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import SecondaryNavigation from "./SecondaryNavigation";
import { SecondaryNavItem } from "@/protoFleet/config/navItems";

describe("Secondary Navigation", () => {
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

  it("should render the correct number nav items", () => {
    const { getByTestId } = render(
      <MemoryRouter initialEntries={["/bar/foo"]}>
        <SecondaryNavigation items={items} />
      </MemoryRouter>,
    );

    const navMenu = getByTestId("secondary-nav");
    const navItems = navMenu.querySelectorAll("li");
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
});
