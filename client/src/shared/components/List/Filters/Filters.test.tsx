import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Filters from "./Filters";
import {
  testFilters,
  TestFilterState,
  TestItem,
  testItems,
} from "@/shared/components/List/mocks/data";

describe("Filters", () => {
  it("renders filter buttons for all filters", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem, TestFilterState>
        filterItems={testFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    for (const filterItem of testFilters) {
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
      <Filters<TestItem, TestFilterState>
        filterItems={testFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    for (const filterItem of testFilters) {
      fireEvent.click(screen.getByText(filterItem.title));
      expect(handleFiltering).toHaveBeenCalledWith(filterItem.value);
    }
  });

  it("renders without crashing when no filters are provided", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem, TestFilterState>
        filterItems={[]}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    expect(screen.queryByText("All Items")).not.toBeInTheDocument();
  });

  it("renders without crashing when no items are provided", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem, TestFilterState>
        filterItems={testFilters}
        items={[]}
        onFilter={handleFiltering}
      />,
    );

    expect(screen.getByText("All Items")).toBeInTheDocument();
  });

  it("changes active filter when clicking filter buttons", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem, TestFilterState>
        filterItems={testFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    // Initially "All Items" should be active
    expect(screen.getByText("All Items").closest("button")).toHaveClass(
      "bg-core-primary-fill",
    );

    // Click "Active" filter
    fireEvent.click(screen.getByText("Active"));

    // "Active" should now be active and "All Items" should be inactive
    expect(screen.getByText("Active").closest("button")).toHaveClass(
      "bg-core-primary-fill",
    );
    expect(screen.getByText("All Items").closest("button")).toHaveClass(
      "bg-surface-default",
    );
  });

  it("displays correct count for each filter status", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem, TestFilterState>
        filterItems={testFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    for (const filterItem of testFilters) {
      expect(
        screen.getByText(filterItem.title).querySelector("span")?.innerHTML,
      ).toEqual(filterItem.count.toString());
    }
  });
});
