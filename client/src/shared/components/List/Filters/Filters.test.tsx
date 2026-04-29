import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Filters from "./Filters";
import { testFilters, TestItem, testItems } from "@/shared/components/List/mocks/data";

describe("Filters", () => {
  it("renders filter buttons for all button filters", () => {
    const handleFiltering = vi.fn();
    render(<Filters<TestItem> filterItems={testFilters} items={testItems} onFilter={handleFiltering} />);

    for (const filterItem of testFilters) {
      if (filterItem.type === "button") {
        const filterButton = screen.getByText(filterItem.title);
        expect(filterButton).toBeInTheDocument();
        expect(filterButton.closest("button")).toHaveTextContent(`${filterItem.title} ${filterItem.count}`);
      }
    }
  });

  it("calls onFilter with the correct filter when a button filter is clicked", () => {
    const handleFiltering = vi.fn();
    render(<Filters<TestItem> filterItems={testFilters} items={testItems} onFilter={handleFiltering} />);

    for (const filterItem of testFilters) {
      if (filterItem.type === "button") {
        fireEvent.click(screen.getByText(filterItem.title));
        expect(handleFiltering).toHaveBeenCalled();
      }
    }
  });

  it("renders without crashing when no filters are provided", () => {
    const handleFiltering = vi.fn();
    render(<Filters<TestItem> filterItems={[]} items={testItems} onFilter={handleFiltering} />);

    expect(screen.queryByText("All Items")).not.toBeInTheDocument();
  });

  it("renders without crashing when no items are provided", () => {
    const handleFiltering = vi.fn();
    render(<Filters<TestItem> filterItems={testFilters} items={[]} onFilter={handleFiltering} />);

    expect(screen.getByText("All Items")).toBeInTheDocument();
  });

  it("changes active filter when clicking filter buttons", () => {
    const handleFiltering = vi.fn();

    // Get button filters only for this test
    const buttonFilters = testFilters.filter((filter) => filter.type === "button");

    render(<Filters<TestItem> filterItems={buttonFilters} items={testItems} onFilter={handleFiltering} />);

    // Initially "All Items" should be active
    const allItemsBtn = screen.getByText("All Items").closest("button");
    expect(allItemsBtn).toHaveAttribute("class", expect.stringContaining("accent"));

    // Find the "Active" filter if it exists
    const activeFilterIdx = buttonFilters.findIndex((f) => f.title === "Active");
    if (activeFilterIdx >= 0) {
      // Click "Active" filter
      fireEvent.click(screen.getByText("Active"));

      // "Active" should now be active
      const activeBtn = screen.getByText("Active").closest("button");
      expect(activeBtn).toHaveAttribute("class", expect.stringContaining("accent"));
    }
  });

  it("displays correct count for each button filter status", () => {
    const handleFiltering = vi.fn();
    render(<Filters<TestItem> filterItems={testFilters} items={testItems} onFilter={handleFiltering} />);

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
    render(<Filters<TestItem> filterItems={testFilters} items={testItems} onFilter={handleFiltering} />);

    // Find dropdown filters
    const dropdownFilters = testFilters.filter((filter) => filter.type === "dropdown");

    for (const dropdownFilter of dropdownFilters) {
      // The dropdown button should show the title, not a selected option
      const dropdownButton = screen.getByText(dropdownFilter.title);
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
        { id: "test1", label: "Test Option 1" },
        { id: "test2", label: "Test Option 2" },
      ],
      defaultOptionIds: [],
    };

    render(<Filters<TestItem> filterItems={[testDropdownFilter]} items={testItems} onFilter={handleFiltering} />);

    // Click the dropdown button to open it (shows title, not selected option)
    const dropdownButton = screen.getByText("Test Dropdown");
    fireEvent.click(dropdownButton);

    // Check that the options are displayed
    // Wait for popover to appear
    await waitFor(() => {
      for (const option of testDropdownFilter.options) {
        const optionElement = screen.queryByText(option.label);
        expect(optionElement).toBeInTheDocument();
      }
    });
  });

  it("updates filter state when a dropdown option is selected", async () => {
    const handleFiltering = vi.fn();
    render(<Filters<TestItem> filterItems={testFilters} items={testItems} onFilter={handleFiltering} />);

    // Find the first dropdown filter
    const dropdownFilters = testFilters.filter((filter) => filter.type === "dropdown");

    if (dropdownFilters.length > 0 && dropdownFilters[0].options?.length > 0) {
      const firstDropdown = dropdownFilters[0];
      const secondOption = firstDropdown.options[1];

      // Click the dropdown button to open it (shows title)
      const dropdownButton = screen.getByText(firstDropdown.title);
      fireEvent.click(dropdownButton);

      // Find and click the first option
      await waitFor(() => {
        screen.findByText(secondOption.label).then((el) => {
          fireEvent.click(el);
        });
      });

      expect(handleFiltering).toHaveBeenCalled();
    }
  });

  it("can hide the select all option for a dropdown filter", async () => {
    const handleFiltering = vi.fn();

    const testDropdownFilter = {
      type: "dropdown" as const,
      title: "Status",
      value: "status",
      showSelectAll: false,
      options: [
        { id: "running", label: "Running" },
        { id: "paused", label: "Paused" },
      ],
      defaultOptionIds: [],
    };

    render(<Filters<TestItem> filterItems={[testDropdownFilter]} items={testItems} onFilter={handleFiltering} />);

    fireEvent.click(screen.getByText("Status"));

    await waitFor(() => {
      expect(screen.getByText("Running")).toBeInTheDocument();
      expect(screen.queryByText("Select all")).not.toBeInTheDocument();
    });
  });

  it("renders pills for meta-only filters that have no standalone trigger", () => {
    const handleFiltering = vi.fn();

    const metaOnly = [
      {
        type: "dropdown" as const,
        title: "Firmware",
        value: "firmware",
        options: [
          { id: "v3.5.1", label: "v3.5.1" },
          { id: "v3.5.2", label: "v3.5.2" },
        ],
        defaultOptionIds: [],
      },
    ];

    render(
      <Filters<TestItem>
        filterItems={[]}
        metaOnlyFilters={metaOnly}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { firmware: ["v3.5.1"] },
        }}
      />,
    );

    // The pill renders the option label even though no standalone "Firmware" trigger exists in the bar.
    expect(screen.getByTestId("active-filter-firmware-v3.5.1")).toBeInTheDocument();
    // No standalone "Firmware" filter trigger.
    expect(screen.queryByTestId("filter-dropdown-Firmware")).not.toBeInTheDocument();
  });

  it("renders leading controls before standalone filter triggers", () => {
    const handleFiltering = vi.fn();

    render(
      <Filters<TestItem>
        filterItems={[]}
        leadingControls={<button data-testid="leading-slot">Filters</button>}
        items={testItems}
        onFilter={handleFiltering}
      />,
    );

    expect(screen.getByTestId("leading-slot")).toBeInTheDocument();
  });
});
