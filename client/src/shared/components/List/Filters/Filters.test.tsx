import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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

  it("renders pills for filters declared inside a nestedFilterDropdown that have no standalone trigger", () => {
    const handleFiltering = vi.fn();

    const firmwareChild = {
      type: "dropdown" as const,
      title: "Firmware",
      value: "firmware",
      options: [
        { id: "v3.5.1", label: "v3.5.1" },
        { id: "v3.5.2", label: "v3.5.2" },
      ],
      defaultOptionIds: [],
    };

    render(
      <Filters<TestItem>
        filterItems={[
          {
            type: "nestedFilterDropdown",
            title: "Filters",
            value: "all-filters",
            children: [firmwareChild],
          },
        ]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { firmware: ["v3.5.1"] },

          numericFilters: {},

          textareaListFilters: {},
        }}
      />,
    );

    // The chip renders even though no standalone "Firmware" trigger exists in the bar.
    const chip = screen.getByTestId("active-filter-firmware");
    expect(chip).toBeInTheDocument();
    // Bifurcated chip: left half is the category title, right half summarizes selections.
    expect(chip).toHaveTextContent("Firmware");
    expect(chip).toHaveTextContent("v3.5.1");
    // No standalone "Firmware" filter trigger.
    expect(screen.queryByTestId("filter-dropdown-Firmware")).not.toBeInTheDocument();
    // The nested-dropdown trigger is in the bar.
    expect(screen.getByTestId("filter-nested-all-filters")).toBeInTheDocument();
  });

  it("dedups pills when the same filter value appears in both standalone and nested-dropdown surfaces", () => {
    const handleFiltering = vi.fn();

    const statusOptions = [
      { id: "hashing", label: "Hashing" },
      { id: "offline", label: "Offline" },
    ];
    const statusFilter = {
      type: "dropdown" as const,
      title: "Status",
      value: "status",
      options: statusOptions,
      defaultOptionIds: [],
    };

    render(
      <Filters<TestItem>
        filterItems={[
          {
            type: "nestedFilterDropdown",
            title: "Filters",
            value: "all-filters",
            children: [statusFilter],
          },
          statusFilter,
        ]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { status: ["hashing"] },

          numericFilters: {},

          textareaListFilters: {},
        }}
      />,
    );

    // Only one chip for `status` even though it lives in two filter sources.
    const chips = screen.getAllByTestId(/^active-filter-status$/);
    expect(chips).toHaveLength(1);
    expect(chips[0]).toHaveTextContent("Status");
    expect(chips[0]).toHaveTextContent("Hashing");
  });

  it("renders one bifurcated chip per category showing the option label when one is selected", () => {
    const handleFiltering = vi.fn();

    const modelFilter = {
      type: "dropdown" as const,
      title: "Model",
      pluralTitle: "models",
      value: "model",
      options: [
        { id: "S19", label: "S19" },
        { id: "S21", label: "S21" },
      ],
      defaultOptionIds: [],
    };

    render(
      <Filters<TestItem>
        filterItems={[modelFilter]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { model: ["S19"] },

          numericFilters: {},

          textareaListFilters: {},
        }}
      />,
    );

    const chip = screen.getByTestId("active-filter-model");
    expect(chip).toHaveTextContent("Model");
    // Single selection shows the option label directly.
    expect(within(chip).getByTestId("active-filter-model-edit")).toHaveTextContent("S19");
  });

  it("summarizes the right side as `n plural` when multiple options are selected", () => {
    const handleFiltering = vi.fn();

    const statusFilter = {
      type: "dropdown" as const,
      title: "Status",
      pluralTitle: "statuses",
      value: "status",
      options: [
        { id: "hashing", label: "Hashing" },
        { id: "offline", label: "Offline" },
        { id: "sleeping", label: "Sleeping" },
      ],
      defaultOptionIds: [],
    };

    render(
      <Filters<TestItem>
        filterItems={[statusFilter]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { status: ["hashing", "offline"] },

          numericFilters: {},

          textareaListFilters: {},
        }}
      />,
    );

    const chip = screen.getByTestId("active-filter-status");
    expect(within(chip).getByTestId("active-filter-status-edit")).toHaveTextContent("2 statuses");
  });

  it("clears every selected option in a category when the chip's clear icon is clicked", () => {
    const handleFiltering = vi.fn();

    const issuesFilter = {
      type: "dropdown" as const,
      title: "Issues",
      pluralTitle: "issues",
      value: "issues",
      options: [
        { id: "control-board", label: "Control board issue" },
        { id: "hash-boards", label: "Hash board issue" },
      ],
      defaultOptionIds: [],
    };

    render(
      <Filters<TestItem>
        filterItems={[issuesFilter]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { issues: ["control-board", "hash-boards"] },

          numericFilters: {},

          textareaListFilters: {},
        }}
      />,
    );

    fireEvent.click(screen.getByTestId("active-filter-issues-clear"));

    expect(screen.queryByTestId("active-filter-issues")).not.toBeInTheDocument();
    expect(handleFiltering).toHaveBeenCalledWith(
      expect.objectContaining({
        dropdownFilters: expect.objectContaining({ issues: [] }),
      }),
    );
  });

  it("opens an editable popover when the chip's right side is clicked and toggles options inline", async () => {
    const handleFiltering = vi.fn();

    const issuesFilter = {
      type: "dropdown" as const,
      title: "Issues",
      pluralTitle: "issues",
      value: "issues",
      options: [
        { id: "control-board", label: "Control board issue" },
        { id: "hash-boards", label: "Hash board issue" },
      ],
      defaultOptionIds: [],
    };

    render(
      <Filters<TestItem>
        filterItems={[issuesFilter]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { issues: ["control-board"] },

          numericFilters: {},

          textareaListFilters: {},
        }}
      />,
    );

    fireEvent.click(screen.getByTestId("active-filter-issues-edit"));

    await waitFor(() => {
      expect(screen.getByTestId("filter-option-hash-boards")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("filter-option-hash-boards"));

    expect(handleFiltering).toHaveBeenCalledWith(
      expect.objectContaining({
        dropdownFilters: expect.objectContaining({
          issues: ["control-board", "hash-boards"],
        }),
      }),
    );
  });

  it("keeps the chip and its popover mounted while the user clears every option from inside it", async () => {
    const handleFiltering = vi.fn();

    const issuesFilter = {
      type: "dropdown" as const,
      title: "Issues",
      pluralTitle: "issues",
      value: "issues",
      options: [
        { id: "control-board", label: "Control board issue" },
        { id: "hash-boards", label: "Hash board issue" },
      ],
      defaultOptionIds: [],
    };

    render(
      <Filters<TestItem>
        filterItems={[issuesFilter]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { issues: ["control-board", "hash-boards"] },

          numericFilters: {},

          textareaListFilters: {},
        }}
      />,
    );

    fireEvent.click(screen.getByTestId("active-filter-issues-edit"));
    await waitFor(() => {
      expect(screen.getByTestId("filter-option-control-board")).toBeInTheDocument();
    });

    // Deselect every option one by one from inside the popover. Without the open-popover
    // guard the chip would unmount on the toggle that drops the last selection because
    // selectedIds becomes empty.
    fireEvent.click(screen.getByTestId("filter-option-control-board"));
    fireEvent.click(screen.getByTestId("filter-option-hash-boards"));

    expect(screen.getByTestId("active-filter-issues")).toBeInTheDocument();
    expect(screen.getByTestId("filter-option-control-board")).toBeInTheDocument();
    expect(screen.getByTestId("active-filter-issues-edit")).toHaveTextContent("0 issues");
  });

  it("does not render a Select all row inside the chip's edit popover", async () => {
    const handleFiltering = vi.fn();

    const issuesFilter = {
      type: "dropdown" as const,
      title: "Issues",
      pluralTitle: "issues",
      value: "issues",
      options: [
        { id: "control-board", label: "Control board issue" },
        { id: "hash-boards", label: "Hash board issue" },
      ],
      defaultOptionIds: [],
    };

    render(
      <Filters<TestItem>
        filterItems={[issuesFilter]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { issues: ["control-board"] },

          numericFilters: {},

          textareaListFilters: {},
        }}
      />,
    );

    fireEvent.click(screen.getByTestId("active-filter-issues-edit"));
    await waitFor(() => {
      expect(screen.getByTestId("filter-option-control-board")).toBeInTheDocument();
    });

    expect(screen.queryByText("Select all")).not.toBeInTheDocument();
  });

  it("clears active filters when initialActiveFilters transitions to undefined", () => {
    const handleFiltering = vi.fn();

    const statusFilter = {
      type: "dropdown" as const,
      title: "Status",
      value: "status",
      options: [
        { id: "hashing", label: "Hashing" },
        { id: "offline", label: "Offline" },
      ],
      defaultOptionIds: [],
    };

    const { rerender } = render(
      <Filters<TestItem>
        filterItems={[statusFilter]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={{
          buttonFilters: [],
          dropdownFilters: { status: ["hashing"] },
          numericFilters: {},
          textareaListFilters: {},
        }}
      />,
    );

    expect(screen.getByTestId("active-filter-status")).toBeInTheDocument();

    // Parent clears the controlled prop — internal state should reset to defaults
    // so stale selections don't linger.
    rerender(
      <Filters<TestItem>
        filterItems={[statusFilter]}
        items={testItems}
        onFilter={handleFiltering}
        initialActiveFilters={undefined}
      />,
    );

    expect(screen.queryByTestId("active-filter-status")).not.toBeInTheDocument();
  });
});
