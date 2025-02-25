   import { MemoryRouter } from "react-router-dom";
 import { render, waitFor } from "@testing-library/react";
  import { describe, expect, it } from "vitest";
  import NavigationMenu from "./NavigationMenu";

  describe("Navigation Menu", () => {
    const routes = [
      {
        path: "/foo",
        label: "Foo",
        navItem: true,
      },
      {
        path: "/bar",
        label: "Bar",
        navItem: true,
      },
      {
        path: "/baz",
        label: "Baz",
      },
    ];

    it("should render the correct number nav items", () => {
      const { getByTestId } = render(
        <MemoryRouter>
          <NavigationMenu routes={routes} />
        </MemoryRouter>
      );

      const navMenu = getByTestId("navigation-menu");
      const navItems = navMenu.querySelectorAll("li");
      expect(navItems.length).toBe(2);
    });

    it("should show the correct active nav item", async () => {
      const { getByText } = render(
        <MemoryRouter initialEntries={["/foo"]}>
          <NavigationMenu routes={routes} />
        </MemoryRouter>
      );

      const currentItem = getByText("Foo");
      await waitFor(() => {
        expect(currentItem).toHaveClass("bg-core-primary-10");
      })
    });
  });
