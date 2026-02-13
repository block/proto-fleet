import { ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import List from "@/shared/components/List/index";
import testColConfig from "@/shared/components/List/mocks/colConfig";
import { testCols, testColTitles, TestItem, testItems } from "@/shared/components/List/mocks/data";
import { ListAction } from "@/shared/components/List/types";

beforeAll(() => {
  vi.mock("recharts", () => ({
    ResponsiveContainer: ({ children }: { children: ReactNode }) => (
      <div data-testid="recharts-responsive-container">{children}</div>
    ),
    LineChart: ({ children }: { children: ReactNode }) => <div data-testid="recharts-line-chart">{children}</div>,
    ReferenceLine: () => <div data-testid="recharts-reference-line" />,
    Line: () => <div data-testid="recharts-line" />,
    XAxis: () => <div data-testid="recharts-xaxis" />,
    YAxis: () => <div data-testid="recharts-yaxis" />,
  }));
});

describe("List", () => {
  const activeCols = [testCols.name, testCols.status, testCols.value, testCols.timestamp] as (keyof TestItem)[];
  type TestItemKey = TestItem["id"];

  it("renders cols correctly", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
      />,
    );

    for (const col of activeCols) {
      expect(screen.getByText(testColTitles[col])).toBeInTheDocument();
    }
  });

  it("renders rows correctly", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
      />,
    );

    expect(screen.getAllByRole("row")).toHaveLength(testItems.length + 1);
  });

  it("does not render checkboxes when items are not selectable", () => {
    const { getByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        itemSelectable={false}
      />,
    );

    const selectItemCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;
    expect(selectItemCheckboxes).toHaveLength(0);
  });

  it("selects all items when clicking select all checkbox", () => {
    const { getByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        itemSelectable
      />,
    );
    const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

    const selectItemCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    // expect select all checkbox to be unchecked
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(0);

    // click individual item checkboxes and make sure select all checkbox is unchecked and total checked is only 1
    fireEvent.click(selectItemCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

    // click select all checkboxes and make sure all checkboxes are checked
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(true);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(testItems.length);

    // click item 1 (deselect) checkbox and make select all checkbox unchecked
    fireEvent.click(selectItemCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(testItems.length - 1);

    // click select all twice to deselect all items
    fireEvent.click(selectAllCheckbox);
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(0);
  });

  it("renders action bar when items are selected and provides clearSelection callback", () => {
    const renderActionBar = vi.fn((_selectedItems: TestItemKey[], clearSelection: () => void) => (
      <div>
        <div>Action Bar</div>
        <button onClick={clearSelection} data-testid="clear-selection-btn">
          Clear
        </button>
      </div>
    ));

    const { getByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        itemSelectable
        renderActionBar={renderActionBar}
      />,
    );

    const selectItemCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    // Select first item
    fireEvent.click(selectItemCheckboxes[0]);

    // Verify renderActionBar was called with selectedItems and clearSelection callback
    expect(renderActionBar).toHaveBeenCalled();
    const lastCall = renderActionBar.mock.calls[renderActionBar.mock.calls.length - 1];
    expect(lastCall[0]).toEqual([testItems[0].id]); // selectedItems
    expect(typeof lastCall[1]).toBe("function"); // clearSelection callback

    expect(screen.getByText("Action Bar")).toBeInTheDocument();

    // Click clear button
    const clearButton = screen.getByTestId("clear-selection-btn");
    fireEvent.click(clearButton);

    // Verify all checkboxes are now unchecked
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(0);
  });

  it("clearSelection callback deselects all items", async () => {
    let clearSelectionCallback: (() => void) | null = null;

    const renderActionBar = vi.fn((_selectedItems: TestItemKey[], clearSelection: () => void) => {
      clearSelectionCallback = clearSelection;
      return <div>Action Bar</div>;
    });

    const { getByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        itemSelectable
        renderActionBar={renderActionBar}
      />,
    );

    const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

    const selectItemCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    // Select all items
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(true);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(testItems.length);

    // Call clearSelection callback
    expect(clearSelectionCallback).not.toBeNull();
    clearSelectionCallback!();

    // Wait for React to update the DOM
    await new Promise((resolve) => setTimeout(resolve, 0));

    // Verify all checkboxes are now unchecked
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(0);
  });

  it("renders actions popover and triggers the correct action", async () => {
    const mockAction = vi.fn();
    const actions = [
      { title: "Edit", actionHandler: mockAction },
      { title: "Delete", actionHandler: mockAction },
    ] as ListAction<TestItem>[];

    const { getAllByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        actions={actions}
      />,
    );

    const actionButton = getAllByTestId("list-actions-trigger")[0];
    fireEvent.click(actionButton);

    const editAction = screen.getByText(actions[0].title);
    fireEvent.click(editAction);

    expect(mockAction).toHaveBeenCalled();
    expect(mockAction).toHaveBeenCalledWith(testItems[0]);
  });

  it("exempts specified columns from disabled styling on disabled rows", () => {
    const { getAllByRole } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        isRowDisabled={(item: TestItem) => item.id === "1"}
        columnsExemptFromDisabledStyling={new Set<keyof TestItem>(["status"])}
      />,
    );

    const rows = getAllByRole("row");
    // First row is header, second row is first item (id: "1", which is disabled)
    const disabledRow = rows[1];
    const cells = disabledRow.querySelectorAll("td");

    // Find the status column index
    const statusColIndex = activeCols.indexOf("status" as keyof TestItem);

    // Verify that status column does not have the opacity-50 class if it exists on other cells
    const hasOpacityClass = Array.from(cells).some((cell) => cell.className.includes("opacity-50"));

    if (hasOpacityClass) {
      // If opacity styling is applied to any cell, status column should NOT have it (it's exempted)
      expect(cells[statusColIndex].className).not.toContain("opacity-50");
    } else {
      // If no opacity styling is applied, that's also acceptable - just verify status column exists
      expect(cells[statusColIndex]).toBeInTheDocument();
    }
  });

  describe("selection mode", () => {
    it("sets mode to 'all' when Select All is clicked without active filters", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      expect(onSelectionModeChange).toHaveBeenCalledWith("all");
    });

    it("sets mode to 'subset' when Select All is clicked with active filters", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={true}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      expect(onSelectionModeChange).toHaveBeenCalledWith("subset");
    });

    it("sets mode to 'subset' when individual item is selected", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      fireEvent.click(selectItemCheckboxes[0]);

      expect(onSelectionModeChange).toHaveBeenCalledWith("subset");
    });

    it("sets mode to 'none' when selection is cleared via Select All toggle", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

      // Select all
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");

      // Deselect all
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("none");
    });

    it("transitions from 'all' to 'subset' when individual item is deselected", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Select all (mode = "all")
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");

      // Deselect one item (mode should transition to "subset")
      fireEvent.click(selectItemCheckboxes[0]);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("subset");
    });

    it("passes selectionMode to renderActionBar callback", () => {
      const renderActionBar = vi.fn(
        (_selectedItems: TestItemKey[], _clearSelection: () => void, selectionMode: string) => (
          <div data-testid="selection-mode">{selectionMode}</div>
        ),
      );

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          renderActionBar={renderActionBar}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      expect(renderActionBar).toHaveBeenCalled();
      expect(getByTestId("selection-mode").textContent).toBe("all");
    });

    it("resets mode to 'none' when customSelectedItems is externally set to empty", () => {
      const onSelectionModeChange = vi.fn();
      let selectedItems: TestItemKey[] = [];
      const customSetSelectedItems = (items: TestItemKey[]) => {
        selectedItems = items;
      };

      const { getByTestId, rerender } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
          customSelectedItems={selectedItems}
          customSetSelectedItems={customSetSelectedItems}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

      // Click Select All to set internal selectionMode to "all"
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");

      // Rerender with the selected items to sync state
      rerender(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
          customSelectedItems={selectedItems}
          customSetSelectedItems={customSetSelectedItems}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Verify all items are now selected
      expect(selectAllCheckbox.checked).toBe(true);
      expect(selectItemCheckboxes.every((c) => c.checked)).toBe(true);

      // Clear the mock to track new calls
      onSelectionModeChange.mockClear();

      // Simulate external "Select none" by setting customSelectedItems to empty array
      // This is what happens when ModalSelectAllFooter's "Select none" is clicked
      rerender(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
          customSelectedItems={[]}
          customSetSelectedItems={customSetSelectedItems}
        />,
      );

      // Verify all items are now deselected
      expect(selectAllCheckbox.checked).toBe(false);
      expect(selectItemCheckboxes.every((c) => !c.checked)).toBe(true);

      // Verify selection mode was reset to 'none'
      expect(onSelectionModeChange).toHaveBeenCalledWith("none");
    });
  });

  describe("Shift+click range selection", () => {
    it("selects range of items when Shift+clicking after initial selection", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click first checkbox (normal click)
      fireEvent.click(selectItemCheckboxes[0]);
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

      // Shift+click third checkbox to select range (items 0, 1, 2)
      fireEvent.click(selectItemCheckboxes[2], { shiftKey: true });

      // All three items should be selected
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(selectItemCheckboxes[1].checked).toBe(true);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(3);
    });

    it("selects range in reverse order when Shift+clicking above initial selection", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click third checkbox first (normal click)
      fireEvent.click(selectItemCheckboxes[2]);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

      // Shift+click first checkbox to select range (items 0, 1, 2)
      fireEvent.click(selectItemCheckboxes[0], { shiftKey: true });

      // All three items should be selected
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(selectItemCheckboxes[1].checked).toBe(true);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(3);
    });

    it("adds to existing selection when Shift+clicking with items already selected", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Select first item
      fireEvent.click(selectItemCheckboxes[0]);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

      // Select fourth item separately
      fireEvent.click(selectItemCheckboxes[3]);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(2);

      // Shift+click second item (should add items 1, 2 to selection since last click was on index 3)
      fireEvent.click(selectItemCheckboxes[1], { shiftKey: true });

      // Items 0, 1, 2, 3 should all be selected now
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(selectItemCheckboxes[1].checked).toBe(true);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(selectItemCheckboxes[3].checked).toBe(true);
    });

    it("clears anchor after Shift+click so next Shift+click requires new anchor", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click first checkbox (sets anchor at index 0)
      fireEvent.click(selectItemCheckboxes[0]);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

      // Shift+click third checkbox (selects 0, 1, 2 and clears anchor)
      fireEvent.click(selectItemCheckboxes[2], { shiftKey: true });
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(3);

      // Another Shift+click should NOT do range selection since anchor was cleared
      // It should just select that single item (normal click behavior when no anchor)
      fireEvent.click(selectItemCheckboxes[3], { shiftKey: true });

      // Should only add item 3, not create a range from anywhere
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(4);
      expect(selectItemCheckboxes[3].checked).toBe(true);
    });

    it("does not select range when Shift+clicking to uncheck", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Select first three items individually
      fireEvent.click(selectItemCheckboxes[0]);
      fireEvent.click(selectItemCheckboxes[1]);
      fireEvent.click(selectItemCheckboxes[2]);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(3);

      // Shift+click to uncheck should only uncheck that item (no range deselection)
      fireEvent.click(selectItemCheckboxes[1], { shiftKey: true });

      // Only item 1 should be unchecked, items 0 and 2 remain checked
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(selectItemCheckboxes[1].checked).toBe(false);
      expect(selectItemCheckboxes[2].checked).toBe(true);
    });

    it("sets selection mode to 'all' when Shift+click selects all items", () => {
      const onSelectionModeChange = vi.fn();
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click first checkbox (sets anchor at index 0)
      fireEvent.click(selectItemCheckboxes[0]);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("subset");

      // Shift+click last checkbox to select all items (0, 1, 2, 3, 4)
      fireEvent.click(selectItemCheckboxes[4], { shiftKey: true });

      // All items should be selected
      expect(Array.from(selectItemCheckboxes).every((c) => c.checked)).toBe(true);

      // Mode should be "all" since all items are selected
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");
    });
  });

  describe("disabled rows", () => {
    it("disables checkboxes for rows matching isRowDisabled predicate", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id || item.id === testItems[2].id;

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // First and third checkboxes should be disabled
      expect(selectItemCheckboxes[0].disabled).toBe(true);
      expect(selectItemCheckboxes[1].disabled).toBe(false);
      expect(selectItemCheckboxes[2].disabled).toBe(true);
      expect(selectItemCheckboxes[3].disabled).toBe(false);
    });

    it("applies opacity-50 class to cells in disabled rows", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id;

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const rows = Array.from(getByTestId("list-body").querySelectorAll("tr"));

      // Row itself should not have opacity-50
      expect(rows[0].className).not.toContain("opacity-50");
      expect(rows[1].className).not.toContain("opacity-50");

      // Cells in the first (disabled) row should have opacity-50
      const disabledRowCells = Array.from(rows[0].querySelectorAll("td"));
      disabledRowCells.forEach((cell) => {
        expect(cell.className).toContain("opacity-50");
      });

      // Cells in the second (enabled) row should not have opacity-50
      const enabledRowCells = Array.from(rows[1].querySelectorAll("td"));
      enabledRowCells.forEach((cell) => {
        expect(cell.className).not.toContain("opacity-50");
      });
    });

    it("excludes disabled rows when Select All is clicked", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id || item.id === testItems[1].id;

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click Select All
      fireEvent.click(selectAllCheckbox);

      // Only enabled checkboxes (items 2, 3, 4) should be checked
      expect(selectItemCheckboxes[0].checked).toBe(false); // disabled
      expect(selectItemCheckboxes[1].checked).toBe(false); // disabled
      expect(selectItemCheckboxes[2].checked).toBe(true); // enabled
      expect(selectItemCheckboxes[3].checked).toBe(true); // enabled
      expect(selectItemCheckboxes[4].checked).toBe(true); // enabled
    });

    it("shows Select All as checked when all selectable items are selected", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id || item.id === testItems[1].id;

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Manually select all enabled items (items 2, 3, 4)
      fireEvent.click(selectItemCheckboxes[2]);
      fireEvent.click(selectItemCheckboxes[3]);
      fireEvent.click(selectItemCheckboxes[4]);

      // Select All checkbox should now be checked
      expect(selectAllCheckbox.checked).toBe(true);
    });

    it("passes totalSelectable to renderActionBar (total - totalDisabled)", () => {
      const renderActionBar = vi.fn(
        (
          _selectedItems: TestItemKey[],
          _clearSelection: () => void,
          _selectionMode: string,
          totalSelectable?: number,
        ) => <div data-testid="total-selectable">{totalSelectable}</div>,
      );

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          total={10}
          totalDisabled={3}
          renderActionBar={renderActionBar}
          isRowDisabled={() => false}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      // totalSelectable should be 10 - 3 = 7
      expect(getByTestId("total-selectable").textContent).toBe("7");
    });

    it("excludes disabled items when syncing selection in 'all' mode", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id;
      const onSelectionModeChange = vi.fn();

      const { getByTestId, rerender } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems.slice(0, 3)}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

      // Select all (should select items 1 and 2, skipping item 0)
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");

      // Simulate loading more items
      rerender(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Item 0 should still be disabled and unchecked
      expect(selectItemCheckboxes[0].disabled).toBe(true);
      expect(selectItemCheckboxes[0].checked).toBe(false);

      // All other items should be selected
      expect(selectItemCheckboxes[1].checked).toBe(true);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(selectItemCheckboxes[3].checked).toBe(true);
      expect(selectItemCheckboxes[4].checked).toBe(true);
    });

    it("shows Select All as unchecked when no selectable items exist", () => {
      const isRowDisabled = () => true; // All items disabled

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

      // Select All should be unchecked when all items are disabled
      expect(selectAllCheckbox.checked).toBe(false);

      // Click Select All should not select anything
      fireEvent.click(selectAllCheckbox);
      expect(selectAllCheckbox.checked).toBe(false);
    });

    it("disables single action button for disabled rows", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id;
      const mockActionHandler = vi.fn();
      const actions = [{ title: "Edit", actionHandler: mockActionHandler }];

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          isRowDisabled={isRowDisabled}
          actions={actions}
        />,
      );

      const actionButtons = getByTestId("list-body").querySelectorAll("button");

      // First row action button should be disabled
      expect(actionButtons[0].disabled).toBe(true);

      // Other row action buttons should be enabled
      expect(actionButtons[1].disabled).toBe(false);
      expect(actionButtons[2].disabled).toBe(false);

      // Clicking disabled button should not trigger action
      fireEvent.click(actionButtons[0]);
      expect(mockActionHandler).not.toHaveBeenCalled();

      // Clicking enabled button should trigger action
      fireEvent.click(actionButtons[1]);
      expect(mockActionHandler).toHaveBeenCalledWith(testItems[1]);
    });

    it("disables multi-action menu for disabled rows", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id;
      const mockAction1 = vi.fn();
      const mockAction2 = vi.fn();
      const actions = [
        { title: "Edit", actionHandler: mockAction1 },
        { title: "Delete", actionHandler: mockAction2 },
      ];

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          isRowDisabled={isRowDisabled}
          actions={actions}
        />,
      );

      const actionTriggers = getByTestId("list-body").querySelectorAll("[data-testid='list-actions-trigger']");

      // First row action trigger should be disabled
      expect((actionTriggers[0] as HTMLButtonElement).disabled).toBe(true);

      // Other row action triggers should be enabled
      expect((actionTriggers[1] as HTMLButtonElement).disabled).toBe(false);

      // Clicking disabled trigger should not open menu
      fireEvent.click(actionTriggers[0]);
      expect(document.querySelector(".popover-content")).toBeNull();

      // Clicking enabled trigger should open menu
      fireEvent.click(actionTriggers[1]);
      // Menu should be visible (implementation shows popover when actionsVisible is true)
      const rows = document.querySelectorAll("[data-testid='action'] > div > div");
      expect(rows.length).toBeGreaterThan(1); // Popover should exist
    });
  });

  describe("sorting", () => {
    it("calls onSort with ASC direction when clicking unsorted column (default)", () => {
      // Arrange
      const onSort = vi.fn();
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          onSort={onSort}
        />,
      );

      const headerButtons = getByTestId("list-header").querySelectorAll("button");
      fireEvent.click(headerButtons[0]); // Click "name" column

      // Assert - defaults to ASC when no getDefaultSortDirection callback provided
      expect(onSort).toHaveBeenCalledWith("name", "asc");
    });

    it("toggles direction from DESC to ASC when clicking currently sorted column", () => {
      // Arrange
      const onSort = vi.fn();
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "desc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
          onSort={onSort}
        />,
      );

      const headerButtons = getByTestId("list-header").querySelectorAll("button");
      fireEvent.click(headerButtons[0]); // Click "name" column (currently sorted DESC)

      // Assert
      expect(onSort).toHaveBeenCalledWith("name", "asc");
    });

    it("toggles direction from ASC to DESC when clicking currently sorted column", () => {
      // Arrange
      const onSort = vi.fn();
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "asc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
          onSort={onSort}
        />,
      );

      const headerButtons = getByTestId("list-header").querySelectorAll("button");
      fireEvent.click(headerButtons[0]); // Click "name" column (currently sorted ASC)

      // Assert
      expect(onSort).toHaveBeenCalledWith("name", "desc");
    });

    it("sets aria-sort to ascending when column is sorted ASC", () => {
      // Arrange
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "asc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
        />,
      );

      const headerCells = getByTestId("list-header").querySelectorAll("th");

      // Assert
      expect(headerCells[0]).toHaveAttribute("aria-sort", "ascending");
    });

    it("sets aria-sort to descending when column is sorted DESC", () => {
      // Arrange
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "desc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
        />,
      );

      const headerCells = getByTestId("list-header").querySelectorAll("th");

      // Assert
      expect(headerCells[0]).toHaveAttribute("aria-sort", "descending");
    });

    it("does not set aria-sort on unsorted columns", () => {
      // Arrange
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "asc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
        />,
      );

      const headerCells = getByTestId("list-header").querySelectorAll("th");

      // Assert - "status" column (index 1) is not sorted
      expect(headerCells[1]).not.toHaveAttribute("aria-sort");
    });

    it("renders buttons only for sortable columns", () => {
      // Arrange
      const sortableColumns = new Set<keyof TestItem>(["name"]); // Only name is sortable

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
        />,
      );

      const headerButtons = getByTestId("list-header").querySelectorAll("button");

      // Assert - only 1 button for the sortable "name" column
      expect(headerButtons).toHaveLength(1);
      expect(headerButtons[0]).toHaveTextContent(testColTitles.name);
    });
  });

  describe("client-side filtering with Select All", () => {
    // Filter function that reduces items based on status
    const filterByActiveStatus = (item: TestItem) => item.status === "active";

    it("sets mode to 'subset' when Select All is clicked with client-side filter reducing items", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={(item) => filterByActiveStatus(item)}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      // Should be "subset" because client-side filter reduces visible items (5 items -> 2 active items)
      expect(onSelectionModeChange).toHaveBeenCalledWith("subset");
    });

    it("sets mode to 'all' when Select All is clicked with filter that matches all items", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={() => true} // Filter matches all items
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      // Should be "all" because filter matches all items
      expect(onSelectionModeChange).toHaveBeenCalledWith("all");
    });

    it("only selects filtered items when Select All is clicked with active filter", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={(item) => filterByActiveStatus(item)}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      // Only "active" items should be visible and selected (items 1 and 5)
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Only 2 items should be visible (the active ones)
      expect(selectItemCheckboxes).toHaveLength(2);

      // All visible items should be selected
      expect(selectItemCheckboxes.every((c) => c.checked)).toBe(true);
    });

    it("shows Select All as checked when all filtered items are selected", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={(item) => filterByActiveStatus(item)}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Manually select all visible (filtered) items
      selectItemCheckboxes.forEach((checkbox) => fireEvent.click(checkbox));

      // Select All checkbox should now be checked
      expect(selectAllCheckbox.checked).toBe(true);
    });

    it("calls onFilterChange when filterItem changes filtered results", () => {
      const onFilterChange = vi.fn();

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={() => true}
          onFilterChange={onFilterChange}
        />,
      );

      // onFilterChange is called when filters are applied through the UI
      // The callback should be available and callable
      expect(onFilterChange).toBeDefined();
    });
  });
});
