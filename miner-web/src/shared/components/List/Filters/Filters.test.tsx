import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Filters from "./Filters";
import {
  MinerFilterState,
  minerFilterStates,
} from "@/protoFleet/components/MinerList/constants";
import { Miner } from "@/protoFleet/components/MinerList/types";
import { defaultListFilter } from "@/shared/components/List/constants";
import { statuses } from "@/shared/components/StatusCircle";

describe("Filters", () => {
  const filters = [
    {
      title: "All miners",
      value: defaultListFilter,
      count: 3,
    },
    {
      title: "Hashing",
      value: minerFilterStates.hashing,
      count: 1,
      status: statuses.normal,
    },
    {
      title: "Broken",
      value: minerFilterStates.broken,
      count: 1,
      status: statuses.error,
    },
    {
      title: "Offline",
      value: minerFilterStates.offline,
      count: 1,
      status: statuses.warning,
    },
    {
      title: "Asleep",
      value: minerFilterStates.asleep,
      count: 0,
      status: statuses.inactive,
    },
  ];

  const mockMiners = [
    {
      name: "Miner 1",
      status: {
        hashing: true,
        broken: false,
        offline: false,
        asleep: false,
      },
    },
    {
      name: "Miner 2",
      status: {
        hashing: false,
        broken: true,
        offline: false,
        asleep: false,
      },
    },
    {
      name: "Miner 3",
      status: {
        hashing: false,
        broken: false,
        offline: true,
        asleep: false,
      },
    },
  ];

  it("renders filter buttons for all filters", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<Miner, MinerFilterState>
        filterItems={filters}
        items={mockMiners as Miner[]}
        onFilter={handleFiltering}
      />,
    );

    for (const filterItem of filters) {
      const filterButton = screen.getByText(filterItem.title);
      expect(filterButton).toBeInTheDocument();
      expect(filterButton.closest("button")).toHaveTextContent(
        `${filterItem.title} ${filterItem.count}`,
      );
    }
  });

  it("calls onFilter with the correct filter when a filter is clicked", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<Miner, MinerFilterState>
        filterItems={filters}
        items={mockMiners as Miner[]}
        onFilter={handleFiltering}
      />,
    );

    for (const filterItem of filters) {
      fireEvent.click(screen.getByText(filterItem.title));
      expect(handleFiltering).toHaveBeenCalledWith(filterItem.value);
    }
  });

  it("renders without crashing when no filters are provided", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<Miner, MinerFilterState>
        filterItems={[]}
        items={mockMiners as Miner[]}
        onFilter={handleFiltering}
      />,
    );

    expect(screen.queryByText("All miners")).not.toBeInTheDocument();
  });

  it("renders without crashing when no items are provided", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<Miner, MinerFilterState>
        filterItems={filters}
        items={[]}
        onFilter={handleFiltering}
      />,
    );

    expect(screen.getByText("All miners")).toBeInTheDocument();
  });

  it("changes active filter when clicking filter buttons", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<Miner, MinerFilterState>
        filterItems={filters}
        items={mockMiners as Miner[]}
        onFilter={handleFiltering}
      />,
    );

    // Initially "All Miners" should be active
    expect(screen.getByText("All miners").closest("button")).toHaveClass(
      "bg-core-primary-fill",
    );

    // Click "Hashing" filter
    fireEvent.click(screen.getByText("Hashing"));

    // "Hashing" should now be active and "All miners" should be inactive
    expect(screen.getByText("Hashing").closest("button")).toHaveClass(
      "bg-core-primary-fill",
    );
    expect(screen.getByText("All miners").closest("button")).toHaveClass(
      "bg-surface-default",
    );
  });

  it("displays correct count for each filter status", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<Miner, MinerFilterState>
        filterItems={filters}
        items={mockMiners as Miner[]}
        onFilter={handleFiltering}
      />,
    );

    for (const filterItem of filters) {
      expect(
        screen.getByText(filterItem.title).querySelector("span")?.innerHTML,
      ).toEqual(filterItem.count.toString());
    }
  });
});
