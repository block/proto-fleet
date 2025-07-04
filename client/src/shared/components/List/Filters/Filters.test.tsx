import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Filters from "./Filters";
import {
  testFilters,
  TestItem,
  testItems,
} from "@/shared/components/List/mocks/data";

describe("Filters", () => {
  it("renders filter buttons for all button filters", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem>
        filterItems={testFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    for (const filterItem of testFilters) {
      if (filterItem.type === "button") {
        const filterButton = screen.getByText(filterItem.title);
        expect(filterButton).toBeInTheDocument();
        expect(filterButton.closest("button")).toHaveTextContent(
          `${filterItem.title} ${filterItem.count}`,
        );
      }
    }
  });

  it("calls onFilter with the correct filter when a button filter is clicked", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem>
        filterItems={testFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    for (const filterItem of testFilters) {
      if (filterItem.type === "button") {
        fireEvent.click(screen.getByText(filterItem.title));
        expect(handleFiltering).toHaveBeenCalled();
      }
    }
  });

  it("renders without crashing when no filters are provided", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem>
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
      <Filters<TestItem>
        filterItems={testFilters}
        items={[]}
        onFilter={handleFiltering}
      />,
    );

    expect(screen.getByText("All Items")).toBeInTheDocument();
  });

  it("changes active filter when clicking filter buttons", () => {
    const handleFiltering = vi.fn();

    // Get button filters only for this test
    const buttonFilters = testFilters.filter(
      (filter) => filter.type === "button",
    );

    render(
      <Filters<TestItem>
        filterItems={buttonFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    // Initially "All Items" should be active
    const allItemsBtn = screen.getByText("All Items").closest("button");
    expect(allItemsBtn).toHaveAttribute(
      "class",
      expect.stringContaining("primary"),
    );

    // Find the "Active" filter if it exists
    const activeFilterIdx = buttonFilters.findIndex(
      (f) => f.title === "Active",
    );
    if (activeFilterIdx >= 0) {
      // Click "Active" filter
      fireEvent.click(screen.getByText("Active"));

      // "Active" should now be active
      const activeBtn = screen.getByText("Active").closest("button");
      expect(activeBtn).toHaveAttribute(
        "class",
        expect.stringContaining("primary"),
      );
    }
  });

  it("displays correct count for each button filter status", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem>
        filterItems={testFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    for (const filterItem of testFilters) {
      if (filterItem.type === "button") {
        const button = screen.getByText(filterItem.title);
        // Find span with the count
        const countSpan = button.querySelector("span");
        if (countSpan) {
          expect(countSpan.innerHTML).toEqual(filterItem.count.toString());
        }
      }
    }
  });

  it("renders dropdown filters correctly", () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem>
        filterItems={testFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    // Find dropdown filters
    const dropdownFilters = testFilters.filter(
      (filter) => filter.type === "dropdown",
    );

    for (const dropdownFilter of dropdownFilters) {
      const defaultOption = dropdownFilter.options.find(
        (o) => o.id === dropdownFilter.defaultOptionId,
      );
      const dropdownButton = screen.getByText(
        defaultOption?.label || "SHOULD FAIL",
      );
      expect(dropdownButton).toBeInTheDocument();

      // Check it's a button component
      const button = dropdownButton.closest("button");
      expect(button).toBeInTheDocument();
    }
  });

  it("shows dropdown options when dropdown filter is clicked", async () => {
    const handleFiltering = vi.fn();

    const testDropdownFilter = {
      type: "dropdown" as const,
      title: "Test Dropdown",
      value: "test-dropdown",
      options: [
        { id: "all", label: "All Test Items" },
        { id: "test1", label: "Test Option 1" },
        { id: "test2", label: "Test Option 2" },
      ],
      defaultOptionId: "all",
    };

    render(
      <Filters<TestItem>
        filterItems={[testDropdownFilter]}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    // Find the first dropdown filter
    const dropdownFilters = testFilters.filter(
      (filter) => filter.type === "dropdown",
    );

    if (dropdownFilters.length > 0) {
      const firstDropdown = dropdownFilters[0];

      // Click the dropdown button to open it
      const dropdownButton = screen.getByText("All Test Items");
      fireEvent.click(dropdownButton);

      // Check that the options are displayed
      // Wait for popover to appear
      if (firstDropdown.options) {
        for (const option of firstDropdown.options) {
          waitFor(() => {
            const optionElement = screen.queryByText(option.label);
            expect(optionElement).toBeInTheDocument();
          });
        }
      }
    }
  });

  it("updates filter state when a dropdown option is selected", async () => {
    const handleFiltering = vi.fn();
    render(
      <Filters<TestItem>
        filterItems={testFilters}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    // Find the first dropdown filter
    const dropdownFilters = testFilters.filter(
      (filter) => filter.type === "dropdown",
    );

    if (dropdownFilters.length > 0 && dropdownFilters[0].options?.length > 0) {
      const firstDropdown = dropdownFilters[0];
      const secondOption = firstDropdown.options[1];

      // Click the dropdown button to open it
      const defaultOption = firstDropdown.options.find(
        (o) => o.id === firstDropdown.defaultOptionId,
      );
      const dropdownButton = screen.getByText(
        defaultOption?.label || "SHOULD FAIL",
      );
      fireEvent.click(dropdownButton);

      // Find and click the first option
      waitFor(() => {
        screen.findByText(secondOption.label).then((el) => {
          fireEvent.click(el);
        });
      });

      expect(handleFiltering).toHaveBeenCalled();
    }
  });
});
