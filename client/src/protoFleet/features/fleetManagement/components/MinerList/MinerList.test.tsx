import { BrowserRouter, MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import MinerList from "./MinerList";

const renderMinerList = (props: Parameters<typeof MinerList>[0], initialEntries?: string[]) => {
  const Router = initialEntries ? MemoryRouter : BrowserRouter;
  const routerProps = initialEntries ? { initialEntries } : {};

  return render(
    <Router {...routerProps}>
      <MinerList {...props} />
    </Router>,
  );
};

describe("MinerList", () => {
  describe("null state", () => {
    it("should show null state when no miners are paired", () => {
      const onAddMiners = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        onAddMiners,
      });

      expect(screen.getByText("You haven't paired any miners")).toBeInTheDocument();
      expect(screen.getByText("Add miners to your fleet to get started.")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Get started" })).toBeInTheDocument();
      // List header and "Add miners" button should not be visible when showing null state
      expect(screen.queryByText("Miners")).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Add miners" })).not.toBeInTheDocument();
    });

    it("should call onAddMiners when Get started button is clicked", async () => {
      const user = userEvent.setup();
      const onAddMiners = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        onAddMiners,
      });

      await user.click(screen.getByRole("button", { name: "Get started" }));

      expect(onAddMiners).toHaveBeenCalledTimes(1);
    });

    it("should not show null state when loading", () => {
      const onAddMiners = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        onAddMiners,
        loading: true,
      });

      expect(screen.queryByText("You haven't paired any miners")).not.toBeInTheDocument();
    });

    it("should not show null state when filters are active and no items match", () => {
      const onAddMiners = vi.fn();

      renderMinerList(
        {
          title: "Miners",
          minerIds: [],
          totalMiners: 0,
          onAddMiners,
        },
        ["/?status=hashing"],
      );

      // Null state should not appear when filters are active
      expect(screen.queryByText("You haven't paired any miners")).not.toBeInTheDocument();
      // Regular list view should be shown instead
      expect(screen.getByText("Miners")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Add miners" })).toBeInTheDocument();
    });
  });
});
