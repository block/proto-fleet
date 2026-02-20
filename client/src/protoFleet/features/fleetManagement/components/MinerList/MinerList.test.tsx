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
  describe("miner count subtitle", () => {
    it("shows total miner count", () => {
      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 14,
        onAddMiners: vi.fn(),
        loading: true,
      });

      expect(screen.getByText("14 miners")).toBeInTheDocument();
    });

    it("shows 'X of Y miners' when filters are active and filtered count differs from total", () => {
      renderMinerList(
        {
          title: "Miners",
          minerIds: [],
          totalMiners: 5,
          totalUnfilteredMiners: 14,
          onAddMiners: vi.fn(),
          loading: true,
        },
        ["/?status=hashing"],
      );

      expect(screen.getByText("5 of 14 miners")).toBeInTheDocument();
    });

    it("shows total count when filters are active but filtered count equals total", () => {
      renderMinerList(
        {
          title: "Miners",
          minerIds: [],
          totalMiners: 14,
          totalUnfilteredMiners: 14,
          onAddMiners: vi.fn(),
          loading: true,
        },
        ["/?status=hashing"],
      );

      expect(screen.getByText("14 miners")).toBeInTheDocument();
    });
  });

  describe("pagination footer", () => {
    it("shows correct range for the first page", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1", "m2", "m3"],
        totalMiners: 10,
        currentPage: 0,
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.getByText("Showing 1–3 of 10 miners")).toBeInTheDocument();
    });

    it("shows correct range for a subsequent page", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1", "m2"],
        totalMiners: 102,
        currentPage: 1,
        pageSize: 100,
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.getByText("Showing 101–102 of 102 miners")).toBeInTheDocument();
    });

    it("does not show pagination footer when there are no miners", () => {
      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.queryByText(/Showing/)).not.toBeInTheDocument();
    });

    it("does not show pagination footer while loading", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        currentPage: 0,
        onAddMiners: vi.fn(),
        loading: true,
      });

      expect(screen.queryByText(/Showing/)).not.toBeInTheDocument();
    });

    it("disables the prev button on the first page", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        currentPage: 0,
        hasPreviousPage: false,
        onPrevPage: vi.fn(),
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
    });

    it("disables the next button on the last page", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasNextPage: false,
        onNextPage: vi.fn(),
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
    });

    it("calls onPrevPage when prev button is clicked", async () => {
      const user = userEvent.setup();
      const onPrevPage = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasPreviousPage: true,
        onPrevPage,
        onAddMiners: vi.fn(),
        loading: false,
      });

      await user.click(screen.getByRole("button", { name: "Previous page" }));

      expect(onPrevPage).toHaveBeenCalledTimes(1);
    });

    it("calls onNextPage when next button is clicked", async () => {
      const user = userEvent.setup();
      const onNextPage = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasNextPage: true,
        onNextPage,
        onAddMiners: vi.fn(),
        loading: false,
      });

      await user.click(screen.getByRole("button", { name: "Next page" }));

      expect(onNextPage).toHaveBeenCalledTimes(1);
    });

    it("scrolls to top when next button is clicked", async () => {
      const user = userEvent.setup();
      const scrollIntoView = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasNextPage: true,
        onNextPage: vi.fn(),
        onAddMiners: vi.fn(),
        loading: false,
      });

      screen.getByText("Miners").closest("div")!.scrollIntoView = scrollIntoView;

      await user.click(screen.getByRole("button", { name: "Next page" }));

      expect(scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth", block: "start" });
    });

    it("scrolls to top when prev button is clicked", async () => {
      const user = userEvent.setup();
      const scrollIntoView = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasPreviousPage: true,
        onPrevPage: vi.fn(),
        onAddMiners: vi.fn(),
        loading: false,
      });

      screen.getByText("Miners").closest("div")!.scrollIntoView = scrollIntoView;

      await user.click(screen.getByRole("button", { name: "Previous page" }));

      expect(scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth", block: "start" });
    });
  });

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
